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

// daemon configuration
type daemonConfig struct {
	serverpollInterval time.Duration
	serverURL          string
	deviceID           string
}

// Daemon section

type menderDaemon struct {
	mender      Controller
	config      daemonConfig
	stopChannel chan (bool)
	stop        bool
}

func NewDaemon(mender Controller, config daemonConfig) *menderDaemon {

	daemon := menderDaemon{
		mender:      mender,
		config:      config,
		stopChannel: make(chan bool),
	}
	return &daemon
}

func (daemon menderDaemon) StopDaemon() {
	daemon.stopChannel <- true
}

func (daemon *menderDaemon) Run() error {
	// figure out the state
	for {
		switch daemon.mender.TransitionState() {
		case MenderStateRunningWithFreshUpdate:
			daemon.mender.CommitUpdate()
		case MenderStateUpdateCheckWait:
			err := daemon.waitForUpdate()
			if err != nil {
				return err
			}
		case MenderStateError:
			log.Errorf("entered error state due to: %s", daemon.mender.LastError())
			return daemon.mender.LastError()
		}

		if daemon.stop {
			return nil
		}
	}
}

func (daemon *menderDaemon) waitForUpdate() error {
	// create channels for timer and stopping daemon
	ticker := time.NewTicker(daemon.config.serverpollInterval)

	for {
		select {
		case <-ticker.C:
			log.Debug("Timer expired. Polling server to check update.")

			update, err := daemon.mender.CheckUpdate()
			if err != nil {
				log.Errorf("failed to check update availability: %v", err)
				return err
			}

			if update == nil {
				log.Debugf("no new updates")
				continue
			}

			updateInstalled := fetchAndInstallUpdate(daemon, update)

			//TODO: maybe stop daemon and clean
			// we have the update; now reboot the device
			if updateInstalled {
				return daemon.mender.Reboot()
			}

		case <-daemon.stopChannel:
			log.Debug("Attempting to stop daemon.")
			// exit daemon
			ticker.Stop()
			close(daemon.stopChannel)
			daemon.stop = true
			return nil
		}
	}
}

func fetchAndInstallUpdate(daemon *menderDaemon, update *UpdateResponse) bool {
	log.Debug("Have update to be fatched from: " + update.Image.URI)
	image, imageSize, err := daemon.mender.FetchUpdate(update.Image.URI)
	if err != nil {
		log.Error("Can not fetch update: ", err)
		return false
	}

	log.Debug("Installing update to inactive partition.")
	if err := daemon.mender.InstallUpdate(image, imageSize); err != nil {
		log.Error("Can not install update: ", err)
		return false
	}

	log.Info("Update installed to inactive partition")
	if err := daemon.mender.EnableUpdatedPartition(); err != nil {
		log.Error("Error enabling inactive partition: ", err)
		return false
	}

	log.Debug("Inactive partition marked as first boot candidate.")
	return true
}
