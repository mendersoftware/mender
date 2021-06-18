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
package app

import (
	"fmt"
	"io"
	"os"
	"path"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/pkg/errors"

	"github.com/mendersoftware/mender/client"
	"github.com/mendersoftware/mender/conf"
	"github.com/mendersoftware/mender/datastore"
	dev "github.com/mendersoftware/mender/device"
	"github.com/mendersoftware/mender/installer"
	inv "github.com/mendersoftware/mender/inventory"
	"github.com/mendersoftware/mender/statescript"
	"github.com/mendersoftware/mender/store"
	"github.com/mendersoftware/mender/utils"
)

type Controller interface {
	IsAuthorized() bool
	Authorize() menderError
	GetAuthToken() client.AuthToken

	GetControlMapPool() *ControlMapPool

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

const (
	errMsgInvalidDependsTypeF = "invalid type %T for dependency with name %s"
)

const (
	authManagerChannelName = "mender"
)

func StateStatus(m datastore.MenderState) string {
	status, ok := stateStatus[m]
	if ok {
		return status
	} else {
		return ""
	}
}

type Mender struct {
	*dev.DeviceManager

	// This state should be maintained private to the app package.
	updater             client.Updater
	state               State
	stateScriptExecutor statescript.Executor
	authManager         AuthManager
	api                 *client.ApiClient
	controlMapPool      *ControlMapPool
}

type MenderPieces struct {
	DualRootfsDevice installer.DualRootfsDevice
	Store            store.Store
	AuthManager      AuthManager
}

func NewMender(config *conf.MenderConfig, pieces MenderPieces) (*Mender, error) {
	api, err := client.New(config.GetHttpConfig())
	if err != nil {
		return nil, errors.Wrap(err, "error creating HTTP client")
	}

	stateScrExec := dev.NewStateScriptExecutor(config)

	controlMapPool := NewControlMap(
		pieces.Store,
		config.UpdateControlMapBootExpirationTimeSeconds,
		config.UpdateControlMapExpirationTimeSeconds,
	)

	m := &Mender{
		DeviceManager:       dev.NewDeviceManager(pieces.DualRootfsDevice, config, pieces.Store),
		updater:             client.NewUpdate(),
		state:               States.Init,
		stateScriptExecutor: stateScrExec,
		authManager:         pieces.AuthManager,
		api:                 api,
		controlMapPool:      controlMapPool,
	}

	return m, nil
}

// cache authorization code
func (m *Mender) loadAuth() (client.AuthToken, menderError) {
	inChan := m.authManager.GetInMessageChan()
	respChan := make(chan AuthManagerResponse)

	// request
	inChan <- AuthManagerRequest{
		Action:          ActionGetAuthToken,
		ResponseChannel: respChan,
	}

	// response
	resp := <-respChan
	if resp.Error != nil {
		return noAuthToken, NewTransientError(resp.Error)
	}

	return resp.AuthToken, nil
}

func (m *Mender) IsAuthorized() bool {
	authToken, err := m.loadAuth()
	if err != nil {
		return false
	}
	if authToken != noAuthToken {
		return true
	}
	return false
}

func (m *Mender) Authorize() menderError {
	inChan := m.authManager.GetInMessageChan()
	broadcastChan := m.authManager.GetBroadcastMessageChan(authManagerChannelName)
	respChan := make(chan AuthManagerResponse)

	// drain the broadcast channel
	select {
	case _ = <-broadcastChan:
	default:
	}

	// request
	inChan <- AuthManagerRequest{
		Action:          ActionFetchAuthToken,
		ResponseChannel: respChan,
	}

	// response
	resp := <-respChan
	if resp.Error != nil {
		return NewTransientError(resp.Error)
	}

	// wait for the broadcast notification
	resp, ok := <-broadcastChan
	if !ok || resp.Error != nil {
		return NewTransientError(errors.Wrap(resp.Error, "authorization request failed"))
	}

	return nil
}

func (m *Mender) GetAuthToken() client.AuthToken {
	authToken, err := m.loadAuth()
	if err != nil {
		log.Errorf("Could not load auth token: %s", err.Error())
	}
	return authToken
}

func (m *Mender) GetControlMapPool() *ControlMapPool {
	return m.controlMapPool
}

func (m *Mender) FetchUpdate(url string) (io.ReadCloser, int64, error) {
	return m.updater.FetchUpdate(m.api, url, m.GetRetryPollInterval())
}

func verifyArtifactDependencies(
	depends map[string]interface{},
	provides map[string]string,
) error {

	for key, depend := range depends {
		if key == "device_type" {
			// handled elsewhere
			continue
		}
		if p, ok := provides[key]; ok {
			switch depend.(type) {
			case []interface{}:
				if ok, err := utils.ElemInSlice(depend, p); ok {
					continue
				} else if err == utils.ErrInvalidType {
					return errors.Errorf(
						errMsgInvalidDependsTypeF,
						depend,
						key,
					)
				}
			case []string:
				// No need to check type here - all deterministic
				if ok, _ := utils.ElemInSlice(depend, p); ok {
					continue
				}
			case string:
				if p == depend.(string) {
					continue
				}
			default:
				return errors.Errorf(
					errMsgInvalidDependsTypeF,
					depend,
					key,
				)
			}
			return errors.Errorf(errMsgDependencyNotSatisfiedF,
				key, depend, provides[key])
		}
		return errors.Errorf(errMsgDependencyNotSatisfiedF,
			key, depend, nil)
	}
	return nil
}

// CheckUpdate Check if new update is available. In case of errors, returns nil
// and error that occurred. If no update is available *UpdateInfo is nil,
// otherwise it contains update information.
func (m *Mender) CheckUpdate() (*datastore.UpdateInfo, menderError) {
	currentArtifactName, err := m.GetCurrentArtifactName()
	if err != nil || currentArtifactName == "" {
		log.Error("could not get the current Artifact name")
		if err == nil {
			err = errors.New("artifact name is empty")
		}
		return nil, NewTransientError(fmt.Errorf("could not read the Artifact name. This is a necessary condition in order for a Mender update to finish safely. Please give the current Artifact a name (This can be done by adding a name to the file /etc/mender/artifact_info) err: %v", err))
	}

	deviceType, err := m.GetDeviceType()
	if err != nil {
		log.Errorf("Unable to verify the existing hardware. Update will continue anyways: %v : %v",
			m.Config.DeviceTypeFile, err)
	}
	provides, err := m.DeviceManager.GetProvides()
	if err != nil {
		log.Errorf("Failed to load the device provides parameters from the datastore. Error: %v. Continuing...",
			err)
	}
	haveUpdate, err := m.updater.GetScheduledUpdate(
		m.api.Request(m.GetAuthToken(),
			nextServerIterator(m.Config),
			reauthorize(m)),
		m.Config.Servers[0].ServerURL,
		&client.CurrentUpdate{
			Artifact:         currentArtifactName,
			DeviceType:       deviceType,
			Provides:         provides,
			UpdateControlMap: true, // Always true for newer clients
		})

	if err != nil {
		// remove authentication token if device is not authorized
		errCause := errors.Cause(err)
		if errCause == client.ErrNotAuthorized {
			if authErr := m.Authorize(); authErr != nil {
				log.Warn("can not perform reauthorization")
			}
		}
		log.Error("Error receiving scheduled update data: ", err)
		return nil, NewTransientError(err)
	}

	if haveUpdate == nil {
		log.Debug("no updates available")
		return nil, nil
	}

	ur, ok := haveUpdate.(client.UpdateResponse)
	if !ok {
		err = fmt.Errorf(
			"The update data received is unexpectedly the wrong type %T. Expected 'client.UpdateResponse'",
			haveUpdate)
		return nil, NewTransientError(err)
	}

	log.Debugf("Received update response: %v", ur)
	if err = m.handleControlMap(&ur); err != nil {
		return nil, NewTransientError(err)
	}

	if ur.UpdateInfo.ArtifactName() == currentArtifactName {
		log.Info("Attempting to upgrade to currently installed artifact name, not performing upgrade.")
		return ur.UpdateInfo, NewTransientError(os.ErrExist)
	}

	return ur.UpdateInfo, nil
}

func (m *Mender) handleControlMap(data *client.UpdateResponse) error {
	if data.UpdateControlMap != nil {
		if data.UpdateControlMap.ID != "" {
			if data.UpdateControlMap.ID != data.UpdateInfo.ID {
				return NewTransientError(
					fmt.Errorf("Mismatched control map ID: %s and deployment ID: %s",
						data.UpdateControlMap.ID, data.UpdateInfo.ID))
			}
		} else {
			data.UpdateControlMap.ID = data.UpdateInfo.ID
		}
		m.controlMapPool.Insert(
			data.UpdateControlMap.Stamp(
				m.DeviceManager.Config.
					MenderConfigFromFile.
					UpdateControlMapExpirationTimeSeconds))
	} else {
		m.controlMapPool.Delete(data.UpdateInfo.ID)
	}
	return nil
}

func (m *Mender) NewStatusReportWrapper(updateId string,
	stateId datastore.MenderState) *client.StatusReportWrapper {

	return &client.StatusReportWrapper{
		API: m.api.Request(m.GetAuthToken(), nextServerIterator(m.Config), reauthorize(m)),
		URL: m.Config.Servers[0].ServerURL,
		Report: client.StatusReport{
			DeploymentID: updateId,
			Status:       StateStatus(stateId),
		},
	}
}

func (m *Mender) ReportUpdateStatus(update *datastore.UpdateInfo, status string) menderError {
	s := client.NewStatus()
	err := s.Report(m.api.Request(m.GetAuthToken(), nextServerIterator(m.Config), reauthorize(m)), m.Config.Servers[0].ServerURL,
		client.StatusReport{
			DeploymentID: update.ID,
			Status:       status,
		})
	if err != nil {
		log.Error("error reporting update status: ", err)
		// remove authentication token if device is not authorized
		errCause := errors.Cause(err)
		if errCause == client.ErrNotAuthorized {
			if authErr := m.Authorize(); authErr != nil {
				log.Warn("can not perform reauthorization")
			}
		} else if errCause == client.ErrDeploymentAborted {
			return NewFatalError(err)
		}
		return NewTransientError(err)
	}
	return nil
}

// see client.go: ApiRequest.Do()

// reauthorize is a closure very similar to mender.Authorize(), but instead of
// walking through all servers in the conf.MenderConfig.Servers list, it only tries
// serverURL.
func reauthorize(m *Mender) func(string) (client.AuthToken, error) {
	// force reauthorization
	return func(serverURL string) (client.AuthToken, error) {
		if err := m.Authorize(); err != nil {
			return noAuthToken, err
		}
		return m.loadAuth()
	}
}

func (m *Mender) UploadLog(update *datastore.UpdateInfo, logs []byte) menderError {
	s := client.NewLog()
	err := s.Upload(m.api.Request(m.GetAuthToken(), nextServerIterator(m.Config), reauthorize(m)), m.Config.Servers[0].ServerURL,
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

func (m *Mender) GetUpdatePollInterval() time.Duration {
	t := time.Duration(m.Config.UpdatePollIntervalSeconds) * time.Second
	if t == 0 {
		log.Warn("UpdatePollIntervalSeconds is not defined")
		t = 30 * time.Minute
	}
	return t
}

func (m *Mender) GetInventoryPollInterval() time.Duration {
	t := time.Duration(m.Config.InventoryPollIntervalSeconds) * time.Second
	if t == 0 {
		log.Warn("InventoryPollIntervalSeconds is not defined")
		t = 30 * time.Minute
	}
	return t
}

func (m *Mender) GetRetryPollInterval() time.Duration {
	t := time.Duration(m.Config.RetryPollIntervalSeconds) * time.Second
	if t == 0 {
		log.Warn("RetryPollIntervalSeconds is not defined")
		t = 5 * time.Minute
	}
	return t
}

func (m *Mender) SetNextState(s State) {
	m.state = s
}

func (m *Mender) GetCurrentState() State {
	return m.state
}

func (m *Mender) GetScriptExecutor() statescript.Executor {
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

func (m *Mender) TransitionState(to State, ctx *StateContext) (State, bool) {
	// In its own function so that we can test it with an alternative
	// Controller.
	return transitionState(to, ctx, m)
}

// spinEventLoop handles the update control map actions returned from the query function.
// This is completely transparent in case no control maps are set, and is in this instance
// a simple identity transformation. If a map is set, this can be used to pause the update process,
// waiting for updates to the map.
func spinEventLoop(c *ControlMapPool, to State, ctx *StateContext, controller Controller, reporter func(string) error) State {
	reported_status := ""
	var mapState string
	switch t := to.Transition(); t {
	case ToArtifactReboot_Enter, ToArtifactCommit_Enter:
		mapState = t.String()
	case ToArtifactInstall:
		mapState = "ArtifactInstall_Enter"
	default:
		log.Debugf("No events for: %s", t.String())
		return to
	}

	for {
		log.Debugf("Spinning the event loop for:  %s", to.Transition())
		action := c.QueryAndUpdate(mapState)
		log.Debugf("Event loop Action: %s", action)
		switch action {
		case "continue":
			return to
		case "pause":
			next, spin := handleClientPause(
				c,
				controller,
				ctx,
				reported_status,
				reporter,
				to,
			)
			log.Debugf("pause event loop result: %v, %t", next, spin)
			if !spin {
				return next
			}
		case "fail":
			log.Infof("Update Control: Failing update at %s state", mapState)
			next, _ := to.HandleError(ctx, controller,
				NewTransientError(errors.New("Forced a failed update")))
			return next
		default:
			log.Warnf("Unknown Action: %s, continuing", action)
			return to
		}
	}
}

func handleClientPause(c *ControlMapPool, controller Controller, ctx *StateContext, reported_status string, reporter func(string) error, to State) (State, bool) {
	status := ""
	switch to.Transition() {
	case ToArtifactReboot_Enter:
		status = "pause_before_rebooting"
	case ToArtifactCommit_Enter:
		status = "pause_before_committing"
	case ToArtifactInstall:
		status = "pause_before_installing"
	}
	if status != reported_status {
		// Upload the status to the deployments/ID/status endpoint
		log.Debugf("Reporting substatus: %s", status)
		err := reporter(status)
		if err == nil {
			reported_status = status
		} else {
			log.Error(err)
		}
	}
	// Schedule a timer for the next update map event to
	// fetch from the server
	updateMapFromServer := time.After(
		ctx.NextUpdateMapTime.Sub(time.Now()))
	log.Debugf("Next update refresh from the server at: %s", ctx.NextUpdateMapTime)

	// wait until further notice
	log.Debug("Pausing the event loop")
	select {
	case <-c.Updates:
		log.Debug("Client control map update")
	case <-updateMapFromServer:
		log.Debug("Refreshing the update control map from the server")
		_, err := controller.CheckUpdate()
		if err == client.ErrNoDeploymentAvailable {
			next, _ := to.HandleError(ctx, controller,
				NewTransientError(errors.New("The deployment was aborted from the server")))
			return next, false
		}
		if err != nil {
			log.Errorf("Failed to update the update control map from the server, %s", err.Error())
			err := refetch(controller)
			if err == client.ErrNoDeploymentAvailable {
				next, _ := to.HandleError(ctx, controller,
					NewTransientError(errors.New("The deployment was aborted from the server")))
				return next, false
			}
		}
	}
	return to, true
}

func refetch(controller Controller) error {
	attempts := 0
	for {
		wait, err := client.GetExponentialBackoffTime(
			attempts,
			controller.GetUpdatePollInterval())
		log.Infof("Next Update Check attempt in %d seconds", wait)
		if err == client.MaxRetriesExceededError {
			log.Errorf("Failed to refetch the update control map in %d attempts, giving up", attempts)
			return err
		} else if err != nil {
			log.Errorf("Unexpected error: %s", err.Error())
			return err
		}
		time.Sleep(wait)
		_, err = controller.CheckUpdate()
		attempts += 1
		if err == client.ErrNoDeploymentAvailable {
			return err
		}
		if err == nil {
			return nil
		}
		log.Errorf("Attempt %d: Failed to update the control map from the server: %s", attempts, err.Error())
	}
}

func transitionState(to State, ctx *StateContext, c Controller) (State, bool) {
	from := c.GetCurrentState()

	log.Infof("State transition: %s [%s] -> %s [%s]",
		from.Id(), from.Transition().String(),
		to.Id(), to.Transition().String())

	// Check if we need to update the control map from the server
	updateState, ok := to.(Updater)
	if ok {
		ctx.NextUpdateMapTime = c.GetControlMapPool().
			NextControlMapHalfTime(updateState.Update().ID)
		if time.Now().Sub(ctx.NextUpdateMapTime) < 0 {
			// Maps are magically updated in CheckUpdate
			_, err := c.CheckUpdate()
			if err != nil {
				log.Errorf("Failed to update the control map")
				refetch(c)
			}
		}
	}

	var report *client.StatusReportWrapper
	if shouldReportUpdateStatus(to.Id()) {
		upd, err := getUpdateFromState(to)
		if err != nil {
			log.Error(err)
		} else {
			report = c.NewStatusReportWrapper(upd.ID, to.Id())
		}
	}

	updateStatusClient := client.NewStatus()
	reporter := func(status string) error {
		return updateStatusClient.Report(report.API, report.URL, client.StatusReport{
			DeploymentID: report.Report.DeploymentID,
			Status:       status,
		})
	}
	to = spinEventLoop(c.GetControlMapPool(), to, ctx, c, reporter)

	if shouldTransit(from, to) {
		if to.Transition().IsToError() && !from.Transition().IsToError() {
			log.Debug("Transitioning to error state")

			// Set the reported status to be the same as the state where the
			// error happened. THIS IS IMPORTANT AS WE CAN SEND THE client.StatusFailure
			// ONLY ONCE.
			if report != nil {
				report.Report.Status = StateStatus(from.Id())
			}
			// call error scripts
			_ = from.Transition().Error(c.GetScriptExecutor(), report)
		} else {
			// do transition to ordinary state
			if err := from.Transition().Leave(c.GetScriptExecutor(), report, ctx.Store); err != nil {
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
		err := datastore.StoreStateData(ctx.Store, datastore.StateData{
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
		if err := to.Transition().Enter(c.GetScriptExecutor(), report, ctx.Store); err != nil {
			merr := NewTransientError(fmt.Errorf(
				"error calling enter script for (error) %s state: %s",
				to.Id(), err.Error()))
			return to.HandleError(ctx, c, merr)
		}
	}

	// execute current state action
	return to.Handle(ctx, c)
}

func (m *Mender) InventoryRefresh() error {
	ic := client.NewInventory()
	idg := inv.NewInventoryDataRunner(path.Join(conf.GetDataDirPath(), "inventory"))

	artifactName, err := m.GetCurrentArtifactName()
	if err != nil || artifactName == "" {
		if err == nil {
			err = errors.New("Artifact name is empty")
		}
		errstr := fmt.Sprintf("could not read the artifact name. This is a necessary condition in order for a Mender update to finish safely. Please give the current Artifact a name (This can be done by adding a name to the file /etc/mender/artifact_info) err: %v", err)
		return errors.Wrap(errNoArtifactName, errstr)
	}

	idata, err := idg.Get()
	if err != nil {
		// at least report device type
		log.Errorf("Failed to obtain inventory data: %s", err.Error())
	}

	deviceType, err := m.GetDeviceType()
	if err != nil {
		log.Errorf("Unable to verify the existing hardware. Update will continue anyways: %v : %v", m.Config.DeviceTypeFile, err)
	}
	reqAttr := []client.InventoryAttribute{
		{Name: "device_type", Value: deviceType},
		{Name: "artifact_name", Value: artifactName},
		{Name: "mender_client_version", Value: conf.VersionString()},
	}

	if idata == nil {
		idata = make(client.InventoryData, 0, len(reqAttr))
	}
	_ = idata.ReplaceAttributes(reqAttr)

	if idata == nil {
		log.Infof("No inventory data to submit")
		return nil
	}

	err = ic.Submit(m.api.Request(m.GetAuthToken(), nextServerIterator(m.Config), reauthorize(m)), m.Config.Servers[0].ServerURL, idata)
	if err != nil {
		return errors.Wrapf(err, "failed to submit inventory data")
	}

	return nil
}

func (m *Mender) CheckScriptsCompatibility() error {
	return m.stateScriptExecutor.CheckRootfsScriptsVersion()
}
