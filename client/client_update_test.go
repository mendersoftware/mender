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

	"github.com/mendersoftware/mender/datastore"
	"github.com/stretchr/testify/assert"
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

func Test_GetScheduledUpdate_errorParsingResponse_UpdateFailing(t *testing.T) {
	// Test server that always responds with 200 code, and specific payload
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(204)
		w.Header().Set("Content-Type", "application/json")

		fmt.Fprint(w, "")
	}))
	defer ts.Close()

	ac, err := NewApiClient(
		Config{"server.crt", true, false},
	)
	assert.NotNil(t, ac)
	assert.NoError(t, err)

	client := NewUpdate()
	assert.NotNil(t, client)

	fakeProcessUpdate := func(response *http.Response) (interface{}, error) { return nil, errors.New("") }

	_, err = client.getUpdateInfo(ac, fakeProcessUpdate, ts.URL, CurrentUpdate{})
	assert.Error(t, err)
}

func Test_GetScheduledUpdate_responseMissingParameters_UpdateFailing(t *testing.T) {
	// Test server that always responds with 200 code, and specific payload
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Header().Set("Content-Type", "application/json")

		fmt.Fprint(w, "")
	}))
	defer ts.Close()

	ac, err := NewApiClient(
		Config{"server.crt", true, false},
	)
	assert.NotNil(t, ac)
	assert.NoError(t, err)

	client := NewUpdate()
	assert.NotNil(t, client)
	fakeProcessUpdate := func(response *http.Response) (interface{}, error) { return nil, nil }

	_, err = client.getUpdateInfo(ac, fakeProcessUpdate, ts.URL, CurrentUpdate{})
	assert.NoError(t, err)
}

func Test_GetScheduledUpdate_ParsingResponseOK_updateSuccess(t *testing.T) {
	// Test server that always responds with 200 code, and specific payload
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Header().Set("Content-Type", "application/json")

		fmt.Fprint(w, correctUpdateResponse)
	}))
	defer ts.Close()

	ac, err := NewApiClient(
		Config{"server.crt", true, false},
	)
	assert.NotNil(t, ac)
	assert.NoError(t, err)

	client := NewUpdate()
	assert.NotNil(t, client)

	data, err := client.GetScheduledUpdate(ac, ts.URL, CurrentUpdate{})
	assert.NoError(t, err)
	update, ok := data.(datastore.UpdateInfo)
	assert.True(t, ok)
	assert.Equal(t, "https://menderupdate.com", update.URI())
}

func Test_FetchUpdate_noContent_UpdateFailing(t *testing.T) {
	// Test server that always responds with 200 code, and specific payload
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Header().Set("Content-Type", "application/json")

		fmt.Fprint(w, "")
	}))
	defer ts.Close()

	ac, err := NewApiClient(
		Config{"server.crt", true, false},
	)
	assert.NotNil(t, ac)
	assert.NoError(t, err)

	client := NewUpdate()
	assert.NotNil(t, client)

	_, _, err = client.FetchUpdate(ac, ts.URL, 1*time.Minute)
	assert.Error(t, err)
}

func Test_FetchUpdate_invalidRequest_UpdateFailing(t *testing.T) {
	// Test server that always responds with 200 code, and specific payload
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Header().Set("Content-Type", "application/json")

		fmt.Fprint(w, "")
	}))
	defer ts.Close()

	ac, err := NewApiClient(
		Config{"server.crt", true, false},
	)
	assert.NotNil(t, ac)
	assert.NoError(t, err)

	client := NewUpdate()
	assert.NotNil(t, client)

	_, _, err = client.FetchUpdate(ac, "broken-request", 1*time.Minute)
	assert.Error(t, err)
}

func Test_FetchUpdate_correctContent_UpdateFetched(t *testing.T) {
	// Test server that always responds with 200 code, and specific payload
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Header().Set("Content-Type", "application/json")

		fmt.Fprint(w, "some content to be fetched")
	}))
	defer ts.Close()

	ac, err := NewApiClient(
		Config{"server.crt", true, false},
	)
	assert.NotNil(t, ac)
	assert.NoError(t, err)

	client := NewUpdate()
	assert.NotNil(t, client)
	client.minImageSize = 1

	_, _, err = client.FetchUpdate(ac, ts.URL, 1*time.Minute)
	assert.NoError(t, err)
}

func Test_UpdateApiClientError(t *testing.T) {
	client := NewUpdate()

	_, err := client.GetScheduledUpdate(NewMockApiClient(nil, errors.New("foo")),
		"http://foo.bar", CurrentUpdate{})
	assert.Error(t, err)

	_, _, err = client.FetchUpdate(NewMockApiClient(nil, errors.New("foo")),
		"http://foo.bar", 1*time.Minute)
	assert.Error(t, err)
}

func TestMakeUpdateCheckRequest(t *testing.T) {
	ent_req, req, err := makeUpdateCheckRequest("http://foo.bar", CurrentUpdate{})
	assert.NotNil(t, ent_req)
	assert.NotNil(t, req)
	assert.NoError(t, err)

	assert.Equal(t, "http://foo.bar/api/devices/v1/deployments/device/deployments/next",
		req.URL.String())
	t.Logf("%s\n", req.URL.String())

	ent_req, req, err = makeUpdateCheckRequest("http://foo.bar", CurrentUpdate{
		Artifact: "foo",
		Provides: map[string]interface{}{
			"artifact_name": "release-1",
		},
	})
	assert.NotNil(t, ent_req)
	assert.NotNil(t, req)
	assert.NoError(t, err)

	assert.Equal(t, "http://foo.bar/api/devices/v1/deployments/device/deployments/next?artifact_name=foo",
		req.URL.String())
	t.Logf("%s\n", req.URL.String())
	body, err := ioutil.ReadAll(ent_req.Body)
	assert.NoError(t, err)
	provides := make(map[string]interface{})
	err = json.Unmarshal(body, &provides)
	assert.NoError(t, err)
	assert.Equal(t, "release-1", provides["artifact_name"], string(body))

	ent_req, req, err = makeUpdateCheckRequest("http://foo.bar", CurrentUpdate{
		Artifact:   "foo",
		DeviceType: "hammer",
		Provides: map[string]interface{}{
			"artifact_name": "release-2",
			"device_type":   "qemu",
		},
	})
	assert.NotNil(t, ent_req)
	assert.NotNil(t, req)
	assert.NoError(t, err)

	assert.Equal(t, "http://foo.bar/api/devices/v1/deployments/device/deployments/next?artifact_name=foo&device_type=hammer",
		req.URL.String())
	t.Logf("%s\n", req.URL.String())
	body, err = ioutil.ReadAll(ent_req.Body)
	assert.NoError(t, err)
	provides = make(map[string]interface{})
	err = json.Unmarshal(body, &provides)
	assert.NoError(t, err)
	assert.Equal(t, "release-2", provides["artifact_name"], string(body))
	assert.Equal(t, "qemu", provides["device_type"], string(body))
}

func TestGetUpdateInfo(t *testing.T) {

	tests := map[string]struct {
		httpHandlerFunc   http.HandlerFunc
		currentUpdateInfo CurrentUpdate
		errorFunc         func(t assert.TestingT, err error, msgAndArgs ...interface{}) bool
	}{
		"Enterprise - Success - Update available": {
			httpHandlerFunc: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(200)
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, "")
			},
			currentUpdateInfo: CurrentUpdate{
				Provides: map[string]interface{}{
					"artifact_name": "release-1",
					"device_type":   "qemu"},
			},
			errorFunc: assert.NoError,
		},
		"Enterprise - Success 204 - No Content": {
			httpHandlerFunc: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(204)
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, "")
			},
			currentUpdateInfo: CurrentUpdate{
				Provides: map[string]interface{}{
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
			currentUpdateInfo: CurrentUpdate{
				Provides: map[string]interface{}{
					"artifact_name": "release-1",
					"device_type":   "qemu"},
			},
			errorFunc: assert.NoError,
		},
	}

	for name, test := range tests {

		// Test server that always responds with 200 code, and specific payload
		ts := httptest.NewTLSServer(http.HandlerFunc(test.httpHandlerFunc))
		defer ts.Close()

		ac, err := NewApiClient(
			Config{"server.crt", true, false},
		)
		assert.NotNil(t, ac)
		assert.NoError(t, err)

		client := NewUpdate()
		assert.NotNil(t, client)

		fakeProcessUpdate := func(response *http.Response) (interface{}, error) { return nil, nil }

		_, err = client.getUpdateInfo(ac, fakeProcessUpdate, ts.URL, test.currentUpdateInfo)
		test.errorFunc(t, err, "Test name: %s", name)

	}

}
