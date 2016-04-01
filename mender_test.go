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
)

func Test_updateState_readBootEnvError_returnsError(t *testing.T) {
	runner := newTestOSCalls("", -1)
	fakeEnv := uBootEnv{&runner}
	mender := NewMender(&fakeEnv)

	if err := mender.updateState(); err == nil {
		t.FailNow()
	}
}

func Test_updateState_haveUpgradeAvailable_returnsMenderRunningWithFreshUpdate(t *testing.T) {
	runner := newTestOSCalls("upgrade_available=1", 0)
	fakeEnv := uBootEnv{&runner}
	mender := NewMender(&fakeEnv)

	if err := mender.updateState(); err != nil || mender.state != MenderRunningWithFreshUpdate {
		t.FailNow()
	}
}

func Test_updateState_haveNoUpgradeAvailable_returnsMenderFreshInstall(t *testing.T) {
	runner := newTestOSCalls("upgrade_available=0", 0)
	fakeEnv := uBootEnv{&runner}
	mender := NewMender(&fakeEnv)

	if err := mender.updateState(); err != nil || mender.state != MenderFreshInstall {
		t.FailNow()
	}
}

func Test_getState_updateStateError_returnsStateUnknown(t *testing.T) {
	runner := newTestOSCalls("", -1)
	fakeEnv := uBootEnv{&runner}
	mender := NewMender(&fakeEnv)

	if state := mender.GetState(); state != MenderStateUnknown {
		t.FailNow()
	}
}

func Test_getImageId_errorReadingFile_returnsEmptyId(t *testing.T) {
	mender := mender{}
	mender.manifestFile = "non-existing"

	if imageID := mender.GetCurrentImageID(); imageID != "" {
		t.FailNow()
	}
}

func Test_getImageId_noImageIdInFile_returnsEmptyId(t *testing.T) {
	mender := mender{}

	manifestFile, _ := os.Create("manifest")
	defer os.Remove("manifest")

	fileContent := "dummy_data"
	manifestFile.WriteString(fileContent)
	// rewind to the beginning of file
	//manifestFile.Seek(0, 0)

	mender.manifestFile = "manifest"

	if imageID := mender.GetCurrentImageID(); imageID != "" {
		t.FailNow()
	}
}

func Test_getImageId_malformedImageIdLine_returnsEmptyId(t *testing.T) {
	mender := mender{}

	manifestFile, _ := os.Create("manifest")
	defer os.Remove("manifest")

	fileContent := "IMAGE_ID"
	manifestFile.WriteString(fileContent)
	// rewind to the beginning of file
	//manifestFile.Seek(0, 0)

	mender.manifestFile = "manifest"

	if imageID := mender.GetCurrentImageID(); imageID != "" {
		t.FailNow()
	}
}

func Test_getImageId_haveImageId_returnsId(t *testing.T) {
	mender := mender{}

	manifestFile, _ := os.Create("manifest")
	defer os.Remove("manifest")

	fileContent := "IMAGE_ID=mender-image"
	manifestFile.WriteString(fileContent)
	mender.manifestFile = "manifest"

	if imageID := mender.GetCurrentImageID(); imageID != "mender-image" {
		t.FailNow()
	}
}

func Test_readConfigFile_noFile_returnsError(t *testing.T) {
	if err := readConfigFile(nil, "non-existing-file"); err == nil {
		t.FailNow()
	}
}

var testConfig = `{
  "pollIntervalSeconds": 60,
  "ServerURL": "mender.io",
	"DeviceID": "1234-ABCD",
  "ServerCertificate": "/data/server.crt",
  "ClientProtocol": "https",
  "HttpsClient": {
    "Certificate": "/data/client.crt",
    "Key": "/data/client.key"
  }
}`

var testBrokenConfig = `{
  "pollIntervalSeconds": 60,
  "ServerURL": "mender
	"DeviceID": "1234-ABCD",
  "ServerCertificate": "/data/server.crt",
  "ClientProtocol": "https",
  "HttpsClient": {
    "Certificate": "/data/client.crt",
    "Key": "/data/client.key"
  }
}`

func Test_readConfigFile_brokenContent_returnsError(t *testing.T) {
	configFile, _ := os.Create("mender.config")
	defer os.Remove("mender.config")

	configFile.WriteString(testBrokenConfig)
	var confFromFile menderFileConfig

	if err := readConfigFile(&confFromFile, "mender.config"); err == nil {
		t.FailNow()
	}

	if confFromFile != (menderFileConfig{}) {
		t.FailNow()
	}
}

func validateConfiguration(actual menderFileConfig) bool {
	expectedConfig := menderFileConfig{
		PollIntervalSeconds: 60,
		DeviceID:            "1234-ABCD",
		ServerURL:           "mender.io",
		ServerCertificate:   "/data/server.crt",
		ClientProtocol:      "https",
		HttpsClient: struct {
			Certificate string
			Key         string
		}{
			Certificate: "/data/client.crt",
			Key:         "/data/client.key",
		},
	}
	return reflect.DeepEqual(actual, expectedConfig)
}

func Test_readConfigFile_correctContent_returnsConfiguration(t *testing.T) {

	var confFromFile menderFileConfig
	configFile, _ := os.Create("mender.config")
	defer os.Remove("mender.config")

	configFile.WriteString(testConfig)

	if err := readConfigFile(&confFromFile, "mender.config"); err != nil {
		t.FailNow()
	}

	// check if content of config is correct
	if !validateConfiguration(confFromFile) {
		t.FailNow()
	}
}

func Test_loadConfig_noConfigFile_returnsError(t *testing.T) {
	mender := mender{}
	if err := mender.LoadConfig("non-existing"); err == nil {
		t.FailNow()
	}
}

func Test_loadConfig_correctConfFile_returnsConfiguration(t *testing.T) {
	mender := mender{}
	configFile, _ := os.Create("mender.config")
	defer os.Remove("mender.config")

	configFile.WriteString(testConfig)

	if err := mender.LoadConfig("mender.config"); err != nil {
		t.FailNow()
	}

	// check if content of config is correct
	if !validateConfiguration(mender.config) {
		t.FailNow()
	}
}
