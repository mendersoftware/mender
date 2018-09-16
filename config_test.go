// Copyright 2017 Northern.tech AS
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

	config, err := loadConfig("mender.config", "does-not-exist.config")
	assert.NoError(t, err)
	assert.NotNil(t, config)
	validateConfiguration(t, config)

	config2, err2 := loadConfig("does-not-exist.config", "mender.config")
	assert.NoError(t, err2)
	assert.NotNil(t, config2)
	validateConfiguration(t, config2)
}

func TestServerURLConfig(t *testing.T) {
	configFile, _ := os.Create("mender.config")
	defer os.Remove("mender.config")

	configFile.WriteString(`{"ServerURL": "https://mender.io/"}`)

	config, err := loadConfig("mender.config", "does-not-exist.config")
	assert.NoError(t, err)
	assert.Equal(t, "https://mender.io", config.ServerURL)
}

func TestConfigurationMergeSettings(t *testing.T) {
	var mainConfigJson = `{
		"RootfsPartA": "Eggplant",
		"UpdatePollIntervalSeconds": 375
	}`

	var fallbackConfigJson = `{
		"RootfsPartA": "Spinach",
		"RootfsPartB": "Lettuce"
	}`

	mainConfigFile, _ := os.Create("main.config")
	defer os.Remove("main.config")
	mainConfigFile.WriteString(mainConfigJson)

	fallbackConfigFile, _ := os.Create("fallback.config")
	defer os.Remove("fallback.config")
	fallbackConfigFile.WriteString(fallbackConfigJson)

	config, err := loadConfig("main.config", "fallback.config")
	assert.NoError(t, err)
	assert.NotNil(t, config)

	// When a setting appears in neither file, it is left with its default value.
	assert.Equal(t, "", config.ServerCertificate)
	assert.Equal(t, 0, config.StateScriptTimeoutSeconds)

	// When a setting appears in both files, the main file takes precedence.
	assert.Equal(t, "Eggplant", config.RootfsPartA)

	// When a setting appears in only one file, its value is used.
	assert.Equal(t, "Lettuce", config.RootfsPartB)
	assert.Equal(t, 375, config.UpdatePollIntervalSeconds)
}

func TestConfigurationNeitherFileExistsIsError(t *testing.T) {
	config, err := loadConfig("does-not-exist", "also-does-not-exist")
	assert.Error(t, err)
	assert.Nil(t, config)
}
