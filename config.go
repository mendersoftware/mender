// Copyright 2018 Northern.tech AS
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
	"github.com/pkg/errors"
)

type menderConfig struct {
	ClientProtocol    string
	ArtifactVerifyKey string
	HttpsClient       struct {
		Certificate string
		Key         string
		SkipVerify  bool
	}
	RootfsPartA                     string
	RootfsPartB                     string
	UpdatePollIntervalSeconds       int
	InventoryPollIntervalSeconds    int
	RetryPollIntervalSeconds        int
	StateScriptTimeoutSeconds       int
	StateScriptRetryTimeoutSeconds  int
	StateScriptRetryIntervalSeconds int
	ServerURL                       string
	ServerCertificate               string
	UpdateLogPath                   string
	TenantToken                     string
}

func loadConfig(mainConfigFile string, fallbackConfigFile string) (*menderConfig, error) {
	// Load fallback configuration first, then main configuration.
	// It is OK if either file does not exist, so long as the other one does exist.
	// It is also OK if both files exist.
	// Because the main configuration is loaded last, its option values
	// override those from the fallback file, for options present in both files.

	var filesLoadedCount int
	var config menderConfig

	if loadErr := loadConfigFile(fallbackConfigFile, &config, &filesLoadedCount); loadErr != nil {
		return nil, loadErr
	}

	if loadErr := loadConfigFile(mainConfigFile, &config, &filesLoadedCount); loadErr != nil {
		return nil, loadErr
	}

	if filesLoadedCount == 0 {
		return nil, errors.New("could not find either configuration file")
	}

	// Normalize the server URL to remove trailing slash if present.
	if strings.HasSuffix(config.ServerURL, "/") {
		config.ServerURL = strings.TrimSuffix(config.ServerURL, "/")
	}

	log.Debugf("Merged configuration = %#v", config)

	return &config, nil
}

func loadConfigFile(configFile string, config *menderConfig, filesLoadedCount *int) error {
	// Do not treat a single config file not existing as an error here.
	// It is up to the caller to fail when both config files don't exist.
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		log.Info("Configuration file does not exist: ", configFile)
		return nil
	}

	if err := readConfigFile(&config, configFile); err != nil {
		log.Errorf("Error loading configuration from file: %s (%s)", configFile, err.Error())
		return err
	}

	(*filesLoadedCount)++
	log.Info("Loaded configuration file: ", configFile)
	return nil
}

func readConfigFile(config interface{}, fileName string) error {
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

func (c menderConfig) GetHttpConfig() client.Config {
	return client.Config{
		ServerCert: c.ServerCertificate,
		IsHttps:    c.ClientProtocol == "https",
		NoVerify:   c.HttpsClient.SkipVerify,
	}
}

func (c menderConfig) GetDeviceConfig() deviceConfig {
	return deviceConfig{
		rootfsPartA: c.RootfsPartA,
		rootfsPartB: c.RootfsPartB,
	}
}

func (c menderConfig) GetDeploymentLogLocation() string {
	return c.UpdateLogPath
}

// GetTenantToken returns a default tenant-token if
// no custom token is set in local.conf
func (c menderConfig) GetTenantToken() []byte {
	return []byte(c.TenantToken)
}

func (c menderConfig) GetVerificationKey() []byte {
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
