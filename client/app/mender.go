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

	"github.com/mendersoftware/mender/client/api"
	"github.com/mendersoftware/mender/client/conf"
	"github.com/mendersoftware/mender/client/datastore"
	"github.com/mendersoftware/mender/client/installer"
	inv "github.com/mendersoftware/mender/client/inventory"
	"github.com/mendersoftware/mender/client/statescript"
	commonconf "github.com/mendersoftware/mender/common/conf"
	"github.com/mendersoftware/mender/common/store"
	"github.com/mendersoftware/mender/common/utils"
)

type Controller interface {
	GetControlMapPool() *ControlMapPool

	GetCurrentArtifactName() (string, error)
	GetUpdatePollInterval() time.Duration
	GetInventoryPollInterval() time.Duration
	GetRetryPollInterval() time.Duration

	CheckUpdate() (*datastore.UpdateInfo, menderError)
	FetchUpdate(url string) (io.ReadCloser, int64, error)

	NewStatusReportWrapper(updateId string,
		stateId datastore.MenderState) *api.StatusReportWrapper
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
		datastore.MenderStateUpdateFetch:         api.StatusDownloading,
		datastore.MenderStateUpdateStore:         api.StatusDownloading,
		datastore.MenderStateUpdateInstall:       api.StatusInstalling,
		datastore.MenderStateUpdateVerify:        api.StatusRebooting,
		datastore.MenderStateUpdateCommit:        api.StatusRebooting,
		datastore.MenderStateReboot:              api.StatusRebooting,
		datastore.MenderStateAfterReboot:         api.StatusRebooting,
		datastore.MenderStateRollback:            api.StatusRebooting,
		datastore.MenderStateRollbackReboot:      api.StatusRebooting,
		datastore.MenderStateAfterRollbackReboot: api.StatusRebooting,
		datastore.MenderStateUpdateError:         api.StatusFailure,
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
	*installer.DeviceManager

	// This state should be maintained private to the app package.
	updater             api.Updater
	state               State
	stateScriptExecutor statescript.Executor
	api                 *api.ApiClient
	download            *api.DownloadApiClient
	controlMapPool      *ControlMapPool
}

type MenderPieces struct {
	DualRootfsDevice installer.DualRootfsDevice
	Store            store.Store
}

func NewMender(config *conf.MenderConfig, pieces MenderPieces) (*Mender, error) {
	client, err := api.NewApiClient(time.Duration(config.AuthTimeoutSeconds) * time.Second,
		config.GetHttpConfig())
	if err != nil {
		return nil, errors.Wrap(err, "error creating HTTP client")
	}

	download, err := api.NewDownloadApiClient(config.GetHttpConfig())
	if err != nil {
		return nil, errors.Wrap(err, "error creating HTTP download client")
	}

	stateScrExec := installer.NewStateScriptExecutor(config)

	controlMapPool := NewControlMap(
		pieces.Store,
		config.GetUpdateControlMapBootExpirationTimeSeconds(),
		config.GetUpdateControlMapExpirationTimeSeconds(),
	)

	m := &Mender{
		DeviceManager:       installer.NewDeviceManager(pieces.DualRootfsDevice, config, pieces.Store),
		updater:             api.NewUpdate(),
		state:               States.Init,
		stateScriptExecutor: stateScrExec,
		api:                 client,
		download:            download,
		controlMapPool:      controlMapPool,
	}

	return m, nil
}

func (m *Mender) GetControlMapPool() *ControlMapPool {
	return m.controlMapPool
}

func (m *Mender) FetchUpdate(url string) (io.ReadCloser, int64, error) {
	return m.updater.FetchUpdate(m.download, url, m.GetRetryPollInterval())
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
		m.api,
		&api.CurrentUpdate{
			Artifact:   currentArtifactName,
			DeviceType: deviceType,
			Provides:   provides,
		})

	ur, urOk := haveUpdate.(api.UpdateResponse)

	if err != nil {
		if !errors.Is(err, api.ErrNoDeploymentAvailable) {
			log.Error("Error receiving scheduled update data: ", err)
		}
		return ur.UpdateInfo, NewTransientError(err)
	}

	if haveUpdate == nil {
		log.Debug("no updates available")
		return nil, nil
	}

	if !urOk {
		err = fmt.Errorf(
			"The update data received is unexpectedly the wrong type %T. Expected 'api.UpdateResponse'",
			haveUpdate)
		return nil, NewTransientError(err)
	}

	log.Debugf("Received update response: %v", ur)
	if err = m.handleControlMap(&ur); err != nil {
		return ur.UpdateInfo, NewTransientError(err)
	}

	if ur.UpdateInfo.ArtifactName() == currentArtifactName {
		log.Info("Attempting to upgrade to currently installed artifact name, not performing upgrade.")
		return ur.UpdateInfo, NewTransientError(os.ErrExist)
	}

	return ur.UpdateInfo, nil
}

func (m *Mender) handleControlMap(data *api.UpdateResponse) error {
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
		m.controlMapPool.InsertReplaceAllPriorities(
			data.UpdateControlMap.Stamp(
				m.DeviceManager.Config.
					MenderConfigFromFile.
					GetUpdateControlMapExpirationTimeSeconds()))
	} else {
		m.controlMapPool.DeleteAllPriorities(data.UpdateInfo.ID)
	}
	return nil
}

func (m *Mender) NewStatusReportWrapper(updateId string,
	stateId datastore.MenderState) *api.StatusReportWrapper {

	return &api.StatusReportWrapper{
		API: m.api,
		Report: api.StatusReport{
			DeploymentID: updateId,
			Status:       StateStatus(stateId),
		},
	}
}

func (m *Mender) ReportUpdateStatus(update *datastore.UpdateInfo, status string) menderError {
	s := api.NewStatus()
	err := s.Report(m.api,
		api.StatusReport{
			DeploymentID: update.ID,
			Status:       status,
		})
	if err != nil {
		log.Error("error reporting update status: ", err)
		if errors.Is(err, api.ErrDeploymentAborted) {
			return NewFatalError(err)
		}
		return NewTransientError(err)
	}
	return nil
}

func (m *Mender) UploadLog(update *datastore.UpdateInfo, logs []byte) menderError {
	s := api.NewLog()
	err := s.Upload(m.api,
		api.LogData{
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

func transitionState(to State, ctx *StateContext, c Controller) (State, bool) {
	from := c.GetCurrentState()

	log.Infof("State transition: %s [%s] -> %s [%s]",
		from.Id(), from.Transition().String(),
		to.Id(), to.Transition().String())

	var report *api.StatusReportWrapper
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
			log.Debug("Transitioning to error state")

			// Set the reported status to be the same as the state where the
			// error happened. THIS IS IMPORTANT AS WE CAN SEND THE api.StatusFailure
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
	if toUs, ok := to.(UpdateState); ok {
		// If either the state we come from, or are going to, permits
		// looping, then we don't increase the state counter which
		// detects state loops.
		permitLooping := toUs.PermitLooping()
		fromUs, ok := from.(UpdateState)
		if ok {
			permitLooping = permitLooping || fromUs.PermitLooping()
		} else {
			// States that are not UpdateStates are assumed to allow
			// looping, since they are outside the main update
			// flow. Usually these relate to retry mechanisms, that
			// have their own means of expiring.
			permitLooping = true
		}

		err := datastore.StoreStateData(ctx.Store, datastore.StateData{
			Name:       toUs.Id(),
			UpdateInfo: *toUs.Update(),
		}, !permitLooping)
		if err != nil {
			log.Error("Could not write state data to persistent storage: ", err.Error())
			state, cancelled := toUs.HandleError(ctx, c, NewFatalError(err))
			return handleStateDataError(ctx, state, cancelled, toUs.Id(), toUs.Update(), err)
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
	ic := api.NewInventory()
	idg := inv.NewInventoryDataRunner(path.Join(commonconf.GetDataDirPath(), "inventory"))

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
	reqAttr := []api.InventoryAttribute{
		{Name: "device_type", Value: deviceType},
		{Name: "artifact_name", Value: artifactName},
		{Name: "mender_client_version", Value: conf.VersionString()},
	}

	if idata == nil {
		idata = make(api.InventoryData, 0, len(reqAttr))
	}
	_ = idata.ReplaceAttributes(reqAttr)

	if idata == nil {
		log.Infof("No inventory data to submit")
		return nil
	}

	err = ic.Submit(m.api, idata)
	if err != nil {
		return errors.Wrapf(err, "failed to submit inventory data")
	}

	return nil
}

func (m *Mender) CheckScriptsCompatibility() error {
	return m.stateScriptExecutor.CheckRootfsScriptsVersion()
}

func verifyAndSetArtifactNameInProvides(provides map[string]string, getArtifactName func() (string, error)) (map[string]string, error) {
	if _, ok := provides["artifact_name"]; !ok {
		artifactName, err := getArtifactName()
		if err != nil || artifactName == "" {
			log.Error("could not get the current Artifact name")
			if err == nil {
				err = errors.New("artifact name is empty")
			}
			return provides, err
		}
		if provides == nil {
			provides = make(map[string]string)
		}
		provides["artifact_name"] = artifactName
	}
	return provides, nil
}
