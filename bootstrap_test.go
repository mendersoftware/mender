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
	"github.com/mendersoftware/log"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	//"net/url"
)

func expect(t *testing.T, a interface{}, b interface{}) {
	if a != b {
		t.Errorf("Expected %v (type %v) - Got %v (type %v)", b, reflect.TypeOf(b), a, reflect.TypeOf(a))
	}
}

func TestBootstrap(t *testing.T) {

	// Test server that always responds with 200 code, and specific payload
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Header().Set("Content-Type", "application/json")
		//TODO
		fmt.Fprintln(w, "OKI")
	}))
	defer ts.Close()

	authParams := authCmdLineArgsType{}
	authParams.setDefaultKeysAndCerts("client.crt", "client.key", "server.crt")

	_, authCreds := initClientAndServerAuthCreds(&authParams)

	client := &Client{ts.URL, initClient(authCreds)}

	_, response := client.doBootstrap()
	log.Error("Received data:", response.Status)

	//expect(t, len(response), 1)
	expect(t, reflect.DeepEqual(response.Status, "200 OK"), true)
}
