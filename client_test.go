// Copyright 2016 Mender Software AS
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
package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHttpClient(t *testing.T) {
	cl, err := NewApiClient(
		httpsClientConfig{"client.crt", "client.key", "server.crt", true, false},
	)
	assert.NotNil(t, cl)

	// no https config, we should obtain a httpClient
	cl, err = NewApiClient(httpsClientConfig{})
	assert.NotNil(t, cl)

	// incomplete config should yield an error
	cl, err = NewApiClient(
		httpsClientConfig{"foobar", "client.key", "", true, false},
	)
	assert.Nil(t, cl)
	assert.NotNil(t, err)
}

func TestApiClientRequest(t *testing.T) {
	cl, err := NewApiClient(
		httpsClientConfig{"client.crt", "client.key", "server.crt", true, false},
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

func TestHttpClientUrl(t *testing.T) {
	u := buildURL("https://foo.bar")
	assert.Equal(t, "https://foo.bar", u)

	u = buildURL("http://foo.bar")
	assert.Equal(t, "http://foo.bar", u)

	u = buildURL("foo.bar")
	assert.Equal(t, "https://foo.bar", u)

	u = buildApiURL("foo.bar", "/zed")
	assert.Equal(t, "https://foo.bar/api/devices/0.1/zed", u)

	u = buildApiURL("foo.bar", "zed")
	assert.Equal(t, "https://foo.bar/api/devices/0.1/zed", u)
}
