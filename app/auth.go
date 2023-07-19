// Copyright 2023 Northern.tech AS
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

package app

import (
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/mendersoftware/mender/app/proxy"
	"github.com/mendersoftware/mender/client"
	"github.com/mendersoftware/mender/conf"
	"github.com/mendersoftware/mender/datastore"
	"github.com/mendersoftware/mender/dbus"
	"github.com/mendersoftware/mender/device"
	"github.com/mendersoftware/mender/store"
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
	noAuthToken                  = client.EmptyAuthToken
	authManagerInMessageChanSize = 1024

	// Keep this at 1 for now. At the time of writing it is only used for
	// fetchAuthToken, and it doesn't make sense to have more than one
	// request in the queue, since additional requests will just accomplish
	// the same thing as one request.
	authManagerWorkerQueueSize = 1
)

// AuthManagerRequest stores a request to the Mender authorization manager
type AuthManagerRequest struct {
	Action          string
	ResponseChannel chan<- AuthManagerResponse
}

// AuthManagerResponse stores a response from the Mender authorization manager
type AuthManagerResponse struct {
	AuthToken client.AuthToken
	ServerURL client.ServerURL
	Event     string
	Error     error
}

// AuthManager is the interface of a Mender authorization manager
type AuthManager interface {
	Bootstrap() menderError
	ForceBootstrap()
	GetInMessageChan() chan<- AuthManagerRequest
	GetBroadcastMessageChan(name string) <-chan AuthManagerResponse
	Start()
	Stop()
	EnableDBus(api dbus.DBusAPI)

	// check if device key is available
	HasKey() bool
	// generate device key (will overwrite an already existing key)
	GenerateKey() error

	client.AuthDataMessenger
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

	broadcastChansMutex sync.Mutex
	broadcastChans      map[string]chan AuthManagerResponse

	workerChan chan AuthManagerRequest

	quitReq  chan bool
	quitResp chan bool

	authReq client.AuthRequester
	api     *client.ApiClient

	forceBootstrap bool
	dbus           dbus.DBusAPI
	dbusConn       dbus.Handle
	config         *conf.MenderConfig
	keyStore       *store.Keystore
	idSrc          device.IdentityDataGetter
	authToken      client.AuthToken
	serverURL      client.ServerURL
	tenantToken    client.AuthToken

	localProxy *proxy.ProxyController
}

// AuthManagerConfig holds the configuration of the auth manager
type AuthManagerConfig struct {
	Config         *conf.MenderConfig        // mender config struct
	AuthDataStore  store.Store               // authorization data store
	KeyStore       *store.Keystore           // key storage
	IdentitySource device.IdentityDataGetter // provider of identity data
	TenantToken    []byte                    // tenant token
}

// NewAuthManager returns a new Mender authorization manager instance
func NewAuthManager(conf AuthManagerConfig) *MenderAuthManager {
	if conf.KeyStore == nil || conf.IdentitySource == nil ||
		conf.AuthDataStore == nil {
		return nil
	}

	httpConfig := client.Config{}
	if conf.Config != nil {
		httpConfig = conf.Config.GetHttpConfig()

	}

	api, err := client.NewApiClient(httpConfig)
	if err != nil {
		return nil
	}

	tenantToken := client.AuthToken(conf.TenantToken)

	wsDialer, err := client.NewWebsocketDialer(httpConfig)
	if err != nil {
		return nil
	}

	proxy, err := proxy.NewProxyController(api, wsDialer, "", "")
	if err != nil {
		log.Errorf("Error creating local proxy: %s", err.Error())
	}

	mgr := &MenderAuthManager{
		&menderAuthManagerService{
			inChan:         make(chan AuthManagerRequest, authManagerInMessageChanSize),
			broadcastChans: map[string]chan AuthManagerResponse{},
			quitReq:        make(chan bool),
			quitResp:       make(chan bool),
			workerChan:     make(chan AuthManagerRequest, authManagerWorkerQueueSize),
			api:            api,
			authReq:        client.NewAuth(),
			config:         conf.Config,
			keyStore:       conf.KeyStore,
			idSrc:          conf.IdentitySource,
			tenantToken:    tenantToken,
			localProxy:     proxy,
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
	_ = conf.AuthDataStore.WriteTransaction(func(txn store.Transaction) error {
		_ = txn.Remove(datastore.AuthTokenName)
		_ = txn.Remove(datastore.AuthTokenCacheInvalidatorName)
		return nil
	})

	return mgr
}

func (m *MenderAuthManager) EnableDBus(api dbus.DBusAPI) {
	if m.hasStarted {
		panic("Calling WithDBus() after the service has started is a programming mistake.")
	}
	m.dbus = api
}

// GetInMessageChan returns the channel to send requests to the auth manager
func (m *MenderAuthManager) GetInMessageChan() chan<- AuthManagerRequest {
	// Auto-start the service if it hasn't been started already.
	m.Start()
	return m.inChan
}

// GetBroadcastMessageChan returns the channel to get responses from the auth manager
func (m *MenderAuthManager) GetBroadcastMessageChan(name string) <-chan AuthManagerResponse {
	// Auto-start the service if it hasn't been started already.
	m.Start()

	m.broadcastChansMutex.Lock()
	defer m.broadcastChansMutex.Unlock()

	if m.broadcastChans[name] == nil {
		m.broadcastChans[name] = make(chan AuthManagerResponse, 1)
	}
	return m.broadcastChans[name]
}

func (m *menderAuthManagerService) registerDBusCallbacks() (unregisterFunc func()) {
	// GetJwtToken
	m.dbus.RegisterMethodCallCallback(
		AuthManagerDBusPath,
		AuthManagerDBusInterfaceName,
		"GetJwtToken",
		func(objectPath, interfaceName, methodName string, parameters string) (interface{}, error) {
			respChan := make(chan AuthManagerResponse, 1)
			m.inChan <- AuthManagerRequest{
				Action:          ActionGetAuthToken,
				ResponseChannel: respChan,
			}
			timeout := timers.Get(time.Second * 5)
			select {
			case message, ok := <-respChan:
				if !ok {
					// (race): AuthManagerService timed out.
					break
				}
				tokenAndServerURL := dbus.TokenAndServerURL{
					Token:     string(message.AuthToken),
					ServerURL: m.localProxy.GetServerUrl(),
				}
				return tokenAndServerURL, message.Error
			case <-timeout.C:
				timers.Put(timeout)
			}
			return string(noAuthToken), errors.New("timeout when calling GetJwtToken")
		},
	)
	// FetchJwtToken
	m.dbus.RegisterMethodCallCallback(
		AuthManagerDBusPath,
		AuthManagerDBusInterfaceName,
		"FetchJwtToken",
		func(objectPath, interfaceName, methodName string, parameters string) (interface{}, error) {
			respChan := make(chan AuthManagerResponse, 1)
			m.inChan <- AuthManagerRequest{
				Action:          ActionFetchAuthToken,
				ResponseChannel: respChan,
			}
			timeout := timers.Get(time.Second * 5)
			select {
			case message, ok := <-respChan:
				if !ok {
					// (race): AuthManagerService timed out.
					break
				}
				return message.Event == EventFetchAuthToken, message.Error
			case <-timeout.C:
				timers.Put(timeout)
			}
			return false, errors.New("timeout when calling FetchJwtToken")
		},
	)

	return func() {
		m.dbus.UnregisterMethodCallCallback(
			AuthManagerDBusPath,
			AuthManagerDBusInterfaceName,
			"FetchJwtToken",
		)
		m.dbus.UnregisterMethodCallCallback(
			AuthManagerDBusPath,
			AuthManagerDBusInterfaceName,
			"GetJwtToken",
		)
	}
}

// This is idempotent, the service will only start once.
func (m *MenderAuthManager) Start() {
	if m.menderAuthManagerService.hasStarted {
		return
	}

	m.menderAuthManagerService.hasStarted = true

	initDone := make(chan struct{}, 1)
	go m.menderAuthManagerService.run(initDone)

	// Wait for initialization to finish.
	<-initDone

	runtime.SetFinalizer(m, func(m *MenderAuthManager) {
		m.Stop()
	})
}

// Run is the main routine of the Mender authorization manager
func (m *menderAuthManagerService) run(initDone chan struct{}) {
	// When we are being stopped, make sure they know that this happened.
	defer func() {
		// Checking for panic here is just to avoid deadlocking if we
		// get an unexpected panic: Let it propagate instead of blocking
		// on the channel. If the program is correct, this should never
		// be non-nil.
		if recover() == nil {
			m.quitResp <- true
		}
	}()

	// run the DBus interface, if available
	if m.dbus != nil {
		if dbusConn, err := m.dbus.BusGet(dbus.GBusTypeSystem); err == nil {
			m.dbusConn = dbusConn

			nameGid, err := m.dbus.BusOwnNameOnConnection(dbusConn, AuthManagerDBusObjectName,
				dbus.DBusNameOwnerFlagsAllowReplacement|dbus.DBusNameOwnerFlagsReplace)
			if err != nil {
				log.Errorf(
					"Could not own DBus name '%s': %s",
					AuthManagerDBusObjectName,
					err.Error(),
				)
				goto mainloop
			}
			defer m.dbus.BusUnownName(nameGid)

			intGid, err := m.dbus.BusRegisterInterface(
				dbusConn,
				AuthManagerDBusPath,
				AuthManagerDBusInterface,
			)
			if err != nil {
				log.Errorf("Could register DBus interface name '%s' at path '%s': %s",
					AuthManagerDBusInterface, AuthManagerDBusPath, err.Error())
				goto mainloop
			}
			defer m.dbus.BusUnregisterInterface(dbusConn, intGid)

			unregisterFunc := m.registerDBusCallbacks()
			defer unregisterFunc()

			dbusLoop := m.dbus.MainLoopNew()
			go m.dbus.MainLoopRun(dbusLoop)
			defer m.dbus.MainLoopQuit(dbusLoop)
		}
	}

mainloop:
	initDone <- struct{}{}

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
				timeout := timers.Get(time.Second * 5)
				select {
				case msg.ResponseChannel <- resp:

				case <-timeout.C:
					timers.Put(timeout)
					close(msg.ResponseChannel)
				}

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
		}
	}
}

// This is a helper to the main loop, for tasks that may take a long time. It's
// running in a separate Go routine.
func (m *menderAuthManagerService) longRunningWorkerLoop() {
	for msg := range m.workerChan {
		switch msg.Action {
		case ActionFetchAuthToken:
			m.fetchAuthToken()
		case "":
			// Quit loop.
			return

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

	m.localProxy.Stop()

	runtime.SetFinalizer(m, nil)
}

// getAuthToken returns the cached auth token
func (m *menderAuthManagerService) getAuthToken(responseChannel chan<- AuthManagerResponse) {
	msg := AuthManagerResponse{
		AuthToken: m.authToken,
		ServerURL: m.serverURL,
		Event:     EventGetAuthToken,
	}

	timeout := timers.Get(time.Second * 5)
	select {
	case responseChannel <- msg:

	case <-timeout.C:
		timers.Put(timeout)
		close(responseChannel)
	}
}

// broadcast broadcasts the notification to all the subscribers
func (m *menderAuthManagerService) broadcast(message AuthManagerResponse) {
	m.broadcastChansMutex.Lock()
	for name, broadcastChan := range m.broadcastChans {
		select {
		case broadcastChan <- message:
		default:
			close(broadcastChan)
			delete(m.broadcastChans, name)
		}
	}
	m.broadcastChansMutex.Unlock()

	// emit signal on dbus, if available
	if m.dbus != nil {
		tokenAndServerURL := dbus.TokenAndServerURL{
			Token:     string(message.AuthToken),
			ServerURL: string(message.ServerURL),
		}
		_ = m.dbus.EmitSignal(m.dbusConn, "", AuthManagerDBusPath,
			AuthManagerDBusInterfaceName, AuthManagerDBusSignalJwtTokenStateChange,
			tokenAndServerURL)
	}
}

// broadcastAuthTokenStateChange broadcasts the notification to all the subscribers
func (m *menderAuthManagerService) broadcastAuthTokenStateChange() {
	m.localProxy.Stop()
	if m.authToken != "" {
		// reconfigure proxy
		err := m.localProxy.Reconfigure(string(m.serverURL), string(m.authToken))
		if err != nil {
			log.Errorf(
				"Could not reconfigure proxy with URL %q and token '%s...'"+
					" Other applications running on the device won't be able"+
					" to reach the Mender server. Error: %s",
				m.serverURL,
				string(m.authToken)[:7],
				err.Error(),
			)
		} else {
			m.localProxy.Start()

		}
	}

	m.broadcast(AuthManagerResponse{
		Event:     EventAuthTokenStateChange,
		AuthToken: m.authToken,
		ServerURL: client.ServerURL(m.localProxy.GetServerUrl()),
	})
}

// fetchAuthToken authenticates with the server and retrieve a new auth token, if needed
func (m *menderAuthManagerService) fetchAuthToken() {
	var rsp []byte
	var err error
	var server *client.MenderServer
	resp := AuthManagerResponse{Event: EventFetchAuthToken}

	defer func() {
		m.broadcastAuthTokenStateChange()
	}()

	if err := m.Bootstrap(); err != nil {
		log.Errorf("Bootstrap failed: %s", err)
		resp.Error = err
		return
	}

	// Cycle through servers and attempt to authorize.
	serverIterator := nextServerIterator(*m.config)
	if serverIterator == nil {
		log.Debug("empty server list in mender.conf, serverIterator is nil")
		err := NewFatalError(errors.New("empty server list in mender.conf"))
		resp.Error = err
		return
	}

	if server = serverIterator(); server == nil {
		log.Debug("empty server list in mender.conf, server is nil")
		err := NewFatalError(errors.New("empty server list in mender.conf"))
		resp.Error = err
		return
	}

	var serverURL string
	for {
		serverURL = server.ServerURL
		rsp, err = m.authReq.Request(m.api, serverURL, m)

		if err == nil {
			// SUCCESS!
			break
		}
		log.Errorf("Failed to authorize with %q: %s",
			server.ServerURL, err.Error())
		server = serverIterator()
		if server == nil {
			break
		}
		log.Infof("Attempting to authorize with %q.",
			server.ServerURL)
	}
	if err != nil {
		// Generate and report error.
		errCause := errors.Cause(err)
		if errCause == client.AuthErrorUnauthorized {
			m.authToken = ""
			m.serverURL = ""
		}
		err := NewTransientError(errors.Wrap(err, "authorization request failed"))
		resp.Error = err
		return
	}

	if len(rsp) == 0 {
		resp.Error = errors.New("empty auth response data")
		return
	}

	m.authToken = client.AuthToken(rsp)
	m.serverURL = client.ServerURL(serverURL)

	log.Infof("successfully received new authorization data from server %s", m.serverURL)
}

// ForceBootstrap forces the bootstrap
func (m *menderAuthManagerService) ForceBootstrap() {
	m.forceBootstrap = true
}

// Bootstrap performs the bootstrap, if needed or forced
func (m *menderAuthManagerService) Bootstrap() menderError {
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

func (m *menderAuthManagerService) doBootstrap() menderError {
	if !m.HasKey() || m.forceBootstrap {
		log.Infof("Device keys not present or bootstrap forced, generating")

		err := m.GenerateKey()
		if err != nil {
			if store.IsStaticKey(err) {
				log.Error("Device key is static, refusing to regenerate.")
			} else {
				return NewFatalError(err)
			}
		}
	}

	m.forceBootstrap = false
	return nil
}

// MakeAuthRequest makes an auth request
func (m *menderAuthManagerService) MakeAuthRequest() (*client.AuthRequest, error) {

	var err error
	authd := client.AuthReqData{}

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

	return &client.AuthRequest{
		Data:      reqdata,
		Token:     client.AuthToken(tentok),
		Signature: sig,
	}, nil
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
		return NewFatalError(err)
	}
	return nil
}
