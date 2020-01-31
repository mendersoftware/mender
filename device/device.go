// Copyright 2020 Northern.tech AS
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
package device

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mendersoftware/log"
	"github.com/mendersoftware/mender/conf"
	"github.com/mendersoftware/mender/datastore"
	"github.com/mendersoftware/mender/installer"
	"github.com/mendersoftware/mender/statescript"
	"github.com/mendersoftware/mender/store"
	"github.com/pkg/errors"
)

type DeviceManager struct {
	ArtifactInfoFile    string
	Config              conf.MenderConfig
	DeviceTypeFile      string
	Installers          []installer.PayloadUpdatePerformer
	InstallerFactories  installer.AllModules
	StateScriptExecutor statescript.Executor
	StateScriptPath     string
	Store               store.Store
}

func NewDeviceManager(dualRootfsDevice installer.DualRootfsDevice, config *conf.MenderConfig, store store.Store) *DeviceManager {
	d := &DeviceManager{
		ArtifactInfoFile: config.ArtifactInfoFile,
		DeviceTypeFile:   config.DeviceTypeFile,
		Config:           *config,
		StateScriptPath:  config.ArtifactScriptsPath,
		Store:            store,
	}
	d.InstallerFactories = installer.AllModules{
		DualRootfs: dualRootfsDevice,
		Modules: installer.NewModuleInstallerFactory(config.ModulesPath,
			config.ModulesWorkPath, d, d, config.ModuleTimeoutSeconds),
	}

	return d
}

func NewStateScriptExecutor(config *conf.MenderConfig) statescript.Launcher {
	ret := statescript.Launcher{
		ArtScriptsPath:          config.ArtifactScriptsPath,
		RootfsScriptsPath:       config.RootfsScriptsPath,
		SupportedScriptVersions: []int{2, 3},
		Timeout:                 config.StateScriptTimeoutSeconds,
		RetryInterval:           config.StateScriptRetryIntervalSeconds,
		RetryTimeout:            config.StateScriptRetryTimeoutSeconds,
	}
	return ret
}

func GetManifestData(dataType, manifestFile string) (string, error) {
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

func (d *DeviceManager) GetProvides() (map[string]interface{}, error) {
	return datastore.LoadProvides(d.Store)
}

func (d *DeviceManager) GetCurrentArtifactName() (string, error) {
	if d.Store != nil {
		dbname, err := d.Store.ReadAll(datastore.ArtifactNameKey)
		if err == nil {
			name := string(dbname)
			log.Debugf("Returning artifact name %s from database.", name)
			return name, nil
		} else if err != os.ErrNotExist {
			log.Errorf("Could not read artifact name from database: %s", err.Error())
		}
	}
	log.Debugf("Returning artifact name from %s file.", d.ArtifactInfoFile)
	return GetManifestData("artifact_name", d.ArtifactInfoFile)
}

func (d *DeviceManager) GetCurrentArtifactGroup() (string, error) {
	return GetManifestData("artifact_group", d.ArtifactInfoFile)
}

func (d *DeviceManager) GetDeviceType() (string, error) {
	return GetDeviceType(d.DeviceTypeFile)
}

func (d *DeviceManager) GetArtifactVerifyKey() []byte {
	return d.Config.GetVerificationKey()
}

func GetDeviceType(deviceTypeFile string) (string, error) {
	return GetManifestData("device_type", deviceTypeFile)
}

func (d *DeviceManager) ReadArtifactHeaders(from io.ReadCloser) (*installer.Installer, error) {

	deviceType, err := d.GetDeviceType()
	if err != nil {
		log.Errorf("Unable to verify the existing hardware. Update will continue anyway: %v : %v", d.Config.DeviceTypeFile, err)
	}

	var i *installer.Installer
	i, d.Installers, err = installer.ReadHeaders(from,
		deviceType,
		d.GetArtifactVerifyKey(),
		d.StateScriptPath,
		&d.InstallerFactories)
	return i, err
}

func (d *DeviceManager) GetInstallers() []installer.PayloadUpdatePerformer {
	return d.Installers
}

func (d *DeviceManager) RestoreInstallersFromTypeList(payloadTypes []string) error {
	var err error
	d.Installers, err = installer.CreateInstallersFromList(&d.InstallerFactories, payloadTypes)
	return err
}
