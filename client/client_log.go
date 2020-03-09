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
	"fmt"
	"net/http"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

type LogUploader interface {
	Upload(api ApiRequester, server string, logs LogData) error
}

type LogData struct {
	DeploymentID string `json:"-"`
	Messages     []byte `json:"messages"`
}

type LogUploadClient struct {
}

func NewLog() LogUploader {
	return &LogUploadClient{}
}

// Report status information to the backend
func (u *LogUploadClient) Upload(api ApiRequester, url string, logs LogData) error {
	req, err := makeLogUploadRequest(url, &logs)
	if err != nil {
		return errors.Wrapf(err, "failed to prepare log upload request")
	}

	r, err := api.Do(req)
	if err != nil {
		log.Error("failed to upload logs: ", err)
		return errors.Wrapf(err, "uploading logs failed")
	}

	defer r.Body.Close()

	// HTTP 204 No Content
	if r.StatusCode != http.StatusNoContent {
		log.Errorf("Got unexpected HTTP status when uploading log: %v", r.StatusCode)
		return NewAPIError(errors.Errorf("uploading logs failed, bad status %v", r.StatusCode), r)
	}
	log.Debugf("Logs uploaded, response %v", r)

	return nil
}

func makeLogUploadRequest(server string, logs *LogData) (*http.Request, error) {
	path := fmt.Sprintf("/deployments/device/deployments/%s/log",
		logs.DeploymentID)
	url := buildApiURL(server, path)

	hreq, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(logs.Messages))
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create log sending HTTP request")
	}

	hreq.Header.Add("Content-Type", "application/json")
	return hreq, nil
}
