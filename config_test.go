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
	"os"
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
  "PollIntervalSeconds": 60,
  "ServerURL": "mender.io",
  "DeviceID": "1234-ABCD",
  "ServerCertificate": "/var/lib/mender/server.crt",
  "UpdateLogPath": "/var/lib/mender/log/deployment.log"
}`

var testConfigDevKey = `{
  "ClientProtocol": "https",
  "DeviceKey": "/foo/bar",
  "HttpsClient": {
    "Certificate": "/data/client.crt",
    "Key": "/data/client.key"
  },
  "RootfsPartA": "/dev/mmcblk0p2",
  "RootfsPartB": "/dev/mmcblk0p3",
  "PollIntervalSeconds": 60,
  "ServerURL": "mender.io",
  "DeviceID": "1234-ABCD",
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
  "DeviceID": "1234-ABCD",
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
		DeviceID:       "1234-ABCD",
		DeviceKey:      defaultKeyFile,
		HttpsClient: struct {
			Certificate string
			Key         string
		}{
			Certificate: "/data/client.crt",
			Key:         "/data/client.key",
		},
		RootfsPartA:         "/dev/mmcblk0p2",
		RootfsPartB:         "/dev/mmcblk0p3",
		PollIntervalSeconds: 60,
		ServerURL:           "mender.io",
		ServerCertificate:   "/var/lib/mender/server.crt",
		UpdateLogPath:       "/var/lib/mender/log/deployment.log",
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

func Test_loadConfig_correctConfFile_returnsConfigurationDeviceKey(t *testing.T) {
	configFile, _ := os.Create("mender.config")
	defer os.Remove("mender.config")

	configFile.WriteString(testConfigDevKey)

	config, err := LoadConfig("mender.config")
	assert.NoError(t, err)
	assert.NotNil(t, config)
	assert.Equal(t, "/foo/bar", config.DeviceKey)
}
