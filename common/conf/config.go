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
	"encoding/json"
	"io/ioutil"
	"os"

	"github.com/mendersoftware/mender/common/tls"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

type Config struct {
	// DBus configuration
	DBus DBusConfig `json:",omitempty"`

	// HTTPS client parameters
	HttpsClient tls.HttpsClient `json:",omitempty"`
	// Security parameters
	Security tls.Security `json:",omitempty"`

	// Skip CA certificate validation
	SkipVerify bool `json:",omitempty"`

	// Path to server SSL certificate
	ServerCertificate string `json:",omitempty"`
}

type DBusConfig struct {
	Enabled bool
}

func NewConfig() *Config {
	// It's no longer possible to turn this off, but set it to true, since
	// it is warned about if set to false in the config file.
	return &Config{
		DBus: DBusConfig{
			Enabled: true,
		},
	}
}

func (c *Config) GetHttpConfig() tls.Config {
	return tls.Config{
		ServerCert: c.ServerCertificate,
		// The HttpsClient config is only loaded when both a cert and
		// key is given
		HttpsClient: maybeHTTPSClient(c),
		NoVerify:    c.SkipVerify,
	}
}

func maybeHTTPSClient(c *Config) *tls.HttpsClient {
	if c.HttpsClient.Certificate != "" && c.HttpsClient.Key != "" {
		c.HttpsClient.Validate()
		return &c.HttpsClient
	}
	return nil
}

type ConfigWithDefaultsChecker interface {
	CheckConfigDefaults()
}

// LoadConfig parses the mender configuration json-files
// (/etc/mender/mender.conf and /var/lib/mender/mender.conf) and loads the
// values into the outConfig structure defining high level client
// configurations.
func LoadConfig(mainConfigFile string, fallbackConfigFile string,
	outConfig ConfigWithDefaultsChecker) error {
	// Load fallback configuration first, then main configuration.
	// It is OK if either file does not exist, so long as the other one does exist.
	// It is also OK if both files exist.
	// Because the main configuration is loaded last, its option values
	// override those from the fallback file, for options present in both files.

	var filesLoadedCount int

	if loadErr := loadConfigFile(fallbackConfigFile, outConfig, &filesLoadedCount); loadErr != nil {
		return loadErr
	}

	if loadErr := loadConfigFile(mainConfigFile, outConfig, &filesLoadedCount); loadErr != nil {
		return loadErr
	}

	log.Debugf("Loaded %d configuration file(s)", filesLoadedCount)

	outConfig.CheckConfigDefaults()

	if filesLoadedCount == 0 {
		log.Info("No configuration files present. Using defaults")
		return nil
	}

	log.Debugf("Loaded %T configuration = %#v", outConfig, outConfig)

	return nil
}

func loadConfigFile(configFile string, outConfig interface{}, filesLoadedCount *int) error {
	// Do not treat a single config file not existing as an error here.
	// It is up to the caller to fail when both config files don't exist.
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		log.Debug("Configuration file does not exist: ", configFile)
		return nil
	}

	if err := readConfigFile(outConfig, configFile); err != nil {
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

func (c *Config) CheckConfigDefaults() {
	// Nothing to check for Config.
}
