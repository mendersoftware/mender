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
	"net/http"
	"time"
)
import "github.com/mendersoftware/log"

var daemonQuit = make(chan bool)

const (
	// pull data from server every 5 minutes by default
	defaultServerPullInterval = time.Duration(5) * time.Minute
	defaultServerAddress      = "127.0.0.1"
	defaultDeviceID           = "1234:5678:90ab:cdef"
)

type daemonConfigType struct {
	serverPullInterval time.Duration
	server             string
	deviceID           string
}

func (config *daemonConfigType) setPullInterval(interval time.Duration) {
	config.serverPullInterval = interval
}

func (config *daemonConfigType) setServerAddress(server string) {
	//TODO: check if starts with https://
	config.server = server
}

func (config *daemonConfigType) setDeviceID() {
	//TODO: get it from somewhere
	config.deviceID = defaultDeviceID
}

type updateRequester struct {
	reqType      string
	request      string
	menderClient client
}

func (ur updateRequester) getClient() client {
	return ur.menderClient
}

func (ur updateRequester) formatRequest() clientRequestType {
	return clientRequestType{ur.reqType, ur.request}
}

type updateAPIResponseType struct {
	Image struct {
		URI       string
		Chaecksum string
		ID        string
	}
	ID string
}

const (
	updateRespponseHaveUpdate = 200
	updateResponseNoUpdates   = 204
	updateResponseError       = 404
)

func (ur updateRequester) parseResponse(response http.Response, respBody []byte) error {
	// TODO: do something with the stuff received
	log.Error("Received response:", response.Status)
	switch response.StatusCode {
	case updateRespponseHaveUpdate:
		log.Error("Have update available")

		var data updateAPIResponseType
		if err := json.Unmarshal(respBody, &data); err != nil {
			log.Error("Error parsing data -> " + err.Error())
			switch err.(type) {
			case *json.SyntaxError:
				log.Error("Error parsing data syntax")
			}
		}

		if data.Image.URI != "" {
			// get the image
			log.Error("Getting image from: " + data.Image.URI)
			return doRootfs(data.Image.URI)
		}

		log.Error("Empty image URI")

	case updateResponseNoUpdates:
		log.Debug("No update available")
	case updateResponseError:

	default:

	}

	return nil
}

func runAsDaemon(config daemonConfigType, client client) error {
	// create channels for timer and stopping daemon
	ticker := time.NewTicker(config.serverPullInterval)

	updateRequester := updateRequester{
		reqType:      http.MethodGet,
		request:      config.server + "/" + config.deviceID + "/update",
		menderClient: client,
	}

	for {
		select {
		case <-ticker.C:

			log.Debug("Timer expired. Pulling server to check update.")
			err := makeJobDone(updateRequester)
			if err != nil {
				log.Error(err)
			}

		case <-daemonQuit:
			log.Debug("Attempting to stop daemon.")
			// exit daemon
			ticker.Stop()
			return nil
		}
	}
}
