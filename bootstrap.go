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

	"github.com/mendersoftware/log"
)
import "net/http"

//TODO: this will be hardcoded for now but should be configurable in future
const (
	defaultCertFile   = "/data/certfile.crt"
	defaultCertKey    = "/data/certkey.key"
	defaultServerCert = "/data/server.crt"
)

var (
	errorBootstrapFailed = errors.New("Bootstraping failed")
)

type bootstrapRequester struct {
	reqType      string
	request      string
	menderClient client
}

func (br bootstrapRequester) getClient() client {
	return br.menderClient
}

func (br bootstrapRequester) formatRequest() clientRequestType {
	return clientRequestType{br.reqType, br.request}
}

func (br bootstrapRequester) parseResponse(response http.Response, respBody []byte) error {
	// TODO: do something with the stuff received
	if response.Status != "200 OK" {
		log.Error("Received failed reply for bootstrap request: " + response.Status)
		return errorBootstrapFailed
	}
	return nil
}

func (c *client) doBootstrap() error {
	bootstrapRequester := bootstrapRequester{
		reqType:      http.MethodGet,
		request:      c.BaseURL + "/bootstrap",
		menderClient: *c,
	}
	return makeJobDone(bootstrapRequester)
}
