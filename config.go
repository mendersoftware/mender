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
	"strings"

	"github.com/mendersoftware/log"
	"github.com/mendersoftware/mender/client"
	"github.com/pkg/errors"
)

type menderServer struct {
	// menderServer: Placeholder for a full server definition
	//               used when multiple servers are given.
	// Entries are the same as in menderConfig
	ServerURL   string
	TenantToken string
}

type menderConfig struct {
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

	// Path to server SSL certificate
	ServerCertificate string
	// (Active) Server URL
	ServerURL string
	// Path to deployment log file
	UpdateLogPath string
	// Server JWT TenantToken
	TenantToken string
	// List of available servers, which client can Failover^{TM} to
	Servers []menderServer
}

func LoadConfig(configFile string) (*menderConfig, error) {
	// Build menderConfig struct from mender.conf json file.

	var confFromFile menderConfig

	if err := readConfigFile(&confFromFile, configFile); err != nil {
		// Some error occured while loading config file.
		// Use default configuration.
		log.Infof("Error loading configuration from file: %s (%s)", configFile, err.Error())
		return nil, err
	}

	// Remove possible trailing '/'.
	if strings.HasSuffix(confFromFile.ServerURL, "/") {
		confFromFile.ServerURL = strings.TrimSuffix(confFromFile.ServerURL, "/")
	}

	return &confFromFile, nil
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
