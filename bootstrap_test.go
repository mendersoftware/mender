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
  "testing"
  "net/http"
  "net/http/httptest"
  "crypto/tls"
  "fmt"
  "reflect"
  //"net/url"
  "crypto/x509"
)

// Response holds details of the message status
type Response struct {
	// the email address of the recipient
	Email string `json:"email"`
	// the sending status of the recipient - either "sent", "queued", "scheduled", "rejected", or "invalid"
	Status string `json:"status"`
	// the reason for the rejection if the recipient status is "rejected" - one of "hard-bounce", "soft-bounce", "spam", "unsub", "custom", "invalid-sender", "invalid", "test-mode-limit", or "rule"
	RejectionReason string `json:"reject_reason"`
	// the message's unique id
	Id string `json:"_id"`
}

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
		fmt.Fprintln(w, "Hello, client")
	}))
	defer ts.Close()

  trustedCerts := *x509.NewCertPool()
	CertPoolAppendCertsFromFile(&trustedCerts, "/tmp/blah_s.crt")

	if len(trustedCerts.Subjects()) == 0 {
		t.Errorf("No server certificate is trusted," +
			" use -trusted-certs with a proper certificate")
	}

	clientCert, err := tls.LoadX509KeyPair("/tmp/blah.crt", "/tmp/blah.key")
  if err != nil {
		t.Errorf("Failed to load certificate and key from files")
	}



  tlsConf := tls.Config{
		RootCAs:      &trustedCerts,
		Certificates: []tls.Certificate{clientCert},
		InsecureSkipVerify : true,
	}

  // Make a transport that reroutes all traffic to the example server
  transport := &http.Transport{

    TLSClientConfig: &tlsConf,
  }

  // Make a http.Client with the transport
  httpClient := &http.Client{Transport: transport}
  //httpClient := &http.Client{}
  client := &Client{ts.URL, httpClient}

  client.doBootstrap()

  client.doBootstrap()

  //correctResponse := &Response{
  //  Email: "bob@example.com",
  //  Status: "sent",
  //  RejectionReason: "hard-bounce",
  //  Id: "1",
  //}

  //expect(t, len(responses), 1)
  //expect(t, reflect.DeepEqual(correctResponse, responses[0]), true)
}
