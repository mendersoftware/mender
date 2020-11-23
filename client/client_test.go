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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"path"
	"testing"
	"time"

	"github.com/mendersoftware/openssl"
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
	cl, _ := NewApiClient(
		Config{ServerCert: "testdata/server.crt", IsHttps: true},
	)
	assert.NotNil(t, cl)

	// no https config, we should obtain a httpClient
	cl, _ = NewApiClient(Config{})
	assert.NotNil(t, cl)

	// missing cert in config should still yield usable client
	cl, err := NewApiClient(
		Config{ServerCert: "testdata/missing.crt", IsHttps: true},
	)
	assert.NotNil(t, cl)
	assert.NoError(t, err)
}

func TestApiClientRequest(t *testing.T) {
	cl, _ := NewApiClient(
		Config{ServerCert: "testdata/server.crt", IsHttps: true},
	)
	assert.NotNil(t, cl)

	responder := &struct {
		httpStatus int
		headers    http.Header
	}{
		http.StatusOK,
		http.Header{},
	}

	ts := startTestHTTPS(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			responder.headers = r.Header
			w.WriteHeader(responder.httpStatus)
			w.Header().Set("Content-Type", "application/json")
		}),
		localhostCert,
		localhostKey)
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

	ts := startTestHTTPS(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// sleep so that client request will timeout
			time.Sleep(defaultClientReadingTimeout + defaultClientReadingTimeout)
		}),
		localhostCert,
		localhostKey)

	defer func() {
		ts.Close()
		defaultClientReadingTimeout = prevReadingTimeout
	}()

	cl, err := NewApiClient(
		Config{ServerCert: "testdata/server.crt", IsHttps: true},
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

func TestLoadingTrust(t *testing.T) {
	t.Run("Test loading server trust", func(t *testing.T) {
		ctx, err := openssl.NewCtx()
		assert.NoError(t, err)

		ctx, err = loadServerTrust(ctx, &Config{
			IsHttps:     true,
			ServerCert:  "missing.crt",
			HttpsClient: nil,
			NoVerify:    false,
		})
		assert.Error(t, err)

		ctx, err = loadServerTrust(ctx, &Config{
			IsHttps:     true,
			ServerCert:  "testdata/server.crt",
			HttpsClient: nil,
			NoVerify:    false,
		})
		assert.NoError(t, err)
	})
	t.Run("Test loading client trust", func(t *testing.T) {

		tests := map[string]struct {
			conf       Config
			assertFunc func(t assert.TestingT, err error, msgAndArgs ...interface{}) bool
		}{
			"No HttpsClient given": {
				conf: Config{
					HttpsClient: nil,
					NoVerify:    false,
				},
				assertFunc: func(t assert.TestingT, err error, msgAndArgs ...interface{}) bool {
					return assert.Error(t, err) &&
						assert.Contains(t, err.Error(), "Empty HttpsClient config given")
				},
			},
			"Missing certificate": {
				conf: Config{
					HttpsClient: &HttpsClient{
						Certificate: "missing.crt",
						Key:         "foobar",
					},
					NoVerify: false,
				},
				assertFunc: func(t assert.TestingT, err error, msgAndArgs ...interface{}) bool {
					return assert.Error(t, err) &&
						assert.Contains(t, err.Error(), "Failed to read the certificate")
				},
			},
			"No PEM certificate found in file": {
				conf: Config{
					HttpsClient: &HttpsClient{
						Certificate: "client.go",
						Key:         "foobar",
					},
					NoVerify: false,
				},
				assertFunc: func(t assert.TestingT, err error, msgAndArgs ...interface{}) bool {
					return assert.Error(t, err) &&
						assert.Contains(t, err.Error(), "No PEM certificate found in")
				},
			},
			"Certificate chain loading": {
				conf: Config{
					HttpsClient: &HttpsClient{
						Certificate: "testdata/chain-cert.crt",
						Key:         "testdata/client-cert.key",
					},
				},
				assertFunc: func(t assert.TestingT, err error, msgAndArgs ...interface{}) bool {
					return assert.NoError(t, err)
				},
			},
			"Missing Private key file": {
				conf: Config{
					HttpsClient: &HttpsClient{
						Certificate: "testdata/client.crt",
						Key:         "non-existing.key",
					},
					NoVerify: false,
				},
				assertFunc: func(t assert.TestingT, err error, msgAndArgs ...interface{}) bool {
					return assert.Error(t, err) &&
						assert.Contains(t, err.Error(), "Private key file from the ")
				},
			},
			"Correct certificate, wrong key": {
				conf: Config{
					HttpsClient: &HttpsClient{
						Certificate: "testdata/client.crt",
						Key:         "testdata/wrong.key",
					},
					NoVerify: false,
				},
				assertFunc: func(t assert.TestingT, err error, msgAndArgs ...interface{}) bool {
					return assert.Error(t, err) &&
						assert.Contains(t, err.Error(), "key values mismatch")
				},
			},
			"Correct certificate, correct key": {
				conf: Config{
					HttpsClient: &HttpsClient{
						Certificate: "testdata/client.crt",
						Key:         "testdata/client-cert.key",
					},
					NoVerify: false,
				},
				assertFunc: assert.NoError,
			},
		}

		for _, test := range tests {
			ctx, err := openssl.NewCtx()
			assert.NoError(t, err)

			ctx, err = loadClientTrust(ctx, &test.conf)
			test.assertFunc(t, err)
		}
	})
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
	cl, _ := NewApiClient(
		Config{ServerCert: "testdata/server.crt", IsHttps: true},
	)
	assert.NotNil(t, cl)

	type responderStruct struct {
		headers http.Header
		body    []byte
	}
	var responder405, responder401, responder401ThenOK [2]responderStruct

	counter := 0
	countingReauthfunc := func(str string) (AuthToken, error) {
		counter += 1
		return AuthToken(fmt.Sprintf("token%d", counter)), nil
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

	ts405CalledOnce := false
	ts405 := startTestHTTPS(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusMethodNotAllowed)

			if !ts405CalledOnce {
				responder405[0].headers = r.Header
				responder405[0].body = extractBody(r.Body)
				ts405CalledOnce = true
			} else {
				responder405[1].headers = r.Header
				responder405[1].body = extractBody(r.Body)
			}
		}),
		localhostCert,
		localhostKey)
	defer ts405.Close()

	ts401CalledOnce := false
	ts401 := startTestHTTPS(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)

			if !ts401CalledOnce {
				responder401[0].headers = r.Header
				responder401[0].body = extractBody(r.Body)
				ts401CalledOnce = true
			} else {
				responder401[1].headers = r.Header
				responder401[1].body = extractBody(r.Body)
			}
		}),
		localhostCert,
		localhostKey)
	defer ts401.Close()

	ts401ThenOKCalledOnce := false
	ts401ThenOK := startTestHTTPS(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !ts401ThenOKCalledOnce {
				w.WriteHeader(http.StatusUnauthorized)

				responder401ThenOK[0].headers = r.Header
				responder401ThenOK[0].body = extractBody(r.Body)

				ts401ThenOKCalledOnce = true
			} else {
				w.WriteHeader(http.StatusOK)
				w.Header().Set("Content-Type", "application/json")

				responder401ThenOK[1].headers = r.Header
				responder401ThenOK[1].body = extractBody(r.Body)
			}
		}),
		localhostCert,
		localhostKey)
	defer ts401ThenOK.Close()

	mulServerfunc := func() func() *MenderServer {
		// mimic multiple servers callback where they have different
		// errors:
		// 1. Cannot be found
		// 2. Returns 405 Method Not Allowed
		// 3. Returns 401 Unauthorized
		// 4. Returns 401 at first, then works
		srvrs := []MenderServer{MenderServer{ServerURL: "fakeURL.404"},
			MenderServer{ServerURL: ts405.URL},
			MenderServer{ServerURL: ts401.URL},
			MenderServer{ServerURL: ts401ThenOK.URL},
		}
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
	req := cl.Request("foobar", mulServerfunc(), countingReauthfunc) /* cl.Request */
	assert.NotNil(t, req)

	body := []byte(`{"foo":"bar"}`)
	bodyReader := bytes.NewBuffer(body)
	hreq, _ := http.NewRequest(http.MethodGet, ts405.URL, bodyReader)

	// ApiRequest should append Authorization header
	rsp, err := req.Do(hreq)
	assert.Nil(t, err)
	assert.NotNil(t, rsp)

	assert.NotNil(t, responder405[0].headers)
	assert.Equal(t, "Bearer foobar", responder405[0].headers.Get("Authorization"))
	assert.Equal(t, body, responder405[0].body)

	// Should never be called.
	assert.Nil(t, responder405[1].headers)

	assert.NotNil(t, responder401[0].headers)
	assert.Equal(t, "Bearer foobar", responder401[0].headers.Get("Authorization"))
	assert.Equal(t, body, responder401[0].body)

	assert.NotNil(t, responder401[1].headers)
	assert.Equal(t, "Bearer token1", responder401[1].headers.Get("Authorization"))
	assert.Equal(t, body, responder401[1].body)

	assert.NotNil(t, responder401ThenOK[0].headers)
	assert.Equal(t, "Bearer token1", responder401ThenOK[0].headers.Get("Authorization"))
	assert.Equal(t, body, responder401ThenOK[0].body)

	assert.NotNil(t, responder401ThenOK[1].headers)
	assert.Equal(t, "Bearer token2", responder401ThenOK[1].headers.Get("Authorization"))
	assert.Equal(t, body, responder401ThenOK[1].body)

	req = cl.Request("foobar", nil, countingReauthfunc) /* cl.Request */
	assert.NotNil(t, req)

	_, err = req.Do(hreq)
	assert.Error(t, err)
}

func TestListSystemCertsFound(t *testing.T) {
	// Setup tmpdir with two certificates and one private key
	tdir, err := ioutil.TempDir("", "TestListSystemCertsFound")
	require.NoError(t, err)
	wd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Symlink(path.Join(wd, "testdata/server.crt"), tdir+"/server.crt"))
	require.NoError(t, os.Symlink(path.Join(wd, "testdata/chain-cert.crt"), tdir+"/chain-cert.crt"))
	require.NoError(t, os.Symlink(path.Join(wd, "testdata/wrong.key"), tdir+"/wrong.key"))
	defer os.Remove(tdir)
	tests := map[string]struct {
		certDir              string
		assertFunc           func(t assert.TestingT, err error, msgAndArgs ...interface{}) bool
		certificatesExpected int
	}{
		"No such directory": {
			certDir:              "/i/do/not/exist",
			assertFunc:           assert.Error,
			certificatesExpected: 0,
		},
		"No system certificates found": {
			certDir:              "..", // There should be no certificates in the root of our repo
			assertFunc:           assert.NoError,
			certificatesExpected: 0,
		},
		"System certificates found": {
			certDir:              tdir,
			assertFunc:           assert.NoError,
			certificatesExpected: 2,
		},
	}

	for name, test := range tests {
		sysCerts, err := nrOfSystemCertsFound(test.certDir)
		test.assertFunc(t, err)
		assert.Equal(t, test.certificatesExpected, sysCerts, name)
	}
}
