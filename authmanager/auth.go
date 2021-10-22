// Copyright 2021 Northern.tech AS
//
//    Licensed under the Apache License, Version 2.0 (the "License");
//    you may not use this file except in compliance with the License.
//    You may obtain a copy of the License at
//
//        http://www.apache.org/licenses/LICENSE-2.0
//
//    Unless required by applicable law or agreed to in writing, software
//    distributed under the License is distributed on an "AS IS" BASIS,
//    WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//    See the License for the specific language governing permissions and
//    limitations under the License.

package authmanager

import (
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/mendersoftware/mender/authmanager/api"
	"github.com/mendersoftware/mender/authmanager/conf"
	"github.com/mendersoftware/mender/authmanager/device"
	commonconf "github.com/mendersoftware/mender/common/conf"
	"github.com/mendersoftware/mender/common/dbkeys"
	"github.com/mendersoftware/mender/common/dbus"
	"github.com/mendersoftware/mender/common/store"
	"github.com/mendersoftware/mender/common/tls"
)

// Constants for auth manager request actions
const (
	ActionFetchAuthToken = "FETCH_AUTH_TOKEN"
	ActionGetAuthToken   = "GET_AUTH_TOKEN"
)

// Constants for auth manager response events
const (
	EventFetchAuthToken       = "FETCH_AUTH_TOKEN"
	EventGetAuthToken         = "GET_AUTH_TOKEN"
	EventAuthTokenStateChange = "AUTH_TOKEN_STATE_CHANGE"
)

// Constants for the auth manager DBus interface
const (
	AuthManagerDBusPath                      = "/io/mender/AuthenticationManager"
	AuthManagerDBusObjectName                = "io.mender.AuthenticationManager"
	AuthManagerDBusInterfaceName             = "io.mender.Authentication1"
	AuthManagerDBusSignalJwtTokenStateChange = "JwtTokenStateChange"
	AuthManagerDBusInterface                 = `<node>
	<interface name="io.mender.Authentication1">
		<method name="GetJwtToken">
			<arg type="s" name="token" direction="out"/>
			<arg type="s" name="server_url" direction="out"/>
		</method>
		<method name="FetchJwtToken">
			<arg type="b" name="success" direction="out"/>
		</method>
		<signal name="JwtTokenStateChange">
			<arg type="s" name="token"/>
			<arg type="s" name="server_url"/>
		</signal>
	</interface>
</node>`
)

const (
	authManagerInMessageChanSize = 1024

	// Keep this at 1 for now. At the time of writing it is only used for
	// fetchAuthToken, and it doesn't make sense to have more than one
	// request in the queue, since additional requests will just accomplish
	// the same thing as one request.
	authManagerWorkerQueueSize = 1
)

type AuthPanic struct {
	error
}

func NewAuthPanic(err error) *AuthPanic {
	return &AuthPanic{
		error: err,
	}
}

// AuthManagerRequest stores a request to the Mender authorization manager
type AuthManagerRequest struct {
	Action          string
	ResponseChannel chan<- AuthManagerResponse
}

// AuthManagerResponse stores a response from the Mender authorization manager
type AuthManagerResponse struct {
	AuthToken api.AuthToken
	Event     string
	Error     error
}

// AuthManager is the interface of a Mender authorization manager
type AuthManager interface {
	Bootstrap() error
	ForceBootstrap()
	Start()
	Stop()

	// check if device key is available
	HasKey() bool
	// generate device key (will overwrite an already existing key)
	GenerateKey() error

	api.AuthDataMessenger
}

// MenderAuthManager is the Mender authorization manager
type MenderAuthManager struct {
	// We use this composition so that we can set a finalizer on the outer
	// struct and clean up the go routine which is running using the inner
	// struct.
	*menderAuthManagerService
}

type menderAuthManagerService struct {
	hasStarted bool
	inChan     chan AuthManagerRequest

	workerChan chan AuthManagerRequest

	quitReq  chan bool
	quitResp chan bool

	authReq api.AuthRequester
	client  *http.Client

	forceBootstrap bool
	dbus           dbus.DBusAPI
	dbusConn       dbus.Handle
	config         *conf.AuthConfig
	keyStore       *store.Keystore
	idSrc          device.IdentityDataGetter

	authToken   api.AuthToken
	serverURL   string
	tenantToken api.AuthToken
}

// AuthManagerConfig holds the configuration of the auth manager
type AuthManagerConfig struct {
	Config         *conf.AuthConfig          // mender config struct
	AuthDataStore  store.Store               // authorization data store
	KeyDirStore    store.Store               // key storage
	KeyPassphrase  string                    // key passphrase
	DBusAPI        dbus.DBusAPI              // provider of DBus API
	IdentitySource device.IdentityDataGetter // provider of identity data
}

// NewAuthManager returns a new Mender authorization manager instance
func NewAuthManager(config AuthManagerConfig) (*MenderAuthManager, error) {
	var ks *store.Keystore
	var privateKey string
	var sslEngine string
	var static bool

	if config.Config == nil || config.AuthDataStore == nil || config.KeyDirStore == nil {
		return nil, errors.New("Config, AuthDataStore and KeyDirStore must be set in AuthManagerConfig")
	}

	if config.Config.HttpsClient.Key != "" {
		privateKey = config.Config.HttpsClient.Key
		sslEngine = config.Config.HttpsClient.SSLEngine
		static = true
	}
	if config.Config.Security.AuthPrivateKey != "" {
		privateKey = config.Config.Security.AuthPrivateKey
		sslEngine = config.Config.Security.SSLEngine
		static = true
	}
	if config.Config.HttpsClient.Key == "" && config.Config.Security.AuthPrivateKey == "" {
		privateKey = commonconf.DefaultKeyFile
		sslEngine = config.Config.HttpsClient.SSLEngine
		static = false
	}

	ks = store.NewKeystore(config.KeyDirStore, privateKey, sslEngine, static, config.KeyPassphrase)
	if ks == nil {
		return nil, errors.New("failed to setup key storage")
	}

	var client *http.Client
	if config.Config != nil {
		var err error
		client, err = tls.NewHttpOrHttpsClient(config.Config.GetHttpConfig())
		if err != nil {
			return nil, err
		}
	}

	tenantToken := api.AuthToken(config.Config.TenantToken)

	var dbusAPI dbus.DBusAPI
	if config.DBusAPI != nil {
		dbusAPI = config.DBusAPI
	} else {
		dbusAPI = dbus.GetDBusAPI()
	}

	var idSrc device.IdentityDataGetter
	if config.IdentitySource != nil {
		idSrc = config.IdentitySource
	} else {
		idSrc = device.NewIdentityDataGetter()
	}

	mgr := &MenderAuthManager{
		&menderAuthManagerService{
			inChan:      make(chan AuthManagerRequest, authManagerInMessageChanSize),
			quitReq:     make(chan bool),
			quitResp:    make(chan bool),
			workerChan:  make(chan AuthManagerRequest, authManagerWorkerQueueSize),
			client:      client,
			authReq:     api.NewAuth(),
			dbus:        dbusAPI,
			config:      config.Config,
			keyStore:    ks,
			idSrc:       idSrc,
			tenantToken: tenantToken,
		},
	}

	if err := mgr.keyStore.Load(); err != nil && !store.IsNoKeys(err) {
		log.Errorf("Failed to load device keys: %v", err)
		// Otherwise ignore error returned from Load() call. It will
		// just result in an empty keyStore which in turn will cause
		// regeneration of keys.
	}

	// Clean up keys that we don't use anymore. This is safe to do even if
	// rolling back, because the old clients will just fetch a new
	// AuthToken. Ignore all errors here; this is just cleanup, and it
	// doesn't matter if it fails.
	config.AuthDataStore.WriteTransaction(func(txn store.Transaction) error {
		txn.Remove(dbkeys.AuthTokenName)
		txn.Remove(dbkeys.AuthTokenCacheInvalidatorName)
		return nil
	})
	// We don't use this anymore after this.
	config.AuthDataStore.Close()

	return mgr, nil
}

// nextServerIterator returns an iterator like function that cycles through the
// list of available servers in mender.conf.MenderConfig.Servers
func nextServerIterator(config *conf.AuthConfig) func() *conf.MenderServer {
	servers := config.Servers

	if servers == nil || len(servers) == 0 {
		if config.ServerURL == "" {
			log.Error("Empty server list! Make sure at least one server " +
				"is specified in /etc/mender/mender.conf")
			return nil
		}

		servers = []conf.MenderServer{conf.MenderServer{config.ServerURL}}
	}

	idx := 0
	return func() (server *conf.MenderServer) {
		var ret *conf.MenderServer
		if idx < len(servers) {
			ret = &servers[idx]
			idx++
		} else {
			// return nil which terminates Do()
			// and reset index (for reuse of request)
			ret = nil
			idx = 0
		}
		return ret
	}
}

func (m *menderAuthManagerService) registerDBusCallbacks() (unregisterFunc func()) {
	// GetJwtToken
	m.dbus.RegisterMethodCallCallback(AuthManagerDBusPath, AuthManagerDBusInterfaceName, "GetJwtToken", func(objectPath, interfaceName, methodName string, parameters string) ([]interface{}, error) {
		respChan := make(chan AuthManagerResponse)
		m.inChan <- AuthManagerRequest{
			Action:          ActionGetAuthToken,
			ResponseChannel: respChan,
		}
		select {
		case message := <-respChan:
			return []interface{}{string(message.AuthToken), m.serverURL}, message.Error
		case <-time.After(5 * time.Second):
		}
		return []interface{}{"", ""}, errors.New("timeout when calling GetJwtToken")
	})
	// FetchJwtToken
	m.dbus.RegisterMethodCallCallback(AuthManagerDBusPath, AuthManagerDBusInterfaceName, "FetchJwtToken", func(objectPath, interfaceName, methodName string, parameters string) ([]interface{}, error) {
		respChan := make(chan AuthManagerResponse)
		m.inChan <- AuthManagerRequest{
			Action:          ActionFetchAuthToken,
			ResponseChannel: respChan,
		}
		select {
		case message := <-respChan:
			return []interface{}{message.Event == EventFetchAuthToken}, message.Error
		case <-time.After(5 * time.Second):
		}
		return []interface{}{false}, errors.New("timeout when calling FetchJwtToken")
	})

	return func() {
		m.dbus.UnregisterMethodCallCallback(AuthManagerDBusPath, AuthManagerDBusInterfaceName, "FetchJwtToken")
		m.dbus.UnregisterMethodCallCallback(AuthManagerDBusPath, AuthManagerDBusInterfaceName, "GetJwtToken")
	}
}

// This is idempotent, the service will only start once.
func (m *MenderAuthManager) Start() {
	if m.menderAuthManagerService.hasStarted {
		return
	}

	m.menderAuthManagerService.hasStarted = true

	initDone := make(chan bool, 1)
	go m.menderAuthManagerService.run(initDone)

	// Wait for initialization to finish.
	<-initDone

	runtime.SetFinalizer(m, func(m *MenderAuthManager) {
		m.Stop()
	})
}

// initDone is written to when the initialization is done.
func (m *menderAuthManagerService) run(initDone chan bool) {
	// When we are being stopped, make sure they know that this happened.
	defer func() {
		// Checking for panic here is just to avoid deadlocking if we
		// get an unexpected panic: Let it propogate instead of blocking
		// on the channel. If the program is correct, this should never
		// be non-nil.
		if recover() == nil {
			m.quitResp <- true
		}
	}()

	// run the DBus interface, if available
	dbusConn := dbus.Handle(nil)
	dbusLoop := dbus.MainLoop(nil)
	if m.dbus != nil {
		var err error
		if dbusConn, err = m.dbus.BusGet(dbus.GBusTypeSystem); err == nil {
			m.dbusConn = dbusConn

			nameGid, err := m.dbus.BusOwnNameOnConnection(dbusConn, AuthManagerDBusObjectName,
				dbus.DBusNameOwnerFlagsAllowReplacement|dbus.DBusNameOwnerFlagsReplace)
			if err != nil {
				log.Errorf("Could not own DBus name '%s': %s", AuthManagerDBusObjectName, err.Error())
				goto mainloop
			}
			defer m.dbus.BusUnownName(nameGid)

			intGid, err := m.dbus.BusRegisterInterface(dbusConn, AuthManagerDBusPath, AuthManagerDBusInterface)
			if err != nil {
				log.Errorf("Could register DBus interface name '%s' at path '%s': %s",
					AuthManagerDBusInterface, AuthManagerDBusPath, err.Error())
				goto mainloop
			}
			defer m.dbus.BusUnregisterInterface(dbusConn, intGid)

			unregisterFunc := m.registerDBusCallbacks()
			defer unregisterFunc()

			dbusLoop = m.dbus.MainLoopNew()
			go m.dbus.MainLoopRun(dbusLoop)
			defer m.dbus.MainLoopQuit(dbusLoop)
		} else {
			log.Errorf("Error when connecting to D-Bus system bus: %s", err.Error())
		}
	}

mainloop:
	initDone <- true

	// Broadcast the TokenStateChange signal once on startup, if we have a
	// valid token. The reason this is important is that clients that use
	// the auth DBus API may already have tried calling GetJwtToken
	// unsuccessfully, and are now waiting for a signal. If we don't
	// broadcast it on startup, these clients may be left without a token
	// until it expires and we get a new one, which can take several days.
	if m.authToken != "" {
		m.broadcastAuthTokenStateChange()
	}

	go m.longRunningWorkerLoop()

	// run the auth manager main loop
	running := true
	for running {
		select {
		case msg := <-m.inChan:
			switch msg.Action {
			case ActionGetAuthToken:
				log.Debug("received the GET_AUTH_TOKEN action")
				m.getAuthToken(msg.ResponseChannel)
			case ActionFetchAuthToken:
				log.Debug("received the FETCH_AUTH_TOKEN action")

				// notify the sender we'll fetch the token
				resp := AuthManagerResponse{Event: EventFetchAuthToken}
				msg.ResponseChannel <- resp

				// Potentially long running operation, use worker.
				select {
				case m.workerChan <- msg:
				default:
					// Already a request in the queue, nothing to do.
				}
			}
		case <-m.quitReq:
			running = false
			m.workerChan <- AuthManagerRequest{}
			break
		}
	}
}

// This is a helper to the main loop, for tasks that may take a long time. It's
// running in a separate Go routine.
func (m *menderAuthManagerService) longRunningWorkerLoop() {
	for {
		select {
		case msg := <-m.workerChan:
			switch msg.Action {
			case ActionFetchAuthToken:
				m.fetchAuthToken()
			case "":
				// Quit loop.
				return
			}
		}
	}
}

// Stop the running MenderAuthManager. Must not be called in the same go
// routine as run(). This is idempotent, it is safe to call on a stopped
// service.
func (m *MenderAuthManager) Stop() {
	if !m.menderAuthManagerService.hasStarted {
		return
	}

	m.menderAuthManagerService.quitReq <- true
	<-m.menderAuthManagerService.quitResp
	m.menderAuthManagerService.hasStarted = false

	runtime.SetFinalizer(m, nil)
}

// getAuthToken returns the cached auth token
func (m *menderAuthManagerService) getAuthToken(responseChannel chan<- AuthManagerResponse) {
	msg := AuthManagerResponse{
		AuthToken: m.authToken,
		Event:     EventGetAuthToken,
		Error:     nil,
	}
	responseChannel <- msg
}

// broadcast broadcasts the notification to all the subscribers
func (m *menderAuthManagerService) broadcast(message AuthManagerResponse) {
	// emit signal on dbus, if available
	if m.dbus != nil {
		m.dbus.EmitSignal(m.dbusConn, "", AuthManagerDBusPath,
			AuthManagerDBusInterfaceName, AuthManagerDBusSignalJwtTokenStateChange,
			string(message.AuthToken), m.serverURL)
	}
}

// broadcastAuthTokenStateChange broadcasts the notification to all the subscribers
func (m *menderAuthManagerService) broadcastAuthTokenStateChange() {
	m.broadcast(AuthManagerResponse{
		Event:     EventAuthTokenStateChange,
		AuthToken: m.authToken,
	})
}

// fetchAuthToken authenticates with the server and retrieve a new auth token, if needed
func (m *menderAuthManagerService) fetchAuthToken() {
	var rsp []byte
	var err error
	var server *conf.MenderServer
	resp := AuthManagerResponse{Event: EventFetchAuthToken}

	defer func() {
		if resp.Error == nil {
			m.broadcastAuthTokenStateChange()
		} else {
			m.broadcast(resp)
		}
	}()

	if err := m.Bootstrap(); err != nil {
		log.Errorf("Bootstrap failed: %s", err)
		resp.Error = err
		return
	}

	// Cycle through servers and attempt to authorize.
	serverIterator := nextServerIterator(m.config)
	if serverIterator == nil {
		log.Debug("empty server list in mender.conf, serverIterator is nil")
		panic(NewAuthPanic(errors.New("empty server list in mender.conf")))
	}

	if server = serverIterator(); server == nil {
		log.Debug("empty server list in mender.conf, server is nil")
		panic(NewAuthPanic(errors.New("empty server list in mender.conf")))
	}

	for {
		log.Debugf("Trying to authenticate with %s", server.ServerURL)
		rsp, err = m.authReq.Request(m.client, server.ServerURL, m)

		if err == nil {
			log.Debugf("Successfully authenticated with %s", server.ServerURL)
			// SUCCESS!
			break
		}
		log.Errorf("Got error when trying to authenticate with server %s: %s",
			server.ServerURL, err.Error())
		prevHost := server.ServerURL
		server = serverIterator()
		if server == nil {
			break
		}
		log.Warnf("Failed to authorize %q; attempting %q.",
			prevHost, server.ServerURL)
	}
	if err != nil {
		// Generate and report error.
		errCause := errors.Cause(err)
		if errCause == api.AuthErrorUnauthorized {
			// make sure to remove auth token once device is rejected
			m.authToken = ""
		}
		err := errors.Wrap(err, "authorization request failed")
		resp.Error = err
		return
	}

	err = m.recvAuthResponse(rsp)
	if err != nil {
		err := errors.Wrap(err, "failed to parse authorization response")
		resp.Error = err
		return
	}

	// store the current server URL
	m.serverURL = server.ServerURL

	log.Info("successfully received new authorization data")
}

// ForceBootstrap forces the bootstrap
func (m *menderAuthManagerService) ForceBootstrap() {
	m.forceBootstrap = true
}

// Bootstrap performs the bootstrap, if needed or forced
func (m *menderAuthManagerService) Bootstrap() error {
	if !m.needsBootstrap() {
		return nil
	}

	return m.doBootstrap()
}

func (m *menderAuthManagerService) needsBootstrap() bool {
	if m.forceBootstrap {
		return true
	}

	if !m.HasKey() {
		log.Debugf("Needs keys")
		return true
	}

	return false
}

func (m *menderAuthManagerService) doBootstrap() error {
	if !m.HasKey() || m.forceBootstrap {
		log.Infof("Device keys not present or bootstrap forced, generating")

		err := m.GenerateKey()
		if err != nil {
			if store.IsStaticKey(err) {
				log.Error("Device key is static, refusing to regenerate.")
			} else {
				panic(NewAuthPanic(err))
			}
		}
	}

	m.forceBootstrap = false
	return nil
}

// MakeAuthRequest makes an auth request
func (m *menderAuthManagerService) MakeAuthRequest() (*api.AuthRequest, error) {

	var err error
	authd := api.AuthReqData{}

	idata, err := m.idSrc.Get()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to obtain identity data")
	}

	authd.IdData = idata

	// fill device public key
	authd.Pubkey, err = m.keyStore.PublicPEM()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to obtain device public key")
	}

	tentok := strings.TrimSpace(string(m.tenantToken))

	log.Debugf("Tenant token: %s", tentok)

	// fill tenant token
	authd.TenantToken = string(tentok)

	log.Debugf("Authorization data: %v", authd)

	reqdata, err := authd.ToBytes()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to convert auth request data")
	}

	// generate signature
	sig, err := m.keyStore.Sign(reqdata)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to sign auth request")
	}

	return &api.AuthRequest{
		Data:      reqdata,
		Token:     api.AuthToken(tentok),
		Signature: sig,
	}, nil
}

// recvAuthResponse processes an auth response
func (m *menderAuthManagerService) recvAuthResponse(data []byte) error {
	if len(data) == 0 {
		return errors.New("empty auth response data")
	}

	m.authToken = api.AuthToken(data)
	return nil
}

// HasKey check if device key is available
func (m *menderAuthManagerService) HasKey() bool {
	return m.keyStore.Private() != nil
}

// GenerateKey generate device key (will overwrite an already existing key)
func (m *menderAuthManagerService) GenerateKey() error {
	if err := m.keyStore.Generate(); err != nil {
		if store.IsStaticKey(err) {
			return err
		}
		log.Errorf("Failed to generate device key: %v", err)
		return errors.Wrapf(err, "failed to generate device key")
	}

	if err := m.keyStore.Save(); err != nil {
		log.Errorf("Failed to save device key: %s", err)
		panic(NewAuthPanic(err))
	}
	return nil
}
