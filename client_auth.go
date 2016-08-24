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
	"bytes"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/mendersoftware/log"
	"github.com/pkg/errors"
)

var AuthErrorUnauthorized = errors.New("authentication request rejected")

type AuthRequester interface {
	Request(api ApiRequester, server string, dataSrc AuthDataMessenger) ([]byte, error)
}

// Auth client wrapper. Instantiate by yourself or use `NewAuthClient()` helper
type AuthClient struct {
}

func NewAuthClient() *AuthClient {
	ac := AuthClient{}
	return &ac
}

func (u *AuthClient) Request(api ApiRequester, server string, dataSrc AuthDataMessenger) ([]byte, error) {

	req, err := makeAuthRequest(server, dataSrc)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to build authorization request")
	}

	log.Debugf("making authorization request to server %s with req: %s", server, req)
	rsp, err := api.Do(req)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to execute authorization request")
	}
	defer rsp.Body.Close()

	log.Debugf("got response: %v", rsp)

	switch rsp.StatusCode {
	case http.StatusUnauthorized:
		return nil, AuthErrorUnauthorized
	case http.StatusOK:
		log.Debugf("receive response data")
		data, err := ioutil.ReadAll(rsp.Body)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to receive authorization response data")
		}

		log.Debugf("received response data %v", data)
		return data, nil
	default:
		return nil, errors.Errorf("unexpected authorization status %v", rsp.StatusCode)
	}
}

func makeAuthRequest(server string, dataSrc AuthDataMessenger) (*http.Request, error) {
	url := buildApiURL(server, "/authentication/auth_requests")

	req, err := dataSrc.MakeAuthRequest()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to obtain authorization message data")
	}

	dataio := bytes.NewBuffer(req.Data)
	hreq, err := http.NewRequest(http.MethodPost, url, dataio)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create authorization HTTP request")
	}

	hreq.Header.Add("Content-Type", "application/json")
	hreq.Header.Add("Authorization", fmt.Sprintf("Bearer %s", req.Token))
	hreq.Header.Add("X-MEN-Signature", base64.StdEncoding.EncodeToString(req.Signature))
	return hreq, nil
}
