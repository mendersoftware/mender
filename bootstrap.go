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
	errorBootstrapFailed = errors.New("Bootstraping failed")
)

func ProcessBootstrapResponse(response *http.Response) error {
	if response.StatusCode != http.StatusOK {
		log.Error("Received failed reply for bootstrap request: " + response.Status)
		return errorBootstrapFailed
	}
	return nil
}

func (c *Client) doBootstrap() error {

	r, err := c.MakeRequest(http.MethodGet, c.BaseURL)

	if err != nil {
		return err
	}

	return ProcessBootstrapResponse(r)
}
