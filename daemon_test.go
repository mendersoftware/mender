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
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

const correctUpdateResponse = `{
"image": {
"uri": "https://aws.my_update_bucket.com/kldjdaklj",
"checksum": "checksum",
"id": "f81d4fae-7dec-11d0-a765-00a0c91e6bf6"
},
"id": "13876-123132-321123"
}`

const malformedUpdateResponse = `{
"image": {
"non_existing": "https://aws.my_update_bucket.com/kldjdaklj",
"checksum": "Hello, world!",
},
"bad_field": "13876-123132-321123"
}`

const brokenUpdateResponse = `{
"image": {
"uri": "https://aws.my_update_bu
"checksum": "Hello, world!",
"id": "f81d4fae-7dec-11d0-a765-00a0c91e6bf6"
},
"id": "13876-123132-321123"
}`

const missingFieldsUpdateResponse = `{
"image": {
"uri": "https://aws.my_update_bucket.com/kldjdaklj",
"id": "f81d4fae-7dec-11d0-a765-00a0c91e6bf6"
},
"id": "13876-123132-321123"
}`

type responseParser func(response http.Response, respBody []byte) error

type testUpdateRequester struct {
	request    string
	testClient client
}

func (tu testUpdateRequester) getClient() client {
	return tu.testClient
}

func (tu testUpdateRequester) formatRequest() clientRequestType {
	// use only GET for testing
	return clientRequestType{http.MethodGet, tu.request}
}

func (tu testUpdateRequester) actOnResponse(response http.Response, respBody []byte) error {
	return nil
}

func TestSendUpdateRequest(t *testing.T) {
	// Test server that always responds with 200 code, and specific payload
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, correctUpdateResponse)
	}))
	defer ts.Close()

	client, _ := initClient(authCmdLineArgsType{ts.URL, "client.crt", "client.key", "server.crt"})
	testUpdateRequester := testUpdateRequester{
		request:    ts.URL,
		testClient: client,
	}

	// make sure we are able to send request to server and parse response
	err := makeJobDone(testUpdateRequester)
	if err != nil {
		t.Fatal("Failed to send request to server")
	}
}

var updateTest = []struct {
	responseStatusCode    int
	responseBody          []byte
	shoulReturnError      bool
	shouldCheckReturnType bool
	returnType            dataActuator
}{
	{200, []byte(correctUpdateResponse), true, true, updateHaveUpdateResponseType{}},
	{204, []byte(""), true, true, updateNoUpdateResponseType{}},
	{404, []byte(`{
"error": "Not found"
}`), true, true, updateErrorResponseType{}},
	{500, []byte(`{
"error": "Invalid request"
}`), false, false, nil},
	{200, []byte(malformedUpdateResponse), false, false, nil},
	{200, []byte(brokenUpdateResponse), false, false, nil},
	{200, []byte(missingFieldsUpdateResponse), false, false, nil},
}

func TestParseUpdateResponse(t *testing.T) {
	updateRequester := updateRequester{
		reqType:              http.MethodGet,
		request:              "",
		menderClient:         client{},
		updateResponseParser: parseUpdateResponse,
	}

	for _, tt := range updateTest {
		uAPIResp, err := updateRequester.updateResponseParser(http.Response{StatusCode: tt.responseStatusCode}, []byte(tt.responseBody))
		if tt.shoulReturnError && err != nil {
			t.Fatal("Update parsing should not return error but it does: ", err)
		} else if !tt.shoulReturnError && err == nil {
			t.Fatal("Update parsing should return an error but is not.")
		}
		if tt.shouldCheckReturnType && reflect.TypeOf(uAPIResp) != reflect.TypeOf(tt.returnType) {
			t.Fatal("Update parse returned unexpected type: ", reflect.TypeOf(uAPIResp), " expecting: ", reflect.TypeOf(tt.returnType))
		}
	}
}

type fakeUpdateType int

func (updateAPIResp fakeUpdateType) actOnData() error {
	return nil
}

func fakeParseUpdateResponse(response http.Response, respBody []byte) (dataActuator, error) {
	var fakeData fakeUpdateType
	return fakeData, nil
}

func TestGetUpdate(t *testing.T) {
	// Test server that always responds with 200 code, and specific payload
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Header().Set("Content-Type", "application/json")
		//TODO
		fmt.Fprint(w, correctUpdateResponse)
	}))
	defer ts.Close()

	client, _ := initClient(authCmdLineArgsType{ts.URL, "client.crt", "client.key", "server.crt"})
	var config daemonConfigType
	config.deviceID = "fake_id"

	// test code with real update requester
	updateRequester := updateRequester{
		reqType:              http.MethodGet,
		request:              ts.URL + "/" + config.deviceID + "/update",
		menderClient:         client,
		updateResponseParser: fakeParseUpdateResponse,
	}

	if err := makeJobDone(updateRequester); err != nil {
		t.Fatal(err)
	}
}

func TestCheckPeriodicDaemonUpdate(t *testing.T) {

	reqHandlingCnt := 0
	pullInterval := time.Duration(100) * time.Millisecond

	// Test server that always responds with 200 code, and specific payload
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, correctUpdateResponse)
		reqHandlingCnt++
	}))
	defer ts.Close()

	client, _ := initClient(authCmdLineArgsType{ts.URL, "client.crt", "client.key", "server.crt"})
	var config daemonConfigType
	config.serverPullInterval = pullInterval
	config.server = ts.URL
	config.deviceID = "fake_id"

	go func() {
		runAsDaemon(config, client, fakeParseUpdateResponse)
	}()

	timesPulled := 5
	time.Sleep(time.Duration(timesPulled) * pullInterval)
	daemonQuit <- true

	if reqHandlingCnt < (timesPulled - 1) {
		t.Fatal("Expected to receive at least ", timesPulled-1, " requests - ", reqHandlingCnt, " received")
	}
}
