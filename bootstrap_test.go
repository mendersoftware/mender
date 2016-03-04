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
	"testing"
)

func setupTestClient(server string) Client {
	authParams := authCmdLineArgsType{}
	authParams.setDefaultKeysAndCerts("client.crt", "client.key", "server.crt")

	_, authCreds := initClientAndServerAuthCreds(&authParams)

	return Client{server, initClient(authCreds)}
}

func TestBootstrapSuccess(t *testing.T) {

	// Test server that always responds with 200 code, and specific payload
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Header().Set("Content-Type", "application/json")
		//TODO
		fmt.Fprintln(w, "OK")
	}))
	defer ts.Close()

	client := setupTestClient(ts.URL)

	err := client.doBootstrap()

	if err != nil {
		t.Fatal(err)
	}
}

func TestBootstrapFailed(t *testing.T) {

	// Test server that always responds with 404 code, and specific payload
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		w.Header().Set("Content-Type", "application/json")
		//TODO
		fmt.Fprintln(w, "Error")
	}))
	defer ts.Close()

	client := setupTestClient(ts.URL)

	err := client.doBootstrap()

	if err == nil {
		t.Fatal(err)
	}
}
