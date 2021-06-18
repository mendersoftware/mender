// Copyright 2021 Northern.tech AS
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

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/mendersoftware/mender/app/updatecontrolmap"
	"github.com/mendersoftware/mender/datastore"
)

const (
	minimumImageSize int64 = 4096 //kB
)

type Updater interface {
	GetScheduledUpdate(api ApiRequester, server string, current *CurrentUpdate) (interface{}, error)
	FetchUpdate(api ApiRequester, url string, maxWait time.Duration) (io.ReadCloser, int64, error)
}

var (
	ErrNotAuthorized         = errors.New("client not authorized")
	ErrNoDeploymentAvailable = errors.New("no deployment available")
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

type updateV1Body *CurrentUpdate

type updateV2Body struct {
	DeviceProvides   *CurrentUpdate `json:"device_provides"`
	UpdateControlMap bool           `json:"update_control_map"`
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
	reqs, err := makeUpdateCheckRequest(server, current)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create update check request")
	}

	r, err := findFirstWorkingEndpoint(api, reqs)
	if err != nil {
		return nil, err
	}

	respdata, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the request body")
	}
	r.Body.Close()

	r.Body = ioutil.NopCloser(bytes.NewReader(respdata))
	data, err := process(r)
	if err != nil {
		r.Body = ioutil.NopCloser(bytes.NewReader(respdata))
		return data, NewAPIError(err, r)
	}
	return data, err
}

func findFirstWorkingEndpoint(api ApiRequester, reqs []*http.Request) (*http.Response, error) {
	var r *http.Response
	var err error
	for _, req := range reqs {
		r, err = api.Do(req)
		if err != nil {
			log.Debugf("Failed sending update check request to the backend: (%s %s): Error: %s",
				req.Method, req.URL.String(), err.Error())
			return nil, errors.Wrapf(err, "update check request failed")
		}

		authStatus := "authorized"
		switch r.StatusCode {
		case http.StatusUnauthorized:
			authStatus = "unauthorized"
			fallthrough
		case http.StatusOK, http.StatusNoContent:
			log.Debugf("Successful (%s) request: (%s %s): Response code: %d",
				authStatus, req.Method, req.URL.String(), r.StatusCode)
			// Unauthorized is also ok, since there is nothing wrong
			// with the request itself.
			return r, nil

		default:
			r.Body.Close()

			// Fall back to alternative methods/endpoints on other error codes.
			if r.StatusCode >= 400 && r.StatusCode < 600 {
				log.Debugf("request not accepted by the server: (%s %s): Response code: %d",
					req.Method, req.URL.String(), r.StatusCode)
				continue
			} else {
				return nil, fmt.Errorf("failed to check update info on the server. Response: %v", r)
			}
		}
	}

	return nil, fmt.Errorf("failed to check update info on the server. Response: %v", r)
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
		err = NewAPIError(errors.New("error receiving scheduled update information"), r)
		r.Body.Close()
		log.Errorf("Error fetching scheduled update info: code (%d)", r.StatusCode)
		return nil, -1, err
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

type UpdateResponse struct {
	*datastore.UpdateInfo
	*updatecontrolmap.UpdateControlMap `json:"update_control_map"`
}

func (u *UpdateResponse) Validate() (err error) {
	if u == nil {
		return errors.New("Empty update response")
	}
	update := u.UpdateInfo
	if update == nil {
		return errors.Errorf("not an update response?")
	}

	if err := update.Validate(); err != nil {
		return errors.Wrapf(err,
			"Failed to validate the update information in the response")
	}

	log.Debugf("Received update response: %v", u)

	if u.UpdateControlMap != nil {
		controlMap := u.UpdateControlMap
		if err := controlMap.Validate(); err != nil {
			log.Errorf("Failed to validate the UpdateControlMap: %s",
				err.Error())
			return err
		}
		controlMap.Sanitize()
	}
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
		var ur UpdateResponse
		if err = json.Unmarshal(respBody, &ur); err != nil {
			return nil, errors.Wrap(err, "failed to parse the HTTP update response")
		}
		if err = ur.Validate(); err != nil {
			return nil, err
		}
		log.Debugf("UpdateResponse received and validated: %v", ur)
		return ur, nil

	case http.StatusNoContent:
		log.Debug("No update available")
		return nil, ErrNoDeploymentAvailable

	case http.StatusUnauthorized:
		log.Warn("Client not authorized to get update schedule.")
		return nil, ErrNotAuthorized

	default:
		log.Warn("Client received invalid response status code: ", response.StatusCode)
		return nil, errors.New("Invalid response received from server")
	}
}

func makeUpdateCheckRequest(server string, current *CurrentUpdate) ([]*http.Request, error) {
	// In this function we are taking a couple of things into account:
	// First, we need to construct a request for the "POST v2" endpoint,
	// which supports `update_control_map`, and which passes artifact
	// provides in a `device_provides` key in the body. Then we construct a
	// request for the "POST v1" endpoint, which does not support
	// `update_control_map`, and which passes the artifact provides directly
	// in the body. Finally we construct a "GET v1" endpoint, which only
	// supports artifact name and device type as URL parameters.

	vals := url.Values{}
	if current.DeviceType != "" {
		vals.Add("device_type", current.DeviceType)
	}
	if current.Artifact != "" {
		vals.Add("artifact_name", current.Artifact)
	}

	v1Body := updateV1Body(current)
	v2Body := &updateV2Body{
		DeviceProvides:   current,
		UpdateControlMap: true,
	}

	reqs := make([]*http.Request, 0, 3)

	// POST v2 -------------------------------------------------------------
	body, err := json.Marshal(v2Body)
	if err != nil {
		return nil, err
	}
	r := bytes.NewBuffer(body)
	ep := "/v2/deployments/device/deployments/next"
	url := buildApiURL(server, ep)
	req, err := http.NewRequest(http.MethodPost, url, r)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Content-Type", "application/json")
	reqs = append(reqs, req)

	// POST v1 -------------------------------------------------------------
	body, err = json.Marshal(v1Body)
	if err != nil {
		return nil, err
	}
	r = bytes.NewBuffer(body)
	ep = "/v1/deployments/device/deployments/next"
	url = buildApiURL(server, ep)
	req, err = http.NewRequest(http.MethodPost, url, r)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Content-Type", "application/json")
	reqs = append(reqs, req)

	// GET v1 --------------------------------------------------------------
	if len(vals) != 0 {
		ep = ep + "?" + vals.Encode()
	}
	url = buildApiURL(server, ep)
	req, err = http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	reqs = append(reqs, req)

	return reqs, nil
}

func makeUpdateFetchRequest(url string) (*http.Request, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	return req, nil
}
