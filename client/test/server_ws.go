// Copyright 2023 Northern.tech AS
//
//	Licensed under the Apache License, Version 2.0 (the "License");
//	you may not use this file except in compliance with the License.
//	You may obtain a copy of the License at
//
//	    http://www.apache.org/licenses/LICENSE-2.0
//
//	Unless required by applicable law or agreed to in writing, software
//	distributed under the License is distributed on an "AS IS" BASIS,
//	WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//	See the License for the specific language governing permissions and
//	limitations under the License.
package test

import (
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

type WsMessage struct {
	MsgType int
	Msg     []byte
}

type connectType struct {
	Called       bool
	SendMessages []WsMessage
	RecvMessages []WsMessage
	RecvClose    int
}

type ClientTestWsServer struct {
	TestServer *ClientTestServer
	Connect    connectType
	wsRunning  bool
}

func NewClientTestWsServer(options ...Options) *ClientTestWsServer {
	ctws := &ClientTestWsServer{}

	mux := http.NewServeMux()

	newOptions := make([]Options, 0, len(options)+1)
	newOptions = append(newOptions, mux)
	newOptions = append(newOptions, options...)
	ctws.TestServer = NewClientTestServer(newOptions...)

	mux.HandleFunc(
		"/api/devices/v1/deviceconnect/connect",
		ctws.TestServer.headersHook(ctws.connectReq),
	)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Infof("fallback request handler, request %v", r)
		w.WriteHeader(http.StatusBadRequest)
	})

	return ctws
}

func (ctws *ClientTestWsServer) Reset() {
	ctws.TestServer.Reset()
	ctws.StopWs()
	ctws.Connect.RecvMessages = []WsMessage{}
	ctws.Connect.SendMessages = []WsMessage{}
}

func (ctws *ClientTestWsServer) Close() {
	ctws.TestServer.Close()
}

func (ctws *ClientTestWsServer) StopWs() {
	ctws.wsRunning = false
}

func (ctws *ClientTestWsServer) connectReq(w http.ResponseWriter, r *http.Request) {
	log.Infof("test/server_ws: got connect request %v", r)
	ctws.Connect.Called = true

	reqProtocols := strings.Split(r.Header.Get("Sec-Websocket-Protocol"), ",")
	for i := range reqProtocols {
		reqProtocols[i] = strings.TrimSpace(reqProtocols[i])
	}
	upgrader := websocket.Upgrader{
		Subprotocols: reqProtocols,
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		err = errors.Wrap(err,
			"failed to upgrade the request to websocket protocol",
		)
		log.Error(err)
		return
	}
	defer conn.Close()

	errChan := make(chan error, 1)

	ctws.wsRunning = true
	go ctws.readWsMessages(conn, errChan)
	go ctws.writeWsMessages(conn, errChan)

	err = <-errChan
	if e, ok := err.(*websocket.CloseError); !ok || e.Code != websocket.CloseNormalClosure {
		log.Error(err)
	}
	ctws.wsRunning = false
}

func (ctws *ClientTestWsServer) readWsMessages(conn *websocket.Conn, errc chan error) {
	for ctws.wsRunning {
		msgType, msg, err := conn.ReadMessage()
		log.Infof("test/server_ws: received: %v [%d] %q", err, msgType, msg)
		if err != nil {
			if e, ok := err.(*websocket.CloseError); ok {
				ctws.Connect.RecvClose = e.Code
			}
			errc <- err
			break
		}
		ctws.Connect.RecvMessages = append(
			ctws.Connect.RecvMessages,
			WsMessage{MsgType: msgType, Msg: msg},
		)
	}
}

func (ctws *ClientTestWsServer) writeWsMessages(conn *websocket.Conn, errc chan error) {
	for _, m := range ctws.Connect.SendMessages {
		err := conn.WriteMessage(m.MsgType, m.Msg)
		if err != nil {
			errc <- err
			break
		}
	}
}
