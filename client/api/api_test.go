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
package api

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/mendersoftware/mender/authmanager"
	authconf "github.com/mendersoftware/mender/authmanager/conf"
	"github.com/mendersoftware/mender/authmanager/device"
	"github.com/mendersoftware/mender/authmanager/test"
	"github.com/mendersoftware/mender/client/conf"
	commonconf "github.com/mendersoftware/mender/common/conf"
	dbustest "github.com/mendersoftware/mender/common/dbus/test"
	"github.com/mendersoftware/mender/common/store"
	stest "github.com/mendersoftware/mender/common/system/testing"
	"github.com/mendersoftware/mender/common/tls"
	"github.com/mendersoftware/mender/common/tls/test_server"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testAuthTokenGetter struct {
	serverURL ServerURL
}

func (g *testAuthTokenGetter) GetAuthToken() (AuthToken, ServerURL, error) {
	return "dummy", g.serverURL, nil
}

func TestExponentialBackoffTimeCalculation(t *testing.T) {
	// Test with one minute maximum interval.
	intvl, err := GetExponentialBackoffTime(0, 1*time.Minute)
	assert.NoError(t, err)
	assert.Equal(t, intvl, 1*time.Minute)

	intvl, err = GetExponentialBackoffTime(1, 1*time.Minute)
	assert.NoError(t, err)
	assert.Equal(t, intvl, 1*time.Minute)

	intvl, err = GetExponentialBackoffTime(2, 1*time.Minute)
	assert.NoError(t, err)
	assert.Equal(t, intvl, 1*time.Minute)

	_, err = GetExponentialBackoffTime(3, 1*time.Minute)
	assert.Error(t, err)

	_, err = GetExponentialBackoffTime(7, 1*time.Minute)
	assert.Error(t, err)

	// Test with two minute maximum interval.
	intvl, err = GetExponentialBackoffTime(5, 2*time.Minute)
	assert.NoError(t, err)
	assert.Equal(t, intvl, 2*time.Minute)

	_, err = GetExponentialBackoffTime(6, 2*time.Minute)
	assert.Error(t, err)

	// Test with 10 minute maximum interval.
	intvl, err = GetExponentialBackoffTime(11, 10*time.Minute)
	assert.NoError(t, err)
	assert.Equal(t, intvl, 8*time.Minute)

	intvl, err = GetExponentialBackoffTime(12, 10*time.Minute)
	assert.NoError(t, err)
	assert.Equal(t, intvl, 10*time.Minute)

	intvl, err = GetExponentialBackoffTime(14, 10*time.Minute)
	assert.NoError(t, err)
	assert.Equal(t, intvl, 10*time.Minute)

	_, err = GetExponentialBackoffTime(15, 10*time.Minute)
	assert.Error(t, err)

	// Test with one second maximum interval.
	intvl, err = GetExponentialBackoffTime(0, 1*time.Second)
	assert.NoError(t, err)
	assert.Equal(t, intvl, 1*time.Minute)

	intvl, err = GetExponentialBackoffTime(1, 1*time.Second)
	assert.NoError(t, err)
	assert.Equal(t, intvl, 1*time.Minute)

	intvl, err = GetExponentialBackoffTime(2, 1*time.Second)
	assert.NoError(t, err)
	assert.Equal(t, intvl, 1*time.Minute)

	_, err = GetExponentialBackoffTime(3, 1*time.Second)
	assert.Error(t, err)
}

func min(a, b int) int {
	if a < b {
		return a
	} else {
		return b
	}
}

// Covers some special corner cases of the failover mechanism that is unique.
// In particular this test uses a list of two server where as one of them are
// fake so as to trigger a "failover" to the second server in the list.
func TestFailoverAPICall(t *testing.T) {
	dbusServer := dbustest.NewDBusTestServer()
	defer dbusServer.Close()
	dbusAPI := dbusServer.GetDBusAPI()

	client, err := NewApiClient(conf.DefaultAuthTimeout,
		tls.Config{ServerCert: "../../common/tls/testdata/server.crt"})
	require.NoError(t, err)
	client.DBusAPI = dbusAPI

	type responderStruct struct {
		headers http.Header
		body    []byte
	}
	const headers = 3
	var responder405, responder401, responder401ThenOK [headers]responderStruct

	counter := 0
	authFunc := func() []byte {
		counter += 1
		return []byte(fmt.Sprintf("token%d", counter))
	}

	extractBody := func(r io.Reader) []byte {
		ret := make([]byte, 100)
		n, err := r.Read(ret)
		sum := n
		for n > 0 {
			n, err = r.Read(ret[n:])
			sum += n
		}
		assert.True(t, err == nil || err == io.EOF)
		return ret[:sum]
	}

	ts405CallCount := 0
	ts405 := test_server.StartTestHTTPS(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusMethodNotAllowed)

			responder405[ts405CallCount].headers = r.Header
			responder405[ts405CallCount].body = extractBody(r.Body)

			ts405CallCount = min(ts405CallCount+1, headers-1)
		}),
		test_server.LocalhostCert,
		test_server.LocalhostKey)
	defer ts405.Close()

	ts401CallCount := 0
	ts401 := test_server.StartTestHTTPS(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)

			responder401[ts401CallCount].headers = r.Header
			responder401[ts401CallCount].body = extractBody(r.Body)

			ts401CallCount = min(ts401CallCount+1, headers-1)
		}),
		test_server.LocalhostCert,
		test_server.LocalhostKey)

	defer ts401.Close()

	ts401ThenOKCallCount := 0
	ts401ThenOK := test_server.StartTestHTTPS(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			responder401ThenOK[ts401ThenOKCallCount].headers = r.Header
			responder401ThenOK[ts401ThenOKCallCount].body = extractBody(r.Body)

			if ts401ThenOKCallCount == 0 {
				w.WriteHeader(http.StatusUnauthorized)
			} else {
				w.WriteHeader(http.StatusOK)
				w.Header().Set("Content-Type", "application/json")

				w.Write(authFunc())
			}

			ts401ThenOKCallCount = min(ts401ThenOKCallCount+1, headers-1)
		}),
		test_server.LocalhostCert,
		test_server.LocalhostKey)
	defer ts401ThenOK.Close()

	// Mock DBus interface io.mender.Proxy
	ctx, cancel := context.WithCancel(context.Background())
	go dbustest.RegisterAndServeIoMenderProxy(dbusServer, ctx, ts401ThenOK.URL)
	defer cancel()

	authManager, err := authmanager.NewAuthManager(authmanager.AuthManagerConfig{
		AuthConfig: &authconf.AuthConfig{
			// mimic multiple servers callback where they have different
			// errors:
			// 1. Cannot be found
			// 2. Returns 405 Method Not Allowed
			// 3. Returns 401 Unauthorized
			// 4. Returns 401 at first, then works
			Servers: []authconf.MenderServer{
				authconf.MenderServer{ServerURL: "fakeURL.404"},
				authconf.MenderServer{ServerURL: ts405.URL},
				authconf.MenderServer{ServerURL: ts401.URL},
				authconf.MenderServer{ServerURL: ts401ThenOK.URL},
			},
			Config: commonconf.Config{
				ServerCertificate: "../../common/tls/testdata/server.crt",
			},
		},
		AuthDataStore: store.NewMemStore(),
		KeyDirStore:   store.NewMemStore(),
		DBusAPI:       dbusAPI,
		IdentitySource: &device.IdentityDataRunner{
			Cmdr: stest.NewTestOSCalls("mac=foobar", 0),
		},
	})
	require.NoError(t, err)
	authManager.Start()
	defer authManager.Stop()

	body := []byte(`{"foo":"bar"}`)
	bodyReader := bytes.NewBuffer(body)
	hreq, _ := http.NewRequest(http.MethodGet, ts405.URL, bodyReader)

	// First attempt should result in Unauthorized
	rsp, err := client.Do(hreq)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Not authorized")

	// ApiRequest should append Authorization header
	rsp, err = client.Do(hreq)
	assert.Nil(t, err)
	assert.NotNil(t, rsp)

	// Two authorization attempts should have been made to this server.
	for i := 0; i < 2; i++ {
		assert.NotNil(t, responder405[i].headers)
		assert.Empty(t, responder405[i].headers.Get("Authorization"))
		assert.NotEmpty(t, responder405[i].headers.Get("X-MEN-Signature"))
		assert.Contains(t, string(responder405[i].body), `"id_data"`)
	}

	// Should never be called.
	assert.Nil(t, responder405[2].headers)

	// Two authorization attempts should have been made to this server as
	// well.
	for i := 0; i < 2; i++ {
		assert.NotNil(t, responder401[i].headers)
		assert.Empty(t, responder401[i].headers.Get("Authorization"))
		assert.NotEmpty(t, responder405[i].headers.Get("X-MEN-Signature"))
		assert.Contains(t, string(responder401[i].body), `"id_data"`)
	}

	// Should never be called.
	assert.Nil(t, responder401[2].headers)

	// Two authorization attempts should have been made to this server as
	// well, however last one succeeded
	for i := 0; i < 2; i++ {
		assert.NotNil(t, responder401ThenOK[i].headers)
		assert.Empty(t, responder401ThenOK[i].headers.Get("Authorization"))
		assert.NotEmpty(t, responder405[i].headers.Get("X-MEN-Signature"))
		assert.Contains(t, string(responder401ThenOK[i].body), `"id_data"`)
	}

	// Finally an authorized request should come through.
	assert.NotNil(t, responder401ThenOK[2].headers)
	assert.Equal(t, "Bearer token1", responder401ThenOK[2].headers.Get("Authorization"))
	assert.Equal(t, body, responder401ThenOK[2].body)
}

func TestApiClientRequest(t *testing.T) {
	dbusServer := dbustest.NewDBusTestServer()
	defer dbusServer.Close()
	dbusAPI := dbusServer.GetDBusAPI()

	ts := test.NewAuthTestServer(
		&test.CertAndKey{test_server.LocalhostCert, test_server.LocalhostKey})
	defer ts.Close()

	auth := false
	ts.Auth.AuthFunc = func() (bool, []byte) {
		if !auth {
			return false, []byte("dummy")
		} else {
			return true, []byte("dummy")
		}
	}

	authManager, err := authmanager.NewAuthManager(authmanager.AuthManagerConfig{
		AuthConfig: &authconf.AuthConfig{
			Servers: []authconf.MenderServer{
				authconf.MenderServer{ServerURL: ts.Server.URL},
			},
			Config: commonconf.Config{
				ServerCertificate: "../../common/tls/testdata/server.crt",
			},
		},
		AuthDataStore: store.NewMemStore(),
		KeyDirStore:   store.NewMemStore(),
		DBusAPI:       dbusAPI,
		IdentitySource: &device.IdentityDataRunner{
			Cmdr: stest.NewTestOSCalls("mac=foobar", 0),
		},
	})
	authManager.Start()
	defer authManager.Stop()

	// Mock DBus interface io.mender.Proxy
	ctx, cancel := context.WithCancel(context.Background())
	go dbustest.RegisterAndServeIoMenderProxy(dbusServer, ctx, ts.Server.URL)
	defer cancel()

	client, err := NewApiClient(conf.DefaultAuthTimeout,
		tls.Config{ServerCert: "../../common/tls/testdata/server.crt"})
	require.NoError(t, err)
	client.DBusAPI = dbusAPI

	hreq, _ := http.NewRequest(http.MethodGet, ts.Server.URL, nil)

	// should attempt reauthorization and fail
	rsp, err := client.Do(hreq)
	assert.Error(t, err)

	// successful reauthorization
	auth = true
	rsp, err = client.Do(hreq)
	assert.NoError(t, err)
	assert.NotNil(t, rsp)
	assert.Equal(t, rsp.StatusCode, http.StatusNotFound)
}

func TestClientConnectionTimeout(t *testing.T) {

	prevReadingTimeout := tls.DefaultClientReadingTimeout
	tls.DefaultClientReadingTimeout = 10 * time.Millisecond

	ts := test_server.StartTestHTTPS(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// sleep so that client request will timeout
			time.Sleep(tls.DefaultClientReadingTimeout * 2)
		}),
		test_server.LocalhostCert,
		test_server.LocalhostKey)

	defer func() {
		ts.Close()
		tls.DefaultClientReadingTimeout = prevReadingTimeout
	}()

	cl, err := NewApiClient(conf.DefaultAuthTimeout,
		tls.Config{ServerCert: "../../common/tls/testdata/server.crt"})
	cl.auth = "foo"
	cl.serverURL = ts.URL
	require.NoError(t, err)
	assert.NotNil(t, cl)

	hreq, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	assert.NoError(t, err)

	resp, err := cl.Do(hreq)

	// test if we received timeout error
	e, ok := err.(net.Error)
	assert.True(t, ok, "e is not net.Error, but %T", e)
	assert.NotNil(t, e.Timeout())
	assert.Nil(t, resp)

}
