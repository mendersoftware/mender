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
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

const correctUpdateResponse = `{
	"id": "deplyoment-123",
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
	"id": "deplyoment-123",
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
	"id": "deplyoment-123",
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
	"id": "deplyoment-123",
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
	"id": "deplyoment-123",
	"artifact": {
		"source": {
			"uri": "https://menderupdate.com",
			"expire": "2016-03-11T13:03:17.063+0000"
		},
		"artifact_name": "myapp-release-z-build-123"
	}
}`

const missingNameUpdateResponse = `{
	"id": "deplyoment-123",
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

	for _, tt := range updateTest {

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
	update, ok := data.(UpdateResponse)
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

	_, _, err = client.FetchUpdate(ac, ts.URL)
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

	_, _, err = client.FetchUpdate(ac, "broken-request")
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

	_, _, err = client.FetchUpdate(ac, ts.URL)
	assert.NoError(t, err)
}

func Test_UpdateApiClientError(t *testing.T) {
	client := NewUpdate()

	_, err := client.GetScheduledUpdate(NewMockApiClient(nil, errors.New("foo")),
		"http://foo.bar", CurrentUpdate{})
	assert.Error(t, err)

	_, _, err = client.FetchUpdate(NewMockApiClient(nil, errors.New("foo")),
		"http://foo.bar")
	assert.Error(t, err)
}

func TestMakeUpdateCheckRequest(t *testing.T) {
	req, err := makeUpdateCheckRequest("http://foo.bar", CurrentUpdate{})
	assert.NotNil(t, req)
	assert.NoError(t, err)

	assert.Equal(t, "http://foo.bar/api/devices/v1/deployments/device/deployments/next",
		req.URL.String())
	t.Logf("%s\n", req.URL.String())

	req, err = makeUpdateCheckRequest("http://foo.bar", CurrentUpdate{
		Artifact: "foo",
	})
	assert.NotNil(t, req)
	assert.NoError(t, err)

	assert.Equal(t, "http://foo.bar/api/devices/v1/deployments/device/deployments/next?artifact_name=foo",
		req.URL.String())
	t.Logf("%s\n", req.URL.String())

	req, err = makeUpdateCheckRequest("http://foo.bar", CurrentUpdate{
		Artifact:   "foo",
		DeviceType: "hammer",
	})
	assert.NotNil(t, req)
	assert.NoError(t, err)

	assert.Equal(t, "http://foo.bar/api/devices/v1/deployments/device/deployments/next?artifact_name=foo&device_type=hammer",
		req.URL.String())
	t.Logf("%s\n", req.URL.String())
}
