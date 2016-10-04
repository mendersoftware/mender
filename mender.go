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
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"time"

	"github.com/mendersoftware/artifacts/parser"
	"github.com/mendersoftware/artifacts/reader"
	"github.com/mendersoftware/log"
	"github.com/pkg/errors"
)

type BootVars map[string]string

type BootEnvReadWriter interface {
	ReadEnv(...string) (BootVars, error)
	WriteEnv(BootVars) error
}

type UInstaller interface {
	InstallUpdate(io.ReadCloser, int64) error
	EnableUpdatedPartition() error
}

type UInstallCommitRebooter interface {
	UInstaller
	CommitUpdate() error
	Reboot() error
	Rollback() error
	HasUpdate() (bool, error)
}

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
	InventoryRefresh() error

	UInstallCommitRebooter
	StateRunner
}

const (
	defaultKeyFile = "mender-agent.pem"
)

var (
	defaultManifestFile = path.Join(getConfDirPath(), "build_mender")
	defaultDataStore    = getStateDirPath()
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
	// inventory update
	MenderStateInventoryUpdate
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

var (
	stateNames = map[MenderState]string{
		MenderStateInit:               "init",
		MenderStateBootstrapped:       "bootstrapped",
		MenderStateAuthorized:         "authorized",
		MenderStateAuthorizeWait:      "authorize-wait",
		MenderStateInventoryUpdate:    "inventory-update",
		MenderStateUpdateCheckWait:    "update-check-wait",
		MenderStateUpdateCheck:        "update-check",
		MenderStateUpdateFetch:        "update-fetch",
		MenderStateUpdateInstall:      "update-install",
		MenderStateUpdateVerify:       "update-verify",
		MenderStateUpdateCommit:       "update-commit",
		MenderStateUpdateStatusReport: "update-status-report",
		MenderStateReportStatusError:  "status-report-error",
		MenderStateReboot:             "reboot",
		MenderStateRollback:           "rollback",
		MenderStateError:              "error",
		MenderStateUpdateError:        "update-error",
		MenderStateDone:               "finished",
	}
)

func (m MenderState) String() string {
	n, ok := stateNames[m]
	if !ok {
		return fmt.Sprintf("unknown (%d)", m)
	}
	return n
}

type mender struct {
	UInstallCommitRebooter
	updater        Updater
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

func (m mender) getManifestData(dataType string) string {
	// This is where Yocto stores buid information
	manifest, err := os.Open(m.manifestFile)
	if err != nil {
		log.Error("Can not read manifest data.")
		return ""
	}

	scanner := bufio.NewScanner(manifest)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		log.Debug("Read data from device manifest file: ", line)
		if strings.HasPrefix(line, dataType) {
			log.Debug("Found needed line: ", line)
			lineID := strings.Split(line, "=")
			if len(lineID) != 2 {
				log.Errorf("Broken device manifest file: (%v)", lineID)
				return ""
			}
			log.Debug("Current manifest data: ", strings.TrimSpace(lineID[1]))
			return strings.TrimSpace(lineID[1])
		}
	}
	if err := scanner.Err(); err != nil {
		log.Error(err)
	}
	return ""
}

func (m mender) GetCurrentImageID() string {
	return m.getManifestData("IMAGE_ID")
}

func (m mender) GetDeviceType() string {
	return m.getManifestData("DEVICE_TYPE")
}

func (m *mender) HasUpgrade() (bool, menderError) {
	has, err := m.UInstallCommitRebooter.HasUpdate()
	if err != nil {
		return false, NewFatalError(err)
	}
	return has, nil
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
		if err == AuthErrorUnauthorized {
			// make sure to remove auth token once device is rejected
			if remErr := m.authMgr.RemoveAuthToken(); remErr != nil {
				log.Warn("can not remove rejected authentication token")
			}
		}
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
		// remove authentication token if device is not authorized
		if err == ErrNotAuthorized {
			if remErr := m.authMgr.RemoveAuthToken(); remErr != nil {
				log.Warn("can not remove rejected authentication token")
			}
		}
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
		log.Info("Attempting to upgrade to currently installed image ID, not performing upgrade.")
		return &update, NewTransientError(os.ErrExist)
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

func (m *mender) InventoryRefresh() error {
	ic := NewInventoryClient()
	idg := NewInventoryDataRunner(path.Join(getDataDirPath(), "inventory"))

	idata, err := idg.Get()
	if err != nil {
		// at least report device type
		log.Errorf("failed to obtain inventory data: %s", err.Error())
	}

	reqAttr := []InventoryAttribute{
		{"device_type", m.GetDeviceType()},
		{"image_id", m.GetCurrentImageID()},
		{"client_version", VersionString()},
	}

	if idata == nil {
		idata = make(InventoryData, 0, len(reqAttr))
	}
	idata.ReplaceAttributes(reqAttr)

	if idata == nil {
		log.Infof("no inventory data to submit")
		return nil
	}

	err = ic.Submit(m.api.Request(m.authToken), m.config.ServerURL, idata)
	if err != nil {
		return errors.Wrapf(err, "failed to submit inventory data")
	}

	return nil
}

func (m *mender) InstallUpdate(from io.ReadCloser, size int64) error {

	var installed bool
	ar := areader.NewReader(from)
	rp := parser.RootfsParser{
		DataFunc: func(r io.Reader, dt string, uf parser.UpdateFile) error {
			if dt != m.GetDeviceType() {
				return errors.Errorf("unexpected device type %v, expected to see %v",
					dt, m.GetDeviceType())
			}

			if installed {
				return errors.Errorf("rootfs image already installed")
			}

			log.Infof("installing update %v of size %v", uf.Name, uf.Size)
			err := m.UInstallCommitRebooter.InstallUpdate(ioutil.NopCloser(r), uf.Size)
			if err != nil {
				log.Errorf("update image installation failed: %v", err)
				return err
			}

			installed = true
			return nil
		},
	}
	ar.PushWorker(&rp, "0000")
	defer ar.Close()

	_, err := ar.Read()
	if err != nil {
		return errors.Wrapf(err, "failed to read update")
	}

	return nil
}
