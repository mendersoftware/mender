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

import "time"
import "github.com/mendersoftware/log"

var daemonQuit = make(chan bool)

const (
	// pull data from server every 5 minutes by default
	defaultServerPullInterval = time.Duration(5) * time.Minute
	defaultServerAddress      = "127.0.0.1"
	defaultDeviceId           = "1234:5678:90ab:cdef"
)

type daemonConfigType struct {
	serverPullInterval time.Duration
	server             string
	deviceId           string
}

func (config *daemonConfigType) setPullInterval(interval time.Duration) {
	config.serverPullInterval = interval
}

func (config *daemonConfigType) setServerAddress(server string) {
	//TODO: check if starts with https://
	config.server = server
}

func (config *daemonConfigType) setDeviceId() {
	//TODO: get it from somewhere
	config.deviceId = defaultDeviceId
}

func runAsDemon(config daemonConfigType, client *Client) error {
	// create channels for timer and stopping daemon
	ticker := time.NewTicker(config.serverPullInterval)

	for {
		select {
		case <-ticker.C:
			// do job here
			log.Debug("Timer expired. Pulling server to check update.")
			err, response := client.sendRequest(GET, config.server+"/"+config.deviceId+"/update")
			if err != nil {
				log.Error(err)
				continue
			}
			client.parseUpdateTesponse(response)
			//quit <- true
		case <-daemonQuit:
			log.Debug("Attempting to stop daemon.")
			// exit daemon
			ticker.Stop()
			return nil
		}
	}

	return nil
}
