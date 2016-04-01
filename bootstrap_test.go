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

func Test_bootstrap_invalidClientConfig_bootstrapFails(t *testing.T) {
	var err error
	if err = doBootstrap(httpsClientConfig{"non-existing", "non-existing", "", true}, "not_used"); err != ErrorBootstrapNoClient {
		t.Fatal(err)
	}
}

func TestBootstrapSuccess(t *testing.T) {
	// Test server that always responds with 200 code, and specific payload
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "application/json")
		//TODO
		fmt.Fprintln(w, "OK")
	}))
	defer ts.Close()

	if err := doMain([]string{"-bootstrap", ts.URL,
		"-cert-key", "client.key", "-certificate", "client.crt",
		"-trusted-certs", "server.crt"}); err != nil {
		t.Fatal("Can not override default auth credentials using command line swhitch: ", err)
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

	client := NewHttpsClient(
		httpsClientConfig{"client.crt", "client.key", "server.crt", true},
	)

	err := client.Bootstrap(ts.URL)

	// make sure we get specific error
	if err != ErrorBootstrapFailed {
		t.Fatal(err)
	}
}
