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
	"encoding/base64"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"testing"

	"github.com/mendersoftware/openssl"
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
	}),
		localhostCert,
		localhostKey)
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

func TestClientAuthExpiredCert(t *testing.T) {
	ts := startTestHTTPS(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
		localhostCertExpired,
		localhostKeyExpired)
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

func TestClientAuthUnknownAuthorityCert(t *testing.T) {
	ts := startTestHTTPS(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
		localhostCertUnknown,
		localhostKeyUnknown)
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
	// see https://github.com/openssl/openssl/blob/OpenSSL_1_1_1-stable/crypto/x509/x509_vfy.c#L3268
	//     for self-signed openssl always returns self-signed error; either
	//     X509_V_ERR_DEPTH_ZERO_SELF_SIGNED_CERT or X509_V_ERR_SELF_SIGNED_CERT_IN_CHAIN
	//     and never X509_V_ERR_UNABLE_TO_GET_ISSUER_CERT or X509_V_ERR_UNABLE_TO_GET_ISSUER_CERT_LOCALLY
	assert.Nil(t, rsp)
}

//X509_V_ERR_DEPTH_ZERO_SELF_SIGNED_CERT
func TestClientAuthDepthZeroSelfSignedCert(t *testing.T) {
	if openssl.GetSecurityLevelGlobal() < 2 {
		t.Skip("skipping TestClientAuthEndEntityKeyTooSmall - security level < 2")
	}
	ts := startTestHTTPS(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
		localhostCert,
		localhostKey)
	defer ts.Close()

	ac, err := NewApiClient(
		Config{"server.zero.depth.self.signed.crt", true, false},
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
	assert.Contains(t, err.Error(), "depth zero self-signed certificate")
	assert.Nil(t, rsp)
}

//X509_V_ERR_EE_KEY_TOO_SMALL
func TestClientAuthEndEntityKeyTooSmall(t *testing.T) {
	if openssl.GetSecurityLevelGlobal() < 2 {
		t.Skip("skipping TestClientAuthEndEntityKeyTooSmall - security level < 2")
	}
	ts := startTestHTTPS(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
		localhostCertShortEEKey,
		localhostKeyShortEEKey)
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
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "end entity key too short")
	assert.Nil(t, rsp)
}

func TestClientAuthNoCert(t *testing.T) {
	ts := startTestHTTPS(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
		localhostCert,
		localhostKey)
	defer ts.Close()

	ac, err := NewApiClient(
		Config{"server.non-existing.crt", true, false},
	)
	assert.NotNil(t, ac)
	assert.NoError(t, err)
}

func TestClientAuthHostValidationNocheck(t *testing.T) {
	ts := startTestHTTPS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
		localhostCert,
		localhostKey)
	defer ts.Close()

	ac, err := NewApiClient(
		Config{"server.crt", true, true},
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
}

func TestClientAuthHostValidationError(t *testing.T) {
	ts := startTestHTTPS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
		localhostCertWrongHost,
		localhostKeyWrongHost)
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
	assert.Contains(t, err.Error(), "Host validation error")
	assert.Nil(t, rsp)
}

// this tests for the error that is handled by default 'not a valid certificate'
// via X509_V_ERR_CA_KEY_TOO_SMALL
func TestClientAuthNotValidCertificate(t *testing.T) {
	if openssl.GetSecurityLevelGlobal() < 2 {
		t.Skip("skipping TestClientAuthEndEntityKeyTooSmall - security level < 2")
	}
	ts := startTestHTTPS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
		localhostCertCAKeyTooShort,
		localhostKeyCAKeyTooShort)
	defer ts.Close()

	ac, err := NewApiClient(
		Config{"server.ca.key.too.small.crt", true, false},
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
	assert.Contains(t, err.Error(), "not a valid certificate")
	assert.Nil(t, rsp)
}
