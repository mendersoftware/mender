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
package client

import (
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

type testAuthDataMessenger struct {
	reqData  []byte
	sigData  []byte
	code     AuthToken
	reqError error
	rspError error
	rspData  []byte
}

func (t *testAuthDataMessenger) MakeAuthRequest() (*AuthRequest, error) {
	return &AuthRequest{
		t.reqData,
		t.code,
		t.sigData,
	}, t.reqError
}

func (t *testAuthDataMessenger) RecvAuthResponse(data []byte) error {
	t.rspData = data
	return t.rspError
}

func TestClientAuthMakeReq(t *testing.T) {

	var req *http.Request
	var err error

	req, err = makeAuthRequest("foo", &testAuthDataMessenger{
		reqError: errors.New("req failed"),
	})
	assert.Nil(t, req)
	assert.Error(t, err)

	req, err = makeAuthRequest("mender.io", &testAuthDataMessenger{
		reqData: []byte("foobar data"),
		code:    "tenanttoken",
		sigData: []byte("foobar"),
	})
	assert.NotNil(t, req)
	assert.NoError(t, err)
	assert.Equal(t, http.MethodPost, req.Method)
	assert.Equal(t, "https://mender.io/api/devices/v1/authentication/auth_requests", req.URL.String())
	assert.Equal(t, "Bearer tenanttoken", req.Header.Get("Authorization"))
	expsignature := base64.StdEncoding.EncodeToString([]byte("foobar"))
	assert.Equal(t, expsignature, req.Header.Get("X-MEN-Signature"))
	assert.NotNil(t, req.Body)
	data, _ := ioutil.ReadAll(req.Body)
	t.Logf("data: %v", string(data))

	assert.Equal(t, []byte("foobar data"), data)
}

func TestClientAuth(t *testing.T) {
	responder := &struct {
		httpStatus int
		data       string
		headers    http.Header
	}{
		http.StatusOK,
		"foobar-token",
		http.Header{},
	}

	ts := startTestHTTPS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		responder.headers = r.Header
		w.WriteHeader(responder.httpStatus)
		w.Header().Set("Content-Type", "application/json")

		fmt.Fprint(w, responder.data)
	}))
	defer ts.Close()

	ac, err := NewApiClient(
		Config{"server.crt", true, false},
	)
	assert.NotNil(t, ac)
	assert.NoError(t, err)

	client := NewAuth()
	assert.NotNil(t, client)

	msger := &testAuthDataMessenger{
		reqData: []byte("foobar"),
	}
	rsp, err := client.Request(ac, ts.URL, msger)
	assert.NoError(t, err)
	assert.NotNil(t, rsp)
	assert.Equal(t, responder.data, string(rsp))
	assert.NotNil(t, responder.headers)
	assert.Equal(t, "application/json", responder.headers.Get("Content-Type"))

	responder.httpStatus = 401
	_, err = client.Request(ac, ts.URL, msger)
	assert.Error(t, err)
}

func startTestHTTPS(handler http.Handler) *httptest.Server {
	ts := httptest.NewUnstartedServer(handler)

	var localhostCert = []byte(`-----BEGIN CERTIFICATE-----
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
ZsAN9fpGcmjFiNkC
-----END CERTIFICATE-----`)

	var localhostKey = []byte(`-----BEGIN RSA PRIVATE KEY-----
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
-----END RSA PRIVATE KEY-----`)

	cert, _ := tls.X509KeyPair(localhostCert, localhostKey)
	existingConfig := ts.TLS
	if existingConfig != nil {
		ts.TLS = existingConfig.Clone()
	} else {
		ts.TLS = new(tls.Config)
	}
	if ts.TLS.NextProtos == nil {
		nextProtos := []string{"http/1.1"}
		if ts.EnableHTTP2 {
			nextProtos = []string{"h2"}
		}
		ts.TLS.NextProtos = nextProtos
	}

	ts.TLS.Certificates = []tls.Certificate{cert}
	ts.StartTLS()
	return ts
}

func TestClientAuthExpiredCert(t *testing.T) {
	ts := startTestHTTPS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer ts.Close()

	ac, err := NewApiClient(
		Config{"server.expired.crt", true, false},
	)
	assert.NotNil(t, ac)
	assert.NoError(t, err)

	client := NewAuth()
	assert.NotNil(t, client)

	msger := &testAuthDataMessenger{
		reqData: []byte("foobar"),
	}
	rsp, err := client.Request(ac, ts.URL, msger)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "certificate has expired")
	assert.Nil(t, rsp)
}

/*
#for i in *.crt; do echo; openssl verify -verbose $i; done

server.crt: O = Acme Co
error 18 at 0 depth lookup:self signed certificate
OK

server.expired.crt: O = Acme Co
error 18 at 0 depth lookup:self signed certificate
O = Acme Co
error 10 at 0 depth lookup:certificate has expired
OK

server.unknown-authority.crt: O = Acme Co
error 18 at 0 depth lookup:self signed certificate
O = Acme Co
error 10 at 0 depth lookup:certificate has expired
OK
*/
func TestClientAuthUnknownAuthorityCert(t *testing.T) {
	t.Skip() //see above
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	}))
	defer ts.Close()

	ac, err := NewApiClient(
		Config{"server.unknown-authority.crt", true, false},
	)
	assert.NotNil(t, ac)
	assert.NoError(t, err)

	client := NewAuth()
	assert.NotNil(t, client)

	msger := &testAuthDataMessenger{
		reqData: []byte("foobar"),
	}
	rsp, err := client.Request(ac, ts.URL, msger)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "certificate signed by unknown authority")
	assert.Nil(t, rsp)
}

func TestClientAuthNoCert(t *testing.T) {
	ts := startTestHTTPS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer ts.Close()

	ac, err := NewApiClient(
		Config{"server.non-existing.crt", true, false},
	)
	assert.NotNil(t, ac)
	assert.NoError(t, err)
}
