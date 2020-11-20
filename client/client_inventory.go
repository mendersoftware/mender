// Copyright 2020 Northern.tech AS
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
package client

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

type InventorySubmitter interface {
	Submit(api ApiRequester, server string, data interface{}) error
}

type InventoryClient struct {
}

func NewInventory() InventorySubmitter {
	return &InventoryClient{}
}

// Submit reports status information to the backend
func (i *InventoryClient) Submit(api ApiRequester, url string, data interface{}) error {
	// PATCH used to be the only method available in Mender Product 2.5, so
	// fall back to that if PUT fails.
	r, err := doSubmitInventory(api, http.MethodPut, url, data)
	if r != nil && r.StatusCode == http.StatusMethodNotAllowed {
		r, err = doSubmitInventory(api, http.MethodPatch, url, data)
	}

	log.Debugf("Inventory update sent, response %v", r)

	if err != nil {
		log.Error(err.Error())
	}

	return err
}

func doSubmitInventory(api ApiRequester, method, url string, data interface{}) (*http.Response, error) {
	req, err := makeInventorySubmitRequest(method, url, data)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to prepare inventory submit request")
	}

	r, err := api.Do(req)
	if err != nil {
		log.Error("Failed to submit inventory data: ", err)
		return r, errors.Wrapf(err, "inventory submit failed")
	}

	defer r.Body.Close()

	if r.StatusCode != http.StatusOK {
		return r, NewAPIError(errors.Errorf("Got unexpected HTTP status when submitting to inventory %d", r.StatusCode), r)
	}
	return r, nil
}

func makeInventorySubmitRequest(method, server string, data interface{}) (*http.Request, error) {
	url := buildApiURL(server, "/inventory/device/attributes")

	out := &bytes.Buffer{}
	enc := json.NewEncoder(out)
	err := enc.Encode(&data)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to encode inventory request data")
	}

	hreq, err := http.NewRequest(method, url, out)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create inventory HTTP request")
	}

	hreq.Header.Add("Content-Type", "application/json")
	return hreq, nil
}
