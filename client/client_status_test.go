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
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStatusClient(t *testing.T) {
	responder := &struct {
		httpStatus int
		recdata    []byte
		path       string
	}{
		http.StatusNoContent, // 204
		[]byte{},
		"",
	}

	// Test server that always responds with 200 code, and specific payload
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(responder.httpStatus)

		responder.recdata, _ = ioutil.ReadAll(r.Body)
		responder.path = r.URL.Path
	}))
	defer ts.Close()

	ac, err := NewApiClient(
		Config{"client.crt", "client.key", "server.crt", true, false},
	)
	assert.NotNil(t, ac)
	assert.NoError(t, err)

	client := StatusClient{}
	assert.NotNil(t, client)

	err = client.Report(ac, ts.URL, StatusReport{
		DeploymentID: "deployment1",
		Status:       StatusFailure,
	})
	assert.NoError(t, err)
	assert.NotNil(t, responder.recdata)
	assert.JSONEq(t, `{"status": "failure"}`, string(responder.recdata))
	assert.Equal(t, apiPrefix+"deployments/device/deployments/deployment1/status", responder.path)

	responder.httpStatus = 401
	err = client.Report(ac, ts.URL, StatusReport{
		DeploymentID: "deployment1",
		Status:       StatusSuccess,
	})
	assert.Error(t, err)
}
