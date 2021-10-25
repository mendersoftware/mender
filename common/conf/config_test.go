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
package conf

import (
	"io/ioutil"
	"os"
	"path"
	"reflect"
	"testing"

	"github.com/mendersoftware/mender/common/tls"
	"github.com/stretchr/testify/assert"
)

var testConfig = `{
  "HttpsClient": {
    "Certificate": "/data/api.crt",
    "Key": "/data/api.key"
  },
  "ServerCertificate": "/var/lib/mender/server.crt"
}`

var testBrokenConfig = `{
  "HttpsClient": {
    "Certificate": "/data/api.crt",
    "Key": "/data/api.key"
  },
  "RootfsPartA": "/dev/mmcblk0p2",
  "RootfsPartB": "/dev/mmcblk0p3",
  "PollIntervalSeconds": 60,
  "ServerURL": "mender
  "ServerCertificate": "/var/lib/mender/server.crt"
}`

var testDBusConfig = `{
  "ServerURL": "mender.io",
  "DBus": {
    "Enabled": true
  }
}`

var testDBusConfigDisabled = `{
  "ServerURL": "mender.io",
  "DBus": {
    "Enabled": false
  }
}`

func validateConfiguration(t *testing.T, actual *Config) {
	expectedConfig := NewConfig()
	*expectedConfig = Config{
		HttpsClient: tls.HttpsClient{
			Certificate: "/data/api.crt",
			Key:         "/data/api.key",
		},
		ServerCertificate:            "/var/lib/mender/server.crt",
		DBus: DBusConfig{
			Enabled: true,
		},
	}
	if !assert.True(t, reflect.DeepEqual(actual, expectedConfig)) {
		t.Logf("got:      %+v", actual)
		t.Logf("expected: %+v", expectedConfig)
	}
}

func Test_readConfigFile_noFile_returnsError(t *testing.T) {
	err := readConfigFile(nil, "non-existing-file")
	assert.Error(t, err)
}

func Test_readConfigFile_brokenContent_returnsError(t *testing.T) {
	configFile, _ := os.Create("mender.config")
	defer os.Remove("mender.config")

	configFile.WriteString(testBrokenConfig)

	// fails on first call to readConfigFile (invalid JSON)
	confFromFile := NewConfig()
	err := LoadConfig("mender.config", "does-not-exist.config", confFromFile)
	assert.Error(t, err)
}

func Test_LoadConfig_correctConfFile_returnsConfiguration(t *testing.T) {
	configFile, _ := os.Create("mender.config")
	defer os.Remove("mender.config")

	configFile.WriteString(testConfig)

	config := NewConfig()
	err := LoadConfig("mender.config", "does-not-exist.config", config)
	assert.NoError(t, err)
	assert.NotNil(t, config)
	validateConfiguration(t, config)

	config2 := NewConfig()
	err2 := LoadConfig("does-not-exist.config", "mender.config", config2)
	assert.NoError(t, err2)
	assert.NotNil(t, config2)
	validateConfiguration(t, config2)
}

func TestDBusConfig(t *testing.T) { // create a temporary mender.conf file
	tdir, _ := ioutil.TempDir("", "mendertest")
	confPath := path.Join(tdir, "mender.conf")
	confFile, err := os.Create(confPath)
	defer os.RemoveAll(tdir)
	assert.NoError(t, err)

	confFile.WriteString(testDBusConfig)
	conf := NewConfig()
	err = LoadConfig(confPath, "does-not-exist.config", conf)
	assert.NoError(t, err)
	assert.Equal(t, true, conf.DBus.Enabled)

}

func TestDbusEnabledDefault(t *testing.T) {
	conf := NewConfig()
	err := LoadConfig("does-not-exist", "also-does-not-exist", conf)
	assert.NoError(t, err)
	assert.True(t, conf.DBus.Enabled)
}

func TestDBusConfigDisabled(t *testing.T) { // create a temporary mender.conf file
	tdir, _ := ioutil.TempDir("", "mendertest")
	confPath := path.Join(tdir, "mender.conf")
	confFile, err := os.Create(confPath)
	defer os.RemoveAll(tdir)
	assert.NoError(t, err)

	confFile.WriteString(testDBusConfigDisabled)
	conf := NewConfig()
	err = LoadConfig(confPath, "does-not-exist", conf)
	assert.NoError(t, err)
	assert.False(t, conf.DBus.Enabled)
}

func TestConfigurationMergeSettings(t *testing.T) {
	var mainConfigJson = `{
		"ServerCertificate": "mycert.crt",
		"HttpsClient": {
			"Certificate": "myothercert.crt"
		}
	}`

	var fallbackConfigJson = `{
		"HttpsClient": {
			"Certificate": "myfallbackcert.crt",
			"Key": "myfallbackkey.key"
		}
	}`

	mainConfigFile, _ := os.Create("main.config")
	defer os.Remove("main.config")
	mainConfigFile.WriteString(mainConfigJson)

	fallbackConfigFile, _ := os.Create("fallback.config")
	defer os.Remove("fallback.config")
	fallbackConfigFile.WriteString(fallbackConfigJson)

	config := NewConfig()
	err := LoadConfig("main.config", "fallback.config", config)
	assert.NoError(t, err)
	assert.NotNil(t, config)

	// When a setting appears in neither file, it is left with its default value.
	assert.Equal(t, true, config.DBus.Enabled)

	// When a setting appears in both files, the main file takes precedence.
	assert.Equal(t, "myothercert.crt", config.HttpsClient.Certificate)

	// When a setting appears in only one file, its value is used.
	assert.Equal(t, "myfallbackkey.key", config.HttpsClient.Key)
	assert.Equal(t, "mycert.crt", config.ServerCertificate)
}

func TestConfigurationNeitherFileExistsIsNotError(t *testing.T) {
	config := NewConfig()
	err := LoadConfig("does-not-exist", "also-does-not-exist", config)
	assert.NoError(t, err)
}
