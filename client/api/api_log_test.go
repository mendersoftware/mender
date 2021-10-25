// Copyright 2021 Northern.tech AS
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
package api

import (
	"io/ioutil"
	"net/http"
	"testing"

	"github.com/mendersoftware/mender/client/conf"
	"github.com/mendersoftware/mender/common/api"
	"github.com/mendersoftware/mender/common/tls"
	"github.com/mendersoftware/mender/common/tls/test_server"

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
	ts := test_server.StartTestHTTPS(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(responder.httpStatus)

			responder.recdata, _ = ioutil.ReadAll(r.Body)
			responder.path = r.URL.Path
		}),
		test_server.LocalhostCert,
		test_server.LocalhostKey)
	defer ts.Close()

	ac, err := NewApiClient(conf.DefaultAuthTimeout,
		tls.Config{ServerCert: "../../common/tls/testdata/server.crt"},
	)
	assert.NotNil(t, ac)
	assert.NoError(t, err)
	ac.authTokenGetter = &testAuthTokenGetter{
		serverURL: ts.URL,
	}

	client := NewLog()
	assert.NotNil(t, client)

	ld := LogData{
		DeploymentID: "deployment1",
		Messages: []byte(`{ "messages":
[{ "time": "12:12:12", "level": "error", "msg": "log foo" },
{ "time": "12:12:13", "level": "debug", "msg": "log bar" }]
}`),
	}
	err = client.Upload(NewMockApiClient(nil, errors.New("foo")), ld)
	assert.Error(t, err)

	err = client.Upload(ac, ld)
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
	assert.Equal(t, api.ApiPrefix+"v1/deployments/device/deployments/deployment1/log", responder.path)

	responder.httpStatus = 401
	err = client.Upload(ac, LogData{
		DeploymentID: "deployment1",
		Messages: []byte(`[{ "time": "12:12:12", "level": "error", "msg": "log foo" },
{ "time": "12:12:13", "level": "debug", "msg": "log bar" }]`),
	})
	assert.Error(t, err)
}
