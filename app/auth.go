// Copyright 2020 Northern.tech AS
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
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

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
	EventFetchAuthToken     = "FETCH_AUTH_TOKEN"
	EventGetAuthToken       = "GET_AUTH_TOKEN"
	EventAuthTokenAvailable = "AUTH_TOKEN_AVAILABLE"
)

// Constants for the auth manager DBus interface
const (
	AuthManagerDBusPath                         = "/io/mender/AuthenticationManager"
	AuthManagerDBusObjectName                   = "io.mender.AuthenticationManager"
	AuthManagerDBusInterfaceName                = "io.mender.Authentication1"
	AuthManagerDBusSignalValidJwtTokenAvailable = "ValidJwtTokenAvailable"
	AuthManagerDBusInterface                    = `<node>
	<interface name="io.mender.Authentication1">
		<method name="GetJwtToken">
			<arg type="s" name="token" direction="out"/>
		</method>
		<method name="FetchJwtToken">
			<arg type="b" name="success" direction="out"/>
		</method>
		<signal name="ValidJwtTokenAvailable"></signal>
	</interface>
</node>`
)

const (
	noAuthToken                  = client.EmptyAuthToken
	authManagerInMessageChanSize = 1024
)

// AuthManagerRequest stores a request to the Mender authorization manager
type AuthManagerRequest struct {
	Action          string
	ResponseChannel chan<- AuthManagerResponse
}

// AuthManagerResponse stores a response from the Mender authorization manager
type AuthManagerResponse struct {
	AuthToken client.AuthToken
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
	WithDBus(api dbus.DBusAPI) AuthManager

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
	hasStarted     bool
	inChan         chan AuthManagerRequest
	broadcastChans map[string]chan AuthManagerResponse

	quitReq  chan bool
	quitResp chan bool

	authReq client.AuthRequester
	api     *client.ApiClient

	forceBootstrap bool
	dbus           dbus.DBusAPI
	dbusConn       dbus.Handle
	config         *conf.MenderConfig
	store          store.Store
	keyStore       *store.Keystore
	idSrc          device.IdentityDataGetter
	tenantToken    client.AuthToken
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
func NewAuthManager(conf AuthManagerConfig) AuthManager {
	if conf.KeyStore == nil || conf.IdentitySource == nil ||
		conf.AuthDataStore == nil {
		return nil
	}

	var api *client.ApiClient
	if conf.Config != nil {
		var err error
		api, err = client.New(conf.Config.GetHttpConfig())
		if err != nil {
			return nil
		}
	}

	mgr := &MenderAuthManager{
		&menderAuthManagerService{
			inChan:         make(chan AuthManagerRequest, authManagerInMessageChanSize),
			broadcastChans: map[string]chan AuthManagerResponse{},
			quitReq:        make(chan bool),
			quitResp:       make(chan bool),
			api:            api,
			authReq:        client.NewAuth(),
			config:         conf.Config,
			store:          conf.AuthDataStore,
			keyStore:       conf.KeyStore,
			idSrc:          conf.IdentitySource,
			tenantToken:    client.AuthToken(conf.TenantToken),
		},
	}

	if err := mgr.keyStore.Load(); err != nil && !store.IsNoKeys(err) {
		log.Errorf("Failed to load device keys: %v", err)
		// Otherwise ignore error returned from Load() call. It will
		// just result in an empty keyStore which in turn will cause
		// regeneration of keys.
	}

	return mgr
}

// WithDBus returns a DBus-enabled MenderAuthManager
func (m *MenderAuthManager) WithDBus(api dbus.DBusAPI) AuthManager {
	if m.hasStarted {
		panic("Calling WithDBus() after the service has started is a programming mistake.")
	}
	m.dbus = api
	return m
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
	if m.broadcastChans[name] == nil {
		m.broadcastChans[name] = make(chan AuthManagerResponse, 1)
	}
	return m.broadcastChans[name]
}

func (m *menderAuthManagerService) registerDBusCallbacks() (unregisterFunc func()) {
	// GetJwtToken
	m.dbus.RegisterMethodCallCallback(AuthManagerDBusPath, AuthManagerDBusInterfaceName, "GetJwtToken", func(objectPath, interfaceName, methodName string) (interface{}, error) {
		respChan := make(chan AuthManagerResponse)
		m.inChan <- AuthManagerRequest{
			Action:          ActionGetAuthToken,
			ResponseChannel: respChan,
		}
		select {
		case message := <-respChan:
			return string(message.AuthToken), message.Error
		case <-time.After(5 * time.Second):
		}
		return string(noAuthToken), errors.New("timeout when calling GetJwtToken")
	})
	// FetchJwtToken
	m.dbus.RegisterMethodCallCallback(AuthManagerDBusPath, AuthManagerDBusInterfaceName, "FetchJwtToken", func(objectPath, interfaceName, methodName string) (interface{}, error) {
		respChan := make(chan AuthManagerResponse)
		m.inChan <- AuthManagerRequest{
			Action:          ActionFetchAuthToken,
			ResponseChannel: respChan,
		}
		select {
		case message := <-respChan:
			return message.Event == EventFetchAuthToken, message.Error
		case <-time.After(5 * time.Second):
		}
		return false, errors.New("timeout when calling GetJwtToken")
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
	go m.menderAuthManagerService.run()
	runtime.SetFinalizer(m, func(m *MenderAuthManager) {
		m.Stop()
	})
}

// Run is the main routine of the Mender authorization manager
func (m *menderAuthManagerService) run() {
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
		}
	}

mainloop:
	// run the auth manager main loop
	running := true
	for running {
		select {
		case msg := <-m.inChan:
			switch msg.Action {
			case ActionGetAuthToken:
				log.Debug("received the GET_AUTH_TOKENS action")
				m.getAuthToken(msg.ResponseChannel)
			case ActionFetchAuthToken:
				log.Debug("received the FETCH_AUTH_TOKEN action")
				m.fetchAuthToken(msg.ResponseChannel)
			}
		case <-m.quitReq:
			running = false
			break
		}
	}
}

// Stops the running MenderAuthManager. Must not be called in the same go
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
	authToken, err := m.authToken()
	msg := AuthManagerResponse{
		AuthToken: authToken,
		Event:     EventGetAuthToken,
		Error:     err,
	}
	responseChannel <- msg
}

// broadcast broadcasts the notification to all the subscribers
func (m *menderAuthManagerService) broadcast(message AuthManagerResponse) {
	for _, broadcastChan := range m.broadcastChans {
		select {
		case broadcastChan <- message:
		default:
		}
	}
	// emit signal on dbus, if available
	if m.dbus != nil {
		m.dbus.EmitSignal(m.dbusConn, "", AuthManagerDBusPath,
			AuthManagerDBusInterfaceName, AuthManagerDBusSignalValidJwtTokenAvailable)
	}
}

// broadcastAuthTokenAvailable broadcasts the notification to all the subscribers
func (m *menderAuthManagerService) broadcastAuthTokenAvailable() {
	authToken, err := m.authToken()
	if err == nil && authToken != noAuthToken {
		m.broadcast(AuthManagerResponse{Event: EventAuthTokenAvailable})
	}
}

// fetchAuthToken authenticates with the server and retrieve a new auth token, if needed
func (m *menderAuthManagerService) fetchAuthToken(responseChannel chan<- AuthManagerResponse) {
	var rsp []byte
	var err error
	var server *client.MenderServer

	// notify the sender we'll fetch the token
	resp := AuthManagerResponse{Event: EventFetchAuthToken}
	responseChannel <- resp

	defer func() {
		if resp.Error == nil {
			m.broadcastAuthTokenAvailable()
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

	for {
		rsp, err = m.authReq.Request(m.api, server.ServerURL, m)

		if err == nil {
			// SUCCESS!
			break
		}
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
		if errCause == client.AuthErrorUnauthorized {
			// make sure to remove auth token once device is rejected
			if remErr := m.removeAuthToken(); remErr != nil {
				log.Warn("can not remove rejected authentication token")
			}
		}
		err := NewTransientError(errors.Wrap(err, "authorization request failed"))
		resp.Error = err
		return
	}

	err = m.recvAuthResponse(rsp)
	if err != nil {
		err := NewTransientError(errors.Wrap(err, "failed to parse authorization response"))
		resp.Error = err
		return
	}

	log.Info("successfully received new authorization data")
	return
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

// recvAuthResponse processes an auth response
func (m *menderAuthManagerService) recvAuthResponse(data []byte) error {
	if len(data) == 0 {
		return errors.New("empty auth response data")
	}

	if err := m.store.WriteAll(datastore.AuthTokenName, data); err != nil {
		return errors.Wrapf(err, "failed to save auth token")
	}
	return nil
}

// AuthToken returns device's authorization token
func (m *menderAuthManagerService) authToken() (client.AuthToken, error) {
	data, err := m.store.ReadAll(datastore.AuthTokenName)
	if err != nil {
		if os.IsNotExist(err) {
			return noAuthToken, nil
		}
		return noAuthToken, errors.Wrapf(err, "failed to read auth token data")
	}

	return client.AuthToken(data), nil
}

// removeAuthToken removes authentication token
func (m *menderAuthManagerService) removeAuthToken() error {
	// remove token only if we have one
	if aToken, err := m.authToken(); err == nil && aToken != noAuthToken {
		return m.store.Remove(datastore.AuthTokenName)
	}
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
		return NewFatalError(err)
	}
	return nil
}
