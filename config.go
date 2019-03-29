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
	"encoding/json"
	"io/ioutil"
	"os"
	"strings"

	"github.com/mendersoftware/log"
	"github.com/mendersoftware/mender/client"
	"github.com/mendersoftware/mender/installer"
	"github.com/pkg/errors"
)

type menderConfigFromFile struct {
	// ClientProtocol "https"
	ClientProtocol string
	// Path to the public key used to verify signed updates
	ArtifactVerifyKey string
	// HTTPS client parameters
	HttpsClient struct {
		Certificate string
		Key         string
		SkipVerify  bool
	}
	// Rootfs device path
	RootfsPartA string
	RootfsPartB string

	// Poll interval for checking for new updates
	UpdatePollIntervalSeconds int
	// Poll interval for periodically sending inventory data
	InventoryPollIntervalSeconds int

	// Global retry polling max interval for fetching update, authorize wait and update status
	RetryPollIntervalSeconds int

	// State script parameters
	StateScriptTimeoutSeconds      int
	StateScriptRetryTimeoutSeconds int
	// Poll interval for checking for update (check-update)
	StateScriptRetryIntervalSeconds int

	// Update module parameters:

	// The timeout for the execution of the update module, after which it
	// will be killed.
	ModuleTimeoutSeconds int

	// Path to server SSL certificate
	ServerCertificate string
	// Server URL (For single server conf)
	ServerURL string
	// Path to deployment log file
	UpdateLogPath string
	// Server JWT TenantToken
	TenantToken string
	// List of available servers, to which client can fall over
	Servers []client.MenderServer
}

type menderConfig struct {
	menderConfigFromFile

	// Additional fields that are in our config struct for convenience, but
	// not actually configurable via the config file.
	ModulesPath      string
	ModulesWorkPath  string
	ArtifactInfoFile string

	ArtifactScriptsPath string
	RootfsScriptsPath   string
}

func NewMenderConfig() *menderConfig {
	return &menderConfig{
		ModulesPath:         defaultModulesPath,
		ModulesWorkPath:     defaultModulesWorkPath,
		ArtifactInfoFile:    defaultArtifactInfoFile,
		ArtifactScriptsPath: defaultArtScriptsPath,
		RootfsScriptsPath:   defaultRootfsScriptsPath,
	}
}

// loadConfig parses the mender configuration json-files
// (/etc/mender/mender.conf and /var/lib/mender/mender.conf) and loads the
// values into the menderConfig structure defining high level client
// configurations.
func loadConfig(mainConfigFile string, fallbackConfigFile string) (*menderConfig, error) {
	// Load fallback configuration first, then main configuration.
	// It is OK if either file does not exist, so long as the other one does exist.
	// It is also OK if both files exist.
	// Because the main configuration is loaded last, its option values
	// override those from the fallback file, for options present in both files.

	var filesLoadedCount int
	config := NewMenderConfig()

	if loadErr := loadConfigFile(fallbackConfigFile, config, &filesLoadedCount); loadErr != nil {
		return nil, loadErr
	}

	if loadErr := loadConfigFile(mainConfigFile, config, &filesLoadedCount); loadErr != nil {
		return nil, loadErr
	}

	if filesLoadedCount == 0 {
		log.Info("No configuration files present. Using defaults")
		return config, nil
	}

	if config.Servers == nil {
		if config.ServerURL == "" {
			log.Warn("No server URL(s) specified in mender configuration.")
		}
		config.Servers = make([]client.MenderServer, 1)
		config.Servers[0].ServerURL = config.ServerURL
	} else if config.ServerURL != "" {
		log.Error("In mender.conf: don't specify both Servers field " +
			"AND the corresponding fields in base structure (i.e. " +
			"ServerURL). The first server on the list on the" +
			"list overwrites these fields.")
		return nil, errors.New("Both Servers AND ServerURL given in " +
			"mender.conf")
	}
	for i := 0; i < len(config.Servers); i++ {
		// Trim possible '/' suffix, which is added back in URL path
		if strings.HasSuffix(config.Servers[i].ServerURL, "/") {
			config.Servers[i].ServerURL =
				strings.TrimSuffix(
					config.Servers[i].ServerURL, "/")
		}
		if config.Servers[i].ServerURL == "" {
			log.Warnf("Server entry %d has no associated server URL.", i+1)
		}
	}

	log.Debugf("Merged configuration = %#v", config)

	return config, nil
}

func loadConfigFile(configFile string, config *menderConfig, filesLoadedCount *int) error {
	// Do not treat a single config file not existing as an error here.
	// It is up to the caller to fail when both config files don't exist.
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		log.Debug("Configuration file does not exist: ", configFile)
		return nil
	}

	if err := readConfigFile(&config.menderConfigFromFile, configFile); err != nil {
		log.Errorf("Error loading configuration from file: %s (%s)", configFile, err.Error())
		return err
	}

	(*filesLoadedCount)++
	log.Info("Loaded configuration file: ", configFile)
	return nil
}

func readConfigFile(config interface{}, fileName string) error {
	// Reads mender configuration (JSON) file.

	log.Debug("Reading Mender configuration from file " + fileName)
	conf, err := ioutil.ReadFile(fileName)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(conf, &config); err != nil {
		switch err.(type) {
		case *json.SyntaxError:
			return errors.New("Error parsing mender configuration file: " + err.Error())
		}
		return errors.New("Error parsing config file: " + err.Error())
	}

	return nil
}

func (c *menderConfig) GetHttpConfig() client.Config {
	return client.Config{
		ServerCert: c.ServerCertificate,
		IsHttps:    c.ClientProtocol == "https",
		NoVerify:   c.HttpsClient.SkipVerify,
	}
}

func (c *menderConfig) GetDeviceConfig() installer.DualRootfsDeviceConfig {
	return installer.DualRootfsDeviceConfig{
		RootfsPartA: c.RootfsPartA,
		RootfsPartB: c.RootfsPartB,
	}
}

func (c *menderConfig) GetDeploymentLogLocation() string {
	return c.UpdateLogPath
}

// GetTenantToken returns a default tenant-token if
// no custom token is set in local.conf
func (c *menderConfig) GetTenantToken() []byte {
	return []byte(c.TenantToken)
}

func (c *menderConfig) GetVerificationKey() []byte {
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
