// Copyright 2022 Northern.tech AS
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

package app

import (
	"encoding/json"
	"io"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/mendersoftware/mender-artifact/artifact"
	"github.com/mendersoftware/mender/client"
	"github.com/mendersoftware/mender/conf"
	"github.com/mendersoftware/mender/datastore"
	dev "github.com/mendersoftware/mender/device"
	"github.com/mendersoftware/mender/installer"
	"github.com/mendersoftware/mender/statescript"
	"github.com/mendersoftware/mender/store"
	"github.com/mendersoftware/mender/tests"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func getTestDeviceManager(dualRootfsDevice installer.DualRootfsDevice,
	config *conf.MenderConfig, deviceTypeFile string, dbdir string) *dev.DeviceManager {

	dbstore := store.NewDBStore(dbdir)

	dm := dev.NewDeviceManager(dualRootfsDevice, config, dbstore)
	dm.DeviceTypeFile = deviceTypeFile
	return dm
}

func zeroLengthDeviceTypeFile(t *testing.T) string {
	file, err := ioutil.TempFile("", "")
	require.NoError(t, err)
	name := file.Name()
	file.Close()
	return name
}

func standaloneInstallSetup(t *testing.T, tmpdir string,
	tma *tests.TestModuleAttr, aao tests.ArtifactAttributeOverrides) (*dev.DeviceManager,
	statescript.Executor) {

	moduleDir := path.Join(tmpdir, "modules")

	require.NoError(t, os.MkdirAll(moduleDir, 0755))

	tests.UpdateModulesSetup(t, tma, tmpdir, aao)

	config := conf.MenderConfig{
		MenderConfigFromFile: conf.MenderConfigFromFile{
			Servers: []client.MenderServer{
				client.MenderServer{
					ServerURL: "https://not-used",
				},
			},
			ModuleTimeoutSeconds: 5,
		},
		ModulesPath:         path.Join(tmpdir, "modules"),
		ModulesWorkPath:     path.Join(tmpdir, "work"),
		ArtifactScriptsPath: path.Join(tmpdir, "scripts"),
		RootfsScriptsPath:   path.Join(tmpdir, "scriptdir"),
	}
	stateExec := dev.NewStateScriptExecutor(&config)
	dbstorePath := path.Join(tmpdir, "store")
	require.NoError(t, os.MkdirAll(dbstorePath, 0755))
	dbstore := store.NewDBStore(dbstorePath)
	device := dev.NewDeviceManager(nil, &config, dbstore)
	device.DeviceTypeFile = path.Join(tmpdir, "device_type")
	device.ArtifactInfoFile = path.Join(tmpdir, "artifact_info")

	return device, stateExec
}

func Test_doManualUpdate_noParams_fail(t *testing.T) {
	dbdir, err := ioutil.TempDir("", "menderDbdir")
	require.NoError(t, err)
	defer os.RemoveAll(dbdir)

	config := conf.MenderConfig{}
	deviceType := zeroLengthDeviceTypeFile(t)
	defer os.Remove(deviceType)

	dualRootfsDevice := installer.NewDualRootfsDevice(nil, nil, conf.DualRootfsDeviceConfig{})
	if err := DoStandaloneInstall(getTestDeviceManager(dualRootfsDevice, &config, deviceType, dbdir),
		"", client.Config{}, dev.NewStateScriptExecutor(&config), false); err == nil {

		t.FailNow()
	}
}

func Test_doManualUpdate_invalidHttpsClientConfig_updateFails(t *testing.T) {
	dbdir, err := ioutil.TempDir("", "menderDbdir")
	require.NoError(t, err)
	defer os.RemoveAll(dbdir)

	runOptions := client.Config{
		ServerCert: "non-existing",
	}
	imageFile := "https://update"
	deviceType := zeroLengthDeviceTypeFile(t)
	defer os.Remove(deviceType)

	config := conf.MenderConfig{}
	dualRootfsDevice := installer.NewDualRootfsDevice(nil, nil, conf.DualRootfsDeviceConfig{})
	if err := DoStandaloneInstall(getTestDeviceManager(
		dualRootfsDevice, &config, deviceType, dbdir),
		imageFile, runOptions,
		dev.NewStateScriptExecutor(&config), false); err == nil {

		t.FailNow()
	}
}

func Test_doManualUpdate_nonExistingFile_fail(t *testing.T) {
	dbdir, err := ioutil.TempDir("", "menderDbdir")
	require.NoError(t, err)
	defer os.RemoveAll(dbdir)

	fakeDevice := installer.NewDualRootfsDevice(nil, nil, conf.DualRootfsDeviceConfig{})
	imageFile := "non-existing"
	deviceType := zeroLengthDeviceTypeFile(t)
	defer os.Remove(deviceType)

	config := conf.MenderConfig{}
	if err := DoStandaloneInstall(getTestDeviceManager(
		fakeDevice, &config, deviceType, dbdir),
		imageFile, client.Config{},
		dev.NewStateScriptExecutor(&config), false); err == nil {

		t.FailNow()
	}
}

func Test_doManualUpdate_networkUpdateNoClient_fail(t *testing.T) {
	dbdir, err := ioutil.TempDir("", "menderDbdir")
	require.NoError(t, err)
	defer os.RemoveAll(dbdir)

	fakeDevice := installer.NewDualRootfsDevice(nil, nil, conf.DualRootfsDeviceConfig{})
	imageFile := "http://non-existing"
	deviceType := zeroLengthDeviceTypeFile(t)
	defer os.Remove(deviceType)

	config := conf.MenderConfig{}
	if err := DoStandaloneInstall(getTestDeviceManager(fakeDevice, &config, deviceType, dbdir),
		imageFile, client.Config{}, dev.NewStateScriptExecutor(&config), false); err == nil {

		t.FailNow()
	}
}

func Test_doManualUpdate_networkClientExistsNoServer_fail(t *testing.T) {
	dbdir, err := ioutil.TempDir("", "menderDbdir")
	require.NoError(t, err)
	defer os.RemoveAll(dbdir)

	fakeDevice := installer.NewDualRootfsDevice(nil, nil, conf.DualRootfsDeviceConfig{})
	imageFile := "http://non-existing"
	deviceType := zeroLengthDeviceTypeFile(t)
	defer os.Remove(deviceType)

	fakeClientConfig := client.Config{
		ServerCert: "server.crt",
		NoVerify:   false,
	}

	config := conf.MenderConfig{}
	if err := DoStandaloneInstall(getTestDeviceManager(
		fakeDevice, &config, deviceType, dbdir),
		imageFile, fakeClientConfig,
		dev.NewStateScriptExecutor(&config), false); err == nil {

		t.FailNow()
	}
}

func Test_doManualUpdate_existingFile_updateSuccess(t *testing.T) {
	// setup

	deviceType := zeroLengthDeviceTypeFile(t)
	defer os.Remove(deviceType)

	artifact, err := MakeRootfsImageArtifact(2, false)
	require.NoError(t, err)
	require.NotNil(t, artifact)

	tmpdir, err := ioutil.TempDir("", "mendertest")
	require.NoError(t, err)
	defer os.RemoveAll(tmpdir)

	f, err := ioutil.TempFile("", "update")
	require.NoError(t, err)
	defer os.Remove(f.Name())

	_, err = io.Copy(f, artifact)
	assert.NoError(t, err)
	f.Close()

	// test

	dbdir, err := ioutil.TempDir("", "menderDbdir")
	require.NoError(t, err)
	defer os.RemoveAll(dbdir)

	fakeDev := FakeDevice{ConsumeUpdate: true}
	imageFileName := f.Name()

	config := conf.MenderConfig{
		ArtifactScriptsPath: tmpdir,
	}
	err = DoStandaloneInstall(getTestDeviceManager(
		fakeDev, &config, deviceType, dbdir),
		imageFileName, client.Config{},
		dev.NewStateScriptExecutor(&config), false)
	assert.NoError(t, err)
}

func Test_doManualUpdate_existingFile_updateSuccess_rebootExitCode(t *testing.T) {
	// setup

	deviceType := zeroLengthDeviceTypeFile(t)
	defer os.Remove(deviceType)

	artifact, err := MakeRootfsImageArtifact(2, false)
	require.NoError(t, err)
	require.NotNil(t, artifact)

	tmpdir, err := ioutil.TempDir("", "mendertest")
	require.NoError(t, err)
	defer os.RemoveAll(tmpdir)

	f, err := ioutil.TempFile("", "update")
	require.NoError(t, err)
	defer os.Remove(f.Name())

	_, err = io.Copy(f, artifact)
	assert.NoError(t, err)
	f.Close()

	// test

	dbdir, err := ioutil.TempDir("", "menderDbdir")
	require.NoError(t, err)
	defer os.RemoveAll(dbdir)

	fakeDev := FakeDevice{ConsumeUpdate: true}
	imageFileName := f.Name()

	config := conf.MenderConfig{
		ArtifactScriptsPath: tmpdir,
	}
	if err = DoStandaloneInstall(getTestDeviceManager(
		fakeDev, &config, deviceType, dbdir),
		imageFileName, client.Config{},
		dev.NewStateScriptExecutor(&config), true); err != ErrorManualRebootRequired {
		t.FailNow()
	}
}

func TestDoManualUpdateArtifactV3Dependencies(t *testing.T) {
	// setup
	deviceType := zeroLengthDeviceTypeFile(t)
	defer os.Remove(deviceType)

	artifactProvides := &tests.ArtifactProvides{
		ArtifactName:  "testName",
		ArtifactGroup: "testGroup",
	}
	artifactDepends := &tests.ArtifactDepends{
		ArtifactName:      []string{"OldArtifact"},
		ArtifactGroup:     []string{"testGroup"},
		CompatibleDevices: []string{"qemux86-64"},
	}
	typeInfoDepends := map[string]interface{}{
		"testKey": "testValue",
	}

	artifactStream, err := tests.CreateTestArtifactV3("test", "gzip",
		artifactProvides, artifactDepends, nil, typeInfoDepends)
	require.NoError(t, err)
	require.NotNil(t, artifactStream)

	tmpdir, err := ioutil.TempDir("", "mendertest")
	require.NoError(t, err)
	defer os.RemoveAll(tmpdir)

	f, err := ioutil.TempFile("", "update")
	require.NoError(t, err)
	defer os.Remove(f.Name())

	_, err = io.Copy(f, artifactStream)
	assert.NoError(t, err)
	f.Close()

	// test
	dbdir, err := ioutil.TempDir("", "menderDbdir")
	require.NoError(t, err)
	defer os.RemoveAll(dbdir)

	fakeDev := FakeDevice{ConsumeUpdate: true}
	imageFileName := f.Name()
	config := conf.MenderConfig{
		ArtifactScriptsPath: tmpdir,
	}

	testDevMgr := getTestDeviceManager(fakeDev, &config, deviceType, dbdir)

	// First check that unmet dependencies returns an error, and add
	// one dependency at the time untill the artifact should be accepted.
	err = DoStandaloneInstall(testDevMgr,
		imageFileName, client.Config{},
		dev.NewStateScriptExecutor(&config), false)
	assert.Error(t, err)

	// Try with existing, but null typeInfoProvides.
	testDevMgr.Store.WriteAll(
		datastore.ArtifactTypeInfoProvidesKey, []byte("null"))
	err = DoStandaloneInstall(testDevMgr,
		imageFileName, client.Config{},
		dev.NewStateScriptExecutor(&config), false)
	assert.Error(t, err)

	// Try with artifact_name inserted.
	testDevMgr.Store.WriteAll(datastore.ArtifactNameKey,
		[]byte("OldArtifact"))
	err = DoStandaloneInstall(testDevMgr,
		imageFileName, client.Config{},
		dev.NewStateScriptExecutor(&config), false)
	assert.Error(t, err)

	// Try with artifact_group inserted.
	testDevMgr.Store.WriteAll(datastore.ArtifactGroupKey,
		[]byte("testGroup"))
	err = DoStandaloneInstall(testDevMgr,
		imageFileName, client.Config{},
		dev.NewStateScriptExecutor(&config), false)
	assert.Error(t, err)

	// Try with typeInfoProvides inserted.
	typeProvidesBuf, err := json.Marshal(typeInfoDepends)
	assert.NoError(t, err)
	testDevMgr.Store.WriteAll(
		datastore.ArtifactTypeInfoProvidesKey, typeProvidesBuf)
	err = DoStandaloneInstall(testDevMgr,
		imageFileName, client.Config{},
		dev.NewStateScriptExecutor(&config), false)
	assert.NoError(t, err)

}

type standaloneModuleInstallCase struct {
	caseName    string
	errInstall  string
	errRollback string
	errCommit   string
	expectedLog []string
	// See consts below.
	stage int

	installOutcome tests.InstallOutcome
	testModuleAttr tests.TestModuleAttr
}

const (
	standaloneCommit = iota
	standaloneInstall
	standaloneRollback
)

const updateModuleDefaultError = "Update module terminated abnormally"

var standaloneModuleInstallCases []standaloneModuleInstallCase = []standaloneModuleInstallCase{
	standaloneModuleInstallCase{
		caseName: "Normal install, no rollback",
		expectedLog: []string{
			"Download_Enter_00",
			"Download",
			"Download_Leave_00",
			"SupportsRollback",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"ArtifactInstall_Leave_00",
			"NeedsArtifactReboot",
			"ArtifactCommit_Enter_00",
			"ArtifactCommit",
			"ArtifactCommit_Leave_00",
			"Cleanup",
		},
		stage: standaloneInstall,
		testModuleAttr: tests.TestModuleAttr{
			RollbackDisabled: true,
		},
		installOutcome: tests.SuccessfulInstall,
	},

	standaloneModuleInstallCase{
		caseName: "Normal install",
		expectedLog: []string{
			"Download_Enter_00",
			"Download",
			"Download_Leave_00",
			"SupportsRollback",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"ArtifactInstall_Leave_00",
			"NeedsArtifactReboot",
		},
		stage:          standaloneInstall,
		testModuleAttr: tests.TestModuleAttr{},
		installOutcome: tests.SuccessfulUncommitted,
	},

	standaloneModuleInstallCase{
		caseName: "Normal commit",
		expectedLog: []string{
			"Download_Enter_00",
			"Download",
			"Download_Leave_00",
			"SupportsRollback",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"ArtifactInstall_Leave_00",
			"NeedsArtifactReboot",
			"ArtifactCommit_Enter_00",
			"ArtifactCommit",
			"ArtifactCommit_Leave_00",
			"Cleanup",
		},
		stage:          standaloneCommit,
		testModuleAttr: tests.TestModuleAttr{},
		installOutcome: tests.SuccessfulInstall,
	},

	standaloneModuleInstallCase{
		caseName: "Normal rollback",
		expectedLog: []string{
			"Download_Enter_00",
			"Download",
			"Download_Leave_00",
			"SupportsRollback",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"ArtifactInstall_Leave_00",
			"NeedsArtifactReboot",
			"SupportsRollback",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"ArtifactRollback_Leave_00",
			"Cleanup",
		},
		stage:          standaloneRollback,
		testModuleAttr: tests.TestModuleAttr{},
		installOutcome: tests.SuccessfulRollback,
	},

	standaloneModuleInstallCase{
		caseName: "Fail in Download_Enter_00",
		expectedLog: []string{
			"Download_Enter_00",
			"Download_Error_00",
		},
		stage:      standaloneInstall,
		errInstall: "Download_Enter_00",
		testModuleAttr: tests.TestModuleAttr{
			ErrorStates: []string{"Download_Enter_00"},
		},
		installOutcome: tests.SuccessfulRollback,
	},

	standaloneModuleInstallCase{
		caseName: "Fail in Download",
		expectedLog: []string{
			"Download_Enter_00",
			"Download",
			"Download_Error_00",
			"Cleanup",
		},
		stage:      standaloneInstall,
		errInstall: updateModuleDefaultError,
		testModuleAttr: tests.TestModuleAttr{
			ErrorStates: []string{"Download"},
		},
		installOutcome: tests.SuccessfulRollback,
	},

	standaloneModuleInstallCase{
		caseName: "Fail in Download_Leave_00",
		expectedLog: []string{
			"Download_Enter_00",
			"Download",
			"Download_Leave_00",
			"Download_Error_00",
			"Cleanup",
		},
		stage:      standaloneInstall,
		errInstall: "Download_Leave_00",
		testModuleAttr: tests.TestModuleAttr{
			ErrorStates: []string{"Download_Leave_00"},
		},
		installOutcome: tests.SuccessfulRollback,
	},

	standaloneModuleInstallCase{
		caseName: "Fail in ArtifactInstall_Enter_00",
		expectedLog: []string{
			"Download_Enter_00",
			"Download",
			"Download_Leave_00",
			"SupportsRollback",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall_Error_00",
			"SupportsRollback",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"ArtifactRollback_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		stage:      standaloneInstall,
		errInstall: "ArtifactInstall_Enter_00",
		testModuleAttr: tests.TestModuleAttr{
			ErrorStates: []string{"ArtifactInstall_Enter_00"},
		},
		installOutcome: tests.SuccessfulRollback,
	},

	standaloneModuleInstallCase{
		caseName: "Fail in ArtifactInstall",
		expectedLog: []string{
			"Download_Enter_00",
			"Download",
			"Download_Leave_00",
			"SupportsRollback",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"ArtifactInstall_Error_00",
			"SupportsRollback",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"ArtifactRollback_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		stage:      standaloneInstall,
		errInstall: updateModuleDefaultError,
		testModuleAttr: tests.TestModuleAttr{
			ErrorStates: []string{"ArtifactInstall"},
		},
		installOutcome: tests.SuccessfulRollback,
	},

	standaloneModuleInstallCase{
		caseName: "Fail in ArtifactInstall_Leave_00",
		expectedLog: []string{
			"Download_Enter_00",
			"Download",
			"Download_Leave_00",
			"SupportsRollback",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"ArtifactInstall_Leave_00",
			"ArtifactInstall_Error_00",
			"SupportsRollback",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"ArtifactRollback_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		stage:      standaloneInstall,
		errInstall: "ArtifactInstall_Leave_00",
		testModuleAttr: tests.TestModuleAttr{
			ErrorStates: []string{"ArtifactInstall_Leave_00"},
		},
		installOutcome: tests.SuccessfulRollback,
	},

	standaloneModuleInstallCase{
		caseName: "Fail in ArtifactCommit_Enter_00",
		expectedLog: []string{
			"Download_Enter_00",
			"Download",
			"Download_Leave_00",
			"SupportsRollback",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"ArtifactInstall_Leave_00",
			"NeedsArtifactReboot",
			"ArtifactCommit_Enter_00",
			"ArtifactCommit_Error_00",
			"SupportsRollback",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"ArtifactRollback_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		stage:     standaloneCommit,
		errCommit: "ArtifactCommit_Enter_00",
		testModuleAttr: tests.TestModuleAttr{
			ErrorStates: []string{"ArtifactCommit_Enter_00"},
		},
		installOutcome: tests.SuccessfulRollback,
	},

	standaloneModuleInstallCase{
		caseName: "Fail in ArtifactCommit",
		expectedLog: []string{
			"Download_Enter_00",
			"Download",
			"Download_Leave_00",
			"SupportsRollback",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"ArtifactInstall_Leave_00",
			"NeedsArtifactReboot",
			"ArtifactCommit_Enter_00",
			"ArtifactCommit",
			"ArtifactCommit_Error_00",
			"SupportsRollback",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"ArtifactRollback_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		stage:     standaloneCommit,
		errCommit: updateModuleDefaultError,
		testModuleAttr: tests.TestModuleAttr{
			ErrorStates: []string{"ArtifactCommit"},
		},
		installOutcome: tests.SuccessfulRollback,
	},

	standaloneModuleInstallCase{
		caseName: "Fail in ArtifactCommit_Leave_00",
		expectedLog: []string{
			"Download_Enter_00",
			"Download",
			"Download_Leave_00",
			"SupportsRollback",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"ArtifactInstall_Leave_00",
			"NeedsArtifactReboot",
			"ArtifactCommit_Enter_00",
			"ArtifactCommit",
			"ArtifactCommit_Leave_00",
			"ArtifactCommit_Error_00",
			"Cleanup",
		},
		stage:     standaloneCommit,
		errCommit: "ArtifactCommit_Leave_00",
		testModuleAttr: tests.TestModuleAttr{
			ErrorStates: []string{"ArtifactCommit_Leave_00"},
		},
		installOutcome: tests.UnsuccessfulInstall,
	},

	standaloneModuleInstallCase{
		caseName: "Fail in ArtifactCommit_Enter_00, no rollback",
		expectedLog: []string{
			"Download_Enter_00",
			"Download",
			"Download_Leave_00",
			"SupportsRollback",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"ArtifactInstall_Leave_00",
			"NeedsArtifactReboot",
			"ArtifactCommit_Enter_00",
			"ArtifactCommit_Error_00",
			"SupportsRollback",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		stage:      standaloneInstall,
		errInstall: "ArtifactCommit_Enter_00",
		testModuleAttr: tests.TestModuleAttr{
			ErrorStates:      []string{"ArtifactCommit_Enter_00"},
			RollbackDisabled: true,
		},
		installOutcome: tests.UnsuccessfulInstall,
	},

	standaloneModuleInstallCase{
		caseName: "Fail in ArtifactCommit, no rollback",
		expectedLog: []string{
			"Download_Enter_00",
			"Download",
			"Download_Leave_00",
			"SupportsRollback",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"ArtifactInstall_Leave_00",
			"NeedsArtifactReboot",
			"ArtifactCommit_Enter_00",
			"ArtifactCommit",
			"ArtifactCommit_Error_00",
			"SupportsRollback",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		stage:      standaloneInstall,
		errInstall: updateModuleDefaultError,
		testModuleAttr: tests.TestModuleAttr{
			ErrorStates:      []string{"ArtifactCommit"},
			RollbackDisabled: true,
		},
		installOutcome: tests.UnsuccessfulInstall,
	},

	standaloneModuleInstallCase{
		caseName: "Fail in ArtifactCommit_Leave_00, no rollback",
		expectedLog: []string{
			"Download_Enter_00",
			"Download",
			"Download_Leave_00",
			"SupportsRollback",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"ArtifactInstall_Leave_00",
			"NeedsArtifactReboot",
			"ArtifactCommit_Enter_00",
			"ArtifactCommit",
			"ArtifactCommit_Leave_00",
			"ArtifactCommit_Error_00",
			"Cleanup",
		},
		stage:      standaloneInstall,
		errInstall: "ArtifactCommit_Leave_00",
		testModuleAttr: tests.TestModuleAttr{
			ErrorStates:      []string{"ArtifactCommit_Leave_00"},
			RollbackDisabled: true,
		},
		installOutcome: tests.UnsuccessfulInstall,
	},

	standaloneModuleInstallCase{
		caseName: "Fail in ArtifactRollback_Enter_00",
		expectedLog: []string{
			"Download_Enter_00",
			"Download",
			"Download_Leave_00",
			"SupportsRollback",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"ArtifactInstall_Leave_00",
			"NeedsArtifactReboot",
			"SupportsRollback",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"ArtifactRollback_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		stage:       standaloneRollback,
		errRollback: "ArtifactRollback_Enter_00",
		testModuleAttr: tests.TestModuleAttr{
			ErrorStates: []string{"ArtifactRollback_Enter_00"},
		},
		installOutcome: tests.UnsuccessfulInstall,
	},

	standaloneModuleInstallCase{
		caseName: "Fail in ArtifactRollback",
		expectedLog: []string{
			"Download_Enter_00",
			"Download",
			"Download_Leave_00",
			"SupportsRollback",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"ArtifactInstall_Leave_00",
			"NeedsArtifactReboot",
			"SupportsRollback",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"ArtifactRollback_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		stage:       standaloneRollback,
		errRollback: updateModuleDefaultError,
		testModuleAttr: tests.TestModuleAttr{
			ErrorStates: []string{"ArtifactRollback"},
		},
		installOutcome: tests.UnsuccessfulInstall,
	},

	standaloneModuleInstallCase{
		caseName: "Fail in ArtifactRollback_Leave_00",
		expectedLog: []string{
			"Download_Enter_00",
			"Download",
			"Download_Leave_00",
			"SupportsRollback",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"ArtifactInstall_Leave_00",
			"NeedsArtifactReboot",
			"SupportsRollback",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"ArtifactRollback_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		stage:       standaloneRollback,
		errRollback: "ArtifactRollback_Leave_00",
		testModuleAttr: tests.TestModuleAttr{
			ErrorStates: []string{"ArtifactRollback_Leave_00"},
		},
		installOutcome: tests.UnsuccessfulInstall,
	},

	standaloneModuleInstallCase{
		caseName: "Fail in automatic ArtifactRollback_Enter_00",
		expectedLog: []string{
			"Download_Enter_00",
			"Download",
			"Download_Leave_00",
			"SupportsRollback",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"ArtifactInstall_Error_00",
			"SupportsRollback",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"ArtifactRollback_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		stage:      standaloneInstall,
		errInstall: updateModuleDefaultError,
		testModuleAttr: tests.TestModuleAttr{
			ErrorStates: []string{"ArtifactInstall", "ArtifactRollback_Enter_00"},
		},
		installOutcome: tests.UnsuccessfulInstall,
	},

	standaloneModuleInstallCase{
		caseName: "Fail in automatic ArtifactRollback",
		expectedLog: []string{
			"Download_Enter_00",
			"Download",
			"Download_Leave_00",
			"SupportsRollback",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"ArtifactInstall_Error_00",
			"SupportsRollback",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"ArtifactRollback_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		stage:      standaloneInstall,
		errInstall: updateModuleDefaultError,
		testModuleAttr: tests.TestModuleAttr{
			ErrorStates: []string{"ArtifactInstall", "ArtifactRollback"},
		},
		installOutcome: tests.UnsuccessfulInstall,
	},

	standaloneModuleInstallCase{
		caseName: "Fail in automatic ArtifactRollback_Leave_00",
		expectedLog: []string{
			"Download_Enter_00",
			"Download",
			"Download_Leave_00",
			"SupportsRollback",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"ArtifactInstall_Error_00",
			"SupportsRollback",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"ArtifactRollback_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		stage:      standaloneInstall,
		errInstall: updateModuleDefaultError,
		testModuleAttr: tests.TestModuleAttr{
			ErrorStates: []string{"ArtifactInstall", "ArtifactRollback_Leave_00"},
		},
		installOutcome: tests.UnsuccessfulInstall,
	},

	standaloneModuleInstallCase{
		caseName: "Hang in Download",
		expectedLog: []string{
			"Download_Enter_00",
			"Download",
			"Download_Error_00",
			"Cleanup",
		},
		stage:      standaloneInstall,
		errInstall: updateModuleDefaultError,
		testModuleAttr: tests.TestModuleAttr{
			HangStates: []string{"Download"},
		},
		installOutcome: tests.SuccessfulRollback,
	},

	standaloneModuleInstallCase{
		caseName: "Hang in ArtifactInstall",
		expectedLog: []string{
			"Download_Enter_00",
			"Download",
			"Download_Leave_00",
			"SupportsRollback",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"ArtifactInstall_Error_00",
			"SupportsRollback",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"ArtifactRollback_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		stage:      standaloneInstall,
		errInstall: updateModuleDefaultError,
		testModuleAttr: tests.TestModuleAttr{
			HangStates: []string{"ArtifactInstall"},
		},
		installOutcome: tests.SuccessfulRollback,
	},
}

func TestStandaloneModuleInstall(t *testing.T) {
	for _, c := range standaloneModuleInstallCases {
		t.Run(c.caseName, func(t *testing.T) {
			tmpdir, err := ioutil.TempDir("", "TestStandaloneModuleInstall")
			require.NoError(t, err)
			defer os.RemoveAll(tmpdir)

			logPath := path.Join(tmpdir, "execution.log")
			artPath := path.Join(tmpdir, "artifact.mender")

			device, stateExec := standaloneInstallSetup(t, tmpdir, &c.testModuleAttr,
				tests.ArtifactAttributeOverrides{})

			err = DoStandaloneInstall(device, artPath,
				client.Config{}, stateExec, false)
			if c.errInstall != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), c.errInstall)
			} else {
				assert.NoError(t, err)
			}

			maybeDoPostStandaloneInstall(t, &c, device, stateExec)

			log, err := os.Open(logPath)
			require.NoError(t, err)

			buf := make([]byte, 10000)
			n, err := log.Read(buf)
			require.NoError(t, err)

			logLines := strings.Split(strings.TrimRight(string(buf[:n]), "\n"), "\n")
			assert.Equal(t, c.expectedLog, logLines)

			artName, err := device.GetCurrentArtifactName()
			require.NoError(t, err)
			switch c.installOutcome {
			case tests.SuccessfulInstall:
				assert.Equal(t, "artifact-name", artName)
			case tests.SuccessfulRollback, tests.SuccessfulUncommitted:
				assert.Equal(t, "old_name", artName)
			case tests.UnsuccessfulInstall:
				assert.Equal(t, "artifact-name"+
					conf.BrokenArtifactSuffix, artName)
			default:
				assert.Truef(t, false, "installOutcome must be given for test case %s", c.caseName)
			}
		})
	}
}

func maybeDoPostStandaloneInstall(t *testing.T, c *standaloneModuleInstallCase,
	device *dev.DeviceManager, stateExec statescript.Executor) {

	var err error

	switch c.stage {
	case standaloneInstall:
		// Already done, nothing to do.
		return
	case standaloneCommit:
		err = DoStandaloneCommit(device, stateExec)
		if c.errCommit != "" {
			require.Error(t, err)
			assert.Contains(t, err.Error(), c.errCommit)
		} else {
			assert.NoError(t, err)
		}
	case standaloneRollback:
		err = DoStandaloneRollback(device, stateExec)
		if c.errRollback != "" {
			require.Error(t, err)
			assert.Contains(t, err.Error(), c.errRollback)
		} else {
			assert.NoError(t, err)
		}
	default:
		require.True(t, false, "Should not happen")
	}
}

// TestStandaloneStoreAndRestore tests that a standaloneStoreArtifactState, and
// restoreStandaloneData are inverse operations. Meaning that the data stored by
// the former is retrieved by the latter.
func TestStandaloneStoreAndRestore(t *testing.T) {

	dbdir, err := ioutil.TempDir("", "TestStanTestStandaloneStoreAndRestore")
	require.NoError(t, err)

	dbstore := store.NewDBStore(dbdir)

	defer os.RemoveAll(dbdir)

	config := conf.MenderConfig{}
	deviceType := zeroLengthDeviceTypeFile(t)
	defer os.Remove(deviceType)

	dualRootfsDevice := installer.NewDualRootfsDevice(nil, nil, conf.DualRootfsDeviceConfig{})

	tmgr := getTestDeviceManager(dualRootfsDevice, &config, deviceType, dbdir)

	tests := []struct {
		name       string
		sd         *standaloneData
		installers []installer.PayloadUpdatePerformer
	}{
		{
			name: "Persist ArtifactName",
			sd: &standaloneData{
				artifactName: "foobar",
				installers:   []installer.PayloadUpdatePerformer{},
			},
			installers: nil,
		},
		{
			name: "Persist ArtifactName and Artifact Group",
			sd: &standaloneData{
				artifactName:  "foobar",
				artifactGroup: "baz",
				installers:    []installer.PayloadUpdatePerformer{},
			},
			installers: nil,
		},
		{
			name: "Persist ArtifactName and Artifact Group and TypeInfo",
			sd: &standaloneData{
				artifactName:  "foobar",
				artifactGroup: "baz",
				artifactTypeInfoProvides: map[string]string{
					"bugs":  "bunny",
					"daffy": "duck",
				},
				installers: []installer.PayloadUpdatePerformer{},
			},
			installers: nil,
		},
	}

	for _, test := range tests {
		t.Logf("Running test: %v", test)
		// First transform
		assert.NoError(
			t,
			storeStandaloneData(dbstore, test.sd),
			"Failed to store the standaloneData: %v",
			test.sd,
		)

		sd, err := restoreStandaloneData(tmgr)
		assert.NoError(t, err, "Failed to restore the standaloneData")
		assert.EqualValues(t, test.sd, sd)
	}
}

func TestStandaloneInstallProvides(t *testing.T) {
	testCases := []struct {
		caseName            string
		overrides           tests.ArtifactAttributeOverrides
		preexistingProvides map[string]string
		expectedProvides    map[string]string
		commitErr           bool
	}{
		{
			caseName: "Upgrading with rootfs-image from old device",
			overrides: tests.ArtifactAttributeOverrides{
				TypeInfoV3: &artifact.TypeInfoV3{
					ArtifactProvides: artifact.TypeInfoProvides{
						"rootfs-image.version": "v1",
					},
					ClearsArtifactProvides: []string{
						"rootfs-image.*",
					},
				},
			},
			expectedProvides: map[string]string{
				"artifact_name":        "artifact-name",
				"rootfs-image.version": "v1",
			},
		},
		{
			caseName: "Normal rootfs-image",
			overrides: tests.ArtifactAttributeOverrides{
				TypeInfoV3: &artifact.TypeInfoV3{
					ArtifactProvides: artifact.TypeInfoProvides{
						"rootfs-image.version": "v1",
					},
					ClearsArtifactProvides: []string{
						"rootfs-image.*",
					},
				},
			},
			preexistingProvides: map[string]string{
				"rootfs-image.version": "v1",
			},
			expectedProvides: map[string]string{
				"artifact_name":        "artifact-name",
				"rootfs-image.version": "v1",
			},
		},
		{
			caseName: "Normal module-image",
			overrides: tests.ArtifactAttributeOverrides{
				TypeInfoV3: &artifact.TypeInfoV3{
					ArtifactProvides: artifact.TypeInfoProvides{
						"rootfs-image.single-file.version": "file1",
					},
					ClearsArtifactProvides: []string{
						"rootfs-image.single-file.*",
					},
				},
			},
			preexistingProvides: map[string]string{
				"rootfs-image.version": "v1",
			},
			expectedProvides: map[string]string{
				"artifact_name":                    "artifact-name",
				"rootfs-image.version":             "v1",
				"rootfs-image.single-file.version": "file1",
			},
		},
		{
			caseName: "rootfs-image, artifact_group preserved",
			overrides: tests.ArtifactAttributeOverrides{
				TypeInfoV3: &artifact.TypeInfoV3{
					ArtifactProvides: artifact.TypeInfoProvides{
						"rootfs-image.version": "v1",
					},
					ClearsArtifactProvides: []string{
						"rootfs-image.*",
					},
				},
			},
			preexistingProvides: map[string]string{
				"rootfs-image.version": "v1",
				"artifact_group":       "group-name",
			},
			expectedProvides: map[string]string{
				"artifact_name":        "artifact-name",
				"rootfs-image.version": "v1",
				"artifact_group":       "group-name",
			},
		},
		{
			caseName: "rootfs-image, new and existing artifact_group",
			overrides: tests.ArtifactAttributeOverrides{
				Provides: &artifact.ArtifactProvides{
					ArtifactName:  "artifact-name",
					ArtifactGroup: "new-group-name",
				},
				TypeInfoV3: &artifact.TypeInfoV3{
					ArtifactProvides: artifact.TypeInfoProvides{
						"rootfs-image.version": "v1",
					},
					ClearsArtifactProvides: []string{
						"rootfs-image.*",
					},
				},
			},
			preexistingProvides: map[string]string{
				"rootfs-image.version": "v1",
				"artifact_group":       "old-group-name",
			},
			expectedProvides: map[string]string{
				"artifact_name":        "artifact-name",
				"rootfs-image.version": "v1",
				"artifact_group":       "new-group-name",
			},
		},
		{
			caseName: "rootfs-image, new artifact_group",
			overrides: tests.ArtifactAttributeOverrides{
				Provides: &artifact.ArtifactProvides{
					ArtifactName:  "artifact-name",
					ArtifactGroup: "new-group-name",
				},
				TypeInfoV3: &artifact.TypeInfoV3{
					ArtifactProvides: artifact.TypeInfoProvides{
						"rootfs-image.version": "v1",
					},
					ClearsArtifactProvides: []string{
						"rootfs-image.*",
					},
				},
			},
			preexistingProvides: map[string]string{
				"rootfs-image.version": "v1",
			},
			expectedProvides: map[string]string{
				"artifact_name":        "artifact-name",
				"rootfs-image.version": "v1",
				"artifact_group":       "new-group-name",
			},
		},
		{
			caseName: "rootfs-image, artifact_group deleted",
			overrides: tests.ArtifactAttributeOverrides{
				TypeInfoV3: &artifact.TypeInfoV3{
					ArtifactProvides: artifact.TypeInfoProvides{
						"rootfs-image.version": "v1",
					},
					ClearsArtifactProvides: []string{
						"rootfs-image.*",
						"artifact_group",
					},
				},
			},
			preexistingProvides: map[string]string{
				"rootfs-image.version": "v1",
				"artifact_group":       "group-name",
			},
			expectedProvides: map[string]string{
				"artifact_name":        "artifact-name",
				"rootfs-image.version": "v1",
			},
		},
		{
			caseName: "rootfs-image, new and existing, deleted artifact_group",
			overrides: tests.ArtifactAttributeOverrides{
				Provides: &artifact.ArtifactProvides{
					ArtifactName:  "artifact-name",
					ArtifactGroup: "new-group-name",
				},
				TypeInfoV3: &artifact.TypeInfoV3{
					ArtifactProvides: artifact.TypeInfoProvides{
						"rootfs-image.version": "v1",
					},
					ClearsArtifactProvides: []string{
						"rootfs-image.*",
						"artifact_group",
					},
				},
			},
			preexistingProvides: map[string]string{
				"rootfs-image.version": "v1",
				"artifact_group":       "old-group-name",
			},
			expectedProvides: map[string]string{
				"artifact_name":        "artifact-name",
				"rootfs-image.version": "v1",
				"artifact_group":       "new-group-name",
			},
		},
		{
			caseName: "rootfs-image, new and nonexisting, deleted artifact_group",
			overrides: tests.ArtifactAttributeOverrides{
				Provides: &artifact.ArtifactProvides{
					ArtifactName:  "artifact-name",
					ArtifactGroup: "new-group-name",
				},
				TypeInfoV3: &artifact.TypeInfoV3{
					ArtifactProvides: artifact.TypeInfoProvides{
						"rootfs-image.version": "v1",
					},
					ClearsArtifactProvides: []string{
						"rootfs-image.*",
						"artifact_group",
					},
				},
			},
			preexistingProvides: map[string]string{
				"rootfs-image.version": "v1",
			},
			expectedProvides: map[string]string{
				"artifact_name":        "artifact-name",
				"rootfs-image.version": "v1",
				"artifact_group":       "new-group-name",
			},
		},
		{
			caseName: "Incorrect clears_artifact_provides expression",
			overrides: tests.ArtifactAttributeOverrides{
				TypeInfoV3: &artifact.TypeInfoV3{
					ArtifactProvides: artifact.TypeInfoProvides{
						"rootfs-image.version": "v1",
					},
					ClearsArtifactProvides: []string{
						"rootfs-image.*",
						"rootfs-image\\",
					},
				},
			},
			preexistingProvides: map[string]string{
				"rootfs-image.version": "v1",
			},
			commitErr: true,
		},
		{
			caseName: "nil clears_provides clears everything",
			overrides: tests.ArtifactAttributeOverrides{
				TypeInfoV3: &artifact.TypeInfoV3{
					ArtifactProvides: artifact.TypeInfoProvides{
						"rootfs-image.version": "v1",
					},
					ClearsArtifactProvides: nil,
				},
			},
			preexistingProvides: map[string]string{
				"artifact_name":        "old-name",
				"rootfs-image.version": "v1",
				"my-other-provide":     "some-value",
				"artifact_group":       "group-name",
			},
			expectedProvides: map[string]string{
				"artifact_name":        "artifact-name",
				"rootfs-image.version": "v1",
			},
		},
	}

	for _, c := range testCases {
		t.Run(c.caseName, func(t *testing.T) {
			tmpdir, err := ioutil.TempDir("", "TestStandaloneModuleInstall")
			require.NoError(t, err)
			defer os.RemoveAll(tmpdir)

			artPath := path.Join(tmpdir, "artifact.mender")

			device, stateExec := standaloneInstallSetup(
				t,
				tmpdir,
				&tests.TestModuleAttr{},
				c.overrides,
			)

			artifactGroup, hasArtifactGroup := c.preexistingProvides["artifact_group"]
			if hasArtifactGroup {
				delete(c.preexistingProvides, "artifact_group")
				err = device.Store.WriteAll(datastore.ArtifactGroupKey, []byte(artifactGroup))
				require.NoError(t, err)
				// Just make sure we did it correctly (test the test!)
				provides, err := device.GetProvides()
				require.NoError(t, err)
				require.Contains(t, provides, "artifact_group")
			}
			if c.preexistingProvides != nil {
				jsonBytes, err := json.Marshal(c.preexistingProvides)
				require.NoError(t, err)
				err = device.Store.WriteAll(datastore.ArtifactTypeInfoProvidesKey, jsonBytes)
				require.NoError(t, err)
			}

			err = DoStandaloneInstall(device, artPath,
				client.Config{}, stateExec, false)
			require.NoError(t, err)

			err = DoStandaloneCommit(device, stateExec)
			if c.commitErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			provides, err := device.GetProvides()
			require.NoError(t, err)
			assert.Equal(t, c.expectedProvides, provides)
		})
	}
}
