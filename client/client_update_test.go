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
"image": {
"uri": "https://menderupdate.com",
"checksum": "checksum",
"yocto_id": "core-image-base",
"id": "f81d4fae-7dec-11d0-a765-00a0c91e6bf6"
},
"id": "13876-123132-321123"
}`

const malformedUpdateResponse = `{
"image": {
"non_existing": "https://menderupdate.com",
"checksum": "Hello, world!",
},
"bad_field": "13876-123132-321123"
}`

const brokenUpdateResponse = `{
"image": {
"uri": "https://menderupdate
"checksum": "Hello, world!",
"id": "f81d4fae-7dec-11d0-a765-00a0c91e6bf6"
},
"id": "13876-123132-321123"
}`

const missingFieldsUpdateResponse = `{
"image": {
"uri": "https://menderupdate.com",
"id": "f81d4fae-7dec-11d0-a765-00a0c91e6bf6"
},
"id": "13876-123132-321123"
}`

var updateTest = []struct {
	responseStatusCode    int
	responseBody          []byte
	shoulReturnError      bool
	shouldCheckReturnCode bool
	returnCode            int
}{
	{200, []byte(correctUpdateResponse), false, true, http.StatusOK},
	{204, []byte(""), false, true, http.StatusNoContent},
	{404, []byte(`{
	 "error": "Not found"
	 }`), true, true, http.StatusNotFound},
	{500, []byte(`{
	"error": "Invalid request"
	}`), true, false, 0},
	{200, []byte(malformedUpdateResponse), true, false, 0},
	{200, []byte(brokenUpdateResponse), true, false, 0},
	{200, []byte(missingFieldsUpdateResponse), true, false, 0},
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
		Config{"client.crt", "client.key", "server.crt", true, false},
	)
	assert.NotNil(t, ac)
	assert.NoError(t, err)

	client := NewUpdate()
	assert.NotNil(t, client)

	fakeProcessUpdate := func(response *http.Response) (interface{}, error) { return nil, errors.New("") }

	_, err = client.getUpdateInfo(ac, fakeProcessUpdate, ts.URL)
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
		Config{"client.crt", "client.key", "server.crt", true, false},
	)
	assert.NotNil(t, ac)
	assert.NoError(t, err)

	client := NewUpdate()
	assert.NotNil(t, client)
	fakeProcessUpdate := func(response *http.Response) (interface{}, error) { return nil, nil }

	_, err = client.getUpdateInfo(ac, fakeProcessUpdate, ts.URL)
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
		Config{"client.crt", "client.key", "server.crt", true, false},
	)
	assert.NotNil(t, ac)
	assert.NoError(t, err)

	client := NewUpdate()
	assert.NotNil(t, client)

	data, err := client.GetScheduledUpdate(ac, ts.URL)
	assert.NoError(t, err)
	update, ok := data.(UpdateResponse)
	assert.True(t, ok)
	assert.Equal(t, "https://menderupdate.com", update.Image.URI)
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
		Config{"client.crt", "client.key", "server.crt", true, false},
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
		Config{"client.crt", "client.key", "server.crt", true, false},
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
		Config{"client.crt", "client.key", "server.crt", true, false},
	)
	assert.NotNil(t, ac)
	assert.NoError(t, err)

	client := NewUpdate()
	assert.NotNil(t, client)
	client.minImageSize = 1

	_, _, err = client.FetchUpdate(ac, ts.URL)
	assert.NoError(t, err)
}
