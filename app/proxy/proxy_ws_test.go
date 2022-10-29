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
	"crypto/tls"
	"crypto/x509"
	"io/ioutil"
	"net/http"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mendersoftware/mender/client"
	cltest "github.com/mendersoftware/mender/client/test"
	"github.com/mendersoftware/mender/conf"
)

func prepareProxyWsTest(t *testing.T, srv *cltest.ClientTestWsServer) *ProxyController {

	wsDialer, err := client.NewWebsocketDialer(client.Config{})
	require.NoError(t, err)

	proxyController, err := NewProxyController(
		&http.Client{},
		wsDialer,
		srv.TestServer.URL,
		"SecretJwtToken",
	)
	require.NoError(t, err)

	return proxyController
}

func connectProxyWsTest(
	t *testing.T,
	srv *cltest.ClientTestWsServer,
	proxyController *ProxyController,
) *websocket.Conn {

	proxyServerUrl := proxyController.GetServerUrl()
	require.Contains(t, proxyServerUrl, "http://"+ProxyHost)

	testUrl := strings.Replace(proxyServerUrl, "http:", "ws:", 1) + ApiUrlDevicesConnect
	headers := http.Header{}
	headers.Add("Authorization", "Bearer SecretJwtToken")
	conn, resp, err := websocket.DefaultDialer.Dial(testUrl, headers)
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)

	return conn
}

func prepareAndConnectProxyWsTest(
	t *testing.T,
	srv *cltest.ClientTestWsServer,
) (*ProxyController, *websocket.Conn) {

	proxyController := prepareProxyWsTest(t, srv)
	conn := connectProxyWsTest(t, srv, proxyController)

	return proxyController, conn
}

func runTestSendReceiveWs(
	t *testing.T,
	srv *cltest.ClientTestWsServer,
	proxyController *ProxyController,
) {
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

	conn := connectProxyWsTest(t, srv, proxyController)

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
	assert.Eventually(
		t,
		func() bool { return websocket.CloseNormalClosure == srv.Connect.RecvClose },
		2*time.Millisecond,
		100*time.Microsecond,
	)
}

func TestProxyWsConnect(t *testing.T) {
	srv := cltest.NewClientTestWsServer()
	defer srv.StopWs()
	defer srv.Close()

	proxyController := prepareProxyWsTest(t, srv)
	defer proxyController.Stop()

	runTestSendReceiveWs(t, srv, proxyController)
}

func TestProxyWsWebSocketProtocolHeader(t *testing.T) {
	srv := cltest.NewClientTestWsServer()
	defer srv.StopWs()
	defer srv.Close()

	proxyController, err := NewProxyController(
		&http.Client{},
		nil,
		srv.TestServer.URL,
		"SecretJwtToken",
	)
	require.NoError(t, err)
	defer proxyController.Stop()

	proxyServerUrl := proxyController.GetServerUrl()
	require.Contains(t, proxyServerUrl, "http://"+ProxyHost)

	testUrl := strings.Replace(proxyServerUrl, "http:", "ws:", 1) + ApiUrlDevicesConnect
	headers := http.Header{}
	headers.Add("Authorization", "Bearer SecretJwtToken")

	// Say that we support two protocls
	dialer := websocket.Dialer{
		Subprotocols: []string{"protocol1, protocol2"},
	}

	// Test server expects header "Sec-WebSocket-Protocol: protocol1, protocol2"
	srv.TestServer.RequestHeader.Header = http.Header{}
	srv.TestServer.RequestHeader.Header.Add("Sec-WebSocket-Protocol", "protocol1, protocol2")

	// Connect!
	conn, resp, err := dialer.Dial(testUrl, headers)
	require.NoError(t, err)
	defer conn.Close()

	// Test server simulates supporting both, so protocol1 is chosen and passed by the proxy
	assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
	assert.Equal(t, "protocol1", resp.Header.Get("Sec-WebSocket-Protocol"))
}

func TestProxyWsTooMany(t *testing.T) {
	srv := cltest.NewClientTestWsServer()
	defer srv.StopWs()
	defer srv.Close()

	proxyController, conn := prepareAndConnectProxyWsTest(t, srv)
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

	proxyController, conn := prepareAndConnectProxyWsTest(t, srv)
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
	// Give the GC a bit of time to clean routines up from previous tests
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
	require.Contains(t, proxyServerUrl, "http://"+ProxyHost)

	// Assert num Goroutines increase
	// assert.Eventually creates one Goroutine on its own, so take that into account
	assert.Eventually(
		t,
		func() bool { return runtime.NumGoroutine() > goRoutinesStart+1 },
		100*time.Millisecond,
		1*time.Millisecond,
	)

	testUrl := strings.Replace(proxyServerUrl, "http:", "ws:", 1) + ApiUrlDevicesConnect
	headers := http.Header{}
	headers.Add("Authorization", "Bearer SecretJwtToken")
	conn, resp, err := websocket.DefaultDialer.Dial(testUrl, headers)
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
	defer conn.Close()

	mClose := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "done for the day")
	conn.WriteMessage(websocket.CloseMessage, mClose)

	proxyController.Stop()

	runtime.GC()
	// Assert num Goroutines evenutally equals start point
	// assert.Eventually creates one Goroutine on its own, so take that into account
	// time.Sleep(100 * time.Millisecond)
	assert.Eventually(
		t,
		func() bool {

			return runtime.NumGoroutine() == goRoutinesStart+1
		},
		100*time.Millisecond,
		1*time.Millisecond,
	)
}

func TestProxyWsConnectCustomCert(t *testing.T) {
	serverCert, err := tls.LoadX509KeyPair(
		"../../client/test/server.crt",
		"../../client/test/server.key",
	)
	require.NoError(t, err)

	tc := tls.Config{
		Certificates: []tls.Certificate{serverCert},
	}

	srv := cltest.NewClientTestWsServer(&tc)
	defer srv.StopWs()
	defer srv.Close()

	conffromfile := conf.MenderConfigFromFile{
		ServerCertificate: "../../client/test/server.crt",
	}
	testconf := &conf.MenderConfig{MenderConfigFromFile: conffromfile}
	httpConfig := testconf.GetHttpConfig()

	api, err := client.NewApiClient(httpConfig)
	require.NoError(t, err)

	wsDialer, err := client.NewWebsocketDialer(httpConfig)
	require.NoError(t, err)

	proxyController, err := NewProxyController(
		api,
		wsDialer,
		srv.TestServer.URL,
		"SecretJwtToken",
	)
	require.NoError(t, err)
	defer proxyController.Stop()

	runTestSendReceiveWs(t, srv, proxyController)
}
func TestProxyWsConnectMutualTLS(t *testing.T) {
	serverCert, err := tls.LoadX509KeyPair(
		"../../client/test/server.crt",
		"../../client/test/server.key",
	)
	require.NoError(t, err)

	clientClientCertPool := x509.NewCertPool()
	pb, err := ioutil.ReadFile("../../client/testdata/client.crt")
	require.NoError(t, err)
	clientClientCertPool.AppendCertsFromPEM(pb)

	tc := tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    clientClientCertPool,
	}

	srv := cltest.NewClientTestWsServer(&tc)
	defer srv.StopWs()
	defer srv.Close()

	conffromfile := conf.MenderConfigFromFile{
		ServerCertificate: "../../client/test/server.crt",
		HttpsClient: client.HttpsClient{
			Certificate: "../../client/testdata/client.crt",
			Key:         "../../client/testdata/client-cert.key",
		},
	}
	testconf := &conf.MenderConfig{MenderConfigFromFile: conffromfile}
	httpConfig := testconf.GetHttpConfig()

	api, err := client.NewApiClient(httpConfig)
	require.NoError(t, err)

	wsDialer, err := client.NewWebsocketDialer(httpConfig)
	require.NoError(t, err)

	proxyController, err := NewProxyController(
		api,
		wsDialer,
		srv.TestServer.URL,
		"SecretJwtToken",
	)
	require.NoError(t, err)
	defer proxyController.Stop()

	runTestSendReceiveWs(t, srv, proxyController)
}

func TestProxyWsConnectCustomCertWithReverseProxy(t *testing.T) {
	httpProxy, err := cltest.NewTestHttpProxy(1, false)
	require.NoError(t, err)
	defer func() {
		err := httpProxy.Stop()
		require.NoError(t, err)
	}()
	t.Setenv("HTTPS_PROXY", httpProxy.GetUrl())

	TestProxyWsConnectCustomCert(t)
}

func TestProxyWsConnectMutualTLSWithReverseProxy(t *testing.T) {
	httpProxy, err := cltest.NewTestHttpProxy(1, true)
	require.NoError(t, err)
	defer func() {
		err := httpProxy.Stop()
		require.NoError(t, err)
	}()
	t.Setenv("HTTPS_PROXY", httpProxy.GetUrl())

	TestProxyWsConnectMutualTLS(t)
}
