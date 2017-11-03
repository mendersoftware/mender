// Copyright 2017 Northern.tech AS
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
	"reflect"
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
)

type MenderState int

const (
	// initial state
	MenderStateInit MenderState = iota
	// idle state; waiting for transition to the new state
	MenderStateIdle // DONE
	// client is bootstrapped, i.e. ready to go
	MenderStateAuthorize // DONE
	// wait before authorization attempt
	MenderStateAuthorizeWait // DONE
	// inventory update
	MenderStateInventoryUpdate // DONE
	// wait for new update or inventory sending
	MenderStateCheckWait // DONE
	// check update
	MenderStateUpdateCheck // DONE
	// update fetch
	MenderStateUpdateFetch // TODO (update)
	// update store
	MenderStateUpdateStore // TODO (io.ReadCloser, size int, update) io.Readcloser is a mender.api instance
	// install update
	MenderStateUpdateInstall // TODO (update)
	// wait before retrying fetch & install after first failing (timeout,
	// for example)
	MenderStateFetchStoreRetryWait // TODO (from State, update, err error)
	// varify update
	MenderStateUpdateVerify // TODO (update)
	// commit needed
	MenderStateUpdateCommit // TODO (update)
	// status report
	MenderStateUpdateStatusReport // TODO (update, client.StatusSuccess/ status string)
	// wait before retrying sending either report or deployment logs
	MenderStatusReportRetryState // TODO (reportState State, update, status, tries, int)
	// error reporting status
	MenderStateReportStatusError // TODO (update, status)
	// reboot
	MenderStateReboot // TODO (update)
	// first state after booting device after rollback reboot
	MenderStateAfterReboot // TODO (update)
	//rollback
	MenderStateRollback // TODO (update, swappartitions, doReboot bool)
	// reboot after rollback
	MenderStateRollbackReboot // TODO (update)
	// first state after booting device after rollback reboot
	MenderStateAfterRollbackReboot // TODO (update)
	// error
	MenderStateError // TODO (err menderError)
	// update error
	MenderStateUpdateError // TODO (err menderError, update)
	// exit state
	MenderStateDone // TODO
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
)

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
		RetryTimeout:            config.StateScriptRetryIntervalSeconds,
		RetryInterval:           config.StateScriptRetryTimeoutSeconds,
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
		log.Info("authorization data present and valid")
		if err := m.loadAuth(); err != nil {
			return false
		}
		return true
	}
	return false
}

func (m *mender) Authorize() menderError {
	if m.authMgr.IsAuthorized() {
		log.Info("authorization data present and valid, skipping authorization attempt")
		return m.loadAuth()
	}

	if err := m.Bootstrap(); err != nil {
		log.Errorf("bootstrap failed: %s", err)
		return err
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
	return m.updater.FetchUpdate(m.api, url, m.GetRetryPollInterval())
}

// Check if new update is available. In case of errors, returns nil and error
// that occurred. If no update is available *UpdateResponse is nil, otherwise it
// contains update information.
func (m *mender) CheckUpdate() (*client.UpdateResponse, menderError) {
	currentArtifactName, err := m.GetCurrentArtifactName()
	if err != nil {
		log.Error("could not get the current artifact name")
	}

	deviceType, err := m.GetDeviceType()
	if err != nil {
		log.Errorf("Unable to verify the existing hardware. Update will continue anyways: %v : %v", defaultDeviceTypeFile, err)
	}
	haveUpdate, err := m.updater.GetScheduledUpdate(m.api.Request(m.authToken),
		m.config.ServerURL, client.CurrentUpdate{
			Artifact:   currentArtifactName,
			DeviceType: deviceType,
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

	if update.ArtifactName() == currentArtifactName {
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

		// remove authentication token if device is not authorized
		if err == client.ErrNotAuthorized {
			if remErr := m.authMgr.RemoveAuthToken(); remErr != nil {
				log.Warn("can not remove rejected authentication token")
			}
		}

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

// // also add all the necessary mender-states
// func GetState(mender *mender, ctx *StateContext, toState bool) State { // TODO - Which states should we recover from?

// 	sd, err := LoadStateData(ctx.store)
// 	if err != nil {
// 		log.Errorf("failed to read state data")
// 	}

// 	var state MenderState
// 	var updateInfo client.UpdateResponse
// 	var updateStatus string
// 	// var mErr menderError
// 	if toState {
// 		state = sd.ToState
// 		updateInfo = sd.ToStateRebootData.UpdateInfo
// 		updateStatus = sd.ToStateRebootData.UpdateStatus
// 		// mErr = sd.ToStateRebootData.MenderError
// 	} else {
// 		state = sd.FromState
// 		updateInfo = sd.FromStateRebootData.UpdateInfo
// 		updateStatus = sd.FromStateRebootData.UpdateStatus
// 		// mErr = sd.FromStateRebootData.MenderError
// 	}

// 	switch state {
// 	case MenderStateIdle:
// 		return idleState
// 	case MenderStateAuthorize:
// 		return authorizeState

// 	case MenderStateAuthorizeWait:
// 		return authorizeWaitState
// 	// inventory update
// 	case MenderStateInventoryUpdate:
// 		return inventoryUpdateState
// 		// wait for new update or inventory sending
// 	case MenderStateCheckWait:
// 		return checkWaitState
// 		// check update
// 	case MenderStateUpdateCheck:
// 		return updateCheckState
// 		// update fetch
// 	case MenderStateUpdateFetch:
// 		return NewUpdateFetchState(updateInfo)
// 		// // update store
// 	case MenderStateUpdateStore: // TODO - tricky dicky
// 		in, size, err := mender.FetchUpdate(updateInfo.URI())
// 		if err != nil {
// 			log.Error("what should we return now?") // FIXME
// 		}
// 		return NewUpdateStoreState(in, size, updateInfo)
// 	// // install update
// 	case MenderStateUpdateInstall:
// 		return NewUpdateInstallState(updateInfo)
// 	// // wait before retrying fetch & install after first failing (timeout,
// 	// // for example)
// 	// case MenderStateFetchStoreRetryWait: // TODO - needs fromState and error
// 	// // varify update
// 	case MenderStateUpdateVerify:
// 		return NewUpdateVerifyState(updateInfo)
// 	// // commit needed
// 	case MenderStateUpdateCommit:
// 		return NewUpdateCommitState(updateInfo)
// 	// // status report
// 	case MenderStateUpdateStatusReport:
// 		return NewUpdateStatusReportState(updateInfo, updateStatus)
// 	// // wait before retrying sending either report or deployment logs
// 	// case MenderStatusReportRetryState: // TODO - needs reportState, update, status and #tries
// 	// // error reporting status
// 	// MenderStateReportStatusError // TODO - currently does not handle any error states. This needs to be fixed
// 	// // reboot
// 	case MenderStateReboot:
// 		return NewRebootState(updateInfo)
// 	// // first state after booting device after rollback reboot
// 	// case MenderStateAfterReboot:
// 	// 	return NewAfterRebootState(s.UpdateInfo) // TODO - this is handled in init, so does it need to be handled here?
// 	// //rollback
// 	// MenderStateRollback
// 	// // reboot after rollback
// 	// MenderStateRollbackReboot // TODO - also handled in init
// 	// // first state after booting device after rollback reboot
// 	// MenderStateAfterRollbackReboot // TODO - handled in init
// 	// // error
// 	// MenderStateError
// 	// // update error
// 	// MenderStateUpdateError
// 	// exit state
// 	case MenderStateDone:
// 		return doneState
// 	default:
// 		log.Debug("GetState default (init) returned")
// 		return initState
// 	}
// }

// func (m *mender) GetCurrentState(s *StateContext) (State, State, TransitionStatus) {
// 	sd, err := LoadStateData(s.store)
// 	if err != nil {
// 		log.Debugf("GetCurrentState: Failed to load state-data: err: %v", err)
// 		return m.state, m.state, NoStatus // init -> init // TODO - what to return here
// 	}
// 	log.Debugf("GetCurrentState: loaded states: from: %v -> to: %v", sd.FromState, sd.ToState)

// 	var fromState, toState State

// 	fromState = GetState(m, s, false)
// 	toState = GetState(m, s, true)

// 	return fromState, toState, sd.TransitionStatus
// }

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

// TransitionStatus is a variable to remember which parts of a transition is done
type TransitionStatus int

const (
	// TODO - necessary to export this?
	UnInitialised TransitionStatus = iota // needed for updatestatedata method
	NoStatus
	LeaveDone
	EnterDone
	StateDone // FIXME - here for flexibility - is it really needed?
)

type StateDataDiscard struct {
	store store.Store
}

func (s *StateDataDiscard) ReadAll(name string) ([]byte, error) {
	return s.store.ReadAll(name)
}

// Do not write to the store
func (s *StateDataDiscard) WriteAll(name string, data []byte) error {
	log.Debugf("Writing all to nil")
	return nil
}

// // open entry 'name' for reading
func (s *StateDataDiscard) OpenRead(name string) (io.ReadCloser, error) {
	return s.store.OpenRead(name)
}

// open entry 'name' for writing, this may create a temporary entry for
// writing data, once finished, one should call Commit() from
// WriteCloserCommitter interface
func (s *StateDataDiscard) OpenWrite(name string) (store.WriteCloserCommitter, error) {
	return s.store.OpenWrite(name)
}

// // remove an entry
func (s *StateDataDiscard) Remove(name string) error {
	return s.store.Remove(name)
}

// // close the store
func (s *StateDataDiscard) Close() error {
	return s.store.Close()
}

func ResetLeaveTransition(s store.Store) {

	sd, err := LoadStateData(s)
	if err != nil {
		log.Error("failed to load state data")
	}
	sd.LeaveTransition = ToNone
	if err := StoreStateData(s, sd); err != nil {
		log.Error("failed to load state data")
	}
}

// TODO - implement this using reflection
func UpdateStateData(s store.Store, newSD StateData) {

	oldSD, err := LoadStateData(s)
	if err != nil {
		log.Error("failed to load state data")
	}

	if newSD.Version != 0 {
		oldSD.Version = newSD.Version
	}

	if newSD.TransitionStatus != UnInitialised {
		oldSD.TransitionStatus = newSD.TransitionStatus
	}

	if newSD.LeaveTransition != ToNone {
		oldSD.LeaveTransition = newSD.LeaveTransition
	}

	// state-recovery data writes should be atomic
	if newSD.Name != MenderStateInit || !reflect.DeepEqual(newSD.UpdateInfo, client.UpdateResponse{}) || newSD.UpdateStatus != "" ||
		newSD.SysErr != nil || newSD.MenderErr != (MenderError{}) {
		log.Debug("Writing state data")
		oldSD.Name = newSD.Name
		oldSD.UpdateInfo = newSD.UpdateInfo
		oldSD.UpdateStatus = newSD.UpdateStatus
		oldSD.SysErr = newSD.SysErr
		oldSD.MenderErr = newSD.MenderErr
	}

	if err := StoreStateData(s, oldSD); err != nil {
		log.Error("failed to store state-data")
	} else {
		log.Debugf("wrote oldSD: %v to disk", oldSD)
	}
}

// TODO - only the relevant data needed to recreate the states should be stored in the handling-functions
// all state storage is done in this transition
func (m *mender) TransitionState(to State, ctx *StateContext) (State, bool) {

	from := m.GetCurrentState()

	var err error

	oldCtx := ctx
	if from == initState && to == initState {
		log.Debugf("In init -> init, do not write to the datastore")
		// Do not write anyting in the initial transition: init -> init
		ctx = &StateContext{
			store: &StateDataDiscard{ctx.store},
		}
	}

	sd, err := LoadStateData(ctx.store)
	status := sd.TransitionStatus // TODO - fixup surpuflous status
	log.Debugf("TransitionStatus: %v", status)
	if err != nil {
		log.Errorf("Failed to read state-data")
	}

	log.Infof("State transition: %s [%s] -> %s [%s]",
		from.Id(), from.Transition().String(),
		to.Id(), to.Transition().String())

	if to.Transition() == ToNone {
		to.SetTransition(from.Transition())
	}

	if shouldTransit(from, to) {
		if to.Transition().IsToError() && !from.Transition().IsToError() {
			log.Debug("transitioning to error state")
			// call error scripts
			from.Transition().Error(m.stateScriptExecutor)
		} else {
			// do transition to ordinary state
			if err := from.Transition().Leave(m.stateScriptExecutor); err != nil {
				log.Errorf("error executing leave script for %s state: %v", from.Id(), err)
				return TransitionError(from, "Leave"), false
			}
			UpdateStateData(ctx.store, StateData{TransitionStatus: LeaveDone})
			ResetLeaveTransition(ctx.store)
			log.Debug("Removed the leave transition from the store")
		}

		m.SetNextState(to)

		if err := to.Transition().Enter(m.stateScriptExecutor); err != nil {
			log.Errorf("error calling enter script for (error) %s state: %v", to.Id(), err)
			// we have not entered to state; so handle from state error
			return TransitionError(from, "Enter"), false
		}
		UpdateStateData(ctx.store, StateData{TransitionStatus: EnterDone})
	}

	m.SetNextState(to)

	// execute current state action
	log.Debugf("Handling state: %s", to.Id())
	nxt, cancel := to.Handle(oldCtx, m)
	log.Debugf("Storing recovery data for: %s", nxt.Id())
	// the transition is finished, so store the needed data
	log.Debugf("from transition: %v", to.Transition())
	nxt.SaveRecoveryData(to.Transition(), ctx.store)
	// also reset the transitioncolouring -> this can be stored in the saverecoverydata functions
	UpdateStateData(ctx.store, StateData{TransitionStatus: NoStatus})
	return nxt, cancel
}

func (m *mender) InventoryRefresh() error {
	ic := client.NewInventory()
	idg := NewInventoryDataRunner(path.Join(getDataDirPath(), "inventory"))

	idata, err := idg.Get()
	if err != nil {
		// at least report device type
		log.Errorf("failed to obtain inventory data: %s", err.Error())
	}

	deviceType, err := m.GetDeviceType()
	if err != nil {
		log.Errorf("Unable to verify the existing hardware. Update will continue anyways: %v : %v", defaultDeviceTypeFile, err)
	}
	artifactName, err := m.GetCurrentArtifactName()
	if err != nil {
		log.Errorf("Cannot determine current artifact. Update will continue anyways: %v : %v", defaultDeviceTypeFile, err)
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

	err = ic.Submit(m.api.Request(m.authToken), m.config.ServerURL, idata)
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
