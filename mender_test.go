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
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_updateState_readBootEnvError_returnsError(t *testing.T) {
	mender := newTestMender(nil)

	// pretend we're boostrapped
	mender.state = MenderStateBootstrapped

	assert.Equal(t, MenderStateError, mender.TransitionState())
}

func Test_updateState_haveUpgradeAvailable_returnsMenderRunningWithFreshUpdate(t *testing.T) {
	runner := newTestOSCalls("upgrade_available=1", 0)
	mender := newTestMender(&runner)

	// pretend we're boostrapped
	mender.state = MenderStateBootstrapped

	assert.Equal(t, MenderStateRunningWithFreshUpdate, mender.TransitionState())
}

func Test_updateState_haveNoUpgradeAvailable_returnsMenderWaitForUpdate(t *testing.T) {
	runner := newTestOSCalls("upgrade_available=0", 0)
	mender := newTestMender(&runner)

	// pretend we're boostrapped
	mender.state = MenderStateBootstrapped

	assert.Equal(t, MenderStateWaitForUpdate, mender.TransitionState())
}

func Test_getImageId_errorReadingFile_returnsEmptyId(t *testing.T) {
	mender := newTestMender(nil)
	mender.manifestFile = "non-existing"

	assert.Equal(t, "", mender.GetCurrentImageID())
}

func Test_getImageId_noImageIdInFile_returnsEmptyId(t *testing.T) {
	mender := newTestMender(nil)

	manifestFile, _ := os.Create("manifest")
	defer os.Remove("manifest")

	fileContent := "dummy_data"
	manifestFile.WriteString(fileContent)
	// rewind to the beginning of file
	//manifestFile.Seek(0, 0)

	mender.manifestFile = "manifest"

	assert.Equal(t, "", mender.GetCurrentImageID())
}

func Test_getImageId_malformedImageIdLine_returnsEmptyId(t *testing.T) {
	mender := newTestMender(nil)

	manifestFile, _ := os.Create("manifest")
	defer os.Remove("manifest")

	fileContent := "IMAGE_ID"
	manifestFile.WriteString(fileContent)
	// rewind to the beginning of file
	//manifestFile.Seek(0, 0)

	mender.manifestFile = "manifest"

	assert.Equal(t, "", mender.GetCurrentImageID())
}

func Test_getImageId_haveImageId_returnsId(t *testing.T) {
	mender := newTestMender(nil)

	manifestFile, _ := os.Create("manifest")
	defer os.Remove("manifest")

	fileContent := "IMAGE_ID=mender-image"
	manifestFile.WriteString(fileContent)
	mender.manifestFile = "manifest"

	assert.Equal(t, "mender-image", mender.GetCurrentImageID())
}

func Test_readConfigFile_noFile_returnsError(t *testing.T) {
	err := readConfigFile(nil, "non-existing-file")
	assert.Error(t, err)
}

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
  "ServerCertificate": "/data/server.crt"
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
  "ServerCertificate": "/data/server.crt"
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
  "ServerCertificate": "/data/server.crt"
}`

func newTestMender(runner *testOSCalls) *mender {
	ms := NewMemStore()
	if runner == nil {
		testrunner := newTestOSCalls("", -1)
		runner = &testrunner
	}
	fakeEnv := uBootEnv{runner}
	mender := NewMender(&fakeEnv, ms)
	return mender
}

func Test_readConfigFile_brokenContent_returnsError(t *testing.T) {
	configFile, _ := os.Create("mender.config")
	defer os.Remove("mender.config")

	configFile.WriteString(testBrokenConfig)
	var confFromFile menderFileConfig

	err := readConfigFile(&confFromFile, "mender.config")
	assert.Error(t, err)

	assert.Equal(t, menderFileConfig{}, confFromFile)
}

func validateConfiguration(t *testing.T, actual menderFileConfig) {
	expectedConfig := menderFileConfig{
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
		ServerCertificate:   "/data/server.crt",
	}
	assert.True(t, reflect.DeepEqual(actual, expectedConfig))
}

func Test_loadConfig_noConfigFile_returnsError(t *testing.T) {
	mender := newTestMender(nil)
	assert.Error(t, mender.LoadConfig("non-existing"))
}

func Test_loadConfig_correctConfFile_returnsConfiguration(t *testing.T) {
	mender := newTestMender(nil)
	configFile, _ := os.Create("mender.config")
	defer os.Remove("mender.config")

	configFile.WriteString(testConfig)

	assert.NoError(t, mender.LoadConfig("mender.config"))

	validateConfiguration(t, mender.config)
}

func Test_loadConfig_correctConfFile_returnsConfigurationDeviceKey(t *testing.T) {
	mender := newTestMender(nil)
	configFile, _ := os.Create("mender.config")
	defer os.Remove("mender.config")

	configFile.WriteString(testConfigDevKey)

	assert.NoError(t, mender.LoadConfig("mender.config"))
	assert.Equal(t, "/foo/bar", mender.config.DeviceKey)
}

func Test_LastError(t *testing.T) {
	mender := newTestMender(nil)

	// pretend we're boostrapped
	mender.state = MenderStateBootstrapped

	assert.Equal(t, MenderStateError, mender.TransitionState())

	assert.NotNil(t, mender.LastError())
}

func Test_ForceBootstrap(t *testing.T) {
	mender := newTestMender(nil)

	mender.ForceBootstrap()

	assert.True(t, mender.needsBootstrap())
}

func Test_Bootstrap(t *testing.T) {
	configFile, _ := os.Create("mender.config")
	defer os.Remove("mender.config")

	d, _ := json.Marshal(struct {
		DeviceKey string
	}{
		"temp.key",
	})
	configFile.Write(d)

	mender := newTestMender(nil)

	assert.NoError(t, mender.LoadConfig("mender.config"))

	assert.True(t, mender.needsBootstrap())

	assert.NoError(t, mender.Bootstrap())

	k := NewKeystore(mender.deviceKey.store)
	assert.NotNil(t, k)
	assert.NoError(t, k.Load("temp.key"))
}

func Test_StateBootstrapGenerateKeys(t *testing.T) {
	configFile, _ := os.Create("mender.config")
	defer os.Remove("mender.config")

	d, _ := json.Marshal(struct {
		DeviceKey string
	}{
		"temp.key",
	})
	configFile.Write(d)

	mender := newTestMender(nil)

	assert.Equal(t, MenderStateInit, mender.state)

	assert.NoError(t, mender.LoadConfig("mender.config"))

	assert.Equal(t, MenderStateInit, mender.state)

	assert.Equal(t, MenderStateBootstrapped, mender.TransitionState())

	k := NewKeystore(mender.deviceKey.store)
	assert.NotNil(t, k)
	assert.NoError(t, k.Load("temp.key"))
}

func Test_StateBootstrappedHaveKeys(t *testing.T) {
	configFile, _ := os.Create("mender.config")
	defer os.Remove("mender.config")

	d, _ := json.Marshal(struct {
		DeviceKey string
	}{
		"temp.key",
	})
	configFile.Write(d)

	// generate valid keys
	ms := NewMemStore()
	k := NewKeystore(ms)
	assert.NotNil(t, k)
	assert.NoError(t, k.Generate())
	assert.NoError(t, k.Save("temp.key"))

	mender := newTestMender(nil)
	// swap mender's devicekey store
	mender.deviceKey.store = ms

	assert.Equal(t, MenderStateInit, mender.state)

	assert.NoError(t, mender.LoadConfig("mender.config"))

	assert.Equal(t, MenderStateBootstrapped, mender.TransitionState())
}

func Test_StateBootstrapError(t *testing.T) {
	configFile, _ := os.Create("mender.config")
	defer os.Remove("mender.config")

	d, _ := json.Marshal(struct {
		DeviceKey string
	}{
		"/foo",
	})
	configFile.Write(d)

	mender := newTestMender(nil)
	// newTestMender uses a MemStore, we want to make it read-only
	ms, ok := mender.deviceKey.store.(*MemStore)
	assert.True(t, ok)
	ms.ReadOnly(true)

	assert.Equal(t, MenderStateInit, mender.state)

	assert.NoError(t, mender.LoadConfig("mender.config"))

	assert.Equal(t, MenderStateError, mender.TransitionState())
}
