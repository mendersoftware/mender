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
	"io/ioutil"
	"time"

	"github.com/mendersoftware/log"
)

// Config section

//TODO: daemon configuration will be hardcoded now
const (
	// poll data from server every 3 minutes by default
	defaultServerpollInterval = time.Duration(3) * time.Minute
	defaultServerAddress      = "menderserver"
	defaultDeviceID           = "ABCD-12345"
	defaultAPIversion         = "0.0.1"
)

// daemon configuration
type daemonConfigType struct {
	serverpollInterval time.Duration
	server             string
	deviceID           string
}

func (daemon *menderDaemon) LoadConfig(configFile string) error {
	//TODO: change to properly load config from file
	var config daemonConfigType
	config.serverpollInterval = defaultServerpollInterval
	config.server = getMenderServer(configFile)
	config.deviceID = defaultDeviceID

	daemon.config = config
	return nil
}

func getMenderServer(serverFile string) string {
	// TODO: this should be taken from configuration or should be set at bootstrap
	server, err := ioutil.ReadFile(serverFile)

	log.Debug("Reading Mender server name from file " + serverFile)

	// return default server address if we can not read it from file
	if err != nil {
		log.Warn("Can not read server file " + err.Error())
		return defaultServerAddress
	}
	return string(server)
}

// Daemon section

type menderDaemon struct {
	Updater
	UInstallCommitRebooter
	Controler
	config      daemonConfigType
	stopChannel chan (bool)
}

func NewDaemon(client Updater, device UInstallCommitRebooter, mender Controler) *menderDaemon {
	if client == nil {
		// create client with configuration from file
		// client := NewClient(runOptions.authCmdLineArgsType)
		// if client == nil {
		// 	return errors.New("Error initializing client")
		// }
	}
	daemon := menderDaemon{client, device, mender, daemonConfigType{}, make(chan bool)}
	return &daemon
}

func (daemon menderDaemon) StopDaemon() {
	daemon.stopChannel <- true
}

func (daemon *menderDaemon) Run() error {
	// figure out the state
	switch daemon.GetState() {
	case MenderFreshInstall:
		//do nothing
	case MenderRunningWithFreshUpdate:
		daemon.CommitUpdate()
	}

	currentImageID := daemon.GetCurrentImageID()

	// create channels for timer and stopping daemon
	ticker := time.NewTicker(daemon.config.serverpollInterval)

	for {
		select {
		case <-ticker.C:
			var update interface{}
			log.Debug("Timer expired. Polling server to check update.")

			if updateID, haveUpdate :=
				checkScheduledUpdate(daemon, processUpdateResponse, update, daemon.config.server); haveUpdate {
				// we have update to be installed
				if currentImageID == updateID {
					// skip update as is the same as the running image id
					log.Info("Current image ID is the same as received from server. Skipping  OTA update.")
					continue
				}
				updateInstalled := fetchAndInstallUpdate(daemon, update.(UpdateResponse))

				//TODO: maybe stop daemon and clean
				// we have the update; now reboot the device
				if updateInstalled {
					return daemon.Reboot()
				}
			}

		case <-daemon.stopChannel:
			log.Debug("Attempting to stop daemon.")
			// exit daemon
			ticker.Stop()
			close(daemon.stopChannel)
			return nil
		}
	}
}

func checkScheduledUpdate(inst Updater, updProcess RequestProcessingFunc,
	data interface{}, server string) (string, bool) {

	data, err := inst.GetScheduledUpdate(updProcess, server)
	if err != nil {
		log.Error(err)
		return "", false
	}

	if update, ok := data.(*UpdateResponse); ok {
		return update.Image.ID, true
	}
	return "", false
}

func fetchAndInstallUpdate(daemon *menderDaemon, update UpdateResponse) bool {
	log.Debug("Have update to be fatched from: " + update.Image.URI)
	image, imageSize, err := daemon.FetchUpdate(update.Image.URI)
	if err != nil {
		log.Error(err)
		return false
	}

	log.Debug("Installing update to inactive partition.")
	err = daemon.InstallUpdate(image, imageSize)
	if err != nil {
		log.Error(err)
		return false
	}

	log.Info("Update instelled to inactive partition")
	if err := daemon.EnableUpdatedPartition(); err != nil {
		log.Error(err)
		return false
	}

	log.Debug("Inactive partition marked as first boot candidate.")
	return true
}
