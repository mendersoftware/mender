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
package proxy

// Inspired by https://github.com/koding/websocketproxy

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
)

const (
	maxWsConnections = 1
)

func newUpgrader() *websocket.Upgrader {
	return &websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
}

func wsSchemeFromHttpScheme(httpScheme string) string {
	switch httpScheme {
	case "http":
		return "ws"
	case "https":
		return "wss"
	default:
		return ""
	}
}

func (pc *ProxyController) wsRunning() bool {
	pc.wsConnectionsMutex.Lock()
	defer pc.wsConnectionsMutex.Unlock()
	return len(pc.wsConnections) > 0
}

func (pc *proxyControllerInner) wsAvailable() bool {
	pc.wsConnectionsMutex.Lock()
	defer pc.wsConnectionsMutex.Unlock()
	return len(pc.wsConnections) < maxWsConnections
}

func (pc *proxyControllerInner) DoWsUpgrade(w http.ResponseWriter, r *http.Request) {
	// Convert server request to client request and override r.URL
	r.RequestURI = ""
	r.Host = ""
	r.URL.Scheme = wsSchemeFromHttpScheme(pc.conf.backend.Scheme)
	r.URL.Host = pc.conf.backend.Host

	// Copy all headers but delete the websocket's handshake related ones
	requestHeader := http.Header{}
	copyHeader(requestHeader, r.Header)
	requestHeader.Del("Sec-Websocket-Key")
	requestHeader.Del("Sec-Websocket-Version")
	requestHeader.Del("Upgrade")
	requestHeader.Del("Connection")

	wsUrl := url.URL{
		// nolint:lll
		// MEN-5273. There is an implementation detail of websocket library, where the method
		// websocket.Dialer.Dial (set by in our case by the client to use OpenSSL advance auth
		// features) is ignored for wss/https requests. Force here to use ws/http for our function
		// to be called. See
		// https://github.com/gorilla/websocket/blob/e8629af678b7fe13f35dff5e197de93b4148a909/client.go#L313
		Scheme: "ws",
		Host:   pc.conf.backend.Host,
		Path:   ApiUrlDevicesConnect,
	}

	connBackend, resp, err := pc.wsDialer.Dial(wsUrl.String(), requestHeader)
	if err != nil {
		log.Errorf("couldn't dial to remote backend url: %s", err)
		if resp != nil {
			// WebSocket handshake failed, reply the client with backend's resp
			if err := copyResponse(w, resp); err != nil {
				log.Errorf("couldn't write response after failed remote backend handshake: %s", err)
			}
		} else {
			http.Error(
				w,
				http.StatusText(http.StatusServiceUnavailable),
				http.StatusServiceUnavailable,
			)
		}
		return
	}
	defer connBackend.Close()

	// Only pass certain headers to the upgrader.
	upgradeHeader := http.Header{}
	if hdr := resp.Header.Get("Sec-Websocket-Protocol"); hdr != "" {
		upgradeHeader.Set("Sec-Websocket-Protocol", hdr)
	}
	if hdr := resp.Header.Get("Set-Cookie"); hdr != "" {
		upgradeHeader.Set("Set-Cookie", hdr)
	}

	// If backend replied with a protocol, use that one for our upgrader
	// It shall only be one after the handshake, but Upgrader.Subprotocols
	// expects an slice, so copy "all".
	upgrader := newUpgrader()
	if hdr := resp.Header.Get("Sec-Websocket-Protocol"); hdr != "" {
		backendProtocols := strings.Split(hdr, ",")
		for i := range backendProtocols {
			backendProtocols[i] = strings.TrimSpace(backendProtocols[i])
		}
		upgrader.Subprotocols = backendProtocols
	}

	// Now ready to upgrade connection from client to proxy!
	connClient, err := upgrader.Upgrade(w, r, upgradeHeader)
	if err != nil {
		log.Errorf("couldn't upgrade %s", err)
		return
	}
	defer connClient.Close()

	newConn := &wsConnection{
		connBackend: connBackend,
		connClient:  connClient,
	}
	pc.wsConnectionsMutex.Lock()
	pc.wsConnections[newConn] = true
	pc.wsConnectionsMutex.Unlock()

	errClient := make(chan error, 1)
	errBackend := make(chan error, 1)

	go pc.forwardWsConnection(
		newConn.connClient,
		newConn.connBackend,
		errClient,
		&newConn.connClientWriteMutex,
	)
	go pc.forwardWsConnection(
		newConn.connBackend,
		newConn.connClient,
		errBackend,
		&newConn.connBackendWriteMutex,
	)

	var messageF string
	select {
	case err = <-errClient:
		messageF = "error forwarding from backend to client: %v"
	case err = <-errBackend:
		messageF = "error forwarding from client to backend: %v"

	}
	if e, ok := err.(*websocket.CloseError); !ok || e.Code != websocket.CloseNormalClosure {
		log.Errorf(messageF, err.Error())
	}

	pc.wsConnectionsMutex.Lock()
	delete(pc.wsConnections, newConn)
	pc.wsConnectionsMutex.Unlock()
}

func (pc *proxyControllerInner) forwardWsConnection(
	dst, src *websocket.Conn,
	errChann chan error,
	mutex *sync.Mutex,
) {
	for {
		msgType, msg, err := src.ReadMessage()
		if err != nil {
			m := websocket.FormatCloseMessage(websocket.CloseNormalClosure, fmt.Sprintf("%v", err))
			if e, ok := err.(*websocket.CloseError); ok {
				if e.Code != websocket.CloseNoStatusReceived {
					m = websocket.FormatCloseMessage(e.Code, e.Text)
				}
			}
			mutex.Lock()
			errWrite := dst.WriteMessage(websocket.CloseMessage, m)
			mutex.Unlock()
			if errWrite != nil {
				log.Warningf("error while sending close message: %v", errWrite.Error())
			}
			errChann <- err
			break
		}
		mutex.Lock()
		err = dst.WriteMessage(msgType, msg)
		mutex.Unlock()
		if err != nil {
			errChann <- err
			break
		}
	}
}

func (pc *ProxyController) CloseWsConnections() {
	log.Info("shutting down websocket connections")
	m := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "shutting down proxy")

	pc.wsConnectionsMutex.Lock()
	defer pc.wsConnectionsMutex.Unlock()
	for c := range pc.wsConnections {
		c.connBackendWriteMutex.Lock()
		errWrite := c.connBackend.WriteMessage(websocket.CloseMessage, m)
		if errWrite != nil {
			log.Errorf("error while sending close message to backend: %v", errWrite.Error())
		}
		c.connBackendWriteMutex.Unlock()

		c.connClientWriteMutex.Lock()
		errWrite = c.connClient.WriteMessage(websocket.CloseMessage, m)
		if errWrite != nil {
			log.Errorf("error while sending close message to backend: %v", errWrite.Error())
		}
		c.connClientWriteMutex.Unlock()

		delete(pc.wsConnections, c)
	}
}
