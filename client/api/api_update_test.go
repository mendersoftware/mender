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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mendersoftware/mender/client/conf"
	"github.com/mendersoftware/mender/common/tls"
	"github.com/mendersoftware/mender/common/tls/test_server"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const correctUpdateResponse = `{
	"id": "deployment-123",
	"artifact": {
		"source": {
			"uri": "https://menderupdate.com",
			"expire": "2016-03-11T13:03:17.063+0000"
		},
		"device_types_compatible": ["BBB"],
		"artifact_name": "myapp-release-z-build-123"
	}
}`

const correctUpdateResponseMultipleDevices = `{
	"id": "deployment-123",
	"artifact": {
		"source": {
			"uri": "https://menderupdate.com",
			"expire": "2016-03-11T13:03:17.063+0000"
		},
		"device_types_compatible": [
			"BBB",
			"ELC AMX",
			"IS 3"
		],
		"artifact_name": "myapp-release-z-build-123"
	}
}`

const updateResponseEmptyDevices = `{
	"id": "deployment-123",
	"artifact": {
		"source": {
			"uri": "https://menderupdate.com",
			"expire": "2016-03-11T13:03:17.063+0000"
		},
		"device_types_compatible": [],
		"artifact_name": "myapp-release-z-build-123"
	}
}`

const malformedUpdateResponse = `{
	"id": "deployment-123",
	"bad_field": "13876-123132-321123",
	"artifact": {
		"source": {
			"uri": "https://menderupdate.com",
			"expire": "2016-03-11T13:03:17.063+0000"
		},
		"device_types_com,
		"artifact_name": "myapp-release-z-build-123"
	}
}`

const missingDevicesUpdateResponse = `{
	"id": "deployment-123",
	"artifact": {
		"source": {
			"uri": "https://menderupdate.com",
			"expire": "2016-03-11T13:03:17.063+0000"
		},
		"artifact_name": "myapp-release-z-build-123"
	}
}`

const missingNameUpdateResponse = `{
	"id": "deployment-123",
	"artifact": {
		"source": {
			"uri": "https://menderupdate.com",
			"expire": "2016-03-11T13:03:17.063+0000"
		},
		"device_types_compatible": [
			"BBB",
			"ELC AMX",
			"IS 3"
		],
	}
}`

type testReadCloser struct {
	body io.ReadSeeker
}

func (d *testReadCloser) Read(p []byte) (n int, err error) {
	n, err = d.body.Read(p)
	if err == io.EOF {
		d.body.Seek(0, 0)
	}
	return n, err
}

func (d *testReadCloser) Close() error {
	return nil
}

func TestParseUpdateResponse(t *testing.T) {

	var updateTest = []struct {
		responseStatusCode    int
		responseBody          []byte
		shoulReturnError      bool
		shouldCheckReturnCode bool
		returnCode            int
	}{
		{200, []byte(correctUpdateResponse), false, true, http.StatusOK},
		{200, []byte(correctUpdateResponseMultipleDevices), false, true, http.StatusOK},
		{200, []byte(updateResponseEmptyDevices), true, false, 0},
		{204, []byte(""), false, true, http.StatusNoContent},
		{404, []byte(`{
		 "error": "Not found"
		 }`), true, true, http.StatusNotFound},
		{500, []byte(`{
		"error": "Invalid request"
		}`), true, false, 0},
		{200, []byte(malformedUpdateResponse), true, false, 0},
		{200, []byte(missingDevicesUpdateResponse), true, false, 0},
		{200, []byte(missingNameUpdateResponse), true, false, 0},
	}

	for c, tt := range updateTest {
		caseName := strconv.Itoa(c)
		t.Run(caseName, func(t *testing.T) {
			t.Parallel()

			response := &http.Response{
				StatusCode: tt.responseStatusCode,
				Body:       &testReadCloser{strings.NewReader(string(tt.responseBody))},
			}

			_, err := processUpdateResponse(response)
			if tt.shoulReturnError {
				assert.Error(t, err)
			} else if !tt.shoulReturnError {
				assert.NoError(t, err)
			}
			if tt.shouldCheckReturnCode {
				assert.Equal(t, tt.returnCode, response.StatusCode)
			}
		})
	}
}

func TestParseUpdateResponseWithControlMap(t *testing.T) {

	// fake server first
	responder := &struct {
		data string
	}{
		"foobar-token",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// responder.headers = http.StatusOK
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "application/json")

		fmt.Fprint(w, responder.data)
	}))
	defer srv.Close()

	tests := map[string]struct {
		data     string
		expected assert.ErrorAssertionFunc
	}{
		"Correct update response - no update control map": {
			data: `{
	"id": "3380e4f2-c913-11eb-9119-c39aba66b261",
	"artifact": {
		"source": {
			"uri": "https://menderupdate.com",
			"expire": "2016-03-11T13:03:17.063+0000"
		},
		"device_types_compatible": ["BBB"],
		"artifact_name": "myapp-release-z-build-123"
	}
}`,
			expected: assert.NoError,
		},
		"Correct update control map": {
			data: `{
	"id": "3380e4f2-c913-11eb-9119-c39aba66b261",
	"artifact": {
		"source": {
			"uri": "https://menderupdate.com",
			"expire": "2016-03-11T13:03:17.063+0000"
		},
		"device_types_compatible": ["BBB"],
		"artifact_name": "myapp-release-z-build-123"
	},
        "update_control_map": {
            "ID": "3380e4f2-c913-11eb-9119-c39aba66b261",
            "Priority": 1,
            "States": {
                "ArtifactInstall_Enter": {
                    "Action": "pause"
                }
            }
        }
}`,
			expected: assert.NoError,
		},
		"Malformed update control map - Invalid Idle_Enter state": {
			data: `{
	"id": "68711312-c913-11eb-a0ab-1ba9e86afdfd",
	"artifact": {
		"source": {
			"uri": "https://menderupdate.com",
			"expire": "2016-03-11T13:03:17.063+0000"
		},
		"device_types_compatible": ["BBB"],
		"artifact_name": "myapp-release-z-build-123"
	},
        "update_control_map": {
            "ID": "68711312-c913-11eb-a0ab-1ba9e86afdfd",
            "Priority": 1,
            "States": {
                "Idle_Enter": {
                    "Foo": "bar"
                }
            }
        }
}`,
			expected: assert.Error,
		},
		"Malformed update control map - Wrong type": {
			data: `{
	"id": "68711312-c913-11eb-a0ab-1ba9e86afdfd",
	"artifact": {
		"source": {
			"uri": "https://menderupdate.com",
			"expire": "2016-03-11T13:03:17.063+0000"
		},
		"device_types_compatible": ["BBB"],
		"artifact_name": "myapp-release-z-build-123"
	},
        "update_control_map": 3
}`,
			expected: assert.Error,
		},
		"Malformed update control map - Wrong content": {
			data: `{
	"id": "68711312-c913-11eb-a0ab-1ba9e86afdfd",
	"artifact": {
		"source": {
			"uri": "https://menderupdate.com",
			"expire": "2016-03-11T13:03:17.063+0000"
		},
		"device_types_compatible": ["BBB"],
		"artifact_name": "myapp-release-z-build-123"
	},
        "update_control_map": {
	    "foo": "bar"
	}
}`,
			expected: assert.Error,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			response := &http.Response{
				StatusCode: http.StatusOK,
				Body:       &testReadCloser{strings.NewReader(string(test.data))},
			}

			_, err := processUpdateResponse(response)
			test.expected(t, err, name)
		})
	}
}

func Test_GetScheduledUpdate_errorParsingResponse_UpdateFailing(t *testing.T) {
	// Test server that always responds with 200 code, and specific payload
	ts := test_server.StartTestHTTPS(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(204)
			w.Header().Set("Content-Type", "application/json")

			fmt.Fprint(w, "")
		}),
		test_server.LocalhostCert,
		test_server.LocalhostKey)
	defer ts.Close()

	ac, err := NewApiClient(conf.DefaultAuthTimeout,
		tls.Config{ServerCert: "../../common/tls/testdata/server.crt"})
	require.NoError(t, err)
	assert.NotNil(t, ac)
	ac.authTokenGetter = &testAuthTokenGetter{
		serverURL: ts.URL,
	}

	client := NewUpdate()
	assert.NotNil(t, client)

	fakeProcessUpdate := func(response *http.Response) (interface{}, error) { return nil, errors.New("") }

	_, err = client.getUpdateInfo(ac, fakeProcessUpdate, &CurrentUpdate{})
	assert.Error(t, err)
}

func Test_GetScheduledUpdate_responseMissingParameters_UpdateFailing(t *testing.T) {
	// Test server that always responds with 200 code, and specific payload
	ts := test_server.StartTestHTTPS(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
			w.Header().Set("Content-Type", "application/json")

			fmt.Fprint(w, "")
		}),
		test_server.LocalhostCert,
		test_server.LocalhostKey)
	defer ts.Close()

	ac, err := NewApiClient(conf.DefaultAuthTimeout,
		tls.Config{ServerCert: "../../common/tls/testdata/server.crt"})
	require.NoError(t, err)
	assert.NotNil(t, ac)
	ac.authTokenGetter = &testAuthTokenGetter{
		serverURL: ts.URL,
	}

	client := NewUpdate()
	assert.NotNil(t, client)
	fakeProcessUpdate := func(response *http.Response) (interface{}, error) { return nil, nil }

	_, err = client.getUpdateInfo(ac, fakeProcessUpdate, &CurrentUpdate{})
	assert.NoError(t, err)
}

func Test_GetScheduledUpdate_ParsingResponseOK_updateSuccess(t *testing.T) {
	// Test server that always responds with 200 code, and specific payload
	ts := test_server.StartTestHTTPS(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
			w.Header().Set("Content-Type", "application/json")

			fmt.Fprint(w, correctUpdateResponse)
		}),
		test_server.LocalhostCert,
		test_server.LocalhostKey)
	defer ts.Close()

	ac, err := NewApiClient(conf.DefaultAuthTimeout,
		tls.Config{ServerCert: "../../common/tls/testdata/server.crt"})
	require.NoError(t, err)
	assert.NotNil(t, ac)
	ac.authTokenGetter = &testAuthTokenGetter{
		serverURL: ts.URL,
	}

	client := NewUpdate()
	assert.NotNil(t, client)

	data, err := client.GetScheduledUpdate(ac, &CurrentUpdate{})
	assert.NoError(t, err)
	update, ok := data.(UpdateResponse)
	assert.True(t, ok)
	assert.Equal(t, "https://menderupdate.com", update.UpdateInfo.URI())
}

func Test_FetchUpdate_noContent_UpdateFailing(t *testing.T) {
	// Test server that always responds with 200 code, and specific payload
	ts := test_server.StartTestHTTPS(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
			w.Header().Set("Content-Type", "application/json")

			fmt.Fprint(w, "")
		}),
		test_server.LocalhostCert,
		test_server.LocalhostKey)
	defer ts.Close()

	ac, err := NewApiClient(conf.DefaultAuthTimeout,
		tls.Config{ServerCert: "../../common/tls/testdata/server.crt"})
	require.NoError(t, err)
	assert.NotNil(t, ac)
	ac.authTokenGetter = &testAuthTokenGetter{
		serverURL: ts.URL,
	}

	client := NewUpdate()
	assert.NotNil(t, client)

	_, _, err = client.FetchUpdate(ac, ts.URL, 1*time.Minute)
	assert.Error(t, err)
}

func Test_FetchUpdate_invalidRequest_UpdateFailing(t *testing.T) {
	// Test server that always responds with 200 code, and specific payload
	ts := test_server.StartTestHTTPS(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
			w.Header().Set("Content-Type", "application/json")

			fmt.Fprint(w, "")
		}),
		test_server.LocalhostCert,
		test_server.LocalhostKey)
	defer ts.Close()

	ac, err := NewApiClient(conf.DefaultAuthTimeout,
		tls.Config{ServerCert: "../../common/tls/testdata/server.crt"})
	require.NoError(t, err)
	assert.NotNil(t, ac)
	ac.authTokenGetter = &testAuthTokenGetter{
		serverURL: ts.URL,
	}

	client := NewUpdate()
	assert.NotNil(t, client)

	_, _, err = client.FetchUpdate(ac, "broken-request", 1*time.Minute)
	assert.Error(t, err)
}

func Test_FetchUpdate_correctContent_UpdateFetched(t *testing.T) {
	// Test server that always responds with 200 code, and specific payload
	ts := test_server.StartTestHTTPS(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
			w.Header().Set("Content-Type", "application/json")

			fmt.Fprint(w, "some content to be fetched")
		}),
		test_server.LocalhostCert,
		test_server.LocalhostKey)
	defer ts.Close()

	ac, err := NewApiClient(conf.DefaultAuthTimeout,
		tls.Config{ServerCert: "../../common/tls/testdata/server.crt"})
	require.NoError(t, err)
	assert.NotNil(t, ac)
	ac.authTokenGetter = &testAuthTokenGetter{
		serverURL: ts.URL,
	}

	client := NewUpdate()
	assert.NotNil(t, client)
	client.minImageSize = 1

	_, _, err = client.FetchUpdate(ac, ts.URL, 1*time.Minute)
	assert.NoError(t, err)
}

func Test_UpdateApiClientError(t *testing.T) {
	client := NewUpdate()

	_, err := client.GetScheduledUpdate(NewMockApiClient(nil, errors.New("foo")),
		&CurrentUpdate{})
	assert.Error(t, err)

	_, _, err = client.FetchUpdate(NewMockApiClient(nil, errors.New("foo")),
		"http://foo.bar", 1*time.Minute)
	assert.Error(t, err)
}

func TestMakeUpdateCheckRequest(t *testing.T) {
	postV2 := 0
	postV1 := 1
	getV1 := 2

	reqs, err := makeUpdateCheckRequest(&CurrentUpdate{})
	require.Equal(t, 3, len(reqs))
	assert.NotNil(t, reqs[postV2])
	assert.NotNil(t, reqs[postV1])
	assert.NotNil(t, reqs[getV1])
	assert.NoError(t, err)

	assert.Equal(t, "/api/devices/v1/deployments/device/deployments/next",
		reqs[getV1].URL.Path)

	reqs, err = makeUpdateCheckRequest(&CurrentUpdate{
		Artifact: "release-1",
	})
	require.Equal(t, 3, len(reqs))
	assert.NotNil(t, reqs[postV2])
	assert.NotNil(t, reqs[postV1])
	assert.NotNil(t, reqs[getV1])
	assert.NoError(t, err)

	assert.Equal(t, "/api/devices/v1/deployments/device/deployments/next",
		reqs[getV1].URL.Path)
	assert.Equal(t, "artifact_name=release-1",
		reqs[getV1].URL.RawQuery)
	assert.Equal(t, "/api/devices/v1/deployments/device/deployments/next",
		reqs[postV1].URL.Path)
	assert.Empty(t, reqs[postV1].URL.RawQuery)
	assert.Equal(t, "/api/devices/v2/deployments/device/deployments/next",
		reqs[postV2].URL.Path)
	assert.Empty(t, reqs[postV2].URL.RawQuery)
	body, err := ioutil.ReadAll(reqs[postV2].Body)
	assert.NoError(t, err)
	params := make(map[string]interface{})
	err = json.Unmarshal(body, &params)
	assert.NoError(t, err)
	require.Contains(t, params, "device_provides")
	require.IsType(t, map[string]interface{}{}, params["device_provides"], string(body))
	require.Contains(t, params["device_provides"], "artifact_name", string(body))
	assert.Equal(t, "release-1", params["device_provides"].(map[string]interface{})["artifact_name"], string(body))
	assert.Equal(t, true, params["update_control_map"])
	body, err = ioutil.ReadAll(reqs[postV1].Body)
	assert.NoError(t, err)
	provides := make(map[string]interface{})
	err = json.Unmarshal(body, &provides)
	assert.NoError(t, err)
	assert.Equal(t, "release-1", provides["artifact_name"], string(body))

	reqs, err = makeUpdateCheckRequest(&CurrentUpdate{
		Artifact:   "foo",
		DeviceType: "hammer",
	})
	require.Equal(t, 3, len(reqs))
	assert.NotNil(t, reqs[postV2])
	assert.NotNil(t, reqs[postV1])
	assert.NotNil(t, reqs[getV1])
	assert.NoError(t, err)

	assert.Equal(t, "/api/devices/v1/deployments/device/deployments/next",
		reqs[getV1].URL.Path)
	assert.Equal(t, "artifact_name=foo&device_type=hammer",
		reqs[getV1].URL.RawQuery)
	assert.Equal(t, "/api/devices/v1/deployments/device/deployments/next",
		reqs[postV1].URL.Path)
	assert.Empty(t, reqs[postV1].URL.RawQuery)
	assert.Equal(t, "/api/devices/v2/deployments/device/deployments/next",
		reqs[postV2].URL.Path)
	assert.Empty(t, reqs[postV2].URL.RawQuery)
	body, err = ioutil.ReadAll(reqs[postV2].Body)
	assert.NoError(t, err)
	params = make(map[string]interface{})
	err = json.Unmarshal(body, &params)
	assert.NoError(t, err)
	require.Contains(t, params, "device_provides")
	require.IsType(t, map[string]interface{}{}, params["device_provides"], string(body))
	require.Contains(t, params["device_provides"], "artifact_name", string(body))
	assert.Equal(t, "foo", params["device_provides"].(map[string]interface{})["artifact_name"], string(body))
	assert.Equal(t, "hammer", params["device_provides"].(map[string]interface{})["device_type"], string(body))
	assert.Equal(t, true, params["update_control_map"])
	body, err = ioutil.ReadAll(reqs[postV1].Body)
	assert.NoError(t, err)
	provides = make(map[string]interface{})
	err = json.Unmarshal(body, &provides)
	assert.NoError(t, err)
	assert.Equal(t, "foo", provides["artifact_name"], string(body))
	assert.Equal(t, "hammer", provides["device_type"], string(body))
}

func TestGetUpdateInfo(t *testing.T) {

	tests := map[string]struct {
		httpHandlerFunc   http.HandlerFunc
		currentUpdateInfo *CurrentUpdate
		errorFunc         func(t assert.TestingT, err error, msgAndArgs ...interface{}) bool
	}{
		"Enterprise v2 - Success - Update available": {
			httpHandlerFunc: func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || !strings.Contains(r.URL.String(), "v2") {
					// Not really a valid server response,
					// but we are just using it to validate
					// the test case.
					w.WriteHeader(500)
				} else {
					w.WriteHeader(200)
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprint(w, "")
				}
			},
			currentUpdateInfo: &CurrentUpdate{
				Provides: map[string]string{
					"artifact_name": "release-1",
					"device_type":   "qemu"},
			},
			errorFunc: assert.NoError,
		},
		"Enterprise v2 - Success 204 - No Content": {
			httpHandlerFunc: func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || !strings.Contains(r.URL.String(), "v2") {
					// Not really a valid server response,
					// but we are just using it to validate
					// the test case.
					w.WriteHeader(500)
				} else {
					w.WriteHeader(204)
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprint(w, "")
				}
			},
			currentUpdateInfo: &CurrentUpdate{
				Provides: map[string]string{
					"artifact_name": "release-1",
					"device_type":   "qemu"},
			},
			errorFunc: assert.NoError,
		},
		"Enterprise v1 - Success - Update available": {
			httpHandlerFunc: func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "POST" && strings.Contains(r.URL.String(), "v2") {
					w.WriteHeader(404)
				} else if r.Method == "POST" {
					w.WriteHeader(200)
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprint(w, "")
				} else {
					// Shouldn't happen.
					w.WriteHeader(500)
				}
			},
			currentUpdateInfo: &CurrentUpdate{
				Provides: map[string]string{
					"artifact_name": "release-1",
					"device_type":   "qemu"},
			},
			errorFunc: assert.NoError,
		},
		"Enterprise v1 - Success 204 - No Content": {
			httpHandlerFunc: func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "POST" && strings.Contains(r.URL.String(), "v2") {
					w.WriteHeader(404)
				} else if r.Method == "POST" {
					w.WriteHeader(204)
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprint(w, "")
				} else {
					// Shouldn't happen.
					w.WriteHeader(500)
				}
			},
			currentUpdateInfo: &CurrentUpdate{
				Provides: map[string]string{
					"artifact_name": "release-1",
					"device_type":   "qemu"},
			},
			errorFunc: assert.NoError,
		},
		"Open source - Success": {
			httpHandlerFunc: func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "POST" {
					w.WriteHeader(404)
				} else {
					w.WriteHeader(200)
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprint(w, "")
				}
			},
			currentUpdateInfo: &CurrentUpdate{
				Provides: map[string]string{
					"artifact_name": "release-1",
					"device_type":   "qemu"},
			},
			errorFunc: assert.NoError,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			// Test server that always responds with 200 code, and specific payload
			ts := test_server.StartTestHTTPS(
				http.HandlerFunc(test.httpHandlerFunc),
				test_server.LocalhostCert,
				test_server.LocalhostKey)
			defer ts.Close()

			ac, err := NewApiClient(conf.DefaultAuthTimeout,
				tls.Config{ServerCert: "../../common/tls/testdata/server.crt"})
			require.NoError(t, err)
			assert.NotNil(t, ac)
			ac.authTokenGetter = &testAuthTokenGetter{
				serverURL: ts.URL,
			}

			client := NewUpdate()
			assert.NotNil(t, client)

			fakeProcessUpdate := func(response *http.Response) (interface{}, error) { return nil, nil }

			_, err = client.getUpdateInfo(ac, fakeProcessUpdate, test.currentUpdateInfo)
			test.errorFunc(t, err, "Test name: %s", name)
		})
	}
}
