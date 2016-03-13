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
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

const correctUpdateResponse = `{
"image": {
"uri": "https://menderupdate.com",
"checksum": "checksum",
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
	{200, []byte(correctUpdateResponse), true, true, updateRespponseHaveUpdate},
	{204, []byte(""), true, true, updateResponseNoUpdates},
	{404, []byte(`{
	"error": "Not found"
	}`), true, true, updateResponseError},
	{500, []byte(`{
	"error": "Invalid request"
	}`), false, false, 0},
	{200, []byte(malformedUpdateResponse), false, false, 0},
	{200, []byte(brokenUpdateResponse), false, false, 0},
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

		var update UpdateResponse
		response := &http.Response{
			StatusCode: tt.responseStatusCode,
			Body:       &testReadCloser{strings.NewReader(string(tt.responseBody))},
		}

		err := ProcessUpdateResponse(response, &update)
		if tt.shoulReturnError && err != nil {
			t.Fatal("Update parsing should not return error but it does: ", err)
		} else if !tt.shoulReturnError && err == nil {
			t.Fatal("Update parsing should return an error but is not.")
		}
		if tt.shouldCheckReturnCode && tt.returnCode != response.StatusCode {
			t.Fatal("Expected ", tt.returnCode, " but got ", response.StatusCode)
		}
	}
}

func TestGetUpdate(t *testing.T) {
	// Test server that always responds with 200 code, and specific payload
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Header().Set("Content-Type", "application/json")

		fmt.Fprint(w, correctUpdateResponse)
	}))
	defer ts.Close()

	client, _ := NewClient(authCmdLineArgsType{ts.URL, "client.crt", "client.key", "server.crt"})

	if _, err := client.GetUpdate(ts.URL); err != nil {
		t.Fatal(err)
	}
}

func TestServerFile(t *testing.T) {
	if server := getMenderServer("non-existing-file.server"); !strings.Contains(server, defaultServerAddress) {
		t.Fatal("Expecting default mender server, received " + server)
	}

	// test if file is parsed correctly
	srvFile, err := os.Create("mender.server")
	if err != nil {
		t.Fail()
	}

	defer os.Remove(os.TempDir() + "mender.server")

	if _, err := srvFile.WriteString("testserver"); err != nil {
		t.Fail()
	}
	if server := getMenderServer("mender.server"); strings.Compare("http://testserver", server) != 0 {
		t.Fatal("Unexpected mender server name, received " + server)
	}

	if _, err := srvFile.WriteAt([]byte("https://testserver"), 0); err != nil {
		t.Fail()
	}
	if server := getMenderServer("mender.server"); strings.Compare("https://testserver", server) != 0 {
		t.Fatal("Unexpected mender server name, received " + server)
	}

}

func TestCheckPeriodicDaemonUpdate(t *testing.T) {
	reqHandlingCnt := 0
	pollInterval := time.Duration(100) * time.Millisecond

	// Test server that always responds with 200 code, and specific payload
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, correctUpdateResponse)
		reqHandlingCnt++
	}))
	defer ts.Close()

	client, _ := NewClient(authCmdLineArgsType{ts.URL, "client.crt", "client.key", "server.crt"})
	daemon := menderDaemon{
		client:      client,
		config:      daemonConfigType{serverpollInterval: pollInterval, server: ts.URL},
		stopChannel: make(chan bool),
	}

	go runAsDaemon(daemon)

	timespolled := 5
	time.Sleep(time.Duration(timespolled) * pollInterval)
	daemon.StopDaaemon()

	if reqHandlingCnt < (timespolled - 1) {
		t.Fatal("Expected to receive at least ", timespolled-1, " requests - ", reqHandlingCnt, " received")
	}
}
