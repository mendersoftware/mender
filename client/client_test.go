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
	"bytes"
	"crypto/x509"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func dummy_reauthfunc(str string) (AuthToken, error) {
	return AuthToken("dummy"), nil
}

func dummy_srvMngmntFunc(url string) func() *MenderServer {
	// mimic single server callback
	srv := MenderServer{ServerURL: url}
	called := false
	return func() *MenderServer {
		if called {
			called = false
			return nil
		} else {
			called = true
			return &srv
		}
	}
}

func TestHttpClient(t *testing.T) {
	cl, err := NewApiClient(
		Config{"server.crt", true, false},
	)
	assert.NotNil(t, cl)

	// no https config, we should obtain a httpClient
	cl, err = NewApiClient(Config{})
	assert.NotNil(t, cl)

	// missing cert in config should still yield usable client
	cl, err = NewApiClient(
		Config{"missing.crt", true, false},
	)
	assert.NotNil(t, cl)
	assert.NoError(t, err)
}

func TestApiClientRequest(t *testing.T) {
	cl, err := NewApiClient(
		Config{"server.crt", true, false},
	)
	assert.NotNil(t, cl)

	responder := &struct {
		httpStatus int
		headers    http.Header
	}{
		http.StatusOK,
		http.Header{},
	}

	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		responder.headers = r.Header
		w.WriteHeader(responder.httpStatus)
		w.Header().Set("Content-Type", "application/json")
	}))
	defer ts.Close()

	auth := false
	req := cl.Request("foobar", dummy_srvMngmntFunc(ts.URL),
		func(url string) (AuthToken, error) {
			if !auth {
				return AuthToken(""), errors.New("")
			} else {
				// reset httpstatus
				responder.httpStatus = http.StatusOK
				return AuthToken("dummy"), nil
			}
		}) /* cl.Request */
	assert.NotNil(t, req)

	hreq, _ := http.NewRequest(http.MethodGet, ts.URL, nil)

	// ApiRequest should append Authorization header
	rsp, err := req.Do(hreq)
	assert.Nil(t, err)
	assert.NotNil(t, rsp)
	assert.NotNil(t, responder.headers)
	assert.Equal(t, "Bearer foobar", responder.headers.Get("Authorization"))

	// but should not override if Authorization header is already set
	hreq.Header.Set("Authorization", "Bearer zed")
	rsp, err = req.Do(hreq)
	assert.Nil(t, err)
	assert.NotNil(t, rsp)
	assert.NotNil(t, responder.headers)
	assert.Equal(t, "Bearer zed", responder.headers.Get("Authorization"))

	// should attempt reauthorization and fail
	responder.httpStatus = http.StatusUnauthorized
	rsp, err = req.Do(hreq)
	assert.NoError(t, err)
	assert.NotNil(t, rsp)
	assert.Equal(t, rsp.StatusCode, http.StatusUnauthorized)

	// successful reauthorization
	auth = true
	rsp, err = req.Do(hreq)
	assert.NoError(t, err)
	assert.NotNil(t, rsp)
	assert.Equal(t, rsp.StatusCode, http.StatusOK)
}

func TestClientConnectionTimeout(t *testing.T) {

	prevReadingTimeout := defaultClientReadingTimeout
	defaultClientReadingTimeout = 10 * time.Millisecond

	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// sleep so that client request will timeout
		time.Sleep(defaultClientReadingTimeout + defaultClientReadingTimeout)
	}))

	defer func() {
		ts.Close()
		defaultClientReadingTimeout = prevReadingTimeout
	}()

	cl, err := NewApiClient(
		Config{"server.crt", true, false},
	)
	assert.NotNil(t, cl)
	assert.NoError(t, err)

	req := cl.Request("foobar", dummy_srvMngmntFunc(ts.URL), dummy_reauthfunc)
	assert.NotNil(t, req)

	hreq, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	assert.NoError(t, err)

	resp, err := req.Do(hreq)

	// test if we received timeout error
	e, ok := err.(net.Error)
	assert.True(t, ok)
	assert.NotNil(t, e.Timeout())
	assert.Nil(t, resp)

}

func TestHttpClientUrl(t *testing.T) {
	u := buildURL("https://foo.bar")
	assert.Equal(t, "https://foo.bar", u)

	u = buildURL("http://foo.bar")
	assert.Equal(t, "http://foo.bar", u)

	u = buildURL("foo.bar")
	assert.Equal(t, "https://foo.bar", u)

	u = buildApiURL("foo.bar", "/zed")
	assert.Equal(t, "https://foo.bar/api/devices/v1/zed", u)

	u = buildApiURL("foo.bar", "zed")
	assert.Equal(t, "https://foo.bar/api/devices/v1/zed", u)
}

// Test that our loaded certificates include the system CAs, and our own.
func TestCaLoading(t *testing.T) {
	conf := Config{
		ServerCert: "server.crt",
	}

	certs := loadServerTrust(&conf)

	// Verify that at least one of the certificates belong to us, and one
	// belongs to a well known certificate authority.
	var systemOK, oursOK bool
	subj := certs.Subjects()
	for i := 0; i < len(subj); i++ {
		if strings.Contains(string(subj[i]), "thawte Primary Root CA") {
			systemOK = true
		}
		// "Acme Co", just a dummy certificate in this repo.
		if strings.Contains(string(subj[i]), "Acme Co") {
			oursOK = true
		}
	}

	assert.True(t, systemOK)
	assert.True(t, oursOK)
}

type emptySystemCert struct{}

func (emptySystemCert) GetSystemCertPool() (*x509.CertPool, error) {
	return x509.NewCertPool(), nil
}

type nullSystemCert struct{}

func (nullSystemCert) GetSystemCertPool() (*x509.CertPool, error) {
	return nil, nil
}

type errorSystemCert struct{}

func (errorSystemCert) GetSystemCertPool() (*x509.CertPool, error) {
	return nil, errors.New("TEST: Cannot load system certificates")
}

func TestEmptySystemCertPool(t *testing.T) {
	version := runtime.Version()
	if strings.HasPrefix(version, "1.6") || strings.HasPrefix(version, "1.7") || strings.HasPrefix(version, "1.8") {
		// Environment variable not included until version 1.9. Therefore skipping this test.
		t.SkipNow()
	}

	conf := Config{}

	conf.ServerCert = "server.crt"
	certs := loadServerTrustImpl(&conf, emptySystemCert{})
	assert.Equal(t, 1, len(certs.Subjects()))

	conf.ServerCert = "does-not-exist.crt"
	certs = loadServerTrustImpl(&conf, emptySystemCert{})
	assert.Equal(t, 0, len(certs.Subjects()))

	conf.ServerCert = "server.crt"
	certs = loadServerTrustImpl(&conf, nullSystemCert{})
	assert.Equal(t, 1, len(certs.Subjects()))

	conf.ServerCert = "does-not-exist.crt"
	certs = loadServerTrustImpl(&conf, nullSystemCert{})
	assert.Equal(t, 0, len(certs.Subjects()))

	conf.ServerCert = "server.crt"
	certs = loadServerTrustImpl(&conf, errorSystemCert{})
	assert.Equal(t, 1, len(certs.Subjects()))

	conf.ServerCert = "does-not-exist.crt"
	certs = loadServerTrustImpl(&conf, errorSystemCert{})
	assert.Equal(t, 0, len(certs.Subjects()))
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

	intvl, err = GetExponentialBackoffTime(3, 1*time.Minute)
	assert.Error(t, err)

	intvl, err = GetExponentialBackoffTime(7, 1*time.Minute)
	assert.Error(t, err)

	// Test with two minute maximum interval.
	intvl, err = GetExponentialBackoffTime(5, 2*time.Minute)
	assert.NoError(t, err)
	assert.Equal(t, intvl, 2*time.Minute)

	intvl, err = GetExponentialBackoffTime(6, 2*time.Minute)
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

	intvl, err = GetExponentialBackoffTime(15, 10*time.Minute)
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

	intvl, err = GetExponentialBackoffTime(3, 1*time.Second)
	assert.Error(t, err)
}

func TestUnMarshalErrorMessage(t *testing.T) {
	errData := new(struct {
		Error string `json:"error"`
	})

	jsonErrMsg := `
  {
    "error" : "failed to decode device group data: JSON payload is empty"
  }
`
	require.Nil(t, json.Unmarshal([]byte(jsonErrMsg), errData))

	expected := "failed to decode device group data: JSON payload is empty"
	assert.Equal(t, expected, unmarshalErrorMessage(bytes.NewReader([]byte(jsonErrMsg))))
}

// Covers some special corner cases of the failover mechanism that is unique.
// In particular this test uses a list of two server where as one of them are
// fake so as to trigger a "failover" to the second server in the list.
// In addition it also covers the case with a 'nil' ServerManagementFunc.
func TestFailoverAPICall(t *testing.T) {
	cl, err := NewApiClient(
		Config{"server.crt", true, false},
	)
	assert.NotNil(t, cl)

	responder := &struct {
		httpStatus int
		headers    http.Header
	}{
		http.StatusOK,
		http.Header{},
	}

	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		responder.headers = r.Header
		w.WriteHeader(responder.httpStatus)
		w.Header().Set("Content-Type", "application/json")
	}))
	defer ts.Close()

	mulServerfunc := func() func() *MenderServer {
		// mimic multiple servers callback where the first one is a faker
		srvrs := []MenderServer{MenderServer{ServerURL: "fakeURL.404"},
			MenderServer{ServerURL: ts.URL}}
		idx := 0
		return func() *MenderServer {
			var ret *MenderServer
			if idx < len(srvrs) {
				ret = &srvrs[idx]
				idx++
			} else {
				ret = nil
			}
			return ret
		}
	}
	req := cl.Request("foobar", mulServerfunc(), dummy_reauthfunc) /* cl.Request */
	assert.NotNil(t, req)

	hreq, _ := http.NewRequest(http.MethodGet, ts.URL, nil)

	// ApiRequest should append Authorization header
	rsp, err := req.Do(hreq)
	assert.Nil(t, err)
	assert.NotNil(t, rsp)
	assert.NotNil(t, responder.headers)
	assert.Equal(t, "Bearer foobar", responder.headers.Get("Authorization"))

	req = cl.Request("foobar", nil, dummy_reauthfunc) /* cl.Request */
	assert.NotNil(t, req)

	rsp, err = req.Do(hreq)
	assert.Error(t, err)
}
