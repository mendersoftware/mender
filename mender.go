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
	"encoding/json"
	"errors"
	"io/ioutil"
	"time"

	"github.com/mendersoftware/log"
)

type Controler interface {
	GetState() MenderState
	GetCurrentImageID() string
	GetDaemonConfig() daemonConfig
	GetUpdaterConfig() httpsClientConfig
}

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
	BootEnvReadWritter
	state  MenderState
	config menderFileConfig
}

func NewMender(env BootEnvReadWritter) *mender {
	mender := mender{}
	mender.BootEnvReadWritter = env
	return &mender
}

func (m *mender) GetState() MenderState {
	if err := m.updateState(); err != nil {
		m.state = MenderStateUnknown
	}
	return m.state
}

// TODO:
func (mender) GetCurrentImageID() string {
	_, err := ioutil.ReadFile("/etc/something")
	if err != nil {
		return ""
	}

	//TODO: process file

	return ""
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

	if err := readCongigFile(&confFromFile, configFile); err != nil {
		// Some error occured while loading config file.
		// Use default configuration.
		log.Infof("Error loading configuration from file: %s (%s)", configFile, err.Error())
		return err
	}

	// We have configuration from file.
	// daemon.config.serverpollInterval =
	// 	time.Duration(confFromFile.PollIntervalSeconds) * time.Second
	// daemon.config.serverURL = confFromFile.ServerURL
	// daemon.config.deviceID = defaultDeviceID
	m.config = confFromFile
	return nil
}

func (m mender) GetUpdaterConfig() httpsClientConfig {
	return httpsClientConfig{
		m.config.HttpsClient.Certificate,
		m.config.HttpsClient.Key,
		m.config.ServerCertificate,
	}
}

func (m mender) GetDaemonConfig() daemonConfig {
	return daemonConfig{
		time.Duration(m.config.PollIntervalSeconds) * time.Second,
		m.config.ServerURL,
		defaultDeviceID,
	}
}

func readCongigFile(config interface{}, fileName string) error {
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
