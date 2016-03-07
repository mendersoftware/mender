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

import "errors"
import "net/http"

//TODO: this will be hardcoded for now but should be configurable in future
const (
	defaultCertFile   = "/data/certfile.crt"
	defaultCertKey    = "/data/certkey.key"
	defaultServerCert = "/data/server.crt"
)

func (c *client) parseBootstrapResponse(response *http.Response) error {
	// TODO: do something with the stuff received
	if response.Status != "200 OK" {
		return errors.New("Bootstraping failed: " + response.Status)
	}
	return nil
}

func (c *client) doBootstrap() error {
	response, err := c.sendRequest(http.MethodGet, c.BaseURL+"/bootstrap")
	if err != nil {
		return err
	}
	return c.parseBootstrapResponse(response)
}
