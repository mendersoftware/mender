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
	"os"
	"path"
	"strings"
	"time"

	"github.com/mendersoftware/log"

	"github.com/mendersoftware/mender/client"
	"github.com/mendersoftware/mender/installer"
	"github.com/pkg/errors"
)

type BootVars map[string]string

type BootEnvReadWriter interface {
	ReadEnv(...string) (BootVars, error)
	WriteEnv(BootVars) error
}

type UInstallCommitRebooter interface {
	installer.UInstaller
	CommitUpdate() error
	Reboot() error
	Rollback() error
	HasUpdate() (bool, error)
}

type Controller interface {
	Authorize() menderError
	Bootstrap() menderError
	GetCurrentArtifactName() string
	GetUpdatePollInterval() time.Duration
	HasUpgrade() (bool, menderError)
	CheckUpdate() (*client.UpdateResponse, menderError)
	FetchUpdate(url string) (io.ReadCloser, int64, error)
	ReportUpdateStatus(update client.UpdateResponse, status string) menderError
	UploadLog(update client.UpdateResponse, logs []byte) menderError
	InventoryRefresh() error

	UInstallCommitRebooter
	StateRunner
}

const (
	defaultKeyFile = "mender-agent.pem"
)

var (
	defaultArtifactInfoFile = path.Join(getConfDirPath(), "artifact_info")
	defaultDeviceTypeFile   = path.Join(getStateDirPath(), "device_type")
	defaultDataStore        = getStateDirPath()
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
	updater          client.Updater
	state            State
	config           menderConfig
	artifactInfoFile string
	deviceTypeFile   string
	forceBootstrap   bool
	authReq          client.AuthRequester
	authMgr          AuthManager
	api              *client.ApiClient
	authToken        client.AuthToken
}

type MenderPieces struct {
	device  UInstallCommitRebooter
	store   Store
	authMgr AuthManager
}

func NewMender(config menderConfig, pieces MenderPieces) (*mender, error) {
	api, err := client.New(config.GetHttpConfig())
	if err != nil {
		return nil, errors.Wrap(err, "error creating HTTP client")
	}

	m := &mender{
		UInstallCommitRebooter: pieces.device,
		updater:                client.NewUpdate(),
		artifactInfoFile:       defaultArtifactInfoFile,
		deviceTypeFile:         defaultDeviceTypeFile,
		state:                  initState,
		config:                 config,
		authMgr:                pieces.authMgr,
		authReq:                client.NewAuth(),
		api:                    api,
		authToken:              noAuthToken,
	}
	return m, nil
}

func getManifestData(dataType, manifestFile string) string {
	// This is where Yocto stores buid information
	manifest, err := os.Open(manifestFile)
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

func (m *mender) GetCurrentArtifactName() string {
	return getManifestData("artifact_name", m.artifactInfoFile)
}

func (m *mender) GetDeviceType() string {
	return getManifestData("device_type", m.deviceTypeFile)
}

func GetCurrentArtifactName(artifactInfoFile string) string {
	return getManifestData("artifact_name", artifactInfoFile)
}

func GetDeviceType(deviceTypeFile string) string {
	return getManifestData("device_type", deviceTypeFile)
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
		if err == client.AuthErrorUnauthorized {
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
	return m.updater.FetchUpdate(m.api, url)
}

// Check if new update is available. In case of errors, returns nil and error
// that occurred. If no update is available *UpdateResponse is nil, otherwise it
// contains update information.
func (m *mender) CheckUpdate() (*client.UpdateResponse, menderError) {
	currentArtifactName := m.GetCurrentArtifactName()
	//TODO: if currentArtifactName == "" {
	// 	return errors.New("")
	// }

	haveUpdate, err := m.updater.GetScheduledUpdate(m.api.Request(m.authToken),
		m.config.ServerURL, client.CurrentUpdate{
			Artifact:   currentArtifactName,
			DeviceType: m.GetDeviceType(),
		})

	if err != nil {
		// remove authentication token if device is not authorized
		if err == client.ErrNotAuthorized {
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
	update, ok := haveUpdate.(client.UpdateResponse)
	if !ok {
		return nil, NewTransientError(errors.Errorf("not an update response?"))
	}

	log.Debugf("received update response: %v", update)

	if update.Image.Name == currentArtifactName {
		log.Info("Attempting to upgrade to currently installed artifact name, not performing upgrade.")
		return &update, NewTransientError(os.ErrExist)
	}
	return &update, nil
}

func (m *mender) ReportUpdateStatus(update client.UpdateResponse, status string) menderError {
	s := client.NewStatus()
	err := s.Report(m.api.Request(m.authToken), m.config.ServerURL,
		client.StatusReport{
			DeploymentID: update.ID,
			Status:       status,
		})
	if err != nil {
		log.Error("error reporting update status: ", err)
		if err == client.ErrDeploymentAborted {
			return NewFatalError(err)
		}
		return NewTransientError(err)
	}
	return nil
}

func (m *mender) UploadLog(update client.UpdateResponse, logs []byte) menderError {
	s := client.NewLog()
	err := s.Upload(m.api.Request(m.authToken), m.config.ServerURL,
		client.LogData{
			DeploymentID: update.ID,
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
	ic := client.NewInventory()
	idg := NewInventoryDataRunner(path.Join(getDataDirPath(), "inventory"))

	idata, err := idg.Get()
	if err != nil {
		// at least report device type
		log.Errorf("failed to obtain inventory data: %s", err.Error())
	}

	reqAttr := []client.InventoryAttribute{
		{Name: "device_type", Value: m.GetDeviceType()},
		{Name: "artifact_name", Value: m.GetCurrentArtifactName()},
		{Name: "client_version", Value: VersionString()},
	}

	if idata == nil {
		idata = make(client.InventoryData, 0, len(reqAttr))
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
	return installer.Install(from, m.GetDeviceType(), m.UInstallCommitRebooter)
}
