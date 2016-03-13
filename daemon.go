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
	"strings"
	"time"

	"github.com/mendersoftware/log"
)

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

func getMenderServer(serverFile string) string {
	// TODO: this should be taken from configuration or should be set at bootstrap
	server, err := ioutil.ReadFile(serverFile)

	log.Debug("Reading Mender server name from file " + serverFile)

	// return default server address if we can not read it from file
	if err != nil {
		log.Warn("Can not read server file " + err.Error())
		// let's use http by default for now
		return "http://" + defaultServerAddress
	}

	menderServer := string(server)
	// check if server name is also specifying the protocol used
	if !strings.HasPrefix(menderServer, "http") {
		menderServer = "http://" + menderServer
	}

	return menderServer
}

// needs to implement clientRequester interface
type menderDaemon struct {
	//updater     updateRequester
	client      *Client
	config      daemonConfigType
	stopChannel chan (bool)
}

func (daemon menderDaemon) StopDaaemon() {
	daemon.stopChannel <- true
}

func runAsDaemon(daemon menderDaemon) error {

	// create channels for timer and stopping daemon
	ticker := time.NewTicker(daemon.config.serverpollInterval)

	for {
		select {
		case <-ticker.C:

			log.Debug("Timer expired. polling server to check update.")
			imageURL, err := daemon.client.GetUpdate(daemon.config.server)
			if err != nil {
				log.Error(err)
			}
			// fetch update if there is one
			if imageURL != "" {
				if err := doRootfs(imageURL); err != nil {
					// we have the update; now reboot the device
					// runner.run("reboot")
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
