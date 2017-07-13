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
	"path"
	"testing"

	"github.com/mendersoftware/mender/client"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
)

func TestInventoryClient(t *testing.T) {
	responder := &struct {
		httpStatus int
		recdata    []byte
		path       string
	}{
		http.StatusOK,
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

	client := NewInventory()
	assert.NotNil(t, client)

	err = client.Submit(NewMockApiClient(nil, errors.New("foo")),
		ts.URL,
		InventoryData{
			{"foo", "bar"},
		})
	assert.Error(t, err)

	err = client.Submit(ac, ts.URL, InventoryData{
		{"foo", "bar"},
		{"bar", []string{"baz", "zen"}},
	})
	assert.NoError(t, err)
	assert.NotNil(t, responder.recdata)
	assert.JSONEq(t,
		`[{"name": "foo", "value": "bar"},{"name": "bar", "value": ["baz", "zen"]}]`,
		string(responder.recdata))
	assert.Equal(t, apiPrefix+"inventory/device/attributes", responder.path)

	responder.httpStatus = 401
	err = client.Submit(ac, ts.URL, nil)
	assert.Error(t, err)
}

func TestDBDiffInventory(t *testing.T) {

	db = NewDBStore(path.Join("/tmp/foobar-path", "db"))

	attrs := []client.InventoryAttribute{
		{Name: "device_type", Value: "foo-device"},
		{Name: "artifact_name", Value: "my-artifact"},
		{Name: "mender_client_version", Value: "1.0"},
	}
}
