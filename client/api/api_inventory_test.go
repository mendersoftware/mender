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
	"github.com/stretchr/testify/require"
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
		tls.Config{ServerCert: "../../common/tls/testdata/server.crt"})
	require.NoError(t, err)
	assert.NotNil(t, ac)
	ac.authTokenGetter = &testAuthTokenGetter{
		serverURL: ts.URL,
	}

	client := NewInventory()
	assert.NotNil(t, client)

	err = client.Submit(NewMockApiClient(nil, errors.New("foo")),
		InventoryData{
			{"foo", "bar"},
		})
	assert.Error(t, err)

	err = client.Submit(ac, InventoryData{
		{"foo", "bar"},
		{"bar", []string{"baz", "zen"}},
	})
	assert.NoError(t, err)
	assert.NotNil(t, responder.recdata)
	assert.JSONEq(t,
		`[{"name": "foo", "value": "bar"},{"name": "bar", "value": ["baz", "zen"]}]`,
		string(responder.recdata))
	assert.Equal(t, api.ApiPrefix+"v1/inventory/device/attributes", responder.path)

	responder.httpStatus = 401
	err = client.Submit(ac, nil)
	assert.Error(t, err)
}

func TestInventoryFallbackToPatch(t *testing.T) {
	ts := test_server.StartTestHTTPS(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPatch {
				w.WriteHeader(http.StatusMethodNotAllowed)
			} else {
				w.WriteHeader(http.StatusOK)
			}
		}),
		test_server.LocalhostCert,
		test_server.LocalhostKey)
	defer ts.Close()

	ac, err := NewApiClient(conf.DefaultAuthTimeout,
		tls.Config{ServerCert: "../../common/tls/testdata/server.crt"})
	require.NoError(t, err)
	assert.NotNil(t, ac)
	ac.authTokenGetter = &testAuthTokenGetter{
		serverURL: ts.URL,
	}

	client := NewInventory()
	assert.NotNil(t, client)

	err = client.Submit(ac, InventoryData{
		{"foo", "bar"},
		{"bar", []string{"baz", "zen"}},
	})
	assert.NoError(t, err)
}
