// Copyright 2016 Mender Software AS
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
	"encoding/json"
	"errors"
	"io/ioutil"
	"os"
	"strings"
	"time"

	"github.com/mendersoftware/log"
)

type Controler interface {
	GetState() MenderState
	GetCurrentImageID() string
	GetDaemonConfig() daemonConfig
	GetUpdaterConfig() httpsClientConfig
}

const (
	defaultManifestFile = "/etc/build_mender"
)

type MenderState int

const (
	MenderStateUnknown MenderState = iota

	MenderFreshInstall
	//MenderBootstrapped
	//MenderUpdateInstalled
	//MenderUpdateFailed
	MenderRunningWithFreshUpdate
	//MenderUpdateBroken
	//MenderSuccesfulyUpdated
	//MenderNormalRun
)

type mender struct {
	BootEnvReadWriter
	state        MenderState
	config       menderFileConfig
	manifestFile string
}

func NewMender(env BootEnvReadWriter) *mender {
	mender := mender{}
	mender.BootEnvReadWriter = env
	mender.manifestFile = defaultManifestFile
	return &mender
}

func (m *mender) GetState() MenderState {
	if err := m.updateState(); err != nil {
		m.state = MenderStateUnknown
	}
	log.Debugf("Mender state: %v", m.state)
	return m.state
}

func (m mender) GetCurrentImageID() string {
	// This is where Yocto stores buid information
	manifest, err := os.Open(m.manifestFile)
	if err != nil {
		log.Error("Can not read current image id.")
		return ""
	}

	imageID := ""

	scanner := bufio.NewScanner(manifest)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		log.Debug("Read data from device manifest file: ", line)
		if strings.HasPrefix(line, "IMAGE_ID") {
			log.Debug("Found device id line: ", line)
			lineID := strings.Split(line, "=")
			if len(lineID) != 2 {
				log.Errorf("Broken device manifest file: (%v)", lineID)
				return ""
			}
			log.Debug("Current image id: ", strings.TrimSpace(lineID[1]))
			return strings.TrimSpace(lineID[1])
		}
	}
	if err := scanner.Err(); err != nil {
		log.Error(err)
	}
	return imageID
}

func (m *mender) updateState() error {
	env, err := m.ReadEnv("upgrade_available")
	if err != nil {
		return err
	}
	upgradeAvailable := env["upgrade_available"]

	// we are after update
	if upgradeAvailable == "1" {
		m.state = MenderRunningWithFreshUpdate
		return nil
	}
	m.state = MenderFreshInstall
	return nil
}

type menderFileConfig struct {
	PollIntervalSeconds int
	DeviceID            string
	ServerURL           string
	ServerCertificate   string
	ClientProtocol      string

	HttpsClient struct {
		Certificate string
		Key         string
	}
}

func (m *mender) LoadConfig(configFile string) error {
	var confFromFile menderFileConfig

	if err := readConfigFile(&confFromFile, configFile); err != nil {
		// Some error occured while loading config file.
		// Use default configuration.
		log.Infof("Error loading configuration from file: %s (%s)", configFile, err.Error())
		return err
	}

	m.config = confFromFile
	return nil
}

func (m mender) GetUpdaterConfig() httpsClientConfig {
	return httpsClientConfig{
		m.config.HttpsClient.Certificate,
		m.config.HttpsClient.Key,
		m.config.ServerCertificate,
		m.config.ClientProtocol == "https",
	}
}

func (m mender) GetDaemonConfig() daemonConfig {
	return daemonConfig{
		time.Duration(m.config.PollIntervalSeconds) * time.Second,
		m.config.ServerURL,
		m.config.DeviceID,
	}
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
			return err
		}
		return errors.New("Error parsing config file: " + err.Error())
	}
	return nil
}
