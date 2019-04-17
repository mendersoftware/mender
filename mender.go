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
	"fmt"
	"io"
	"os"
	"path"
	"time"

	"github.com/mendersoftware/log"

	"github.com/mendersoftware/mender/client"
	"github.com/mendersoftware/mender/datastore"
	"github.com/mendersoftware/mender/installer"
	"github.com/mendersoftware/mender/statescript"
	"github.com/mendersoftware/mender/store"
	"github.com/pkg/errors"
)

type Controller interface {
	IsAuthorized() bool
	Authorize() menderError

	GetCurrentArtifactName() (string, error)
	GetUpdatePollInterval() time.Duration
	GetInventoryPollInterval() time.Duration
	GetRetryPollInterval() time.Duration

	CheckUpdate() (*datastore.UpdateInfo, menderError)
	FetchUpdate(url string) (io.ReadCloser, int64, error)

	NewStatusReportWrapper(updateId string,
		stateId datastore.MenderState) *client.StatusReportWrapper
	ReportUpdateStatus(update *datastore.UpdateInfo, status string) menderError
	UploadLog(update *datastore.UpdateInfo, logs []byte) menderError
	InventoryRefresh() error

	CheckScriptsCompatibility() error
	GetScriptExecutor() statescript.Executor

	ReadArtifactHeaders(from io.ReadCloser) (*installer.Installer, error)
	GetInstallers() []installer.PayloadUpdatePerformer

	RestoreInstallersFromTypeList(payloadTypes []string) error

	StateRunner
}

const (
	defaultKeyFile = "mender-agent.pem"
)

var (
	errNoArtifactName = errors.New("cannot determine current artifact name")
)

var (
	//IMPORTANT: make sure that all the statuses that require
	// the report to be sent to the backend are assigned here.
	// See shouldReportUpdateStatus() function for how we are
	// deciding if report needs to be send to the backend.
	stateStatus = map[datastore.MenderState]string{
		datastore.MenderStateUpdateFetch:         client.StatusDownloading,
		datastore.MenderStateUpdateStore:         client.StatusDownloading,
		datastore.MenderStateUpdateInstall:       client.StatusInstalling,
		datastore.MenderStateUpdateVerify:        client.StatusRebooting,
		datastore.MenderStateUpdateCommit:        client.StatusRebooting,
		datastore.MenderStateReboot:              client.StatusRebooting,
		datastore.MenderStateAfterReboot:         client.StatusRebooting,
		datastore.MenderStateRollback:            client.StatusRebooting,
		datastore.MenderStateRollbackReboot:      client.StatusRebooting,
		datastore.MenderStateAfterRollbackReboot: client.StatusRebooting,
		datastore.MenderStateUpdateError:         client.StatusFailure,
	}
)

func StateStatus(m datastore.MenderState) string {
	status, ok := stateStatus[m]
	if ok {
		return status
	} else {
		return ""
	}
}

type mender struct {
	*deviceManager

	updater             client.Updater
	state               State
	stateScriptExecutor statescript.Executor
	forceBootstrap      bool
	authReq             client.AuthRequester
	authMgr             AuthManager
	api                 *client.ApiClient
	authToken           client.AuthToken
}

type MenderPieces struct {
	dualRootfsDevice installer.DualRootfsDevice
	store            store.Store
	authMgr          AuthManager
}

func NewMender(config *menderConfig, pieces MenderPieces) (*mender, error) {
	api, err := client.New(config.GetHttpConfig())
	if err != nil {
		return nil, errors.Wrap(err, "error creating HTTP client")
	}

	stateScrExec := newStateScriptExecutor(config)

	m := &mender{
		deviceManager:       NewDeviceManager(pieces.dualRootfsDevice, config, pieces.store),
		updater:             client.NewUpdate(),
		state:               initState,
		stateScriptExecutor: stateScrExec,
		authMgr:             pieces.authMgr,
		authReq:             client.NewAuth(),
		api:                 api,
		authToken:           noAuthToken,
	}

	if m.authMgr != nil {
		if err := m.loadAuth(); err != nil {
			log.Errorf("error loading authentication for HTTP client: %v", err)
		}
	}

	return m, nil
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
// that occurred. If no update is available *UpdateInfo is nil, otherwise it
// contains update information.
func (m *mender) CheckUpdate() (*datastore.UpdateInfo, menderError) {
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
	update, ok := haveUpdate.(datastore.UpdateInfo)
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

func (m *mender) NewStatusReportWrapper(updateId string,
	stateId datastore.MenderState) *client.StatusReportWrapper {

	return &client.StatusReportWrapper{
		API: m.api.Request(m.authToken, nextServerIterator(m), reauthorize(m)),
		URL: m.config.Servers[0].ServerURL,
		Report: client.StatusReport{
			DeploymentID: updateId,
			Status:       StateStatus(stateId),
		},
	}
}

func (m *mender) ReportUpdateStatus(update *datastore.UpdateInfo, status string) menderError {
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

func (m *mender) UploadLog(update *datastore.UpdateInfo, logs []byte) menderError {
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

func (m *mender) GetUpdatePollInterval() time.Duration {
	t := time.Duration(m.config.UpdatePollIntervalSeconds) * time.Second
	if t == 0 {
		log.Warn("UpdatePollIntervalSeconds is not defined")
		t = 30 * time.Minute
	}
	return t
}

func (m *mender) GetInventoryPollInterval() time.Duration {
	t := time.Duration(m.config.InventoryPollIntervalSeconds) * time.Second
	if t == 0 {
		log.Warn("InventoryPollIntervalSeconds is not defined")
		t = 30 * time.Minute
	}
	return t
}

func (m *mender) GetRetryPollInterval() time.Duration {
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

func (m *mender) GetScriptExecutor() statescript.Executor {
	return m.stateScriptExecutor
}

func shouldTransit(from, to State) bool {
	return from.Transition() != to.Transition()
}

func shouldReportUpdateStatus(state datastore.MenderState) bool {
	return StateStatus(state) != ""
}

func getUpdateFromState(state State) (*datastore.UpdateInfo, error) {
	upd, ok := state.(UpdateState)
	if ok {
		return upd.Update(), nil
	}
	return &datastore.UpdateInfo{},
		errors.Errorf("failed to extract the update from state: %s", state)
}

func (m *mender) TransitionState(to State, ctx *StateContext) (State, bool) {
	// In its own function so that we can test it with an alternative
	// Controller.
	return transitionState(to, ctx, m)
}

func transitionState(to State, ctx *StateContext, c Controller) (State, bool) {
	from := c.GetCurrentState()

	log.Infof("State transition: %s [%s] -> %s [%s]",
		from.Id(), from.Transition().String(),
		to.Id(), to.Transition().String())

	var report *client.StatusReportWrapper
	if shouldReportUpdateStatus(to.Id()) {
		upd, err := getUpdateFromState(to)
		if err != nil {
			log.Error(err)
		} else {
			report = c.NewStatusReportWrapper(upd.ID, to.Id())
		}
	}

	if shouldTransit(from, to) {
		if to.Transition().IsToError() && !from.Transition().IsToError() {
			log.Debug("transitioning to error state")

			// Set the reported status to be the same as the state where the
			// error happened. THIS IS IMPORTANT AS WE CAN SEND THE client.StatusFailure
			// ONLY ONCE.
			if report != nil {
				report.Report.Status = StateStatus(from.Id())
			}
			// call error scripts
			from.Transition().Error(c.GetScriptExecutor(), report)
		} else {
			// do transition to ordinary state
			if err := from.Transition().Leave(c.GetScriptExecutor(), report, ctx.store); err != nil {
				merr := NewTransientError(fmt.Errorf(
					"error executing leave script for %s state: %s",
					from.Id(), err.Error()))
				return from.HandleError(ctx, c, merr)
			}
		}
	}

	c.SetNextState(to)

	// If this is an update state, store new state in database.
	if us, ok := to.(UpdateState); ok {
		err := StoreStateData(ctx.store, datastore.StateData{
			Name:       us.Id(),
			UpdateInfo: *us.Update(),
		})
		if err != nil {
			log.Error("Could not write state data to persistent storage: ", err.Error())
			state, cancelled := us.HandleError(ctx, c, NewFatalError(err))
			return handleStateDataError(ctx, state, cancelled, us.Id(), us.Update(), err)
		}
	}

	if shouldTransit(from, to) {
		if err := to.Transition().Enter(c.GetScriptExecutor(), report, ctx.store); err != nil {
			merr := NewTransientError(fmt.Errorf(
				"error calling enter script for (error) %s state: %s",
				to.Id(), err.Error()))
			return to.HandleError(ctx, c, merr)
		}
	}

	// execute current state action
	return to.Handle(ctx, c)
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
