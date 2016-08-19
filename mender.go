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
	"bufio"
	"io"
	"os"
	"strings"
	"time"

	"github.com/mendersoftware/log"
	"github.com/pkg/errors"
)

type Controller interface {
	Authorize() menderError
	Bootstrap() menderError
	GetCurrentImageID() string
	GetUpdatePollInterval() time.Duration
	HasUpgrade() (bool, menderError)
	CheckUpdate() (*UpdateResponse, menderError)
	FetchUpdate(url string) (io.ReadCloser, int64, error)
	ReportUpdateStatus(update UpdateResponse, status string) menderError
	UploadLog(update UpdateResponse, logs []byte) menderError

	UInstallCommitRebooter
	StateRunner
}

const (
	defaultManifestFile = "/etc/build_mender"
	defaultKeyFile      = "mender-agent.pem"
	defaultDataStore    = "/var/lib/mender"
)

type MenderState int

const (
	// initial state
	MenderStateInit MenderState = iota
	// client is bootstrapped, i.e. ready to go
	MenderStateBootstrapped
	// client has all authorization data available
	MenderStateAuthorized
	// wait before authorization attempt
	MenderStateAuthorizeWait
	// wait for new update
	MenderStateUpdateCheckWait
	// check update
	MenderStateUpdateCheck
	// update fetch
	MenderStateUpdateFetch
	// update install
	MenderStateUpdateInstall
	// varify update
	MenderStateUpdateVerify
	// commit needed
	MenderStateUpdateCommit
	// status report
	MenderStateUpdateStatusReport
	// errro reporting status
	MenderStateReportStatusError
	// reboot
	MenderStateReboot
	//rollback
	MenderStateRollback
	// error
	MenderStateError
	// update error
	MenderStateUpdateError
	// exit state
	MenderStateDone
)

type mender struct {
	UInstallCommitRebooter
	updater        Updater
	env            BootEnvReadWriter
	state          State
	config         menderConfig
	manifestFile   string
	forceBootstrap bool
	authReq        AuthRequester
	authMgr        AuthManager
	api            *ApiClient
	authToken      AuthToken
}

type MenderPieces struct {
	device  UInstallCommitRebooter
	env     BootEnvReadWriter
	store   Store
	authMgr AuthManager
}

func NewMender(config menderConfig, pieces MenderPieces) (*mender, error) {
	api, err := NewApiClient(config.GetHttpConfig())
	if err != nil {
		return nil, errors.Wrap(err, "error creating HTTP client")
	}

	m := &mender{
		UInstallCommitRebooter: pieces.device,
		updater:                NewUpdateClient(),
		env:                    pieces.env,
		manifestFile:           defaultManifestFile,
		state:                  initState,
		config:                 config,
		authMgr:                pieces.authMgr,
		authReq:                NewAuthClient(),
		api:                    api,
		authToken:              noAuthToken,
	}
	return m, nil
}

func (m mender) GetCurrentImageID() string {
	// This is where Yocto stores buid information
	manifest, err := os.Open(m.manifestFile)
	if err != nil {
		log.Error("Can not read current image id.")
		return ""
	}

	imageID := ""

	scanner := bufio.NewScanner(manifest)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		log.Debug("Read data from device manifest file: ", line)
		if strings.HasPrefix(line, "IMAGE_ID") {
			log.Debug("Found device id line: ", line)
			lineID := strings.Split(line, "=")
			if len(lineID) != 2 {
				log.Errorf("Broken device manifest file: (%v)", lineID)
				return ""
			}
			log.Debug("Current image id: ", strings.TrimSpace(lineID[1]))
			return strings.TrimSpace(lineID[1])
		}
	}
	if err := scanner.Err(); err != nil {
		log.Error(err)
	}
	return imageID
}

func (m *mender) HasUpgrade() (bool, menderError) {
	env, err := m.env.ReadEnv("upgrade_available")
	if err != nil {
		return false, NewFatalError(err)
	}
	upgradeAvailable := env["upgrade_available"]

	// we are after update
	if upgradeAvailable == "1" {
		return true, nil
	}
	return false, nil
}

func (m *mender) ForceBootstrap() {
	m.forceBootstrap = true
}

func (m *mender) needsBootstrap() bool {
	if m.forceBootstrap {
		return true
	}

	if !m.authMgr.HasKey() {
		log.Debugf("needs keys")
		return true
	}

	return false
}

func (m *mender) Bootstrap() menderError {
	if !m.needsBootstrap() {
		return nil
	}

	return m.doBootstrap()
}

// cache authorization code
func (m *mender) loadAuth() menderError {
	if m.authToken != noAuthToken {
		return nil
	}

	code, err := m.authMgr.AuthToken()
	if err != nil {
		return NewFatalError(errors.Wrap(err, "failed to cache authorization code"))
	}

	m.authToken = code
	return nil
}

func (m *mender) Authorize() menderError {
	if m.authMgr.IsAuthorized() {
		log.Info("authorization data present and valid, skipping authorization attempt")
		return m.loadAuth()
	}

	m.authToken = noAuthToken

	rsp, err := m.authReq.Request(m.api, m.config.ServerURL, m.authMgr)
	if err != nil {
		return NewTransientError(errors.Wrap(err, "authorization request failed"))
	}

	err = m.authMgr.RecvAuthResponse(rsp)
	if err != nil {
		return NewTransientError(errors.Wrap(err, "failed to parse authorization response"))
	}

	log.Info("successfuly received new authorization data")

	return m.loadAuth()
}

func (m *mender) doBootstrap() menderError {
	if !m.authMgr.HasKey() || m.forceBootstrap {
		log.Infof("device keys not present or bootstrap forced, generating")
		if err := m.authMgr.GenerateKey(); err != nil {
			return NewFatalError(err)
		}

	}

	m.forceBootstrap = false

	return nil
}

func (m *mender) FetchUpdate(url string) (io.ReadCloser, int64, error) {
	return m.updater.FetchUpdate(m.api.Request(m.authToken), url)
}

// Check if new update is available. In case of errors, returns nil and error
// that occurred. If no update is available *UpdateResponse is nil, otherwise it
// contains update information.
func (m *mender) CheckUpdate() (*UpdateResponse, menderError) {
	currentImageID := m.GetCurrentImageID()
	//TODO: if currentImageID == "" {
	// 	return errors.New("")
	// }

	haveUpdate, err := m.updater.GetScheduledUpdate(m.api.Request(m.authToken),
		m.config.ServerURL)
	if err != nil {
		log.Error("Error receiving scheduled update data: ", err)
		return nil, NewTransientError(err)
	}

	if haveUpdate == nil {
		log.Debug("no updates available")
		return nil, nil
	}
	update, ok := haveUpdate.(UpdateResponse)
	if !ok {
		return nil, NewTransientError(errors.Errorf("not an update response?"))
	}

	log.Debugf("received update response: %v", update)

	if update.Image.YoctoID == currentImageID {
		return nil, nil
	}
	return &update, nil
}

func (m *mender) ReportUpdateStatus(update UpdateResponse, status string) menderError {
	s := NewStatusClient()
	err := s.Report(m.api.Request(m.authToken), m.config.ServerURL,
		StatusReport{
			deploymentID: update.ID,
			Status:       status,
		})
	if err != nil {
		log.Error("error reporting update status: ", err)
		return NewTransientError(err)
	}
	return nil
}

func (m *mender) UploadLog(update UpdateResponse, logs []byte) menderError {
	s := NewLogUploadClient()
	err := s.Upload(m.api.Request(m.authToken), m.config.ServerURL,
		LogData{
			deploymentID: update.ID,
			Messages:     logs,
		})
	if err != nil {
		log.Error("error uploading logs: ", err)
		return NewTransientError(err)
	}
	return nil
}

func (m mender) GetUpdatePollInterval() time.Duration {
	return time.Duration(m.config.PollIntervalSeconds) * time.Second
}

func (m *mender) SetState(s State) {
	log.Infof("Mender state: %v -> %v", m.state.Id(), s.Id())
	m.state = s
}

func (m *mender) GetState() State {
	return m.state
}

func (m *mender) RunState(ctx *StateContext) (State, bool) {
	return m.state.Handle(ctx, m)
}
