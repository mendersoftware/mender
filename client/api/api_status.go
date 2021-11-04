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
package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	common "github.com/mendersoftware/mender/common/api"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

const (
	StatusInstalling       = "installing"
	StatusDownloading      = "downloading"
	StatusRebooting        = "rebooting"
	StatusSuccess          = "success"
	StatusFailure          = "failure"
	StatusAlreadyInstalled = "already-installed"
)

var (
	ErrDeploymentAborted = errors.New("deployment was aborted")
)

type StatusReporter interface {
	Report(api ApiRequester, report StatusReport) error
}

type StatusReport struct {
	DeploymentID string `json:"-"`
	Status       string `json:"status"`
	SubState     string `json:"substate,omitempty"`
}

// StatusReportWrapper holds the data that is passed to the
// statescript functions upon reporting script exectution-status
// to the backend.
type StatusReportWrapper struct {
	Report StatusReport
	API    ApiRequester
}

type StatusClient struct {
}

func NewStatus() StatusReporter {
	return &StatusClient{}
}

// Report status information to the backend
func (u *StatusClient) Report(api ApiRequester, report StatusReport) error {
	req, err := makeStatusReportRequest(report)
	if err != nil {
		return errors.Wrapf(err, "failed to prepare status report request")
	}

	r, err := api.Do(req)
	if err != nil {
		log.Error("Failed to report status: ", err)
		return errors.Wrapf(err, "reporting status failed")
	}

	defer r.Body.Close()

	// HTTP 204 No Content
	switch {
	case r.StatusCode == http.StatusConflict:
		log.Warnf("Status report rejected, deployment aborted at the backend")
		return common.NewAPIError(ErrDeploymentAborted, r)
	case r.StatusCode != http.StatusNoContent:
		log.Errorf("Got unexpected HTTP status when reporting status: %v", r.StatusCode)
		return common.NewAPIError(errors.Errorf("reporting status failed, bad status %v", r.StatusCode), r)
	}

	log.Debugf("Status reported, response %s", r.Status)

	return nil
}

func makeStatusReportRequest(report StatusReport) (*http.Request, error) {
	path := fmt.Sprintf("/v1/deployments/device/deployments/%s/status",
		report.DeploymentID)
	url := BuildApiURL(path)

	out := &bytes.Buffer{}
	enc := json.NewEncoder(out)
	err := enc.Encode(&report)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to encode status request data")
	}

	hreq, err := http.NewRequest(http.MethodPut, url, out)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create status HTTP request")
	}

	hreq.Header.Add("Content-Type", "application/json")
	return hreq, nil
}
