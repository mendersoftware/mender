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
	"net/http"

	"github.com/mendersoftware/log"
)

// possible API responses received for update request
const (
	updateRespponseHaveUpdate = 200
	updateResponseNoUpdates   = 204
	updateResponseError       = 404
)

// have update for the client
type UpdateResponse struct {
	Image struct {
		URI      string
		Checksum string
		ID       string
	}
	ID string
}

func ProcessUpdateResponse(response *http.Response, data interface{}) error {
	log.Debug("Received response:", response.Status)

	respBody, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return err
	}

	switch response.StatusCode {
	case updateRespponseHaveUpdate:
		log.Debug("Have update available")

		if err := json.Unmarshal(respBody, data); err != nil {
			switch err.(type) {
			case *json.SyntaxError:
				return errors.New("Error parsing data syntax")
			}
			return errors.New("Error parsing data: " + err.Error())
		}
		return nil

	case updateResponseNoUpdates:
		log.Debug("No update available")
		return nil

	case updateResponseError:
		return nil

	default:
		return errors.New("Invalid response received from server")
	}
}

func (c *Client) GetUpdate(url string) error {
	r, err := c.MakeRequest(http.MethodGet, url)
	if err != nil {
		return err
	}

	defer r.Body.Close()

	var update UpdateResponse
	err = ProcessUpdateResponse(r, &update)

	if err != nil {
		return err
	}

	if r.StatusCode == updateRespponseHaveUpdate {
		// check if we have JSON data correctky decoded
		if update.ID != "" && update.Image.ID != "" && update.Image.Checksum != "" && update.Image.URI != "" {
			log.Info("Received correct request for getting image from: " + update.Image.URI)
			return nil
		}
		return errors.New("Missing parameters in encoded JSON response")
	}

	return nil
}
