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
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
	{200, []byte(correctUpdateResponse), false, true, updateResponseHaveUpdate},
	{204, []byte(""), false, true, updateResponseNoUpdates},
	{404, []byte(`{
	 "error": "Not found"
	 }`), true, true, updateResponseError},
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
		if tt.shoulReturnError && err == nil {
			t.Fatal("Update parsing should not return error but it does: ", err)
		} else if !tt.shoulReturnError && err != nil {
			t.Fatal("Update parsing should return an error but is not.")
		}
		if tt.shouldCheckReturnCode && tt.returnCode != response.StatusCode {
			t.Fatal("Expected ", tt.returnCode, " but got ", response.StatusCode)
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

	client := NewHttpsClient(
		httpsClientConfig{"client.crt", "client.key", "server.crt", true},
	)
	fakeProcessUpdate := func(response *http.Response) (interface{}, error) { return nil, errors.New("") }

	if _, err := client.GetScheduledUpdate(fakeProcessUpdate, ts.URL, ""); err == nil {
		t.Fatal(err)
	}
}

func Test_GetScheduledUpdate_responseMissingParameters_UpdateFailing(t *testing.T) {
	// Test server that always responds with 200 code, and specific payload
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Header().Set("Content-Type", "application/json")

		fmt.Fprint(w, "")
	}))
	defer ts.Close()

	client := NewHttpsClient(
		httpsClientConfig{"client.crt", "client.key", "server.crt", true},
	)
	fakeProcessUpdate := func(response *http.Response) (interface{}, error) { return nil, nil }

	if _, err := client.GetScheduledUpdate(fakeProcessUpdate, ts.URL, ""); err != nil {
		t.Fatal(err)
	}
}

func Test_GetScheduledUpdate_ParsingResponseOK_updateSuccess(t *testing.T) {
	// Test server that always responds with 200 code, and specific payload
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Header().Set("Content-Type", "application/json")

		fmt.Fprint(w, correctUpdateResponse)
	}))
	defer ts.Close()

	client := NewHttpsClient(
		httpsClientConfig{"client.crt", "client.key", "server.crt", true},
	)

	data, err := client.GetScheduledUpdate(processUpdateResponse, ts.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	update, ok := data.(UpdateResponse)
	if !ok {
		t.FailNow()
	}
	if update.Image.URI != "https://menderupdate.com" {
		t.FailNow()
	}
}

func Test_FetchUpdate_noContent_UpdateFailing(t *testing.T) {
	// Test server that always responds with 200 code, and specific payload
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Header().Set("Content-Type", "application/json")

		fmt.Fprint(w, "")
	}))
	defer ts.Close()

	client := NewHttpsClient(
		httpsClientConfig{"client.crt", "client.key", "server.crt", true},
	)
	if _, _, err := client.FetchUpdate(ts.URL); err == nil {
		t.Fatal(err)
	}
}

func Test_FetchUpdate_invalidRequest_UpdateFailing(t *testing.T) {
	// Test server that always responds with 200 code, and specific payload
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Header().Set("Content-Type", "application/json")

		fmt.Fprint(w, "")
	}))
	defer ts.Close()

	client := NewHttpsClient(
		httpsClientConfig{"client.crt", "client.key", "server.crt", true},
	)

	if _, _, err := client.FetchUpdate("broken-request"); err == nil {
		t.Fatal(err)
	}
}

func Test_FetchUpdate_correctContent_UpdateFetched(t *testing.T) {
	// Test server that always responds with 200 code, and specific payload
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Header().Set("Content-Type", "application/json")

		fmt.Fprint(w, "some content to be fetched")
	}))
	defer ts.Close()

	client := NewHttpsClient(
		httpsClientConfig{"", "", "server.crt", true},
	)
	client.minImageSize = 1

	if _, _, err := client.FetchUpdate(ts.URL); err != nil {
		t.Fatal(err)
	}
}
