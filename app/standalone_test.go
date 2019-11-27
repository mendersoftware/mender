// Copyright 2019 Northern.tech AS
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
	"io"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/mendersoftware/mender/client"
	"github.com/mendersoftware/mender/conf"
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

func Test_doManualUpdate_noParams_fail(t *testing.T) {
	dbdir, err := ioutil.TempDir("", "menderDbdir")
	require.NoError(t, err)
	defer os.RemoveAll(dbdir)

	config := conf.MenderConfig{}
	deviceType := zeroLengthDeviceTypeFile(t)
	defer os.Remove(deviceType)

	dualRootfsDevice := installer.NewDualRootfsDevice(nil, nil, installer.DualRootfsDeviceConfig{})
	if err := DoStandaloneInstall(getTestDeviceManager(dualRootfsDevice, &config, deviceType, dbdir),
		"", client.Config{}, nil, dev.NewStateScriptExecutor(&config)); err == nil {

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
	dualRootfsDevice := installer.NewDualRootfsDevice(nil, nil, installer.DualRootfsDeviceConfig{})
	if err := DoStandaloneInstall(getTestDeviceManager(
		dualRootfsDevice, &config, deviceType, dbdir),
		imageFile, runOptions, nil,
		dev.NewStateScriptExecutor(&config)); err == nil {

		t.FailNow()
	}
}

func Test_doManualUpdate_nonExistingFile_fail(t *testing.T) {
	dbdir, err := ioutil.TempDir("", "menderDbdir")
	require.NoError(t, err)
	defer os.RemoveAll(dbdir)

	fakeDevice := installer.NewDualRootfsDevice(nil, nil, installer.DualRootfsDeviceConfig{})
	imageFile := "non-existing"
	deviceType := zeroLengthDeviceTypeFile(t)
	defer os.Remove(deviceType)

	config := conf.MenderConfig{}
	if err := DoStandaloneInstall(getTestDeviceManager(
		fakeDevice, &config, deviceType, dbdir),
		imageFile, client.Config{}, nil,
		dev.NewStateScriptExecutor(&config)); err == nil {

		t.FailNow()
	}
}

func Test_doManualUpdate_networkUpdateNoClient_fail(t *testing.T) {
	dbdir, err := ioutil.TempDir("", "menderDbdir")
	require.NoError(t, err)
	defer os.RemoveAll(dbdir)

	fakeDevice := installer.NewDualRootfsDevice(nil, nil, installer.DualRootfsDeviceConfig{})
	imageFile := "http://non-existing"
	deviceType := zeroLengthDeviceTypeFile(t)
	defer os.Remove(deviceType)

	config := conf.MenderConfig{}
	if err := DoStandaloneInstall(getTestDeviceManager(fakeDevice, &config, deviceType, dbdir),
		imageFile, client.Config{}, nil, dev.NewStateScriptExecutor(&config)); err == nil {

		t.FailNow()
	}
}

func Test_doManualUpdate_networkClientExistsNoServer_fail(t *testing.T) {
	dbdir, err := ioutil.TempDir("", "menderDbdir")
	require.NoError(t, err)
	defer os.RemoveAll(dbdir)

	fakeDevice := installer.NewDualRootfsDevice(nil, nil, installer.DualRootfsDeviceConfig{})
	imageFile := "http://non-existing"
	deviceType := zeroLengthDeviceTypeFile(t)
	defer os.Remove(deviceType)

	fakeClientConfig := client.Config{
		ServerCert: "server.crt",
		IsHttps:    true,
		NoVerify:   false,
	}

	config := conf.MenderConfig{}
	if err := DoStandaloneInstall(getTestDeviceManager(
		fakeDevice, &config, deviceType, dbdir),
		imageFile, fakeClientConfig, nil,
		dev.NewStateScriptExecutor(&config)); err == nil {

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
		nil, dev.NewStateScriptExecutor(&config))
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

			moduleDir := path.Join(tmpdir, "modules")
			logPath := path.Join(tmpdir, "execution.log")
			artPath := path.Join(tmpdir, "artifact.mender")

			require.NoError(t, os.MkdirAll(moduleDir, 0755))

			tests.UpdateModulesSetup(t, &c.testModuleAttr, tmpdir)

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
			require.NoError(t, os.MkdirAll(dbstorePath, 755))
			dbstore := store.NewDBStore(dbstorePath)
			device := dev.NewDeviceManager(nil, &config, dbstore)
			device.DeviceTypeFile = path.Join(tmpdir, "device_type")
			device.ArtifactInfoFile = path.Join(tmpdir, "artifact_info")

			err = DoStandaloneInstall(device, artPath,
				client.Config{}, nil, stateExec)
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
