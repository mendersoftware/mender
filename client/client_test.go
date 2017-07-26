// Copyright 2017 Northern.tech AS
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
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestHttpClient(t *testing.T) {
	cl, err := NewApiClient(
		Config{"server.crt", true, false},
	)
	assert.NotNil(t, cl)

	// no https config, we should obtain a httpClient
	cl, err = NewApiClient(Config{})
	assert.NotNil(t, cl)

	// missing cert in config should yield an error
	cl, err = NewApiClient(
		Config{"missing.crt", true, false},
	)
	assert.Nil(t, cl)
	assert.NotNil(t, err)
}

func TestApiClientRequest(t *testing.T) {
	cl, err := NewApiClient(
		Config{"server.crt", true, false},
	)
	assert.NotNil(t, cl)

	req := cl.Request("foobar")
	assert.NotNil(t, req)

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

	req := cl.Request("foobar")
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

	certs, err := loadServerTrust(conf)
	assert.NoError(t, err)

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
