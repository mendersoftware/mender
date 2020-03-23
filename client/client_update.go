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
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"

	"github.com/mendersoftware/mender/datastore"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

const (
	minimumImageSize int64 = 4096 //kB
)

type Updater interface {
	GetScheduledUpdate(api ApiRequester, server string, current *CurrentUpdate) (interface{}, error)
	FetchUpdate(api ApiRequester, url string, maxWait time.Duration) (io.ReadCloser, int64, error)
}

var (
	ErrNotAuthorized = errors.New("client not authorized")
)

type UpdateClient struct {
	minImageSize int64
}

func NewUpdate() *UpdateClient {
	up := UpdateClient{
		minImageSize: minimumImageSize,
	}
	return &up
}

// CurrentUpdate describes currently installed update. Non empty fields will be
// used when querying for the next update.
type CurrentUpdate struct {
	Artifact   string
	DeviceType string
	Provides   map[string]string
}

func (u *CurrentUpdate) MarshalJSON() ([]byte, error) {
	if u.Provides == nil {
		u.Provides = make(map[string]string)
	}
	u.Provides["artifact_name"] = u.Artifact
	u.Provides["device_type"] = u.DeviceType
	return json.Marshal(u.Provides)
}

func (u *UpdateClient) GetScheduledUpdate(api ApiRequester, server string,
	current *CurrentUpdate) (interface{}, error) {

	return u.getUpdateInfo(api, processUpdateResponse, server, current)
}

// getUpdateInfo Tries to get the next update information from the backend. This
// is done in two stages. First it tries a POST request with the devices provide
// parameters. Then if this fails with an error code response, then it falls
// back to the open source version with GET, and the parameters encoded in the
// URL.
func (u *UpdateClient) getUpdateInfo(api ApiRequester, process RequestProcessingFunc,
	server string, current *CurrentUpdate) (interface{}, error) {
	postReq, getReq, err := makeUpdateCheckRequest(server, current)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create update check request")
	}

	r, err := api.Do(postReq)
	if err != nil {
		log.Debugf("Failed sending device provides to the backend: Error: %v", err)
		return nil, errors.Wrapf(err, "POST update check request failed")
	}

	defer r.Body.Close()

	if r.StatusCode != http.StatusOK && r.StatusCode != http.StatusNoContent {

		// Fall back to the GET (Open-Source) functionality on all error codes
		if r.StatusCode >= 400 && r.StatusCode < 600 {

			log.Debugf("device provides not accepted by the server. Response code: %d", r.StatusCode)

			r, err = api.Do(getReq)

			if err != nil {
				log.Debug("Sending request error: ", err)
				return nil, errors.Wrapf(err, "update check request failed")
			}

			defer r.Body.Close()

		} else {
			return nil, fmt.Errorf("failed to post update info to the server. Response: %v", r)
		}
	}

	respdata, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the request body")
	}

	r.Body = ioutil.NopCloser(bytes.NewReader(respdata))
	data, err := process(r)
	if err != nil {
		r.Body = ioutil.NopCloser(bytes.NewReader(respdata))
		return data, NewAPIError(err, r)
	}
	return data, err
}

// FetchUpdate returns a byte stream which is a download of the given link.
func (u *UpdateClient) FetchUpdate(api ApiRequester, url string, maxWait time.Duration) (io.ReadCloser, int64, error) {
	req, err := makeUpdateFetchRequest(url)
	if err != nil {
		return nil, -1, errors.Wrapf(err, "failed to create update fetch request")
	}

	r, err := api.Do(req)
	if err != nil {
		log.Error("Can not fetch update image: ", err)
		return nil, -1, errors.Wrapf(err, "update fetch request failed")
	}

	log.Debugf("Received fetch update response %v+", r)

	if r.StatusCode != http.StatusOK {
		r.Body.Close()
		log.Errorf("Error fetching shcheduled update info: code (%d)", r.StatusCode)
		return nil, -1, NewAPIError(errors.New("error receiving scheduled update information"), r)
	}

	if r.ContentLength < 0 {
		r.Body.Close()
		return nil, -1, errors.New("Will not continue with unknown image size.")
	} else if r.ContentLength < u.minImageSize {
		r.Body.Close()
		log.Errorf("Image smaller than expected. Expected: %d, received: %d", u.minImageSize, r.ContentLength)
		return nil, -1, errors.New("Image size is smaller than expected. Aborting.")
	}

	return NewUpdateResumer(r.Body, r.ContentLength, maxWait, api, req), r.ContentLength, nil
}

func validateGetUpdate(update datastore.UpdateInfo) error {
	// check if we have JSON data correctly decoded
	if update.ID == "" ||
		len(update.Artifact.CompatibleDevices) == 0 ||
		update.Artifact.ArtifactName == "" ||
		update.Artifact.Source.URI == "" {
		return errors.New("Missing parameters in encoded JSON update response")
	}

	log.Infof("Correct request for getting image from: %s [name: %v; devices: %v]",
		update.Artifact.Source.URI,
		update.ArtifactName(),
		update.Artifact.CompatibleDevices)
	return nil
}

func processUpdateResponse(response *http.Response) (interface{}, error) {
	log.Debug("Received response:", response.Status)

	respBody, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	switch response.StatusCode {
	case http.StatusOK:
		log.Debug("Have update available")

		var data datastore.UpdateInfo
		if err := json.Unmarshal(respBody, &data); err != nil {
			return nil, errors.Wrapf(err, "failed to parse response")
		}

		if err := validateGetUpdate(data); err != nil {
			return nil, err
		}

		return data, nil

	case http.StatusNoContent:
		log.Debug("No update available")
		return nil, nil

	case http.StatusUnauthorized:
		log.Warn("Client not authorized to get update schedule.")
		return nil, ErrNotAuthorized

	default:
		log.Warn("Client received invalid response status code: ", response.StatusCode)
		return nil, errors.New("Invalid response received from server")
	}
}

func makeUpdateCheckRequest(server string, current *CurrentUpdate) (*http.Request, *http.Request, error) {
	vals := url.Values{}
	if current.DeviceType != "" {
		vals.Add("device_type", current.DeviceType)
	}
	if current.Artifact != "" {
		vals.Add("artifact_name", current.Artifact)
	}

	providesBody, err := json.Marshal(current)
	if err != nil {
		return nil, nil, err
	}

	r := bytes.NewBuffer(providesBody)

	ep := "/deployments/device/deployments/next"

	url := buildApiURL(server, ep)

	postReq, err := http.NewRequest(http.MethodPost, url, r)
	if err != nil {
		return nil, nil, err
	}

	postReq.Header.Add("Content-Type", "application/json")

	if len(vals) != 0 {
		ep = ep + "?" + vals.Encode()
	}
	url = buildApiURL(server, ep)
	getReq, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, nil, err
	}
	return postReq, getReq, nil
}

func makeUpdateFetchRequest(url string) (*http.Request, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	return req, nil
}
