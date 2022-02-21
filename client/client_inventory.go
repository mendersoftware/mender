// Copyright 2022 Northern.tech AS
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
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

type InventorySubmitter interface {
	Submit(api ApiRequester, server string, data interface{}) error
}

type InventoryClient struct {
	retryInterval time.Duration
	maxTries      int
}

func NewInventory(retryInterval time.Duration,
	maxTries int) InventorySubmitter {
	return &InventoryClient{
		retryInterval: retryInterval,
		maxTries:      maxTries,
	}
}

// Submit reports status information to the backend
func (i *InventoryClient) Submit(api ApiRequester, url string, data interface{}) error {
	// PATCH used to be the only method available in Mender Product 2.5, so
	// fall back to that if PUT fails.
	r, err := doSubmitInventory(api,
		http.MethodPut,
		url,
		data,
		i.retryInterval,
		i.maxTries)
	if err == nil {
		defer r.Body.Close()
	} else if r != nil && r.StatusCode == http.StatusMethodNotAllowed {
		r, err = doSubmitInventory(api,
			http.MethodPatch,
			url,
			data,
			i.retryInterval,
			i.maxTries)
		if err == nil {
			defer r.Body.Close()
		}
	}

	log.Debugf("Inventory update sent, response %v", r)

	if err != nil {
		log.Error(err.Error())
	}

	return err
}

func doSubmitInventory(
	api ApiRequester,
	method, url string,
	data interface{},
	retryInterval time.Duration,
	tries int,
) (*http.Response, error) {
	req, err := makeInventorySubmitRequest(method, url, data)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to prepare inventory submit request")
	}

	r, err := api.Do(req)
	if err != nil {
		var e error
		try := 1
		for e == nil {
			var sleepInterval time.Duration
			sleepInterval, e = GetExponentialBackoffTime(try, retryInterval, tries)
			if e != nil {
				break
			}
			log.Infof("Send inventory waiting for re-try: %d in %.0f",
				try, sleepInterval.Seconds())
			time.Sleep(sleepInterval)
			try++
			r, err = api.Do(req)
			if err == nil {
				break
			}
		}
		if err != nil {
			log.Errorf("Failed to submit inventory data: %s", err.Error())
			return r, errors.Wrapf(err, "inventory submit failed")
		}
	}

	defer r.Body.Close()

	if r.StatusCode != http.StatusOK {
		return r, NewAPIError(
			errors.Errorf(
				"Got unexpected HTTP status when submitting to inventory %d",
				r.StatusCode,
			),
			r,
		)
	}
	return r, nil
}

func makeInventorySubmitRequest(method, server string, data interface{}) (*http.Request, error) {
	url := buildApiURL(server, "/v1/inventory/device/attributes")

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
