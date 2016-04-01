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
	"errors"
	"net/http"

	"github.com/mendersoftware/log"
)

var (
	ErrorBootstrapFailed   = errors.New("Bootstraping failed")
	ErrorBootstrapNoClient = errors.New("Error initializing client for bootstrapping to server.")
)

func (c *httpsClient) Bootstrap(server string) error {

	r, err := c.makeAndSendRequest(http.MethodGet, server)

	if err != nil {
		return err
	}

	return processBootstrapResponse(r, nil)
}

// This will be called from the command line ONLY
func doBootstrap(conf httpsClientConfig, server string) error {
	// for bootstrapping we need https connection
	client := NewHttpsClient(conf)
	if client == nil {
		return ErrorBootstrapNoClient
	}

	if err := client.Bootstrap(server); err != nil {
		return err
	}

	//TODO: store bootstrap credentials so that we will be able to reuse in future
	return nil
}

func processBootstrapResponse(response *http.Response, data interface{}) error {
	if response.StatusCode != http.StatusOK {
		log.Error("Received failed reply for bootstrap request: " + response.Status)
		return ErrorBootstrapFailed
	}
	return nil
}
