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

import (
	"net/http"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cltest "github.com/mendersoftware/mender/client/test"
)

func prepareProxyWsTest(
	t *testing.T,
	srv *cltest.ClientTestWsServer,
) (*ProxyController, *websocket.Conn) {
	proxyController, err := NewProxyController(
		&http.Client{},
		nil,
		srv.TestServer.URL,
		"SecretJwtToken",
	)
	require.NoError(t, err)

	proxyServerUrl := proxyController.GetServerUrl()
	require.Contains(t, proxyServerUrl, "http://localhost")

	testUrl := strings.Replace(proxyServerUrl, "http:", "ws:", 1) + ApiUrlDevicesConnect
	headers := http.Header{}
	headers.Add("Authorization", "Bearer SecretJwtToken")
	conn, resp, err := websocket.DefaultDialer.Dial(testUrl, headers)
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)

	return proxyController, conn
}

func TestProxyWsConnect(t *testing.T) {
	srv := cltest.NewClientTestWsServer()
	defer srv.StopWs()
	defer srv.Close()

	// Expectations for the test
	srv.Connect.SendMessages = append(
		srv.Connect.SendMessages,
		cltest.WsMessage{MsgType: websocket.TextMessage, Msg: []byte("one")},
	)
	srv.Connect.SendMessages = append(
		srv.Connect.SendMessages,
		cltest.WsMessage{MsgType: websocket.TextMessage, Msg: []byte("two")},
	)
	srv.Connect.SendMessages = append(
		srv.Connect.SendMessages,
		cltest.WsMessage{MsgType: websocket.TextMessage, Msg: []byte("three")},
	)
	srv.Connect.SendMessages = append(
		srv.Connect.SendMessages,
		cltest.WsMessage{MsgType: websocket.TextMessage, Msg: []byte("five")},
	)
	sendMessages := []cltest.WsMessage{
		{MsgType: websocket.TextMessage, Msg: []byte("first-message")},
		{MsgType: websocket.TextMessage, Msg: []byte("another-message")},
		{MsgType: websocket.TextMessage, Msg: []byte("hello-world")},
	}

	proxyController, conn := prepareProxyWsTest(t, srv)
	defer proxyController.Stop()
	defer conn.Close()

	wg := sync.WaitGroup{}

	// Send messages from client to backend
	wg.Add(1)
	go func() {
		defer wg.Done()
		for _, m := range sendMessages {
			err := conn.WriteMessage(m.MsgType, m.Msg)
			assert.NoError(t, err)
		}
	}()

	// Receive messages from backend to client
	var recvMessages []cltest.WsMessage
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			msgType, msg, err := conn.ReadMessage()
			assert.NoError(t, err)
			recvMessages = append(recvMessages, cltest.WsMessage{MsgType: msgType, Msg: msg})
			if len(recvMessages) == len(srv.Connect.SendMessages) {
				break
			}
		}
	}()

	// Check all messages received by the backend
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			time.Sleep(1 * time.Millisecond)
			if len(srv.Connect.RecvMessages) == len(sendMessages) {
				break
			}
		}
	}()

	done := make(chan struct{})
	go func() {
		defer close(done)
		wg.Wait()
	}()
	select {
	case <-done:
		break // ok
	case <-time.After(1 * time.Second):
		t.FailNow()
	}

	assert.True(t, reflect.DeepEqual(srv.Connect.SendMessages, recvMessages))
	assert.True(t, reflect.DeepEqual(sendMessages, srv.Connect.RecvMessages))

	mClose := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "done for the day")
	conn.WriteMessage(websocket.CloseMessage, mClose)
	time.Sleep(1 * time.Millisecond)
	assert.Equal(t, websocket.CloseNormalClosure, srv.Connect.RecvClose)
}

func TestProxyWsTooMany(t *testing.T) {
	srv := cltest.NewClientTestWsServer()
	defer srv.StopWs()
	defer srv.Close()

	proxyController, conn := prepareProxyWsTest(t, srv)
	defer proxyController.Stop()
	defer conn.Close()

	testUrl := strings.Replace(
		proxyController.GetServerUrl(),
		"http:",
		"ws:",
		1,
	) + ApiUrlDevicesConnect
	headers := http.Header{}
	headers.Add("Authorization", "Bearer SecretJwtToken")
	conn2, resp, err := websocket.DefaultDialer.Dial(testUrl, headers)
	assert.Nil(t, conn2)
	assert.Error(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
}

func TestProxyWsStop(t *testing.T) {
	srv := cltest.NewClientTestWsServer()
	defer srv.StopWs()
	defer srv.Close()

	proxyController, conn := prepareProxyWsTest(t, srv)
	defer proxyController.Stop()
	defer conn.Close()

	wg := sync.WaitGroup{}

	// Receive close message after reconfiguring
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			time.Sleep(1 * time.Millisecond)
			if srv.Connect.RecvClose == websocket.CloseNormalClosure {
				break
			}
		}
	}()

	done := make(chan struct{})
	go func() {
		defer close(done)
		wg.Wait()
	}()

	proxyController.Stop()

	select {
	case <-done:
		break // ok
	case <-time.After(1 * time.Second):
		t.FailNow()
	}
}

func TestProxyWsGoroutines(t *testing.T) {
	srv := cltest.NewClientTestWsServer()
	defer srv.StopWs()
	defer srv.Close()

	runtime.GC()
	time.Sleep(100 * time.Millisecond)
	goRoutinesStart := runtime.NumGoroutine()

	proxyController, err := NewProxyController(
		&http.Client{},
		nil,
		srv.TestServer.URL,
		"SecretJwtToken",
	)
	require.NoError(t, err)

	proxyServerUrl := proxyController.GetServerUrl()
	require.Contains(t, proxyServerUrl, "http://localhost")

	// a bit of time for the HTTP server to start
	time.Sleep(100 * time.Millisecond)
	goRoutinesProxyStart := runtime.NumGoroutine()
	assert.Greater(t, goRoutinesProxyStart, goRoutinesStart)

	testUrl := strings.Replace(proxyServerUrl, "http:", "ws:", 1) + ApiUrlDevicesConnect
	headers := http.Header{}
	headers.Add("Authorization", "Bearer SecretJwtToken")
	conn, resp, err := websocket.DefaultDialer.Dial(testUrl, headers)
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
	defer conn.Close()

	// bi-directional connections established
	goRoutinesConnOpen := runtime.NumGoroutine()
	assert.Greater(t, goRoutinesConnOpen, goRoutinesStart)

	mClose := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "done for the day")
	conn.WriteMessage(websocket.CloseMessage, mClose)
	time.Sleep(1 * time.Millisecond)

	// bi-directional connections closed
	runtime.GC()
	time.Sleep(100 * time.Millisecond)
	goRoutinesConnClose := runtime.NumGoroutine()
	assert.Equal(t, goRoutinesConnClose, goRoutinesProxyStart)

	proxyController.Stop()

	runtime.GC()
	time.Sleep(100 * time.Millisecond)
	goRoutinesStop := runtime.NumGoroutine()
	assert.Equal(t, goRoutinesStart, goRoutinesStop)

	log.Infof(
		"goRoutines start %d proxy %d open %d close %d stop %d",
		goRoutinesStart,
		goRoutinesProxyStart,
		goRoutinesConnOpen,
		goRoutinesConnClose,
		goRoutinesStop,
	)
}
