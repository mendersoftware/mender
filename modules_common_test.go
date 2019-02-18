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

// Common functions for tests that test update modules, or use update modules to
// test other areas.

package main

import (
	"fmt"
	"os"
	"path"
	"testing"

	"github.com/mendersoftware/mender-artifact/artifact"
	"github.com/mendersoftware/mender-artifact/awriter"
	"github.com/mendersoftware/mender-artifact/handlers"
	"github.com/stretchr/testify/require"
)

const (
	successfulInstallUnknown = iota
	successfulInstall
	successfulRollback
	successfulUncommitted
	unsuccessfulInstall
)

type installOutcome int

type testModuleAttr struct {
	errorStates        []string
	errorForever       bool
	spontRebootStates  []string
	spontRebootForever bool
	hangStates         []string
	rollbackDisabled   bool
	rebootDisabled     bool
}

func makeImageForUpdateModules(t *testing.T, path string, scripts artifact.Scripts) {
	depends := artifact.ArtifactDepends{
		CompatibleDevices: []string{"test-device"},
	}
	provides := artifact.ArtifactProvides{
		ArtifactName: "artifact-name",
	}
	typeInfoV3 := artifact.TypeInfoV3{
		Type: "test-module",
	}
	upd := awriter.Updates{
		Updates: []handlers.Composer{handlers.NewModuleImage("test-type")},
	}
	args := awriter.WriteArtifactArgs{
		Format:            "mender",
		Version:           3,
		Devices:           []string{"test-device"},
		Name:              "test-name",
		Updates:           &upd,
		Scripts:           &scripts,
		Depends:           &depends,
		Provides:          &provides,
		TypeInfoV3:        &typeInfoV3,
		MetaData:          nil,
		AugmentTypeInfoV3: nil,
		AugmentMetaData:   nil,
	}

	fd, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	require.NoError(t, err)
	defer fd.Close()
	writer := awriter.NewWriter(fd, artifact.NewCompressorGzip())

	require.NoError(t, writer.WriteArtifact(&args))
}

func makeTestUpdateModule(t *testing.T, path, logPath string,
	attr *testModuleAttr) {

	fd, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0755)
	require.NoError(t, err)
	defer fd.Close()

	fd.Write([]byte(fmt.Sprintf(`#!/bin/bash
echo "$1" >> %s
`, logPath)))

	fd.Write([]byte("if [ \"$1\" = \"SupportsRollback\" ]; then\n"))
	if attr.rollbackDisabled {
		fd.Write([]byte("echo No\n"))
	} else {
		fd.Write([]byte("echo Yes\n"))
	}
	fd.Write([]byte("fi\n"))

	fd.Write([]byte("if [ \"$1\" = \"NeedsArtifactReboot\" ]; then\n"))
	if attr.rebootDisabled {
		fd.Write([]byte("echo No\n"))
	} else {
		fd.Write([]byte("echo Yes\n"))
	}
	fd.Write([]byte("fi\n"))

	// Kill parent (mender) in specified state
	for _, state := range attr.spontRebootStates {
		s := fmt.Sprintf("if [ \"$1\" = \"%s\" ]; then\n", state)
		fd.Write([]byte(s))

		// Prevent spontaneous rebooting forever.
		if !attr.spontRebootForever {
			fd.Write([]byte("if [ ! -e \"$2/tmp/$1.already-killed\" ]; then\n"))
			fd.Write([]byte("touch \"$2/tmp/$1.already-killed\"\n"))
		}

		fd.Write([]byte("kill -9 $PPID\n"))

		if !attr.spontRebootForever {
			fd.Write([]byte("fi\n"))
		}

		fd.Write([]byte("fi\n"))
	}

	// Produce error in specified state
	for _, state := range attr.errorStates {
		s := fmt.Sprintf("if [ \"$1\" = \"%s\" ]; then\n", state)
		fd.Write([]byte(s))

		// Prevent returning same error forever.
		if !attr.errorForever {
			fd.Write([]byte("if [ ! -e \"$2/tmp/$1.already-errored\" ]; then\n"))
			fd.Write([]byte("touch \"$2/tmp/$1.already-errored\"\n"))
		}

		fd.Write([]byte("exit 1\n"))

		if !attr.errorForever {
			fd.Write([]byte("fi\n"))
		}

		fd.Write([]byte("fi\n"))
	}

	// Hang in specified state
	for _, state := range attr.hangStates {
		s := fmt.Sprintf("if [ \"$1\" = \"%s\" ]; then\n", state)
		fd.Write([]byte(s))

		fd.Write([]byte("sleep 120\n"))

		fd.Write([]byte("fi\n"))
	}

	fd.Write([]byte("exit 0\n"))
}

func stateInList(state string, list []string) bool {
	for _, s := range list {
		if s == state {
			return true
		}
	}
	return false
}

func makeTestArtifactScripts(t *testing.T,
	attr *testModuleAttr,
	tmpdir, logPath string) artifact.Scripts {

	var artScripts artifact.Scripts

	stateScriptList := []string{
		"Download",
		"ArtifactInstall",
		"ArtifactReboot",
		"ArtifactCommit",
		"ArtifactRollback",
		"ArtifactRollbackReboot",
		"ArtifactFailure",
	}

	scriptsDir := path.Join(tmpdir, "scriptdir")
	require.NoError(t, os.MkdirAll(scriptsDir, 0755))
	fd, err := os.OpenFile(path.Join(scriptsDir, "version"),
		os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	require.NoError(t, err)
	fd.Write([]byte("3"))
	fd.Close()

	for _, state := range stateScriptList {
		for _, enterLeave := range []string{"Enter", "Leave", "Error"} {
			scriptFile := fmt.Sprintf("%s_%s_00", state, enterLeave)
			scriptPath := path.Join(scriptsDir, scriptFile)
			if state != "Download" {
				require.NoError(t, artScripts.Add(scriptPath))
			}

			fd, err := os.OpenFile(scriptPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
			require.NoError(t, err)
			defer fd.Close()

			fd.Write([]byte("#!/bin/bash\n"))
			fd.Write([]byte(fmt.Sprintf("echo %s >> %s\n",
				scriptFile, logPath)))

			if stateInList(scriptFile, attr.errorStates) {
				if !attr.errorForever {
					fd.Write([]byte(fmt.Sprintf("if [ ! -e \"%s/%s.already-errored\" ]; then\n",
						tmpdir, scriptFile)))
					fd.Write([]byte(fmt.Sprintf("touch \"%s/%s.already-errored\"\n",
						tmpdir, scriptFile)))
				}
				fd.Write([]byte("exit 1\n"))
				if !attr.errorForever {
					fd.Write([]byte("fi\n"))
				}
			}

			if stateInList(scriptFile, attr.spontRebootStates) {
				if !attr.spontRebootForever {
					fd.Write([]byte(fmt.Sprintf("if [ ! -e \"%s/%s.already-killed\" ]; then\n",
						tmpdir, scriptFile)))
					fd.Write([]byte(fmt.Sprintf("touch \"%s/%s.already-killed\"\n",
						tmpdir, scriptFile)))
				}
				fd.Write([]byte("kill -9 $PPID\n"))
				if !attr.spontRebootForever {
					fd.Write([]byte("fi\n"))
				}
			}

			fd.Write([]byte("exit 0\n"))
		}
	}

	return artScripts
}

func updateModulesSetup(t *testing.T, attr *testModuleAttr, tmpdir string) {
	logPath := path.Join(tmpdir, "execution.log")

	require.NoError(t, os.MkdirAll(path.Join(tmpdir, "var/lib/mender"), 0755))
	require.NoError(t, os.MkdirAll(path.Join(tmpdir, "etc/mender"), 0755))

	scripts := makeTestArtifactScripts(t, attr, tmpdir, logPath)

	artPath := path.Join(tmpdir, "artifact.mender")
	makeImageForUpdateModules(t, artPath, scripts)

	require.NoError(t, os.Mkdir(path.Join(tmpdir, "logs"), 0755))

	require.NoError(t, os.Mkdir(path.Join(tmpdir, "db"), 0755))

	modulesPath := path.Join(tmpdir, "modules")
	require.NoError(t, os.MkdirAll(modulesPath, 0755))
	makeTestUpdateModule(t, path.Join(modulesPath, "test-type"), logPath, attr)

	deviceTypeFile := path.Join(tmpdir, "device_type")
	deviceTypeFd, err := os.OpenFile(deviceTypeFile, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	require.NoError(t, err)
	defer deviceTypeFd.Close()
	deviceTypeFd.Write([]byte("device_type=test-device\n"))

	artifactInfoFd, err := os.OpenFile(path.Join(tmpdir, "artifact_info"),
		os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	require.NoError(t, err)
	defer artifactInfoFd.Close()
	artifactInfoFd.Write([]byte("artifact_name=old_name\n"))
}
