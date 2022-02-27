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
package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/mendersoftware/openssl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func dummy_reauthfunc() (AuthToken, ServerURL, error) {
	return AuthToken("dummy"), ServerURL("https://example.com/"), nil
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
		Config{ServerCert: "testdata/server.crt"},
	)
	assert.NotNil(t, cl)

	// no https config, we should obtain a httpClient
	cl, _ = NewApiClient(Config{})
	assert.NotNil(t, cl)

	// missing cert in config should still yield usable client
	cl, err := NewApiClient(
		Config{ServerCert: "testdata/missing.crt"},
	)
	assert.NotNil(t, cl)
	assert.NoError(t, err)
}

func TestApiClientRequest(t *testing.T) {
	responder := &struct {
		httpStatus int
		headers    http.Header
	}{
		http.StatusOK,
		http.Header{},
	}

	checkAuthToken := "token1"
	ts := startTestHTTPS(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			responder.headers = r.Header
			if r.Header["Authorization"][0] == ("Bearer " + checkAuthToken) {
				w.WriteHeader(responder.httpStatus)
			} else {
				w.WriteHeader(http.StatusUnauthorized)
			}
			w.Header().Set("Content-Type", "application/json")
		}),
		localhostCert,
		localhostKey)
	defer ts.Close()

	const (
		testNoAuth = iota
		testBogusURL
		testAuthOK
		testNewToken
	)
	authCase := testNoAuth
	authCallCount := 0
	testServerError := errors.New("test server error")
	cl, _ := NewReauthorizingClient(
		Config{ServerCert: "testdata/server.crt"},
		func() (AuthToken, ServerURL, error) {
			authCallCount++
			switch authCase {
			case testNoAuth:
				return AuthToken(""), ServerURL(""), testServerError
			case testBogusURL:
				return AuthToken("token1"), ServerURL("bogusURL will not work"), nil
			case testAuthOK:
				return AuthToken("token1"), ServerURL(ts.URL), nil
			case testNewToken:
				return AuthToken("token2"), ServerURL(ts.URL), nil
			default:
				panic("Should not get here")
			}
		},
	)
	assert.NotNil(t, cl)

	hreq, _ := http.NewRequest(http.MethodGet, ts.URL, nil)

	// should attempt reauthorization and fail
	authCallCount = 0
	rsp, err := cl.Do(hreq)
	assert.ErrorIs(t, err, testServerError)
	assert.Equal(t, authCallCount, 1)

	authCase = testBogusURL

	// Will authorize correctly, but should fail to parse server URL.
	// Not a very realistic case, but then all bases are covered.
	authCallCount = 0
	rsp, err = cl.Do(hreq)
	assert.Error(t, err)
	assert.NotErrorIs(t, err, testServerError)
	assert.Contains(t, err.Error(), "unsupported protocol scheme")
	assert.Equal(t, authCallCount, 1)

	authCase = testAuthOK

	// Auth responds ok, but endpoints responds unauthorized.
	authCallCount = 0
	responder.httpStatus = http.StatusUnauthorized
	rsp, err = cl.Do(hreq)
	assert.NoError(t, err)
	assert.NotNil(t, rsp)
	assert.Equal(t, rsp.StatusCode, http.StatusUnauthorized)
	assert.Equal(t, authCallCount, 1)
	// Redoing should authorize again, because of Unauthorized status code.
	authCallCount = 0
	rsp, err = cl.Do(hreq)
	assert.NoError(t, err)
	assert.NotNil(t, rsp)
	assert.Equal(t, rsp.StatusCode, http.StatusUnauthorized)
	assert.Equal(t, authCallCount, 1)

	// Auth responds ok, but endpoints responds 404.
	cl.serverURL = "" // force reauth
	authCallCount = 0
	responder.httpStatus = http.StatusNotFound
	rsp, err = cl.Do(hreq)
	assert.NoError(t, err)
	assert.NotNil(t, rsp)
	assert.Equal(t, rsp.StatusCode, http.StatusNotFound)
	assert.Equal(t, authCallCount, 1)
	// Redoing should not authorize again, this status code is unrelated.
	authCallCount = 0
	rsp, err = cl.Do(hreq)
	assert.NoError(t, err)
	assert.NotNil(t, rsp)
	assert.Equal(t, rsp.StatusCode, http.StatusNotFound)
	assert.Equal(t, authCallCount, 0)

	// successful reauthorization
	cl.serverURL = "" // force reauth
	authCallCount = 0
	responder.httpStatus = http.StatusOK
	rsp, err = cl.Do(hreq)
	assert.NoError(t, err)
	assert.NotNil(t, rsp)
	assert.Equal(t, rsp.StatusCode, http.StatusOK)
	assert.Equal(t, authCallCount, 1)
	// Redoing should not authorize again.
	authCallCount = 0
	responder.httpStatus = http.StatusOK
	rsp, err = cl.Do(hreq)
	assert.NoError(t, err)
	assert.NotNil(t, rsp)
	assert.Equal(t, rsp.StatusCode, http.StatusOK)
	assert.Equal(t, authCallCount, 0)

	// Reject token, which should cause reauth, and should then return
	// success.
	authCase = testNewToken
	checkAuthToken = "token2"
	authCallCount = 0
	responder.httpStatus = http.StatusOK
	rsp, err = cl.Do(hreq)
	assert.NoError(t, err)
	assert.NotNil(t, rsp)
	assert.Equal(t, rsp.StatusCode, http.StatusOK)
	assert.Equal(t, authCallCount, 1)
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

	cl, err := NewReauthorizingClient(
		Config{ServerCert: "testdata/server.crt"},
		dummy_reauthfunc,
	)
	assert.NotNil(t, cl)
	assert.NoError(t, err)

	hreq, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	assert.NoError(t, err)

	resp, err := cl.Do(hreq)

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

	u = buildApiURL("foo.bar", "/v1/zed")
	assert.Equal(t, "https://foo.bar/api/devices/v1/zed", u)

	u = buildApiURL("foo.bar", "/v1/zed")
	assert.Equal(t, "https://foo.bar/api/devices/v1/zed", u)
}

func TestLoadingTrust(t *testing.T) {
	t.Run("Test loading server trust", func(t *testing.T) {
		ctx, err := openssl.NewCtx()
		assert.NoError(t, err)

		ctx, err = loadServerTrust(ctx, &Config{
			ServerCert:  "missing.crt",
			HttpsClient: nil,
			NoVerify:    false,
		})
		assert.Error(t, err)

		ctx, err = loadServerTrust(ctx, &Config{
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
	intvl, err := GetExponentialBackoffTime(0, 1*time.Minute, 0)
	assert.NoError(t, err)
	assert.Equal(t, intvl, 1*time.Minute)

	intvl, err = GetExponentialBackoffTime(1, 1*time.Minute, 0)
	assert.NoError(t, err)
	assert.Equal(t, intvl, 1*time.Minute)

	intvl, err = GetExponentialBackoffTime(2, 1*time.Minute, 0)
	assert.NoError(t, err)
	assert.Equal(t, intvl, 1*time.Minute)

	_, err = GetExponentialBackoffTime(3, 1*time.Minute, 0)
	assert.Error(t, err)

	_, err = GetExponentialBackoffTime(7, 1*time.Minute, 0)
	assert.Error(t, err)

	// Test with two minute maximum interval.
	intvl, err = GetExponentialBackoffTime(5, 2*time.Minute, 0)
	assert.NoError(t, err)
	assert.Equal(t, intvl, 2*time.Minute)

	_, err = GetExponentialBackoffTime(6, 2*time.Minute, 0)
	assert.Error(t, err)

	// Test with 10 minute maximum interval.
	intvl, err = GetExponentialBackoffTime(11, 10*time.Minute, 0)
	assert.NoError(t, err)
	assert.Equal(t, intvl, 8*time.Minute)

	intvl, err = GetExponentialBackoffTime(12, 10*time.Minute, 0)
	assert.NoError(t, err)
	assert.Equal(t, intvl, 10*time.Minute)

	intvl, err = GetExponentialBackoffTime(14, 10*time.Minute, 0)
	assert.NoError(t, err)
	assert.Equal(t, intvl, 10*time.Minute)

	_, err = GetExponentialBackoffTime(15, 10*time.Minute, 0)
	assert.Error(t, err)

	// Test with one second maximum interval.
	intvl, err = GetExponentialBackoffTime(0, 1*time.Second, 0)
	assert.NoError(t, err)
	assert.Equal(t, intvl, 1*time.Minute)

	intvl, err = GetExponentialBackoffTime(1, 1*time.Second, 0)
	assert.NoError(t, err)
	assert.Equal(t, intvl, 1*time.Minute)

	intvl, err = GetExponentialBackoffTime(2, 1*time.Second, 0)
	assert.NoError(t, err)
	assert.Equal(t, intvl, 1*time.Minute)

	_, err = GetExponentialBackoffTime(3, 1*time.Second, 0)
	assert.Error(t, err)

	maxAttempts := 8
	expectedIntervalMinutes := 1 * time.Minute
	for try := 0; try < maxAttempts; try++ {
		intvl, err := GetExponentialBackoffTime(try, 12*time.Minute, maxAttempts)
		assert.NoError(t, err)
		assert.Equal(t, expectedIntervalMinutes, intvl)
		if ((try + 1) % 3) == 0 {
			expectedIntervalMinutes *= 2
		}
	}
	intvl, err = GetExponentialBackoffTime(maxAttempts+1, 1*time.Minute, maxAttempts)
	assert.Error(t, err, MaxRetriesExceededError.Error())
	assert.Equal(t, time.Duration(0), intvl)

	maxAttempts = 5
	expectedIntervalMinutes = 1 * time.Minute
	for try := 0; try < maxAttempts; try++ {
		intvl, err := GetExponentialBackoffTime(try, 4*time.Minute, maxAttempts)
		assert.NoError(t, err)
		assert.Equal(t, expectedIntervalMinutes, intvl)
		if ((try + 1) % 3) == 0 {
			expectedIntervalMinutes *= 2
		}
	}
	intvl, err = GetExponentialBackoffTime(maxAttempts+1, 1*time.Minute, maxAttempts)
	assert.Error(t, err, MaxRetriesExceededError.Error())
	assert.Equal(t, time.Duration(0), intvl)
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
			certificatesExpected: 3,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			sysCerts, err := nrOfSystemCertsFound(test.certDir)
			test.assertFunc(t, err)
			assert.Equal(t, test.certificatesExpected, sysCerts, name)
		})
	}
}

func TestErrorUnmarshaling(t *testing.T) {

	tests := map[string]struct {
		input    string
		expected string
	}{
		"Regular JSON": {
			input: `{
    "error": "foobar"
}`,
			expected: "foobar",
		},
		"Simply an error string": {
			input:    "Error message from the server",
			expected: "Error message from the server",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			res := unmarshalErrorMessage(strings.NewReader(test.input))
			assert.Equal(t, test.expected, res)
		})
	}

}
