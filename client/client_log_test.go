// Copyright 2023 Northern.tech AS
//
//	Licensed under the Apache License, Version 2.0 (the "License");
//	you may not use this file except in compliance with the License.
//	You may obtain a copy of the License at
//
//	    http://www.apache.org/licenses/LICENSE-2.0
//
//	Unless required by applicable law or agreed to in writing, software
//	distributed under the License is distributed on an "AS IS" BASIS,
//	WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//	See the License for the specific language governing permissions and
//	limitations under the License.
package client

import (
	"io/ioutil"
	"net/http"
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
)

func TestLogUploadClient(t *testing.T) {
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
	ts := startTestHTTPS(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(responder.httpStatus)

			responder.recdata, _ = ioutil.ReadAll(r.Body)
			responder.path = r.URL.Path
		}),
		localhostCert,
		localhostKey)
	defer ts.Close()

	ac, err := NewApiClient(
		Config{ServerCert: "testdata/server.crt"},
	)
	assert.NotNil(t, ac)
	assert.NoError(t, err)

	client := NewLog()
	assert.NotNil(t, client)

	ld := LogData{
		DeploymentID: "deployment1",
		Messages: []byte(`{ "messages":
[{ "time": "12:12:12", "level": "error", "msg": "log foo" },
{ "time": "12:12:13", "level": "debug", "msg": "log bar" }]
}`),
	}
	err = client.Upload(NewMockApiClient(nil, errors.New("foo")), ts.URL, ld)
	assert.Error(t, err)

	err = client.Upload(ac, ts.URL, ld)
	assert.NoError(t, err)
	assert.NotNil(t, responder.recdata)
	assert.JSONEq(t, `{
	  "messages": [
	      {
	          "time": "12:12:12",
	          "level": "error",
	          "msg": "log foo"
	      },
	      {
	          "time": "12:12:13",
	          "level": "debug",
	          "msg": "log bar"
	      }
	   ]}`, string(responder.recdata))
	assert.Equal(t, apiPrefix+"v1/deployments/device/deployments/deployment1/log", responder.path)

	responder.httpStatus = 401
	err = client.Upload(ac, ts.URL, LogData{
		DeploymentID: "deployment1",
		Messages: []byte(`[{ "time": "12:12:12", "level": "error", "msg": "log foo" },
{ "time": "12:12:13", "level": "debug", "msg": "log bar" }]`),
	})
	assert.Error(t, err)
}
