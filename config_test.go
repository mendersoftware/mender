// Copyright 2018 Northern.tech AS
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
	"io/ioutil"
	"os"
	"path"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

var testConfig = `{
  "ClientProtocol": "https",
  "HttpsClient": {
    "Certificate": "/data/client.crt",
    "Key": "/data/client.key"
  },
  "RootfsPartA": "/dev/mmcblk0p2",
  "RootfsPartB": "/dev/mmcblk0p3",
  "UpdatePollIntervalSeconds": 10,
  "InventoryPollIntervalSeconds": 60,
  "ServerURL": "mender.io",
  "ServerCertificate": "/var/lib/mender/server.crt",
  "UpdateLogPath": "/var/lib/mender/log/deployment.log"
}`

var testBrokenConfig = `{
  "ClientProtocol": "https",
  "HttpsClient": {
    "Certificate": "/data/client.crt",
    "Key": "/data/client.key"
  },
  "RootfsPartA": "/dev/mmcblk0p2",
  "RootfsPartB": "/dev/mmcblk0p3",
  "PollIntervalSeconds": 60,
  "ServerURL": "mender
	"ServerCertificate": "/var/lib/mender/server.crt"
}`

func Test_readConfigFile_noFile_returnsError(t *testing.T) {
	err := readConfigFile(nil, "non-existing-file")
	assert.Error(t, err)
}

func Test_readConfigFile_brokenContent_returnsError(t *testing.T) {
	configFile, _ := os.Create("mender.config")
	defer os.Remove("mender.config")

	configFile.WriteString(testBrokenConfig)
	var confFromFile menderConfig

	err := readConfigFile(&confFromFile, "mender.config")
	assert.Error(t, err)

	assert.Equal(t, menderConfig{}, confFromFile)
}

func validateConfiguration(t *testing.T, actual *menderConfig) {
	expectedConfig := menderConfig{
		ClientProtocol: "https",
		HttpsClient: struct {
			Certificate string
			Key         string
			SkipVerify  bool
		}{
			Certificate: "/data/client.crt",
			Key:         "/data/client.key",
			SkipVerify:  false,
		},
		RootfsPartA:                  "/dev/mmcblk0p2",
		RootfsPartB:                  "/dev/mmcblk0p3",
		UpdatePollIntervalSeconds:    10,
		InventoryPollIntervalSeconds: 60,
		ServerURL:                    "mender.io",
		ServerCertificate:            "/var/lib/mender/server.crt",
		UpdateLogPath:                "/var/lib/mender/log/deployment.log",
	}
	if !assert.True(t, reflect.DeepEqual(actual, &expectedConfig)) {
		t.Logf("got:      %+v", actual)
		t.Logf("expected: %+v", expectedConfig)
	}
}

func Test_loadConfig_correctConfFile_returnsConfiguration(t *testing.T) {
	configFile, _ := os.Create("mender.config")
	defer os.Remove("mender.config")

	configFile.WriteString(testConfig)

	config, err := LoadConfig("mender.config")
	assert.NoError(t, err)
	assert.NotNil(t, config)

	validateConfiguration(t, config)
}

func TestServerURLConfig(t *testing.T) {
	configFile, _ := os.Create("mender.config")
	defer os.Remove("mender.config")

	configFile.WriteString(`{"ServerURL": "https://mender.io/"}`)

	config, err := LoadConfig("mender.config")
	assert.NoError(t, err)
	assert.Equal(t, "https://mender.io", config.ServerURL)
}

// TestMultipleServersConfig attempts to add multiple servers to config-
// file, as well as overriding the ServerURL / TenantToken from the first
// server.
func TestMultipleServersConfig(t *testing.T) {

	// create a temporary mender.conf file
	tdir, _ := ioutil.TempDir("", "mendertest")
	confPath := path.Join(tdir, "mender.conf")
	confFile, _ := os.Create(confPath)
	defer os.RemoveAll(tdir)

	confFile.WriteString(`{
    "Servers": [
        {
            "ServerURL": "https://server.one/",
            "TenantToken": "hostedTokenInc"
        },
        {
            "ServerURL": "https://server.two/",
            "TenantToken": ""
        },
        {
            "ServerURL": "https://server.three/"
        }
    ]
}`)
	// load config and assert expected values i.e. check that
	// first server is loaded as "active", and that all server
	// URL's trailing forward slash is trimmed off.
	conf, err := LoadConfig(confPath)
	assert.NoError(t, err)
	assert.Equal(t, "https://server.one", conf.ServerURL)
	assert.Equal(t, "hostedTokenInc", conf.TenantToken)

	assert.Equal(t, "https://server.one", conf.Servers[0].ServerURL)
	assert.Equal(t, "https://server.two", conf.Servers[1].ServerURL)
	assert.Equal(t, "https://server.three", conf.Servers[2].ServerURL)

	assert.Equal(t, "hostedTokenInc", conf.Servers[0].TenantToken)
	assert.Equal(t, "", conf.Servers[1].TenantToken)
	assert.Equal(t, "", conf.Servers[2].TenantToken)
}
