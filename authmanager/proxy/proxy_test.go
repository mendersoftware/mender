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
package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mendersoftware/mender/client/api"
	cltest "github.com/mendersoftware/mender/client/api/test"
)

func TestProxyCommonRequests(t *testing.T) {
	srv := cltest.NewClientTestServer()
	defer srv.Close()

	srv.Update.Has = false

	proxyController, err := NewProxyController(&http.Client{}, nil, srv.Server.URL, "SecretJwtToken")
	require.NoError(t, err)

	proxyServerUrl := proxyController.GetServerUrl()
	assert.Contains(t, proxyServerUrl, "http://localhost")

	proxyController.Start()

	// API call /deployments/next
	testUrl := fmt.Sprintf("%s/api/devices/v1/deployments/device/deployments/next?artifact_name=something&device_type=else", proxyServerUrl)
	req, err := http.NewRequest("GET", testUrl, nil)
	require.NoError(t, err)
	req.Header.Add("Authorization", "Bearer SecretJwtToken")
	resp, err := http.DefaultClient.Do(req)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	// API call /device/attributes
	testUrl = fmt.Sprintf("%s/api/devices/v1/inventory/device/attributes", proxyServerUrl)
	inv, err := json.Marshal([]api.InventoryAttribute{
		{Name: "something", Value: "very-valuable"},
	})
	require.NoError(t, err)
	req, err = http.NewRequest("PUT", testUrl, bytes.NewBuffer(inv))
	require.NoError(t, err)
	req.Header.Add("Authorization", "Bearer SecretJwtToken")
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Any URL to /api/.../authentication shall return 403 from the proxy
	testUrl = fmt.Sprintf("%s/api/devices/v1/authentication/something-else", proxyServerUrl)
	resp, err = http.Get(testUrl)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)

	// Any URL out of /api/devices shall return 404 from the proxy
	testUrl = fmt.Sprintf("%s/api/management", proxyServerUrl)
	resp, err = http.Get(testUrl)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)

	proxyController.Stop()
}

func TestProxyHeadersForward(t *testing.T) {
	srv := cltest.NewClientTestServer()
	defer srv.Close()

	proxyController, err := NewProxyController(&http.Client{}, nil, srv.Server.URL, "Beaver")
	require.NoError(t, err)
	proxyServerUrl := proxyController.GetServerUrl()

	proxyController.Start()

	srv.RequestHeader.Header = http.Header{}
	srv.RequestHeader.Header.Add("Authorization", "Bearer Beaver")

	apiUrl := fmt.Sprintf("%s/api/devices/v1/deployments/device/deployments/next?artifact_name=name&device_type=type", proxyServerUrl)

	client := &http.Client{}
	req, err := http.NewRequest("GET", apiUrl, nil)
	require.NoError(t, err)
	req.Header.Add("Authorization", "Bearer Beaver")
	resp, err := client.Do(req)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	srv.Reset()
	srv.ResponseHeader.Header = http.Header{}
	srv.ResponseHeader.Header.Add("X-MEN", "something from the server")

	req, err = http.NewRequest("GET", apiUrl, nil)
	require.NoError(t, err)
	req.Header.Add("Authorization", "Bearer Beaver")
	resp, err = client.Do(req)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	assert.Equal(t, "something from the server", resp.Header.Get("X-MEN"))

	proxyController.Stop()
}

func TestProxyCheckAuthorization(t *testing.T) {
	srv := cltest.NewClientTestServer()
	defer srv.Close()

	srv.Update.Has = false

	// Start proxy with no JWT
	proxyController, err := NewProxyController(&http.Client{}, nil, srv.Server.URL, "")
	require.NoError(t, err)
	proxyServerUrl := proxyController.GetServerUrl()
	proxyController.Start()

	testUrl := fmt.Sprintf("%s/api/devices/v1/deployments/device/deployments/next?artifact_name=something&device_type=else", proxyServerUrl)
	req, err := http.NewRequest("GET", testUrl, nil)
	require.NoError(t, err)

	// Client not authorized, shall return 403
	req.Header.Set("Authorization", "Bearer Whatever")
	resp, err := http.DefaultClient.Do(req)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	// Client authorized, reconfigure proxy
	proxyController.Reconfigure(srv.Server.URL, "FreshToken")
	req.Header.Set("Authorization", "Bearer OldToken")
	resp, err = http.DefaultClient.Do(req)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	proxyController.Reconfigure(srv.Server.URL, "FreshToken")
	req.Header.Set("Authorization", "Something FreshToken")
	resp, err = http.DefaultClient.Do(req)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	req.Header.Set("Authorization", "Bearer FreshToken")
	resp, err = http.DefaultClient.Do(req)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	// Client lost authorization, reset proxy
	proxyController.Reconfigure(srv.Server.URL, "")
	req.Header.Set("Authorization", "Bearer FreshToken")
	resp, err = http.DefaultClient.Do(req)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	proxyController.Stop()
}
