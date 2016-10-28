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
	"errors"
	"os"
	"testing"

	"github.com/mendersoftware/mender/client"
)

func Test_doManualUpdate_noParams_fail(t *testing.T) {
	if err := doRootfs(new(device), runOptionsType{}); err == nil {
		t.FailNow()
	}
}

func Test_doManualUpdate_invalidHttpsClientConfig_updateFails(t *testing.T) {
	runOptions := runOptionsType{}
	iamgeFileName := "https://update"
	runOptions.imageFile = &iamgeFileName
	runOptions.ServerCert = "non-existing"

	if err := doRootfs(new(device), runOptions); err == nil {
		t.FailNow()
	}
}

func Test_doManualUpdate_nonExistingFile_fail(t *testing.T) {
	fakeDevice := device{}
	fakeRunOptions := runOptionsType{}
	imageFileName := "non-existing"
	fakeRunOptions.imageFile = &imageFileName

	if err := doRootfs(&fakeDevice, fakeRunOptions); err == nil {
		t.FailNow()
	}
}

func Test_doManualUpdate_networkUpdateNoClient_fail(t *testing.T) {
	fakeDevice := device{}
	fakeRunOptions := runOptionsType{}
	imageFileName := "http://non-existing"
	fakeRunOptions.imageFile = &imageFileName

	if err := doRootfs(&fakeDevice, fakeRunOptions); err == nil {
		t.FailNow()
	}
}

func Test_doManualUpdate_networkClientExistsNoServer_fail(t *testing.T) {
	fakeDevice := device{}
	fakeRunOptions := runOptionsType{}
	imageFileName := "http://non-existing"
	fakeRunOptions.imageFile = &imageFileName

	fakeRunOptions.Config =
		client.Config{"client.crt", "client.key", "server.crt", true, false}

	if err := doRootfs(&fakeDevice, fakeRunOptions); err == nil {
		t.FailNow()
	}
}

func Test_doManualUpdate_installFailing_updateFails(t *testing.T) {
	fakeDevice := fakeDevice{}
	fakeDevice.retInstallUpdate = errors.New("")
	fakeRunOptions := runOptionsType{}
	imageFileName := "imageFile"
	fakeRunOptions.imageFile = &imageFileName

	image, _ := os.Create("imageFile")
	imageContent := "test content"
	image.WriteString(imageContent)
	// rewind to the beginning of file
	image.Seek(0, 0)

	defer os.Remove("imageFile")

	if err := doRootfs(fakeDevice, fakeRunOptions); err == nil {
		t.FailNow()
	}
}

func Test_doManualUpdate_existingFile_updateSuccess(t *testing.T) {
	fakeDevice := fakeDevice{}
	fakeRunOptions := runOptionsType{}
	imageFileName := "imageFile"
	fakeRunOptions.imageFile = &imageFileName

	image, _ := os.Create("imageFile")
	imageContent := "test content"
	image.WriteString(imageContent)
	// rewind to the beginning of file
	image.Seek(0, 0)

	defer os.Remove("imageFile")

	if err := doRootfs(fakeDevice, fakeRunOptions); err != nil {
		t.FailNow()
	}
}
