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
package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/mendersoftware/log"
	"github.com/mendersoftware/mender/datastore"
	"github.com/mendersoftware/mender/installer"
	"github.com/mendersoftware/mender/statescript"
	"github.com/mendersoftware/mender/store"
	"github.com/pkg/errors"
)

var (
	defaultArtifactInfoFile  = path.Join(getConfDirPath(), "artifact_info")
	defaultDeviceTypeFile    = path.Join(getStateDirPath(), "device_type")
	defaultArtScriptsPath    = path.Join(getStateDirPath(), "scripts")
	defaultRootfsScriptsPath = path.Join(getConfDirPath(), "scripts")
	defaultModulesPath       = path.Join(getDataDirPath(), "modules")
	defaultModulesWorkPath   = path.Join(getStateDirPath(), "modules")
)

const (
	brokenArtifactSuffix = "_INCONSISTENT"
)

type deviceManager struct {
	installers          []installer.PayloadInstaller
	installerFactories  installer.PayloadInstallerProducers
	stateScriptExecutor statescript.Executor
	stateScriptPath     string
	config              menderConfig
	artifactInfoFile    string
	deviceTypeFile      string
	store               store.Store
}

func NewDeviceManager(dualRootfsDevice dualRootfsDevice, config *menderConfig, store store.Store) *deviceManager {
	d := &deviceManager{
		artifactInfoFile: config.ArtifactInfoFile,
		deviceTypeFile:   defaultDeviceTypeFile,
		config:           *config,
		stateScriptPath:  config.ArtifactScriptsPath,
		store:            store,
	}
	if d.stateScriptPath == "" {
		d.stateScriptPath = defaultArtScriptsPath
	}
	if d.artifactInfoFile == "" {
		d.artifactInfoFile = defaultArtifactInfoFile
	}
	d.installerFactories = installer.PayloadInstallerProducers{
		DualRootfs: dualRootfsDevice,
		Modules: installer.NewModuleInstallerFactory(config.ModulesPath,
			config.ModulesWorkPath, d, d, config.ModuleTimeoutSeconds),
	}

	return d
}

func newStateScriptExecutor(config *menderConfig) statescript.Launcher {
	ret := statescript.Launcher{
		ArtScriptsPath:          config.ArtifactScriptsPath,
		RootfsScriptsPath:       config.RootfsScriptsPath,
		SupportedScriptVersions: []int{2, 3},
		Timeout:                 config.StateScriptTimeoutSeconds,
		RetryTimeout:            config.StateScriptRetryIntervalSeconds,
		RetryInterval:           config.StateScriptRetryTimeoutSeconds,
	}
	if ret.ArtScriptsPath == "" {
		ret.ArtScriptsPath = defaultArtScriptsPath
	}
	if ret.RootfsScriptsPath == "" {
		ret.RootfsScriptsPath = defaultRootfsScriptsPath
	}
	return ret
}

func getManifestData(dataType, manifestFile string) (string, error) {
	// This is where Yocto stores buid information
	manifest, err := os.Open(manifestFile)
	if err != nil {
		return "", err
	}

	var found *string
	scanner := bufio.NewScanner(manifest)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#") {
			// Comments.
			continue
		}

		log.Debug("Read data from device manifest file: ", line)
		lineID := strings.SplitN(line, "=", 2)
		if len(lineID) != 2 {
			log.Errorf("Broken device manifest file: (%v)", lineID)
			return "", fmt.Errorf("Broken device manifest file: (%v)", lineID)
		}
		if lineID[0] == dataType {
			log.Debug("Found needed line: ", line)
			log.Debug("Current manifest data: ", strings.TrimSpace(lineID[1]))
			if found != nil {
				return "", errors.Errorf("More than one instance of %s found in manifest file %s.",
					dataType, manifestFile)
			}
			str := strings.TrimSpace(lineID[1])
			found = &str
		}
	}
	err = scanner.Err()
	if err != nil {
		log.Error(err)
		return "", err
	}
	if found == nil {
		return "", nil
	} else {
		return *found, nil
	}
}

func (d *deviceManager) GetCurrentArtifactName() (string, error) {
	if d.store != nil {
		dbname, err := d.store.ReadAll(datastore.ArtifactNameKey)
		if err == nil {
			name := string(dbname)
			log.Debugf("Returning artifact name %s from database.", name)
			return name, nil
		} else if err != os.ErrNotExist {
			log.Errorf("Could not read artifact name from database: %s", err.Error())
		}
	}
	log.Debugf("Returning artifact name from %s file.", d.artifactInfoFile)
	return getManifestData("artifact_name", d.artifactInfoFile)
}

func (d *deviceManager) GetCurrentArtifactGroup() (string, error) {
	return getManifestData("artifact_group", d.artifactInfoFile)
}

func (d *deviceManager) GetDeviceType() (string, error) {
	return getManifestData("device_type", d.deviceTypeFile)
}

func (d *deviceManager) GetArtifactVerifyKey() []byte {
	return d.config.GetVerificationKey()
}

func GetDeviceType(deviceTypeFile string) (string, error) {
	return getManifestData("device_type", deviceTypeFile)
}

func (d *deviceManager) ReadArtifactHeaders(from io.ReadCloser) (*installer.Installer, error) {

	deviceType, err := d.GetDeviceType()
	if err != nil {
		log.Errorf("Unable to verify the existing hardware. Update will continue anyways: %v : %v", defaultDeviceTypeFile, err)
	}

	var i *installer.Installer
	i, d.installers, err = installer.ReadHeaders(from,
		deviceType,
		d.GetArtifactVerifyKey(),
		d.stateScriptPath,
		&d.installerFactories)
	return i, err
}

func (d *deviceManager) GetInstallers() []installer.PayloadInstaller {
	return d.installers
}

func (d *deviceManager) RestoreInstallersFromTypeList(payloadTypes []string) error {
	var err error
	d.installers, err = installer.CreateInstallersFromList(&d.installerFactories, payloadTypes)
	return err
}
