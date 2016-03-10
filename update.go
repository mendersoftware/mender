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
	"net/http"

	"github.com/mendersoftware/log"
)

// possible API responses received for update request
const (
	updateRespponseHaveUpdate = 200
	updateResponseNoUpdates   = 204
	updateResponseError       = 404
)

type responseParserFunc func(response http.Response, respBody []byte) (dataActuator, error)

type updateRequester struct {
	reqType              string
	request              string
	menderClient         Client
	updateResponseParser responseParserFunc
}

// implementation of clientWorker interface
func (ur updateRequester) getClient() Client {
	return ur.menderClient
}

func (ur updateRequester) formatRequest() clientRequestType {
	return clientRequestType{ur.reqType, ur.request}
}

func (ur updateRequester) actOnResponse(response http.Response, respBody []byte) error {
	data, err := ur.updateResponseParser(response, respBody)
	if err != nil {
		return err
	}
	return data.actOnData()
}

// Current API is supporting different responses from the server for update request.
// Each of the data structures received after sending update request needs to
// implement dataActuator interface
type dataActuator interface {
	actOnData() error
}

// have update for the client
type updateHaveUpdateResponseType struct {
	Image struct {
		URI      string
		Checksum string
		ID       string
	}
	ID string
}

func (resp updateHaveUpdateResponseType) actOnData() error {
	// perform update of the device
	return doRootfs(resp.Image.URI)
}

// there is no update for the device
type updateNoUpdateResponseType struct {
	// empty for now
}

func (resp updateNoUpdateResponseType) actOnData() error {
	return nil
}

// there was an error geting update information
type updateErrorResponseType struct {
	//empty for now
}

func (resp updateErrorResponseType) actOnData() error {
	return nil
}

func parseUpdateResponse(response http.Response, respBody []byte) (dataActuator, error) {

	log.Debug("Received response:", response.Status)

	switch response.StatusCode {
	case updateRespponseHaveUpdate:
		log.Debug("Have update available")

		var data updateHaveUpdateResponseType
		if err := json.Unmarshal(respBody, &data); err != nil {
			switch err.(type) {
			case *json.SyntaxError:
				return updateHaveUpdateResponseType{}, errors.New("Error parsing data syntax")
			}
			return updateHaveUpdateResponseType{}, errors.New("Error parsing data: " + err.Error())
		}

		// check if we have JSON data correctky decoded
		if data.ID != "" && data.Image.ID != "" && data.Image.Checksum != "" && data.Image.URI != "" {
			log.Info("Received correct request for getting image from: " + data.Image.URI)
			return data, nil
		}

		return updateHaveUpdateResponseType{}, errors.New("Mallformed update response")

	case updateResponseNoUpdates:
		log.Debug("No update available")

		//TODO: check body to see if message is mallformed
		return updateNoUpdateResponseType{}, nil

	case updateResponseError:
		//TODO: check body to see if message is mallformed
		return updateErrorResponseType{}, nil

	default:
		return nil, errors.New("Invalid response received from server")

	}
	// ureachable
}
