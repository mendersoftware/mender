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
	"path"
	"time"

	common "github.com/mendersoftware/mender/common/conf"
	log "github.com/sirupsen/logrus"
)

const (
	DefaultUpdateControlMapBootExpirationTimeSeconds = 600
	DefaultAuthTimeoutSeconds                        = 300
	DefaultAuthTimeout                               = DefaultAuthTimeoutSeconds * time.Second

	BrokenArtifactSuffix = "_INCONSISTENT"
)

var (
	// device specific paths
	DefaultArtifactInfoFile  = path.Join(common.GetConfDirPath(), "artifact_info")
	DefaultArtScriptsPath    = path.Join(common.GetStateDirPath(), "scripts")
	DefaultRootfsScriptsPath = path.Join(common.GetConfDirPath(), "scripts")
	DefaultModulesPath       = path.Join(common.GetDataDirPath(), "modules", "v3")
	DefaultModulesWorkPath   = path.Join(common.GetStateDirPath(), "modules", "v3")
)

type MenderConfigFromFile struct {
	common.Config

	// Path to the public key used to verify signed updates
	ArtifactVerifyKey string `json:",omitempty"`

	// Rootfs device path
	RootfsPartA string `json:",omitempty"`
	RootfsPartB string `json:",omitempty"`

	// Command to set active partition.
	BootUtilitiesSetActivePart string `json:",omitempty"`
	// Command to get the partition which will boot next.
	BootUtilitiesGetNextActivePart string `json:",omitempty"`

	// Path to the device type file
	DeviceTypeFile string `json:",omitempty"`

	// Expiration timeout for the control map
	UpdateControlMapExpirationTimeSeconds int `json:",omitempty"`
	// Expiration timeout for the control map when just booted
	UpdateControlMapBootExpirationTimeSeconds int `json:",omitempty"`

	// Poll interval for checking for new updates
	UpdatePollIntervalSeconds int `json:",omitempty"`
	// Poll interval for periodically sending inventory data
	InventoryPollIntervalSeconds int `json:",omitempty"`

	// Global retry polling max interval for fetching update, authorize wait and update status
	RetryPollIntervalSeconds int `json:",omitempty"`

	// State script parameters
	StateScriptTimeoutSeconds      int `json:",omitempty"`
	StateScriptRetryTimeoutSeconds int `json:",omitempty"`
	// Poll interval for checking for update (check-update)
	StateScriptRetryIntervalSeconds int `json:",omitempty"`

	// Update module parameters:

	// The timeout for the execution of the update module, after which it
	// will be killed.
	ModuleTimeoutSeconds int `json:",omitempty"`

	// Path to deployment log file
	UpdateLogPath string `json:",omitempty"`
}

type MenderConfig struct {
	MenderConfigFromFile

	// Additional fields that are in our config struct for convenience, but
	// not actually configurable via the config file.
	ModulesPath      string
	ModulesWorkPath  string
	ArtifactInfoFile string

	ArtifactScriptsPath string
	RootfsScriptsPath   string

	// The time to wait for the auth manager to send a new JWT token after
	// sending a FetchJwtToken request.
	AuthTimeoutSeconds int
}

func NewMenderConfig() *MenderConfig {
	return &MenderConfig{
		MenderConfigFromFile: MenderConfigFromFile{
			Config: *common.NewConfig(),
		},
		ModulesPath:         DefaultModulesPath,
		ModulesWorkPath:     DefaultModulesWorkPath,
		ArtifactInfoFile:    DefaultArtifactInfoFile,
		ArtifactScriptsPath: DefaultArtScriptsPath,
		RootfsScriptsPath:   DefaultRootfsScriptsPath,
		AuthTimeoutSeconds:  DefaultAuthTimeoutSeconds,
	}
}

func (c *MenderConfigFromFile) GetUpdateControlMapExpirationTimeSeconds() int {
	if c.UpdateControlMapExpirationTimeSeconds == 0 {
		return 2 * c.UpdatePollIntervalSeconds
	}
	return c.UpdateControlMapExpirationTimeSeconds
}

func (c *MenderConfigFromFile) GetUpdateControlMapBootExpirationTimeSeconds() int {
	if c.UpdateControlMapBootExpirationTimeSeconds == 0 {
		return DefaultUpdateControlMapBootExpirationTimeSeconds
	}
	return c.UpdateControlMapBootExpirationTimeSeconds
}

func (c *MenderConfigFromFile) CheckConfigDefaults() {
	if c.UpdateControlMapExpirationTimeSeconds == 0 {
		log.Info("'UpdateControlMapExpirationTimeSeconds' is not set " +
			"in the Mender configuration file." +
			" Falling back to the default of 2*UpdatePollIntervalSeconds")
	}

	if c.UpdateControlMapBootExpirationTimeSeconds == 0 {
		log.Infof("'UpdateControlMapBootExpirationTimeSeconds' is not set "+
			"in the Mender configuration file."+
			" Falling back to the default of %d seconds", DefaultUpdateControlMapBootExpirationTimeSeconds)
	}

	if !c.DBus.Enabled {
		log.Warn(`Support for turning off DBus has been removed. "DBus.Enabled: false" setting ignored.`)
	}
}

type DualRootfsDeviceConfig struct {
	RootfsPartA string
	RootfsPartB string
}

func (c *MenderConfig) GetDeviceConfig() DualRootfsDeviceConfig {
	return DualRootfsDeviceConfig{
		RootfsPartA: c.RootfsPartA,
		RootfsPartB: c.RootfsPartB,
	}
}

func (c *MenderConfig) GetDeploymentLogLocation() string {
	return c.UpdateLogPath
}

func (c *MenderConfig) GetVerificationKey() []byte {
	if c.ArtifactVerifyKey == "" {
		return nil
	}
	key, err := ioutil.ReadFile(c.ArtifactVerifyKey)
	if err != nil {
		log.Info("config: error reading artifact verify key")
		return nil
	}
	return key
}
