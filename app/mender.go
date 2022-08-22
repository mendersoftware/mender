// Copyright 2022 Northern.tech AS
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

	"github.com/mendersoftware/mender-artifact/areader"
	"github.com/mendersoftware/mender-artifact/artifact"
	"github.com/mendersoftware/mender/app/updatecontrolmap"
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
	Authorize() (client.AuthToken, client.ServerURL, error)
	ClearAuthorization()

	GetControlMapPool() *ControlMapPool

	GetCurrentArtifactName() (string, error)
	GetUpdatePollInterval() time.Duration
	GetInventoryPollInterval() time.Duration
	GetRetryPollInterval() time.Duration
	GetRetryPollCount() int

	HandleBootstrapArtifact(s store.Store) error

	CheckUpdate() (*datastore.UpdateInfo, menderError)
	FetchUpdate(url string) (io.ReadCloser, int64, error)
	RefreshServerUpdateControlMap(deploymentID string) error

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
	errNoArtifactName        = errors.New("cannot determine current artifact name")
	errControlMapIDMismatchF = "Mismatched control map ID: %s and deployment ID: %s"
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
	// Used for requesting API URLs.
	api client.AuthorizedApiRequester
	// Used for downloading artifacts.
	download client.ApiRequester

	controlMapPool *ControlMapPool
}

type MenderPieces struct {
	DualRootfsDevice installer.DualRootfsDevice
	Store            store.Store
	AuthManager      AuthManager
}

func NewMender(config *conf.MenderConfig, pieces MenderPieces) (*Mender, error) {
	stateScrExec := dev.NewStateScriptExecutor(config)

	controlMapPool := NewControlMap(
		pieces.Store,
		config.GetUpdateControlMapBootExpirationTimeSeconds(),
		config.GetUpdateControlMapExpirationTimeSeconds(),
	)

	m := &Mender{
		DeviceManager:       dev.NewDeviceManager(pieces.DualRootfsDevice, config, pieces.Store),
		updater:             client.NewUpdate(),
		state:               States.Init,
		stateScriptExecutor: stateScrExec,
		authManager:         pieces.AuthManager,
		controlMapPool:      controlMapPool,
	}

	api, err := client.NewReauthorizingClient(config.GetHttpConfig(), m.Authorize)
	if err != nil {
		return nil, errors.Wrap(err, "error creating HTTP API client")
	}
	m.api = api

	m.download, err = client.NewApiClient(config.GetHttpConfig())
	if err != nil {
		return nil, errors.Wrap(err, "error creating HTTP download client")
	}

	return m, nil
}

func (m *Mender) Authorize() (client.AuthToken, client.ServerURL, error) {
	inChan := m.authManager.GetInMessageChan()
	broadcastChan := m.authManager.GetBroadcastMessageChan(authManagerChannelName)
	respChan := make(chan AuthManagerResponse)

	// drain the broadcast channel
	select {
	case <-broadcastChan:
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
		return "", "", NewTransientError(resp.Error)
	}

	// wait for the broadcast notification
	resp, ok := <-broadcastChan
	if !ok {
		return "", "", NewTransientError(
			errors.New("authorization request failed: channel read failed"),
		)
	} else if resp.Error != nil {
		return "", "", NewTransientError(
			errors.Wrap(resp.Error, "authorization request failed"),
		)
	} else if len(resp.AuthToken) == 0 || len(resp.ServerURL) == 0 {
		return "", "", NewTransientError(
			errors.New("authorization request failed"),
		)
	}

	return resp.AuthToken, resp.ServerURL, nil
}

func (m *Mender) ClearAuthorization() {
	m.api.ClearAuthorization()
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
			switch pVal := depend.(type) {
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
				if p == pVal {
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
		return nil, NewTransientError(
			fmt.Errorf(
				"could not read the Artifact name. This is a programming error."+
					" err: %v",
				err,
			),
		)
	}

	deviceType, err := m.GetDeviceType()
	if err != nil {
		log.Errorf("Unable to verify the existing hardware. Update will continue anyways: %v : %v",
			m.Config.DeviceTypeFile, err)
	}
	provides, err := m.DeviceManager.GetProvides()
	if err != nil {
		log.Errorf(
			"Failed to load the device provides parameters from the datastore. Error: %v."+
				" Continuing...",
			err,
		)
	}
	haveUpdate, err := m.updater.GetScheduledUpdate(
		m.api,
		m.Config.Servers[0].ServerURL,
		&client.CurrentUpdate{
			Artifact:   currentArtifactName,
			DeviceType: deviceType,
			Provides:   provides,
		})

	ur, urOk := haveUpdate.(client.UpdateResponse)

	if err != nil {
		if errors.Is(err, client.ErrNoDeploymentAvailable) {
			return ur.UpdateInfo, NewTransientError(err)
		}
		log.Error("Error receiving scheduled update data: ", err)
		return ur.UpdateInfo, NewTransientError(err)
	}

	if haveUpdate == nil {
		log.Debug("no updates available")
		return nil, nil
	}

	if !urOk {
		err = fmt.Errorf(
			"The update data received is unexpectedly the wrong type %T. Expected"+
				" 'client.UpdateResponse'",
			haveUpdate,
		)
		return nil, NewTransientError(err)
	}

	log.Debugf("Received update response: %v", ur)
	if err = m.HandleControlMap(ur.ID, ur.UpdateControlMap); err != nil {
		return ur.UpdateInfo, NewTransientError(err)
	}

	if ur.UpdateInfo.ArtifactName() == currentArtifactName {
		log.Info(
			"Attempting to upgrade to currently installed artifact name, not performing upgrade.",
		)
		return ur.UpdateInfo, NewTransientError(os.ErrExist)
	}

	return ur.UpdateInfo, nil
}

// RefreshServerUpdateControlMap updates the control maps from the server during
// a deployment.
func (m *Mender) RefreshServerUpdateControlMap(deploymentID string) error {
	cm, err := client.GetUpdateControlMap(
		m.api,
		m.Config.Servers[0].ServerURL,
		deploymentID,
	)
	if err != nil {
		return err
	}

	return m.HandleControlMap(deploymentID, cm)
}

func (m *Mender) HandleControlMap(
	deploymentID string,
	updateControlMap *updatecontrolmap.UpdateControlMap,
) error {
	if updateControlMap != nil {
		if updateControlMap.ID != "" {
			if updateControlMap.ID != deploymentID {
				return NewTransientError(
					fmt.Errorf(errControlMapIDMismatchF,
						updateControlMap.ID, deploymentID))
			}
		} else {
			updateControlMap.ID = deploymentID
		}
		m.controlMapPool.InsertReplaceAllPriorities(
			updateControlMap.Stamp(
				m.DeviceManager.Config.
					MenderConfigFromFile.
					GetUpdateControlMapExpirationTimeSeconds()))
	} else {
		m.controlMapPool.DeleteAllPriorities(deploymentID)
	}
	return nil
}

func (m *Mender) NewStatusReportWrapper(updateId string,
	stateId datastore.MenderState) *client.StatusReportWrapper {

	return &client.StatusReportWrapper{
		API: m.api,
		URL: m.Config.Servers[0].ServerURL,
		Report: client.StatusReport{
			DeploymentID: updateId,
			Status:       StateStatus(stateId),
		},
	}
}

func (m *Mender) ReportUpdateStatus(update *datastore.UpdateInfo, status string) menderError {
	s := client.NewStatus()
	err := s.Report(
		m.api,
		m.Config.Servers[0].ServerURL,
		client.StatusReport{
			DeploymentID: update.ID,
			Status:       status,
		},
	)
	if err != nil {
		log.Error("error reporting update status: ", err)
		errCause := errors.Cause(err)
		if errCause == client.ErrDeploymentAborted {
			return NewFatalError(err)
		}
		return NewTransientError(err)
	}
	return nil
}

func (m *Mender) UploadLog(update *datastore.UpdateInfo, logs []byte) menderError {
	s := client.NewLog()
	err := s.Upload(
		m.api,
		m.Config.Servers[0].ServerURL,
		client.LogData{
			DeploymentID: update.ID,
			Messages:     logs,
		},
	)
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

func (m *Mender) GetRetryPollCount() int {
	return m.Config.RetryPollCount
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
			if err := from.Transition().
				Leave(c.GetScriptExecutor(), report, ctx.Store); err != nil {
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
	ic := client.NewInventory()
	idg := inv.NewInventoryDataRunner(path.Join(conf.GetDataDirPath(), "inventory"))

	artifactName, err := m.GetCurrentArtifactName()
	if err != nil || artifactName == "" {
		if err == nil {
			err = errors.New("Artifact name is empty")
		}
		errstr := fmt.Sprintf(
			"could not read the artifact name. . This is a programming error."+
				" err: %v",
			err,
		)
		return errors.Wrap(errNoArtifactName, errstr)
	}

	idata, err := idg.Get()
	if err != nil {
		// at least report device type
		log.Errorf("Failed to obtain inventory data: %s", err.Error())
	}

	deviceType, err := m.GetDeviceType()
	if err != nil {
		log.Errorf(
			"Unable to verify the existing hardware. Update will continue anyways: %v : %v",
			m.Config.DeviceTypeFile,
			err,
		)
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

	err = ic.Submit(m.api, m.Config.Servers[0].ServerURL, idata)
	if err != nil {
		return errors.Wrapf(err, "failed to submit inventory data")
	}

	return nil
}

func (m *Mender) CheckScriptsCompatibility() error {
	return m.stateScriptExecutor.CheckRootfsScriptsVersion()
}

func verifyAndSetArtifactNameInProvides(
	provides map[string]string,
	getArtifactName func() (string, error),
) (map[string]string, error) {
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

func (m *Mender) HandleBootstrapArtifact(s store.Store) error {
	// Warn if deprecated file exists
	_, err := os.Stat(conf.DeprecatedArtifactInfoFile)
	if err == nil {
		log.Warnf(
			"Deprecated %s file found in the system, it will be ignored",
			conf.DeprecatedArtifactInfoFile,
		)
	}

	updatePath := m.BootstrapArtifactFile

	databaseInitialized := false
	bootstrapArtifactFileFound := false

	defer func() {
		// Remove file even on failures. Otherwise we will try to install it on every boot
		if bootstrapArtifactFileFound {
			log.Debugf("Removing bootstrap Artifact %s", updatePath)
			removeErr := os.Remove(updatePath)
			if removeErr != nil {
				log.Warnf("Removing bootstarp Artifact errored: %s", removeErr.Error())
			}
		}

		// Initialize database on errors or file not found
		if !databaseInitialized {
			log.Debug("Bootstrap Artifact not installed, committing artifact_name 'unknown'")
			if dbErr := m.Store.WriteTransaction(func(txn store.Transaction) error {
				return datastore.CommitArtifactData(
					txn,
					"unknown",
					"",
					map[string]string{"artifact_name": "unknown"},
					nil,
				)
			}); dbErr != nil {
				if err != nil {
					err = errors.Wrapf(err, dbErr.Error())
				} else {
					err = dbErr
				}
			}
		}
	}()

	// Check if the database is already initialized. Or, in other words, if the database has
	// either state data (for an in-progress update) or provides data. Only when none of these
	// is true we consider the database not initialized and proceed with either installing the
	// bootstrap Artifact or falling back to initializing artifact-name to "unknown".
	// Any earlier version of the client not supporting bootstrap Artifact will have empty
	// provides until the first update is _committed_, so we must check state data in addition.
	_, sdErr := datastore.LoadStateData(s)
	if sdErr == nil {
		databaseInitialized = true
	} else {
		log.Debug("No state data stored")
		p, pErr := datastore.LoadProvides(s)
		if pErr == nil && len(p) > 0 {
			databaseInitialized = true
		} else {
			log.Debug("No provides stored, database not initialized")
		}
	}

	// Check if file exists
	_, err = os.Stat(updatePath)
	if err == nil {
		log.Debugf("Bootstrap Artifact %s found", updatePath)
		bootstrapArtifactFileFound = true
	} else {
		log.Debugf("Bootstrap Artifact %s not found", updatePath)
	}

	// Early exit, let the deferred function do the clean-ups
	if databaseInitialized || !bootstrapArtifactFileFound {
		if databaseInitialized && bootstrapArtifactFileFound {
			log.Info(
				"Bootstrap Artifact found but database already initialized, " +
					"the Artifact will be ignored",
			)
		}
		return nil
	}

	// Validate bootstrap Artifact
	log.Debugf("Validating bootstrap Artifact from %s", updatePath)
	dt, err := m.GetDeviceType()
	if err != nil {
		return errors.Wrap(err, "could not get device type")
	}
	artifactName, artifactGroup, provides, err := validateAndParseBootstrapArtifact(dt, updatePath)
	if err != nil {
		return errors.Wrap(err, "invalid bootstrap Artifact")
	}

	// Initialize database
	log.Debugf("Initializing database from bootstrap Artifact")
	err = m.Store.WriteTransaction(func(txn store.Transaction) error {
		return datastore.CommitArtifactData(txn, artifactName, artifactGroup, provides, nil)
	})
	if err != nil {
		return errors.Wrap(err, "Could not update database")
	}

	log.Info("Bootstrap Artifact installed successfully")
	databaseInitialized = true

	return nil

}

func validateAndParseBootstrapArtifact(
	dt, path string,
) (string, string, artifact.TypeInfoProvides, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", nil, err
	}
	defer f.Close()

	ar := areader.NewReader(f)

	ar.CompatibleDevicesCallback = func(devices []string) error {
		for _, dev := range devices {
			if dev == dt {
				return nil
			}
		}
		return fmt.Errorf(
			"image (device types %s) not compatible with device %s",
			devices,
			dt,
		)
	}

	var scripts []string
	ar.ScriptsReadCallback = func(r io.Reader, info os.FileInfo) error {
		scripts = append(scripts, info.Name())
		return nil
	}

	err = ar.ReadArtifact()
	if err != nil {
		return "", "", nil, err
	}

	if ar.GetArtifactName() == "" {
		return "", "", nil, errors.New("artifact_name cannot be empty")
	}

	if len(scripts) > 0 {
		return "", "", nil, errors.New("bootstrap Artifact cannot contain state scripts")
	}

	info := ar.GetInfo()
	if info.Format != "mender" {
		return "", "", nil, errors.New("wrong Artifact format")
	}
	if info.Version < 3 {
		return "", "", nil, errors.New("wrong Artifact version")
	}

	groupDepends := ar.GetArtifactDepends()
	if groupDepends != nil && groupDepends.ArtifactGroup != nil {
		return "", "", nil, errors.New("artifact_depends must be empty")
	}

	updates := ar.GetHandlers()
	if len(updates) != 1 {
		return "", "", nil, errors.New("bootstrap Artifact must contain exactly 1 update")
	}

	update := updates[0]
	provides, err := update.GetUpdateProvides()
	if err != nil {
		return "", "", nil, errors.Wrap(err, "error reading provides")
	}
	if provides == nil {
		return "", "", nil, errors.New("update must include artifact_provides")
	}

	depends, err := update.GetUpdateDepends()
	if err != nil {
		return "", "", nil, errors.Wrap(err, "error reading depends")
	}
	if depends != nil {
		return "", "", nil, errors.New("update artifact_depends must be empty")
	}

	if len(update.GetUpdateAllFiles()) != 0 {
		return "", "", nil, errors.New("update must not contain files")
	}

	return ar.GetArtifactName(), ar.GetArtifactProvides().ArtifactGroup, provides, nil
}
