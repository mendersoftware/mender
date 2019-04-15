// Copyright 2019 Northern.tech AS
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
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"time"

	"github.com/mendersoftware/log"

	"github.com/mendersoftware/mender/client"
	"github.com/mendersoftware/mender/installer"
	"github.com/mendersoftware/mender/statescript"
	"github.com/mendersoftware/mender/store"
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
	SwapPartitions() error
	HasUpdate() (bool, error)
}

type Controller interface {
	IsAuthorized() bool
	Authorize() menderError
	GetCurrentArtifactName() (string, error)
	GetUpdatePollInterval() time.Duration
	GetInventoryPollInterval() time.Duration
	GetRetryPollInterval() time.Duration
	HasUpgrade() (bool, menderError)
	CheckUpdate() (*client.UpdateResponse, menderError)
	FetchUpdate(url string) (io.ReadCloser, int64, error)
	ReportUpdateStatus(update client.UpdateResponse, status string) menderError
	UploadLog(update client.UpdateResponse, logs []byte) menderError
	InventoryRefresh() error
	CheckScriptsCompatibility() error

	UInstallCommitRebooter
	StateRunner
}

const (
	defaultKeyFile = "mender-agent.pem"
)

var (
	defaultArtifactInfoFile  = path.Join(getConfDirPath(), "artifact_info")
	defaultDeviceTypeFile    = path.Join(getStateDirPath(), "device_type")
	defaultDataStore         = getStateDirPath()
	defaultArtScriptsPath    = path.Join(getStateDirPath(), "scripts")
	defaultRootfsScriptsPath = path.Join(getConfDirPath(), "scripts")

	errNoArtifactName = errors.New("cannot determine current artifact name")
)

type MenderState int

const (
	// initial state
	MenderStateInit MenderState = iota
	// idle state; waiting for transition to the new state
	MenderStateIdle
	// client is bootstrapped, i.e. ready to go
	MenderStateAuthorize
	// wait before authorization attempt
	MenderStateAuthorizeWait
	// inventory update
	MenderStateInventoryUpdate
	// wait for new update or inventory sending
	MenderStateCheckWait
	// check update
	MenderStateUpdateCheck
	// update fetch
	MenderStateUpdateFetch
	// update store
	MenderStateUpdateStore
	// install update
	MenderStateUpdateInstall
	// wait before retrying fetch & install after first failing (timeout,
	// for example)
	MenderStateFetchStoreRetryWait
	// varify update
	MenderStateUpdateVerify
	// commit needed
	MenderStateUpdateCommit
	// status report
	MenderStateUpdateStatusReport
	// wait before retrying sending either report or deployment logs
	MenderStatusReportRetryState
	// error reporting status
	MenderStateReportStatusError
	// reboot
	MenderStateReboot
	// first state after booting device after rollback reboot
	MenderStateAfterReboot
	//rollback
	MenderStateRollback
	// reboot after rollback
	MenderStateRollbackReboot
	// first state after booting device after rollback reboot
	MenderStateAfterRollbackReboot
	// error
	MenderStateError
	// update error
	MenderStateUpdateError
	// exit state
	MenderStateDone
)

var (
	stateNames = map[MenderState]string{
		MenderStateInit:                "init",
		MenderStateIdle:                "idle",
		MenderStateAuthorize:           "authorize",
		MenderStateAuthorizeWait:       "authorize-wait",
		MenderStateInventoryUpdate:     "inventory-update",
		MenderStateCheckWait:           "check-wait",
		MenderStateUpdateCheck:         "update-check",
		MenderStateUpdateFetch:         "update-fetch",
		MenderStateUpdateStore:         "update-store",
		MenderStateUpdateInstall:       "update-install",
		MenderStateFetchStoreRetryWait: "fetch-install-retry-wait",
		MenderStateUpdateVerify:        "update-verify",
		MenderStateUpdateCommit:        "update-commit",
		MenderStateUpdateStatusReport:  "update-status-report",
		MenderStatusReportRetryState:   "update-retry-report",
		MenderStateReportStatusError:   "status-report-error",
		MenderStateReboot:              "reboot",
		MenderStateAfterReboot:         "after-reboot",
		MenderStateRollback:            "rollback",
		MenderStateRollbackReboot:      "rollback-reboot",
		MenderStateAfterRollbackReboot: "after-rollback-reboot",
		MenderStateError:               "error",
		MenderStateUpdateError:         "update-error",
		MenderStateDone:                "finished",
	}

	//IMPORTANT: make sure that all the statuses that require
	// the report to be sent to the backend are assigned here.
	// See shouldReportUpdateStatus() function for how we are
	// deciding if report needs to be send to the backend.
	stateStatus = map[MenderState]string{
		MenderStateInit:                "",
		MenderStateIdle:                "",
		MenderStateAuthorize:           "",
		MenderStateAuthorizeWait:       "",
		MenderStateInventoryUpdate:     "",
		MenderStateCheckWait:           "",
		MenderStateUpdateCheck:         "",
		MenderStateUpdateFetch:         client.StatusDownloading,
		MenderStateUpdateStore:         client.StatusDownloading,
		MenderStateUpdateInstall:       client.StatusInstalling,
		MenderStateFetchStoreRetryWait: "",
		MenderStateUpdateVerify:        client.StatusRebooting,
		MenderStateUpdateCommit:        client.StatusRebooting,
		MenderStateUpdateStatusReport:  "",
		MenderStatusReportRetryState:   "",
		MenderStateReportStatusError:   "",
		MenderStateReboot:              client.StatusRebooting,
		MenderStateAfterReboot:         client.StatusRebooting,
		MenderStateRollback:            client.StatusRebooting,
		MenderStateRollbackReboot:      client.StatusRebooting,
		MenderStateAfterRollbackReboot: client.StatusRebooting,
		MenderStateError:               "",
		MenderStateUpdateError:         client.StatusFailure,
		MenderStateDone:                "",
	}
)

func (m MenderState) Status() string {
	return stateStatus[m]
}

func (m MenderState) MarshalJSON() ([]byte, error) {
	n, ok := stateNames[m]
	if !ok {
		return nil, fmt.Errorf("marshal error; unknown state %v", m)
	}
	return json.Marshal(n)
}

func (m MenderState) String() string {
	return stateNames[m]
}

func (m *MenderState) UnmarshalJSON(data []byte) error {
	var s string
	err := json.Unmarshal(data, &s)
	if err != nil {
		return err
	}
	for k, v := range stateNames {
		if v == s {
			*m = k
			return nil
		}
	}
	return fmt.Errorf("unmarshal error; unknown state %s", s)
}

type mender struct {
	UInstallCommitRebooter
	updater             client.Updater
	state               State
	stateScriptExecutor statescript.Executor
	stateScriptPath     string
	config              menderConfig
	artifactInfoFile    string
	deviceTypeFile      string
	forceBootstrap      bool
	authReq             client.AuthRequester
	authMgr             AuthManager
	api                 *client.ApiClient
	authToken           client.AuthToken
}

type MenderPieces struct {
	device  UInstallCommitRebooter
	store   store.Store
	authMgr AuthManager
}

func NewMender(config menderConfig, pieces MenderPieces) (*mender, error) {
	api, err := client.New(config.GetHttpConfig())
	if err != nil {
		return nil, errors.Wrap(err, "error creating HTTP client")
	}

	stateScrExec := statescript.Launcher{
		ArtScriptsPath:          defaultArtScriptsPath,
		RootfsScriptsPath:       defaultRootfsScriptsPath,
		SupportedScriptVersions: []int{2},
		Timeout:                 config.StateScriptTimeoutSeconds,
		RetryInterval:           config.StateScriptRetryIntervalSeconds,
		RetryTimeout:            config.StateScriptRetryTimeoutSeconds,
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
		stateScriptExecutor:    stateScrExec,
		stateScriptPath:        defaultArtScriptsPath,
	}

	if m.authMgr != nil {
		if err := m.loadAuth(); err != nil {
			log.Errorf("error loading authentication for HTTP client: %v", err)
		}
	}

	return m, nil
}

func getManifestData(dataType, manifestFile string) (string, error) {
	// This is where Yocto stores buid information
	manifest, err := os.Open(manifestFile)
	if err != nil {
		return "", err
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
				return "", fmt.Errorf("Broken device manifest file: (%v)", lineID)
			}
			log.Debug("Current manifest data: ", strings.TrimSpace(lineID[1]))
			return strings.TrimSpace(lineID[1]), nil
		}
	}
	err = scanner.Err()
	if err != nil {
		log.Error(err)
	}
	return "", err
}

func (m *mender) GetCurrentArtifactName() (string, error) {
	return getManifestData("artifact_name", m.artifactInfoFile)
}

func (m *mender) GetDeviceType() (string, error) {
	return getManifestData("device_type", m.deviceTypeFile)
}

func (m *mender) GetArtifactVerifyKey() []byte {
	return m.config.GetVerificationKey()
}

func GetCurrentArtifactName(artifactInfoFile string) (string, error) {
	return getManifestData("artifact_name", artifactInfoFile)
}

func GetDeviceType(deviceTypeFile string) (string, error) {
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

func (m *mender) IsAuthorized() bool {
	if m.authMgr.IsAuthorized() {
		// AuthToken is present in store

		if err := m.loadAuth(); err != nil {
			return false
		}
		log.Info("authorization data present and valid")
		return true
	}
	return false
}

func (m *mender) Authorize() menderError {
	var rsp []byte
	var err error
	var server *client.MenderServer

	if m.authMgr.IsAuthorized() {
		log.Info("authorization data present and valid, skipping authorization attempt")
		return m.loadAuth()
	}

	if err := m.Bootstrap(); err != nil {
		log.Errorf("bootstrap failed: %s", err)
		return err
	}

	// Cycle through servers and attempt to authorize.
	m.authToken = noAuthToken
	serverIterator := nextServerIterator(m)
	if serverIterator == nil {
		return NewFatalError(errors.New("Empty server list in mender.conf!"))
	}
	if server = serverIterator(); server == nil {
		return NewFatalError(errors.New("Empty server list in mender.conf!"))
	}
	for {
		rsp, err = m.authReq.Request(m.api, server.ServerURL, m.authMgr)

		if err == nil {
			// SUCCESS!
			break
		}
		prevHost := server.ServerURL
		server = serverIterator()
		if server == nil {
			break
		}
		log.Warnf("Failed to authorize %q; attempting %q.",
			prevHost, server.ServerURL)
	}
	if err != nil {
		// Generate and report error.
		errCause := errors.Cause(err)
		if errCause == client.AuthErrorUnauthorized {
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

	log.Info("successfully received new authorization data")

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
	return m.updater.FetchUpdate(m.api, url, m.GetRetryPollInterval())
}

// Check if new update is available. In case of errors, returns nil and error
// that occurred. If no update is available *UpdateResponse is nil, otherwise it
// contains update information.
func (m *mender) CheckUpdate() (*client.UpdateResponse, menderError) {
	currentArtifactName, err := m.GetCurrentArtifactName()
	if err != nil || currentArtifactName == "" {
		log.Error("could not get the current artifact name")
		if err == nil {
			err = errors.New("artifact name is empty")
		}
		return nil, NewTransientError(fmt.Errorf("could not read the artifact name. This is a necessary condition in order for a mender update to finish safely. Please give the current artifact a name (This can be done by adding a name to the file /etc/mender/artifact_info) err: %v", err))
	}

	deviceType, err := m.GetDeviceType()
	if err != nil {
		log.Errorf("Unable to verify the existing hardware. Update will continue anyways: %v : %v", defaultDeviceTypeFile, err)
	}
	haveUpdate, err := m.updater.GetScheduledUpdate(m.api.Request(m.authToken, nextServerIterator(m), reauthorize(m)),
		m.config.Servers[0].ServerURL, client.CurrentUpdate{
			Artifact:   currentArtifactName,
			DeviceType: deviceType,
		})

	if err != nil {
		// remove authentication token if device is not authorized
		errCause := errors.Cause(err)
		if errCause == client.ErrNotAuthorized {
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

	if update.ArtifactName() == currentArtifactName {
		log.Info("Attempting to upgrade to currently installed artifact name, not performing upgrade.")
		return &update, NewTransientError(os.ErrExist)
	}
	return &update, nil
}

func (m *mender) ReportUpdateStatus(update client.UpdateResponse, status string) menderError {
	s := client.NewStatus()
	err := s.Report(m.api.Request(m.authToken, nextServerIterator(m), reauthorize(m)), m.config.Servers[0].ServerURL,
		client.StatusReport{
			DeploymentID: update.ID,
			Status:       status,
		})
	if err != nil {
		log.Error("error reporting update status: ", err)
		// remove authentication token if device is not authorized
		errCause := errors.Cause(err)
		if errCause == client.ErrNotAuthorized {
			if remErr := m.authMgr.RemoveAuthToken(); remErr != nil {
				log.Warn("can not remove rejected authentication token")
			}
		} else if errCause == client.ErrDeploymentAborted {
			return NewFatalError(err)
		}
		return NewTransientError(err)
	}
	return nil
}

/* client closures */
// see client.go: ApiRequest.Do()

// reauthorize is a closure very similar to mender.Authorize(), but instead of
// walking through all servers in the menderConfig.Servers list, it only tries
// serverURL.
func reauthorize(m *mender) func(string) (client.AuthToken, error) {
	// force reauthorization
	return func(serverURL string) (client.AuthToken, error) {
		var rsp []byte
		var err error

		if err := m.Bootstrap(); err != nil {
			log.Errorf("bootstrap failed: %s", err)
			return noAuthToken, err
		}
		// assume token is invalid - remove from storage
		if err := m.authMgr.RemoveAuthToken(); err != nil {
			return noAuthToken, errors.New("Failed to remove auth token")
		}

		m.authToken = noAuthToken
		rsp, err = m.authReq.Request(m.api, serverURL, m.authMgr)
		if err != nil {
			// Generate and report error.
			errCause := errors.Cause(err)
			if errCause == client.AuthErrorUnauthorized {
				// make sure to remove auth token once device is rejected
				if remErr := m.authMgr.RemoveAuthToken(); remErr != nil {
					log.Warn("can not remove rejected authentication token")
				}
			}
			return noAuthToken, NewTransientError(errors.Wrap(err, "authorization request failed"))
		}

		err = m.authMgr.RecvAuthResponse(rsp)
		if err != nil {
			return noAuthToken, NewTransientError(errors.Wrap(err, "failed to parse authorization response"))
		}

		err = m.loadAuth()
		if err == nil {
			return m.authMgr.AuthToken()
		}
		return noAuthToken, err
	}
}

// nextServerIterator returns an iterator like function that cycles through the
// list of available servers in mender.menderConfig.Servers
func nextServerIterator(m *mender) func() *client.MenderServer {
	numServers := len(m.config.Servers)
	if m.config.Servers == nil || numServers == 0 {
		log.Error("Empty server list! Make sure at least one server" +
			"is specified in /etc/mender/mender.conf")
		return nil
	}

	idx := 0
	return func() (server *client.MenderServer) {
		var ret *client.MenderServer
		if idx < numServers {
			ret = &m.config.Servers[idx]
			idx++
		} else {
			// return nil which terminates Do()
			// and reset index (for reuse of request)
			ret = nil
			idx = 0
		}
		return ret
	}
}

/* client closures END */

func (m *mender) UploadLog(update client.UpdateResponse, logs []byte) menderError {
	s := client.NewLog()
	err := s.Upload(m.api.Request(m.authToken, nextServerIterator(m), reauthorize(m)), m.config.Servers[0].ServerURL,
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
	t := time.Duration(m.config.UpdatePollIntervalSeconds) * time.Second
	if t == 0 {
		log.Warn("UpdatePollIntervalSeconds is not defined")
		t = 30 * time.Minute
	}
	return t
}

func (m mender) GetInventoryPollInterval() time.Duration {
	t := time.Duration(m.config.InventoryPollIntervalSeconds) * time.Second
	if t == 0 {
		log.Warn("InventoryPollIntervalSeconds is not defined")
		t = 30 * time.Minute
	}
	return t
}

func (m mender) GetRetryPollInterval() time.Duration {
	t := time.Duration(m.config.RetryPollIntervalSeconds) * time.Second
	if t == 0 {
		log.Warn("RetryPollIntervalSeconds is not defined")
		t = 5 * time.Minute
	}
	return t
}

func (m *mender) SetNextState(s State) {
	m.state = s
}

func (m *mender) GetCurrentState() State {
	return m.state
}

func shouldTransit(from, to State) bool {
	return from.Transition() != to.Transition()
}

func TransitionError(s State, action string) State {
	me := NewTransientError(errors.New("error executing state script"))
	log.Errorf("will transit to error state from: %s [%s]",
		s.Id().String(), s.Transition().String())
	switch t := s.(type) {
	case *UpdateFetchState:
		new := NewUpdateStatusReportState(t.update, client.StatusFailure)
		new.SetTransition(ToError)
		return new
	case *UpdateStoreState:
		if action == "Leave" {
			new := NewUpdateStatusReportState(t.update, client.StatusFailure)
			new.SetTransition(ToError)
			return new
		}
		return NewUpdateErrorState(me, t.update)
	case *UpdateInstallState:
		return NewRollbackState(t.Update(), false, false)
	case *RebootState:
		return NewRollbackState(t.Update(), false, false)
	case *AfterRebootState:
		return NewRollbackState(t.Update(), true, true)
	case *UpdateVerifyState:
		return NewRollbackState(t.Update(), true, true)
	case *UpdateCommitState:
		return NewRollbackState(t.Update(), true, true)
	case *RollbackState:
		if t.reboot {
			return NewRollbackRebootState(t.Update())
		}
		return NewUpdateErrorState(me, t.Update())
	case *RollbackRebootState:
		NewUpdateErrorState(me, t.Update())
	default:
		return NewErrorState(me)
	}
	return NewErrorState(me)
}

func shouldReportUpdateStatus(state MenderState) bool {
	return state.Status() != ""
}

func getUpdateFromState(state State) (client.UpdateResponse, error) {
	upd, ok := state.(UpdateState)
	if ok {
		return upd.Update(), nil
	}
	return client.UpdateResponse{},
		errors.Errorf("failed to extract the update from state: %s", state)
}

func (m *mender) TransitionState(to State, ctx *StateContext) (State, bool) {
	from := m.GetCurrentState()

	log.Infof("State transition: %s [%s] -> %s [%s]",
		from.Id(), from.Transition().String(),
		to.Id(), to.Transition().String())

	if to.Transition() == ToNone {
		to.SetTransition(from.Transition())
	}

	var report *client.StatusReportWrapper
	if shouldReportUpdateStatus(to.Id()) {
		upd, err := getUpdateFromState(to)
		if err != nil {
			log.Error(err)
		} else {
			report = &client.StatusReportWrapper{
				API: m.api.Request(m.authToken, nextServerIterator(m), reauthorize(m)),
				URL: m.config.Servers[0].ServerURL,
				Report: client.StatusReport{
					DeploymentID: upd.ID,
					Status:       to.Id().Status(),
				},
			}
		}
	}

	if shouldTransit(from, to) {
		if to.Transition().IsToError() && !from.Transition().IsToError() {
			log.Debug("transitioning to error state")

			// Set the reported status to be the same as the state where the
			// error happened. THIS IS IMPORTANT AS WE CAN SEND THE client.StatusFailure
			// ONLY ONCE.
			if report != nil {
				report.Report.Status = from.Id().Status()
			}
			// call error scripts
			from.Transition().Error(m.stateScriptExecutor, report)
		} else {
			// do transition to ordinary state
			if err := from.Transition().Leave(m.stateScriptExecutor, report, ctx.store); err != nil {
				log.Errorf("error executing leave script for %s state: %v", from.Id(), err)
				return TransitionError(from, "Leave"), false
			}
		}

		m.SetNextState(to)

		if err := to.Transition().Enter(m.stateScriptExecutor, report, ctx.store); err != nil {
			log.Errorf("error calling enter script for (error) %s state: %v", to.Id(), err)
			// we have not entered to state; so handle from state error
			return TransitionError(from, "Enter"), false
		}
	}

	m.SetNextState(to)

	// execute current state action
	return to.Handle(ctx, m)
}

func (m *mender) InventoryRefresh() error {
	ic := client.NewInventory()
	idg := NewInventoryDataRunner(path.Join(getDataDirPath(), "inventory"))

	artifactName, err := m.GetCurrentArtifactName()
	if err != nil || artifactName == "" {
		if err == nil {
			err = errors.New("artifact name is empty")
		}
		errstr := fmt.Sprintf("could not read the artifact name. This is a necessary condition in order for a mender update to finish safely. Please give the current artifact a name (This can be done by adding a name to the file /etc/mender/artifact_info) err: %v", err)
		return errors.Wrap(errNoArtifactName, errstr)
	}

	idata, err := idg.Get()
	if err != nil {
		// at least report device type
		log.Errorf("failed to obtain inventory data: %s", err.Error())
	}

	deviceType, err := m.GetDeviceType()
	if err != nil {
		log.Errorf("Unable to verify the existing hardware. Update will continue anyways: %v : %v", defaultDeviceTypeFile, err)
	}
	reqAttr := []client.InventoryAttribute{
		{Name: "device_type", Value: deviceType},
		{Name: "artifact_name", Value: artifactName},
		{Name: "mender_client_version", Value: VersionString()},
	}

	if idata == nil {
		idata = make(client.InventoryData, 0, len(reqAttr))
	}
	idata.ReplaceAttributes(reqAttr)

	if idata == nil {
		log.Infof("no inventory data to submit")
		return nil
	}

	err = ic.Submit(m.api.Request(m.authToken, nextServerIterator(m), reauthorize(m)), m.config.Servers[0].ServerURL, idata)
	if err != nil {
		return errors.Wrapf(err, "failed to submit inventory data")
	}

	return nil
}

func (m *mender) CheckScriptsCompatibility() error {
	return m.stateScriptExecutor.CheckRootfsScriptsVersion()
}

func (m *mender) InstallUpdate(from io.ReadCloser, size int64) error {
	deviceType, err := m.GetDeviceType()
	if err != nil {
		log.Errorf("Unable to verify the existing hardware. Update will continue anyways: %v : %v", defaultDeviceTypeFile, err)
	}
	return installer.Install(from, deviceType,
		m.GetArtifactVerifyKey(), m.stateScriptPath, m.UInstallCommitRebooter, true)
}
