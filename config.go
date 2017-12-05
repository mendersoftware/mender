// Copyright 2017 Northern.tech AS
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

const defaultTenantToken = "authtentoken"

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

func LoadConfig(configFile string) (*menderConfig, error) {
	var confFromFile menderConfig

	if err := readConfigFile(&confFromFile, configFile); err != nil {
		// Some error occured while loading config file.
		// Use default configuration.
		log.Infof("Error loading configuration from file: %s (%s)", configFile, err.Error())
		return nil, err
	}

	if strings.HasSuffix(confFromFile.ServerURL, "/") {
		confFromFile.ServerURL = strings.TrimSuffix(confFromFile.ServerURL, "/")
	}

	return &confFromFile, nil
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
	if c.TenantToken != "" {
		return []byte(c.TenantToken)
	}
	return []byte(defaultTenantToken)
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
