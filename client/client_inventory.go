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
package client

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/mendersoftware/log"
	"github.com/pkg/errors"
)

type InventorySubmitter interface {
	Submit(api ApiRequester, server string, data interface{}) error
}

type InventoryClient struct {
}

func NewInventory() InventorySubmitter {
	return &InventoryClient{}
}

// Report status information to the backend
func (i *InventoryClient) Submit(api ApiRequester, url string, data interface{}) error {
	req, err := makeInventorySubmitRequest(url, data)
	if err != nil {
		return errors.Wrapf(err, "failed to prepare inventory submit request")
	}

	r, err := api.Do(req)
	if err != nil {
		log.Error("failed to submit inventory data: ", err)
		return errors.Wrapf(err, "inventory submit failed")
	}

	defer r.Body.Close()

	if r.StatusCode != http.StatusOK {
		log.Errorf("got unexpected HTTP status when submitting to inventory: %v", r.StatusCode)
		return errors.Errorf("inventory submit failed, bad status %v", r.StatusCode)
	}
	log.Debugf("inventory update sent, response %v", r)

	return nil
}

func makeInventorySubmitRequest(server string, data interface{}) (*http.Request, error) {
	url := buildApiURL(server, "/inventory/device/attributes")

	out := &bytes.Buffer{}
	enc := json.NewEncoder(out)
	enc.Encode(&data)

	hreq, err := http.NewRequest(http.MethodPatch, url, out)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create inventory HTTP request")
	}

	hreq.Header.Add("Content-Type", "application/json")
	return hreq, nil
}
