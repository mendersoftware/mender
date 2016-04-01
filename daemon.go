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
	"time"

	"github.com/mendersoftware/log"
)

// Config section

//TODO: some of daemon configuration will be hardcoded now
const (
	defaultDeviceID   = "ABCD-12345"
	defaultAPIversion = "0.0.1"
)

// daemon configuration
type daemonConfig struct {
	serverpollInterval time.Duration
	serverURL          string
	deviceID           string
}

// Daemon section

type menderDaemon struct {
	Updater
	UInstallCommitRebooter
	Controler
	config      daemonConfig
	stopChannel chan (bool)
}

func NewDaemon(client Updater, device UInstallCommitRebooter,
	mender Controler) *menderDaemon {

	config := mender.GetDaemonConfig()

	daemon := menderDaemon{
		Updater:                client,
		UInstallCommitRebooter: device,
		Controler:              mender,
		config:                 config,
		stopChannel:            make(chan bool),
	}
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
	//TODO: if currentImageID == "" {
	// 	return errors.New("")
	// }

	// create channels for timer and stopping daemon
	ticker := time.NewTicker(daemon.config.serverpollInterval)

	for {
		select {
		case <-ticker.C:
			var update UpdateResponse
			log.Debug("Timer expired. Polling server to check update.")

			if updateID, haveUpdate :=
				checkScheduledUpdate(daemon, processUpdateResponse, &update,
					daemon.config.serverURL, daemon.config.deviceID); haveUpdate {
				// we have update to be installed
				if currentImageID == updateID {
					// skip update as is the same as the running image id
					log.Info("Current image ID is the same as received from server. Skipping  OTA update.")
					continue
				}

				updateInstalled := fetchAndInstallUpdate(daemon, update)

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
	data *UpdateResponse, server string, deviceID string) (string, bool) {

	haveUpdate, err := inst.GetScheduledUpdate(updProcess, server, deviceID)
	if err != nil {
		log.Error("Error receiving scheduled update data: ", err)
		return "", false
	}

	log.Debug("Received correct response for update request.")

	if update, ok := haveUpdate.(UpdateResponse); ok {
		*data = update
		return update.Image.YoctoID, true
	}
	return "", false
}

func fetchAndInstallUpdate(daemon *menderDaemon, update UpdateResponse) bool {
	log.Debug("Have update to be fatched from: " + update.Image.URI)
	image, imageSize, err := daemon.FetchUpdate(update.Image.URI)
	if err != nil {
		log.Error("Can not fetch update: ", err)
		return false
	}

	log.Debug("Installing update to inactive partition.")
	if err := daemon.InstallUpdate(image, imageSize); err != nil {
		log.Error("Can not install update: ", err)
		return false
	}

	log.Info("Update installed to inactive partition")
	if err := daemon.EnableUpdatedPartition(); err != nil {
		log.Error("Error enabling inactive partition: ", err)
		return false
	}

	log.Debug("Inactive partition marked as first boot candidate.")
	return true
}
