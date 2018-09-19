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
	// Server URL (For single server conf)
	ServerURL string
	// Path to deployment log file
	UpdateLogPath string
	// Server JWT TenantToken
	TenantToken string
	// List of available servers, to which client can fall over
	Servers []client.MenderServer
}

// LoadConfig parses the mender configuration json-file (/etc/mender/mender.conf)
// and loads the values into the menderConfig structure defining high level client
// configurations.
func LoadConfig(configFile string) (*menderConfig, error) {

	var confFromFile menderConfig

	if err := readConfigFile(&confFromFile, configFile); err != nil {
		// Some error occured while loading config file.
		// Use default configuration.
		log.Infof("Error loading configuration from file: %s (%s)", configFile, err.Error())
		return nil, err
	}

	if confFromFile.Servers == nil {
		if confFromFile.ServerURL == "" {
			log.Warn("No server URL(s) specified in mender configuration.")
		}
		confFromFile.Servers = make([]client.MenderServer, 1)
		confFromFile.Servers[0].ServerURL = confFromFile.ServerURL
	} else if confFromFile.ServerURL != "" {
		log.Error("In mender.conf: don't specify both Servers field " +
			"AND the corresponding fields in base structure (i.e. " +
			"ServerURL). The first server on the list on the" +
			"list overwrites these fields.")
		return nil, errors.New("Both Servers AND ServerURL given in " +
			"mender.conf")
	}
	for i := 0; i < len(confFromFile.Servers); i++ {
		// Trim possible '/' suffix, which is added back in URL path
		if strings.HasSuffix(confFromFile.Servers[i].ServerURL, "/") {
			confFromFile.Servers[i].ServerURL =
				strings.TrimSuffix(
					confFromFile.Servers[i].ServerURL, "/")
		}
		if confFromFile.Servers[i].ServerURL == "" {
			log.Warnf("Server entry %d has no associated server URL.")
		}
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
