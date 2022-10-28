// Copyright 2022 Northern.tech AS
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
package proxy

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	"github.com/mendersoftware/mender/client"
)

var log = logrus.WithFields(logrus.Fields{"thread": "proxy"})

const (
	ProxyHost        = "127.0.0.1"
	InetAddrLoopback = "127.0.0.1:0"
)

const (
	ApiUrlDevicesPrefix         = "/api/devices/"
	ApiUrlDevicesAuthentication = "/api/devices/v1/authentication/"
	ApiUrlDevicesConnect        = "/api/devices/v1/deviceconnect/connect"
)

var (
	ErrNoAuthHeader = errors.New("no authorization header")
)

// proxyConf holds the configuration of the local proxy
type proxyConf struct {
	backend  *url.URL
	jwtToken string
	listener net.Listener
}

// ProxyController proxies device API requests to Mender server
type ProxyController struct {
	// We use this composition so that we can set a finalizer on the outer
	// struct and clean up the go routine which is running using the inner
	// struct.
	*proxyControllerInner
}
type proxyControllerInner struct {
	isRunning       bool
	ctx             context.Context
	cancelGlobalCtx context.CancelFunc

	conf   *proxyConf
	client client.ApiRequester
	server *http.Server

	quitReq  chan struct{}
	quitResp chan struct{}

	wsDialer           *websocket.Dialer
	wsConnections      map[*wsConnection]bool
	wsConnectionsMutex sync.Mutex
}

type wsConnection struct {
	connClient            *websocket.Conn
	connClientWriteMutex  sync.Mutex
	connBackend           *websocket.Conn
	connBackendWriteMutex sync.Mutex
}

// NewProxyController creates a new controller. If menderUrl and menderJwtToken are specified,
// the proxy is also started.
func NewProxyController(
	client client.ApiRequester,
	dialer *websocket.Dialer,
	menderUrl, menderJwtToken string,
) (*ProxyController, error) {
	ctx, cancel := context.WithCancel(context.Background())
	pc := &ProxyController{
		&proxyControllerInner{
			ctx:             ctx,
			cancelGlobalCtx: cancel,
			client:          client,
			wsDialer:        dialer,
			conf:            &proxyConf{},
			quitReq:         make(chan struct{}, 1),
			quitResp:        make(chan struct{}, 1),
			wsConnections:   make(map[*wsConnection]bool),
		},
	}

	if menderUrl != "" && menderJwtToken != "" {
		l, err := newNetListener()
		if err != nil {
			return nil, errors.Wrap(err, "failed to create proxy")
		}
		pc.conf.listener = l
		err = pc.configureBackend(menderUrl, menderJwtToken)
		if err != nil {
			return nil, errors.Wrap(err, "failed to create proxy")
		}
		pc.Start()
	}

	return pc, nil
}

func newNetListener() (net.Listener, error) {
	l, err := net.Listen("tcp", InetAddrLoopback)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create listener")
	}
	return l, nil
}

func (pc *ProxyController) configureBackend(menderUrl, menderJwtToken string) error {
	u, err := url.Parse(menderUrl)
	if err != nil {
		return errors.Wrap(err, "failed to configure proxy")
	}

	pc.conf.backend = u
	pc.conf.jwtToken = menderJwtToken
	return nil
}

// Reconfigure reconfigures the local proxy server
func (pc *ProxyController) Reconfigure(menderUrl, menderJwtToken string) error {
	if pc.isRunning {
		return errors.New("failed to reconfigure proxy: cannot reconfigure while running")
	}

	l, err := newNetListener()
	if err != nil {
		return errors.Wrap(err, "failed to reconfigure proxy")
	}
	pc.conf.listener = l

	err = pc.configureBackend(menderUrl, menderJwtToken)
	if err != nil {
		return errors.Wrap(err, "failed to reconfigure proxy")
	}

	return nil
}

func (pc *ProxyController) getPort() int {
	return pc.conf.listener.Addr().(*net.TCPAddr).Port
}

// GetServerUrl returns the URL of the proxy
func (pc *ProxyController) GetServerUrl() string {
	if pc.isRunning {
		return fmt.Sprintf("http://%s:%d", ProxyHost, pc.getPort())
	}
	return ""
}

func copyResponse(rw http.ResponseWriter, resp *http.Response) error {
	copyHeader(rw.Header(), resp.Header)
	rw.WriteHeader(resp.StatusCode)
	defer resp.Body.Close()

	_, err := io.Copy(rw, resp.Body)
	return err
}

func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

// Start starts the local proxy server
func (pc *ProxyController) Start() {
	if pc.proxyControllerInner.isRunning {
		return
	}

	// TODO: how to ensure it has a listener before Start?
	//       it is responsability of the caller to check error from Reconfigure()
	//       before calling Start()
	if pc.proxyControllerInner.conf.listener != nil {
		pc.proxyControllerInner.isRunning = true

		initDone := make(chan struct{}, 1)
		go pc.proxyControllerInner.run(initDone)

		// Wait for the server to start
		<-initDone

		runtime.SetFinalizer(pc, func(pc *ProxyController) {
			pc.Stop()
		})
	}
}

// Stop stops the local proxy server
func (pc *ProxyController) Stop() {

	// Safe to cancel multiple times, so just cancel everytime
	pc.cancelGlobalCtx()

	if !pc.isRunning {
		return
	}

	if pc.wsRunning() {
		pc.CloseWsConnections()
	}

	pc.quitReq <- struct{}{}
	// Wait for server to shutdown
	<-pc.quitResp
	pc.isRunning = false

	// Clear the finalizer associated with the proxyController
	runtime.SetFinalizer(pc, nil)
}

func (pc *proxyControllerInner) run(initDone chan struct{}) {
	mux := http.NewServeMux()
	mux.HandleFunc(ApiUrlDevicesPrefix, pc.checkAuthorizationHook(pc.doHttpRequest))
	mux.HandleFunc(ApiUrlDevicesAuthentication, pc.apiDevicesAuthenticationHandler)
	mux.HandleFunc(ApiUrlDevicesConnect, pc.checkAuthorizationHook(pc.apiDevicesConnectHandler))

	// Give each run a seperate context
	globalCtx, globalCancel := context.WithCancel(context.Background())
	pc.ctx = globalCtx
	pc.cancelGlobalCtx = globalCancel

	// Register the ServeMux as the sole Handler. It will delegate to the subhandlers.
	server := http.Server{
		Handler: mux,
	}
	pc.server = &server

	go func(l net.Listener, initDone chan struct{}) {
		initDone <- struct{}{}
		err := pc.server.Serve(l)
		if err != nil && err != http.ErrServerClosed {
			log.Errorf("Proxy Serve failed: %s\n", err)
		}
	}(pc.conf.listener, initDone)

	log.Info("Local proxy started")

	<-pc.quitReq

	log.Info("Local proxy stopped")

	ctx, cancel := context.WithTimeout(globalCtx, 5*time.Second)
	defer cancel()

	if err := pc.server.Shutdown(ctx); err != nil {
		log.Errorf("Proxy Shutdown failed: %s\n", err)
	}

	pc.quitResp <- struct{}{}
}

// extracts JWT from authorization header
func extractToken(header http.Header) (string, error) {
	const authHeaderName = "Authorization"
	authHeader := header.Get(authHeaderName)
	if authHeader == "" {
		return "", ErrNoAuthHeader
	}
	if !(strings.HasPrefix(authHeader, "Bearer ") || strings.HasPrefix(authHeader, "bearer ")) {
		return "", ErrNoAuthHeader
	}
	tokenStr := strings.Replace(authHeader, "Bearer", "", 1)
	tokenStr = strings.Replace(tokenStr, "bearer", "", 1)
	return strings.TrimSpace(tokenStr), nil
}

func (pc *proxyControllerInner) checkAuthorizationHook(f http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if pc.conf.jwtToken == "" {
			http.Error(w, "authmanager not authorized yet", http.StatusUnauthorized)
			return
		}
		token, err := extractToken(r.Header)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return

		}
		if token != pc.conf.jwtToken {
			http.Error(w, "invalid JWT token in Authorization header", http.StatusUnauthorized)
			return
		}
		f(w, r)
	}
}

func (pc *proxyControllerInner) doHttpRequest(w http.ResponseWriter, r *http.Request) {
	// Convert server request to client request and override r.URL
	r.RequestURI = ""
	r.Host = ""
	r.URL.Scheme = pc.conf.backend.Scheme
	r.URL.Host = pc.conf.backend.Host
	log.Debugf(
		"Request: %q %q %q %q %q",
		r.RequestURI,
		r.Host,
		r.URL.Scheme,
		r.URL.Host,
		r.URL.Path,
	)

	// Do the request
	rsp, err := pc.client.Do(r)
	if err != nil {
		log.Error(err)
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer rsp.Body.Close()

	// Copy response to the user
	_ = copyResponse(w, rsp)
}

func (pc *proxyControllerInner) apiDevicesAuthenticationHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
}

func (pc *proxyControllerInner) apiDevicesConnectHandler(w http.ResponseWriter, r *http.Request) {
	if !pc.wsAvailable() {
		http.Error(w, "too many websocket connections", http.StatusServiceUnavailable)
		return
	}
	log.Debugf("Upgrading %s\n", r.URL)
	pc.DoWsUpgrade(pc.ctx, w, r)
}
