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

const (
	// pull data from server every 5 minutes by default
	defaultServerPullInterval = 5 * 60
)

type daemonConfigType struct {
	serverPullInterval int
}

func (config *daemonConfigType) setPullInterval(interval int) {
	config.serverPullInterval = interval
}

func runAsDemon(config daemonConfigType, client *Client) error {
	// create channels for timer and stopping daemon
	ticker := time.NewTicker(time.Duration(config.serverPullInterval) * time.Second)
	quit := make(chan bool)

	for {
		select {
		case <-ticker.C:
			// do job here
			println("Timer expired")
			//client.Get()
			//quit <- true
		case <-quit:
			// exit daemon
			ticker.Stop()

			return nil
		}
	}

	return nil
}
