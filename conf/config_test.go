// Copyright 2022 Northern.tech AS
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
package conf

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"reflect"
	"testing"

	"github.com/mendersoftware/mender/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testConfig = `{
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
  "UpdateLogPath": "/var/lib/mender/log/deployment.log",
  "DeviceTypeFile": "/var/lib/mender/test_device_type"
}`

var testBrokenConfig = `{
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

var testMultipleServersConfig = `{
  "Servers": [
    {"ServerURL": "https://server.one/"},
    {"ServerURL": "https://server.two/"},
    {"ServerURL": "https://server.three/"}
  ]
}`

var testTooManyServerDefsConfig = `{
  "ServerURL": "mender.io",
  "ServerCertificate": "/var/lib/mender/server.crt",
  "Servers": [{"ServerURL": "mender.io"}]
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

var testArtifactVerifyKeysJoinArtifactVerifyKey = `{
	"ServerURL": "mender.io",
	"ArtifactVerifyKey": %q,
	"ArtifactVerifyKeys": [
		{
			"Path": %q,
			"UpdateTypes": [
				"rootfs-image",
				"software-a-image"
			]
		},
		{
			"Path": %q,
			"UpdateTypes": [
				"rootfs-image"
			]
		}
	]
}`

func Test_readConfigFile_noFile_returnsError(t *testing.T) {
	err := readConfigFile(nil, "non-existing-file")
	assert.Error(t, err)
}

func Test_readConfigFile_brokenContent_returnsError(t *testing.T) {
	configFile, _ := os.Create("mender.config")
	defer os.Remove("mender.config")

	configFile.WriteString(testBrokenConfig)

	// fails on first call to readConfigFile (invalid JSON)
	confFromFile, err := LoadConfig("mender.config", "does-not-exist.config")
	assert.Error(t, err)

	assert.Nil(t, confFromFile)
}

func validateConfiguration(t *testing.T, actual *MenderConfig) {
	expectedConfig := NewMenderConfig()
	expectedConfig.MenderConfigFromFile = MenderConfigFromFile{
		RootfsPartA:               "/dev/mmcblk0p2",
		RootfsPartB:               "/dev/mmcblk0p3",
		UpdatePollIntervalSeconds: 10,
		HttpsClient: client.HttpsClient{
			Certificate: "/data/client.crt",
			Key:         "/data/client.key",
		},
		InventoryPollIntervalSeconds: 60,
		ServerURL:                    "mender.io",
		ServerCertificate:            "/var/lib/mender/server.crt",
		UpdateLogPath:                "/var/lib/mender/log/deployment.log",
		DeviceTypeFile:               "/var/lib/mender/test_device_type",
		Servers:                      []client.MenderServer{{ServerURL: "mender.io"}},
		DBus: DBusConfig{
			Enabled: true,
		},
	}
	if !assert.True(t, reflect.DeepEqual(actual, expectedConfig)) {
		t.Logf("got:      %+v", actual)
		t.Logf("expected: %+v", expectedConfig)
	}
}

func Test_LoadConfig_correctConfFile_returnsConfiguration(t *testing.T) {
	configFile, _ := os.Create("mender.config")
	defer os.Remove("mender.config")

	configFile.WriteString(testConfig)

	config, err := LoadConfig("mender.config", "does-not-exist.config")
	assert.NoError(t, err)
	assert.NotNil(t, config)
	err = config.Validate()
	assert.NoError(t, err)
	validateConfiguration(t, config)

	config2, err2 := LoadConfig("does-not-exist.config", "mender.config")
	assert.NoError(t, err2)
	assert.NotNil(t, config2)
	err = config2.Validate()
	assert.NoError(t, err)
	validateConfiguration(t, config2)
}

func TestServerURLConfig(t *testing.T) {
	configFile, _ := os.Create("mender.config")
	defer os.Remove("mender.config")

	configFile.WriteString(`{"ServerURL": "https://mender.io/"}`)

	config, err := LoadConfig("mender.config", "does-not-exist.config")
	assert.NoError(t, err)
	err = config.Validate()
	assert.NoError(t, err)
	assert.Equal(t, "https://mender.io", config.Servers[0].ServerURL)

	// Not allowed to specify server(s) both as a list and string entry.
	configFile.Seek(0, io.SeekStart)
	configFile.WriteString(testTooManyServerDefsConfig)
	config, err = LoadConfig("mender.config", "does-not-exist.config")
	assert.NoError(t, err)
	err = config.Validate()
	assert.Error(t, err)
}

// TestMultipleServersConfig attempts to add multiple servers to config-
// file, as well as overriding the ServerURL from the first server.
func TestMultipleServersConfig(t *testing.T) {

	// create a temporary mender.conf file
	tdir, _ := ioutil.TempDir("", "mendertest")
	confPath := path.Join(tdir, "mender.conf")
	confFile, err := os.Create(confPath)
	defer os.RemoveAll(tdir)
	assert.NoError(t, err)

	confFile.WriteString(testMultipleServersConfig)
	// load config and assert expected values i.e. check that all entries
	// are present and URL's trailing forward slash is trimmed off.
	conf, err := LoadConfig(confPath, "does-not-exist.config")
	assert.NoError(t, err)
	conf.Validate()
	assert.NoError(t, err)
	assert.Equal(t, "https://server.one", conf.Servers[0].ServerURL)
	assert.Equal(t, "https://server.two", conf.Servers[1].ServerURL)
	assert.Equal(t, "https://server.three", conf.Servers[2].ServerURL)
}

func TestDBusConfig(t *testing.T) { // create a temporary mender.conf file
	tdir, _ := ioutil.TempDir("", "mendertest")
	confPath := path.Join(tdir, "mender.conf")
	confFile, err := os.Create(confPath)
	defer os.RemoveAll(tdir)
	assert.NoError(t, err)

	confFile.WriteString(testDBusConfig)
	conf, err := LoadConfig(confPath, "does-not-exist.config")
	assert.NoError(t, err)
	err = conf.Validate()
	assert.NoError(t, err)
	assert.Equal(t, true, conf.DBus.Enabled)

}

func TestDbusEnabledDefault(t *testing.T) {
	conf, err := LoadConfig("does-not-exist", "also-does-not-exist")
	assert.NoError(t, err)
	err = conf.Validate()
	assert.NoError(t, err)
	assert.IsType(t, &MenderConfig{}, conf)
	assert.True(t, conf.DBus.Enabled)
}

func TestDBusConfigDisabled(t *testing.T) { // create a temporary mender.conf file
	tdir, _ := ioutil.TempDir("", "mendertest")
	confPath := path.Join(tdir, "mender.conf")
	confFile, err := os.Create(confPath)
	defer os.RemoveAll(tdir)
	assert.NoError(t, err)

	confFile.WriteString(testDBusConfigDisabled)
	conf, err := LoadConfig(confPath, "does-not-exist")
	assert.NoError(t, err)
	assert.NoError(t, conf.Validate())
	assert.False(t, conf.DBus.Enabled)
}

func TestArtifactVerifyKeys_JoinArtifactVerifyKey(t *testing.T) {
	tdir := t.TempDir()
	legacyKeyPath := path.Join(tdir, "key0.pub")
	require.NoError(t, ioutil.WriteFile(legacyKeyPath, []byte("legacy-key-contents"), 0644))
	rootfsKeyPath := path.Join(tdir, "key1.pub")
	require.NoError(t, ioutil.WriteFile(rootfsKeyPath, []byte("rootfs-key-contents"), 0644))
	softwareAKeyPath := path.Join(tdir, "key2.pub")
	require.NoError(t, ioutil.WriteFile(softwareAKeyPath, []byte("software-a-key-contents"), 0644))
	confPath := path.Join(tdir, "mender.conf")
	confContents := fmt.Sprintf(testArtifactVerifyKeysJoinArtifactVerifyKey, legacyKeyPath, softwareAKeyPath, rootfsKeyPath)
	require.NoError(t, ioutil.WriteFile(confPath, []byte(confContents), 0644))

	conf, err := LoadConfig(confPath, "does-not-exist")
	require.NoError(t, err)
	require.NoError(t, conf.Validate())
	
	assert.Empty(t, conf.ArtifactVerifyKey)
	wantArtifactVerifyKeys := []*VerificationKeyConfig{
		{
			Path: softwareAKeyPath,
			UpdateTypes: []string{"rootfs-image", "software-a-image"},
		},
		{
			Path: rootfsKeyPath,
			UpdateTypes: []string{"rootfs-image"},
		},
		// We expect the legacy key to be last in the list.
		{
			Path: legacyKeyPath,
		},
	}
	assert.Equal(t, wantArtifactVerifyKeys, conf.ArtifactVerifyKeys)

	
	gotRootfsVerificationKeys := conf.SelectVerificationKeys("rootfs-image")
	// We want the returned verification keys to be in the order of most to least specific.
	wantRootfsVerificationKeys := []*VerificationKey{
		{
			Data: []byte("rootfs-key-contents"),
			Config: &VerificationKeyConfig{
				Path: rootfsKeyPath,
				UpdateTypes: []string{"rootfs-image"},
			},
		},
		{
			Data: []byte("software-a-key-contents"),
			Config: &VerificationKeyConfig{
				Path: softwareAKeyPath,
				UpdateTypes: []string{"rootfs-image", "software-a-image"},
			},
		},
		{
			Data: []byte("legacy-key-contents"),
			Config: &VerificationKeyConfig{
				Path: legacyKeyPath,
			},
		},
	}
	assert.Equal(t, wantRootfsVerificationKeys, gotRootfsVerificationKeys)
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

	config, err := LoadConfig("main.config", "fallback.config")
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

func TestConfigurationNeitherFileExistsIsNotError(t *testing.T) {
	config, err := LoadConfig("does-not-exist", "also-does-not-exist")
	assert.NoError(t, err)
	assert.IsType(t, &MenderConfig{}, config)
}

func TestDBusUpdateControlMapExpirationTimeSecondsConfig(t *testing.T) {
	noJson, err := ioutil.TempFile("", "noJson")
	require.NoError(t, err)
	noJson.WriteString("{}")

	// unset UpdateControlMapExpirationTimeSeconds , default to 2*UpdatePollIntervalSeconds
	noVariableSet := `{
                "ServerURL": "mender.io",
                "UpdatePollIntervalSeconds": 6
        }`
	tfile, err := ioutil.TempFile("", "noVarSet")
	require.NoError(t, err)
	tfile.WriteString(noVariableSet)
	config, err := LoadConfig(tfile.Name(), noJson.Name())
	require.NoError(t, err)
	assert.Equal(t, 6*2, config.GetUpdateControlMapExpirationTimeSeconds())
	assert.Equal(
		t,
		DefaultUpdateControlMapBootExpirationTimeSeconds,
		config.GetUpdateControlMapBootExpirationTimeSeconds(),
	)

	// set UpdateControlMapExpirationTimeSeconds
	variableSet := `{
                "ServerURL": "mender.io",
                "UpdatePollIntervalSeconds": 6,
                "UpdateControlMapExpirationTimeSeconds": 10,
                "UpdateControlMapBootExpirationTimeSeconds": 15
        }`
	tfile, err = ioutil.TempFile("", "VarSet")
	require.NoError(t, err)
	tfile.WriteString(variableSet)
	config, err = LoadConfig(tfile.Name(), noJson.Name())
	require.NoError(t, err)
	assert.Equal(t, 10, config.GetUpdateControlMapExpirationTimeSeconds())
	assert.Equal(t, 15, config.GetUpdateControlMapBootExpirationTimeSeconds())
}
