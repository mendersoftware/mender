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
	"encoding/json"
	"io"
	"os"
	"time"

	"github.com/mendersoftware/log"
	"github.com/mendersoftware/mender/client"
	"github.com/mendersoftware/mender/store"
	"github.com/pkg/errors"
)

// Each state implements Handle() - a state handler method that performs actions
// on the Controller. The handler returns a new state, thus performing a state
// transition. Each state can transition to an instance of ErrorState (or
// UpdateErrorState for update related states). The handling of error states is
// described further down.
//
// Regular state transitions:
//
//                               init
//
//                                 |        (wait timeout expired)
//                                 |   +---------------------------------+
//                                 |   |                                 |
//                                 v   v                                 |
//                                           (auth req. failed)
//                            bootstrapped ----------------------> authorize wait
//
//                                  |
//                                  |
//                                  |  (auth data avail.)
//                                  |
//                                  v
//
//                             authorized
//
//            (update needs     |   |
//             verify)          |   |
//           +------------------+   |
//           |                      |
//           v                      |
//                                  |
//     update verify                |
//                                  |
//      |        |                  |
// (ok) |        | (update error)   |
//      |        |                  |
//      v        v                  |
//                                  |
//   update    update               |           (wait timeout expired)
//   commit    report state         |    +-----------------------------+
//                                  |    |                             |
//      |         |                 |    |                             |
//      +----+----+                 v    v                             |
//           |                                (no update)
//           +---------------> update check ---------------->  update check wait
//
//                                  |
//                                  | (update ready)
//                                  |
//                                  |   +-----------------------------+
//                                  |   |                             |
//                                  v   v                             |
//
//                             update fetch ------------------> retry update
//
//                                  |                                 ^
//                                  | (update fetched)                |
//                                  v                                 |
//                                                                    |
//                            update install -------------------------+
//
//                                  |
//                                  | (update installed,
//                                  |  enabled)
//                                  |
//                                  v
//
//                                reboot
//
//                                  |
//                                  v
//
//                                final (daemon exit)
//
// Errors and their context are captured in Error states. Non-update states
// transition to an ErrorState, while update related states (fetch, install,
// commit) transition to UpdateErrorState that captures additional update
// context information. Error states implement IsFatal() method to check whether
// the cause is fatal or not.
//
//        +------------------> init <-----------------------+
//        |                                                 |
//        |                      |                          |
//        |                      |                          |
//        |                      |                          |
//        |                      v                          |
//                                             (bootstrap)  |
//   error state <--------- non-update states  (authorized) |
//                                             (* wait)     |
//        |                       ^            (check)      |
//        |                       |                         |
//        |                       |                         |
//        |                       |                         |
//        |      (fetch  )        v                         |
//        |      (install)
//        |      (enable )  update states ---------> update error state
//        |      (verify )
//        |      (commit )        |                         |
//        |      (report )        |                         |
//        |      (reboot )        |                         |
//        |                       |                         |
//        |                       v                         |
//        |                                                 |
//        +-------------------> final <---------------------+
//                           (daemon exit)
//

// state context carrying over data that may be used by all state handlers
type StateContext struct {
	// data store access
	store                store.Store
	lastUpdateCheck      time.Time
	lastInventoryUpdate  time.Time
	fetchInstallAttempts int
}

type StateRunner interface {
	// Set runner's state to 's'
	SetNextState(s State)
	// Obtain runner's state
	GetCurrentState() State
	// Run the currently set state with this context
	TransitionState(next State, ctx *StateContext) (State, bool)
}

// StateData is state information that can be used for restoring state from storage
type StateData struct {
	// version is providing information about the format of the data
	Version int
	// number representing the id of the last state to execute
	Name MenderState
	// update reponse data for the update that was in progress
	UpdateInfo client.UpdateResponse
	// update status
	UpdateStatus string
}

const (
	// name of key that state data is stored under across reboots
	stateDataKey = "state"
)

var (
	initState = &InitState{
		baseState{
			id: MenderStateInit,
		},
	}

	idleState = &IdleState{
		baseState{
			id: MenderStateIdle,
		},
	}

	authorizeWaitState = NewAuthorizeWaitState()

	authorizeState = &AuthorizeState{
		baseState{
			id: MenderStateAuthorize,
		},
	}

	inventoryUpdateState = &InventoryUpdateState{
		baseState{
			id: MenderStateInventoryUpdate,
		},
	}

	checkWaitState = NewCheckWaitState()

	updateCheckState = &UpdateCheckState{
		baseState{
			id: MenderStateUpdateCheck,
		},
	}

	doneState = &FinalState{
		baseState{
			id: MenderStateDone,
		},
	}
)

type State interface {
	// Perform state action, returns next state and boolean flag indicating if
	// execution was cancelled or not
	Handle(ctx *StateContext, c Controller) (State, bool)
	// Cancel state action, returns true if action was cancelled
	Cancel() bool
	// Return numeric state ID
	Id() MenderState
	// Return transition
	Transition() Transition
	SetTransition(t Transition)
}

type WaitState interface {
	Id() MenderState
	Cancel() bool
	Wait(next, same State, wait time.Duration) (State, bool)
	Transition() Transition
	SetTransition(t Transition)
}

// baseState is a helper state with some convenience methods
type baseState struct {
	id MenderState
	t  Transition
}

func (b *baseState) Id() MenderState {
	return b.id
}

func (b *baseState) Cancel() bool {
	return false
}

func (b *baseState) Transition() Transition {
	return b.t
}

func (b *baseState) SetTransition(tran Transition) {
	b.t = tran
}

type waitState struct {
	baseState
	cancel chan bool
}

func NewWaitState(id MenderState) WaitState {
	return &waitState{
		baseState: baseState{id: id},
		cancel:    make(chan bool),
	}
}

// Wait performs wait for time `wait` and return state (`next`, false) after the wait
// has completed. If wait was interrupted returns (`same`, true)
func (ws *waitState) Wait(next, same State,
	wait time.Duration) (State, bool) {
	ticker := time.NewTicker(wait)

	defer ticker.Stop()
	select {
	case <-ticker.C:
		log.Debugf("wait complete")
		return next, false
	case <-ws.cancel:
		log.Infof("wait canceled")
	}
	return same, true
}

func (ws *waitState) Cancel() bool {
	ws.cancel <- true
	return true
}

type IdleState struct {
	baseState
}

func (i *IdleState) Handle(ctx *StateContext, c Controller) (State, bool) {
	// check if client is authorized
	if c.IsAuthorized() {
		return checkWaitState, false
	}
	return authorizeState, false
}

type InitState struct {
	baseState
}

func (i *InitState) Handle(ctx *StateContext, c Controller) (State, bool) {
	// restore previous state information
	sd, err := LoadStateData(ctx.store)

	// handle easy case first: no previous state stored,
	// means no update was in progress; we should continue from idle
	if err != nil && os.IsNotExist(err) {
		log.Debug("no state data stored")
		return idleState, false
	}

	if err != nil {
		log.Errorf("failed to restore state data: %v", err)
		me := NewFatalError(errors.Wrapf(err, "failed to restore state data"))
		return NewUpdateErrorState(me, client.UpdateResponse{
			ID: "unknown",
		}), false
	}

	log.Infof("handling loaded state: %s", sd.Name)

	// chack last known state
	switch sd.Name {
	// update process was finished; check what is the status of update
	case MenderStateReboot:
		return NewUpdateVerifyState(sd.UpdateInfo), false

	case MenderStateRollbackReboot:
		return NewAfterRollbackRebootState(sd.UpdateInfo), false

	// this should not happen
	default:
		log.Errorf("got invalid state: %v", sd.Name)
		me := NewFatalError(errors.Errorf("got invalid state stored: %v", sd.Name))

		return NewUpdateErrorState(me, sd.UpdateInfo), false
	}
}

type AuthorizeWaitState struct {
	WaitState
}

func NewAuthorizeWaitState() State {
	return &AuthorizeWaitState{
		WaitState: NewWaitState(MenderStateAuthorizeWait),
	}
}

func (a *AuthorizeWaitState) Handle(ctx *StateContext, c Controller) (State, bool) {
	log.Debugf("handle authorize wait state")
	intvl := c.GetRetryPollInterval()

	log.Debugf("wait %v before next authorization attempt", intvl)
	return a.Wait(authorizeState, a, intvl)
}

type AuthorizeState struct {
	baseState
}

func (a *AuthorizeState) Handle(ctx *StateContext, c Controller) (State, bool) {
	log.Debugf("handle authorize state")
	if err := c.Authorize(); err != nil {
		log.Errorf("authorize failed: %v", err)
		if !err.IsFatal() {
			return authorizeWaitState, false
		}
		return NewErrorState(err), false
	}
	// if everything is OK we should let Mender figure out what to do
	// in MenderStateCheckWait state
	return checkWaitState, false
}

type UpdateVerifyState struct {
	baseState
	update client.UpdateResponse
}

func NewUpdateVerifyState(update client.UpdateResponse) State {
	return &UpdateVerifyState{
		baseState{
			id: MenderStateUpdateVerify,
		},
		update,
	}
}

func (uv *UpdateVerifyState) Handle(ctx *StateContext, c Controller) (State, bool) {

	// start deployment logging
	if err := DeploymentLogger.Enable(uv.update.ID); err != nil {
		// just log error
		log.Errorf("failed to enable deployment logger: %s", err)
	}

	log.Debug("handle update verify state")

	// look at the update flag
	has, haserr := c.HasUpgrade()
	if haserr != nil {
		log.Errorf("has upgrade check failed: %v", haserr)
		me := NewFatalError(errors.Wrapf(haserr, "failed to perform 'has upgrade' check"))
		return NewUpdateErrorState(me, uv.update), false
	}

	if has {
		artifactName, err := c.GetCurrentArtifactName()
		if err != nil {
			log.Errorf("Cannot determine name of new artifact. Update will not continue: %v : %v", defaultDeviceTypeFile, err)
			me := NewFatalError(errors.Wrapf(err, "Cannot determine name of new artifact. Update will not continue: %v : %v", defaultDeviceTypeFile, err))
			return NewUpdateErrorState(me, uv.update), false
		}
		if uv.update.ArtifactName() == artifactName {
			log.Infof("successfully running with new image %v", artifactName)
			// update info and has upgrade flag are there, we're running the new
			// update, everything looks good, proceed with committing
			return NewUpdateCommitState(uv.update), false
		}
		// seems like we're running in a different image than expected from update
		// information, best report an error
		// this can ONLY happen if the artifact name does not match information
		// stored in `/etc/mender/artifact_info` file
		log.Errorf("running with image %v, expected updated image %v",
			artifactName, uv.update.ArtifactName())
		return NewRebootState(uv.update), false
	}

	// HasUpgrade() returned false
	// most probably booting new image failed and u-boot rolledback to
	// previous image
	log.Errorf("update info for deployment %v present, but update flag is not set;"+
		" running rollback image (previous active partition)",
		uv.update.ID)
	return NewUpdateStatusReportState(uv.update, client.StatusFailure), false
}

type UpdateCommitState struct {
	baseState
	update client.UpdateResponse
}

func NewUpdateCommitState(update client.UpdateResponse) State {
	return &UpdateCommitState{
		baseState{
			id: MenderStateUpdateCommit,
		},
		update,
	}
}

func (uc *UpdateCommitState) Handle(ctx *StateContext, c Controller) (State, bool) {

	// start deployment logging
	if err := DeploymentLogger.Enable(uc.update.ID); err != nil {
		log.Errorf("Can not enable deployment logger: %s", err)
	}

	log.Debugf("handle update commit state")

	// reset inventory sending timer
	var zeroTime time.Time
	ctx.lastInventoryUpdate = zeroTime

	err := c.CommitUpdate()
	if err != nil {
		log.Errorf("update commit failed: %s", err)
		// we need to perform roll-back here; one scenario is when u-boot fw utils
		// won't work after update; at this point without rolling-back it won't be
		// possible to perform new update
		// as the update was not commited we can safely reboot only
		return NewRebootState(uc.update), false
	}

	// update is commited now; report status
	return NewUpdateStatusReportState(uc.update, client.StatusSuccess), false
}

type UpdateCheckState struct {
	baseState
}

func (u *UpdateCheckState) Handle(ctx *StateContext, c Controller) (State, bool) {
	log.Debugf("handle update check state")
	ctx.lastUpdateCheck = time.Now()

	update, err := c.CheckUpdate()

	if err != nil {
		if err.Cause() == os.ErrExist {
			// We are already running image which we are supposed to install.
			// Just report successful update and return to normal operations.
			return NewUpdateStatusReportState(*update, client.StatusAlreadyInstalled), false
		}

		log.Errorf("update check failed: %s", err)
		// maybe transient error?
		return NewErrorState(err), false
	}

	if update != nil {
		return NewUpdateFetchState(*update), false
	}
	return checkWaitState, false
}

type UpdateFetchState struct {
	baseState
	update client.UpdateResponse
}

func NewUpdateFetchState(update client.UpdateResponse) State {
	return &UpdateFetchState{
		baseState{
			id: MenderStateUpdateFetch,
		},
		update,
	}
}

func (u *UpdateFetchState) Handle(ctx *StateContext, c Controller) (State, bool) {
	// start deployment logging
	if err := DeploymentLogger.Enable(u.update.ID); err != nil {
		return NewUpdateErrorState(NewTransientError(err), u.update), false
	}

	log.Debugf("handle update fetch state")

	if err := StoreStateData(ctx.store, StateData{
		Name:       u.Id(),
		UpdateInfo: u.update,
	}); err != nil {
		log.Errorf("failed to store state data in fetch state: %v", err)
		return NewUpdateErrorState(NewTransientError(err), u.update), false
	}

	merr := c.ReportUpdateStatus(u.update, client.StatusDownloading)
	if merr != nil && merr.IsFatal() {
		return NewUpdateErrorState(NewTransientError(merr.Cause()), u.update), false
	}

	in, size, err := c.FetchUpdate(u.update.URI())
	if err != nil {
		log.Errorf("update fetch failed: %s", err)
		return NewFetchInstallRetryState(u, u.update, err), false
	}

	return NewUpdateInstallState(in, size, u.update), false
}

type UpdateInstallState struct {
	baseState
	// reader for obtaining image data
	imagein io.ReadCloser
	// expected image size
	size   int64
	update client.UpdateResponse
}

func NewUpdateInstallState(in io.ReadCloser, size int64, update client.UpdateResponse) State {
	return &UpdateInstallState{
		baseState{
			id: MenderStateUpdateInstall,
		},
		in,
		size,
		update,
	}
}

func (u *UpdateInstallState) Handle(ctx *StateContext, c Controller) (State, bool) {

	// make sure to close the stream with image data
	defer u.imagein.Close()

	// start deployment logging
	if err := DeploymentLogger.Enable(u.update.ID); err != nil {
		return NewUpdateErrorState(NewTransientError(err), u.update), false
	}

	log.Debugf("handle update install state")

	if err := StoreStateData(ctx.store, StateData{
		Name:       u.Id(),
		UpdateInfo: u.update,
	}); err != nil {
		log.Errorf("failed to store state data in install state: %v", err)
		return NewUpdateErrorState(NewTransientError(err), u.update), false
	}

	merr := c.ReportUpdateStatus(u.update, client.StatusInstalling)
	if merr != nil && merr.IsFatal() {
		return NewUpdateErrorState(NewTransientError(merr.Cause()), u.update), false
	}

	if err := c.InstallUpdate(u.imagein, u.size); err != nil {
		log.Errorf("update install failed: %s", err)
		return NewFetchInstallRetryState(u, u.update, err), false
	}

	// restart counter so that we are able to retry next time
	ctx.fetchInstallAttempts = 0

	// check if update is not aborted
	// this step is needed as installing might take a while and we might end up with
	// proceeding with already cancelled update
	merr = c.ReportUpdateStatus(u.update, client.StatusInstalling)
	if merr != nil && merr.IsFatal() {
		return NewUpdateErrorState(NewTransientError(merr.Cause()), u.update), false
	}

	// if install was successful mark inactive partition as active one
	if err := c.EnableUpdatedPartition(); err != nil {
		return NewUpdateErrorState(NewTransientError(err), u.update), false
	}

	return NewRebootState(u.update), false
}

type FetchInstallRetryState struct {
	WaitState
	from   State
	update client.UpdateResponse
	err    error
}

func NewFetchInstallRetryState(from State, update client.UpdateResponse,
	err error) State {
	return &FetchInstallRetryState{
		WaitState: NewWaitState(MenderStateFetchInstallRetryWait),
		from:      from,
		update:    update,
		err:       err,
	}
}

// Simple algorhithm: Start with one minute, and try three times, then double
// interval (regularInterval is maximum) and try again. Repeat until we tried
// three times with regularInterval.
func getFetchInstallRetry(tried int, regularInterval time.Duration) (time.Duration, error) {
	const perIntervalAttempts = 3

	interval := 1 * time.Minute
	nextInterval := interval

	for c := 0; c <= tried; c += perIntervalAttempts {
		interval = nextInterval
		nextInterval *= 2
		if interval >= regularInterval {
			if tried-c >= perIntervalAttempts {
				// At max interval and already tried three
				// times. Give up.
				return 0, errors.New("Tried maximum amount of times")
			}

			// Don't use less than one minute.
			if regularInterval < 1*time.Minute {
				return 1 * time.Minute, nil
			}
			return regularInterval, nil
		}
	}

	return interval, nil
}

func (fir *FetchInstallRetryState) Handle(ctx *StateContext, c Controller) (State, bool) {
	log.Debugf("handle fetch install retry state")

	intvl, err := getFetchInstallRetry(ctx.fetchInstallAttempts, c.GetUpdatePollInterval())
	if err != nil {
		if fir.err != nil {
			return NewErrorState(NewTransientError(errors.Wrap(fir.err, err.Error()))), false
		}
		return NewErrorState(NewTransientError(err)), false
	}

	ctx.fetchInstallAttempts++

	log.Debugf("wait %v before next fetch/install attempt", intvl)
	return fir.Wait(NewUpdateFetchState(fir.update), fir, intvl)
}

type CheckWaitState struct {
	WaitState
}

func NewCheckWaitState() State {
	return &CheckWaitState{
		WaitState: NewWaitState(MenderStateCheckWait),
	}
}

func (cw *CheckWaitState) Handle(ctx *StateContext, c Controller) (State, bool) {

	log.Debugf("handle check wait state")

	// calculate next interval
	update := ctx.lastUpdateCheck.Add(c.GetUpdatePollInterval())
	inventory := ctx.lastInventoryUpdate.Add(c.GetInventoryPollInterval())

	log.Debugf("check wait state; next checks: (update: %v) (inventory: %v)",
		update, inventory)

	next := struct {
		when  time.Time
		state State
	}{
		// assume update will be the next state
		when:  update,
		state: updateCheckState,
	}

	if inventory.Before(update) {
		next.when = inventory
		next.state = inventoryUpdateState
	}

	now := time.Now()
	log.Debugf("next check: %v:%v, (%v)", next.when, next.state, now)

	// check if we should wait for the next state or we should return
	// immediately
	if next.when.After(time.Now()) {
		wait := next.when.Sub(now)
		log.Debugf("waiting %s for the next state", wait)
		return cw.Wait(next.state, cw, wait)
	}

	log.Debugf("check wait returned: %v", next.state)
	return next.state, false
}

type InventoryUpdateState struct {
	baseState
}

func (iu *InventoryUpdateState) Handle(ctx *StateContext, c Controller) (State, bool) {

	ctx.lastInventoryUpdate = time.Now()

	err := c.InventoryRefresh()
	if err != nil {
		log.Warnf("failed to refresh inventory: %v", err)
	} else {
		log.Debugf("inventory refresh complete")
	}
	return checkWaitState, false
}

type ErrorState struct {
	baseState
	cause menderError
}

func NewErrorState(err menderError) State {
	if err == nil {
		err = NewFatalError(errors.New("general error"))
	}

	return &ErrorState{
		baseState{
			id: MenderStateError,
		},
		err,
	}
}

func (e *ErrorState) Handle(ctx *StateContext, c Controller) (State, bool) {
	log.Infof("handling error state, current error: %v", e.cause.Error())
	// decide if error is transient, exit for now
	if e.cause.IsFatal() {
		return doneState, false
	}
	return initState, false
}

func (e *ErrorState) IsFatal() bool {
	return e.cause.IsFatal()
}

type UpdateErrorState struct {
	ErrorState
	update client.UpdateResponse
}

func NewUpdateErrorState(err menderError, update client.UpdateResponse) State {
	return &UpdateErrorState{
		ErrorState{
			baseState{id: MenderStateUpdateError},
			err,
		},
		update,
	}
}

func (ue *UpdateErrorState) Handle(ctx *StateContext, c Controller) (State, bool) {
	return NewUpdateStatusReportState(ue.update, client.StatusFailure), false
}

// Wrapper for mandatory update state reporting. The state handler will attempt
// to report state for a number of times. In case of recurring failure, the
// update is deemed as failed.
type UpdateStatusReportState struct {
	baseState
	update             client.UpdateResponse
	status             string
	triesSendingReport int
	reportSent         bool
	triesSendingLogs   int
	logs               []byte
}

func NewUpdateStatusReportState(update client.UpdateResponse, status string) State {
	return &UpdateStatusReportState{
		baseState: baseState{id: MenderStateUpdateStatusReport},
		update:    update,
		status:    status,
	}
}

func sendDeploymentLogs(update client.UpdateResponse, sentTries *int,
	logs []byte, c Controller) menderError {
	if logs == nil {
		var err error
		logs, err = DeploymentLogger.GetLogs(update.ID)
		if err != nil {
			log.Errorf("Failed to get deployment logs for deployment [%v]: %v",
				update.ID, err)
			// there is nothing more we can do here
			return NewFatalError(errors.New("can not get deployment logs from file"))
		}
	}

	*sentTries++

	if err := c.UploadLog(update, logs); err != nil {
		// we got error while sending deployment logs to server;
		log.Errorf("failed to report deployment logs: %v", err)
		return NewTransientError(errors.Wrapf(err, "failed to send deployment logs"))
	}
	return nil
}

func sendDeploymentStatus(update client.UpdateResponse, status string,
	tries *int, sent *bool, c Controller) menderError {
	// check if the report was already sent
	if !*sent {
		*tries++
		if err := c.ReportUpdateStatus(update, status); err != nil {
			return err
		}
		*sent = true
	}
	return nil
}

func (usr *UpdateStatusReportState) Handle(ctx *StateContext, c Controller) (State, bool) {

	// start deployment logging; no error checking
	// we can do nothing here; either we will have the logs or not...
	DeploymentLogger.Enable(usr.update.ID)

	log.Debug("handle update status report state")

	if err := StoreStateData(ctx.store, StateData{
		Name:         usr.Id(),
		UpdateInfo:   usr.update,
		UpdateStatus: usr.status,
	}); err != nil {
		log.Errorf("failed to store state data in update status report state: %v",
			err)
		return NewReportErrorState(usr.update, usr.status), false
	}

	if err := sendDeploymentStatus(usr.update, usr.status,
		&usr.triesSendingReport, &usr.reportSent, c); err != nil {
		log.Errorf("failed to send status to server: %v", err)
		return NewUpdateStatusReportRetryState(usr, usr.update,
			usr.status, usr.triesSendingReport), false
	}

	if usr.status == client.StatusFailure {
		log.Debugf("attempting to upload deployment logs for failed update")
		if err := sendDeploymentLogs(usr.update,
			&usr.triesSendingLogs, usr.logs, c); err != nil {
			log.Errorf("failed to send deployment logs to server: %v", err)
			if err.IsFatal() {
				// there is no point in retrying
				return NewReportErrorState(usr.update, usr.status), false
			}
			return NewUpdateStatusReportRetryState(usr, usr.update, usr.status,
				usr.triesSendingLogs), false
		}
	}

	log.Debug("reporting complete")
	// stop deployment logging as the update is completed at this point
	DeploymentLogger.Disable()
	// status reported, logs uploaded if needed, remove state data
	RemoveStateData(ctx.store)

	return initState, false
}

type UpdateStatusReportRetryState struct {
	WaitState
	reportState  State
	update       client.UpdateResponse
	status       string
	triesSending int
}

func NewUpdateStatusReportRetryState(reportState State,
	update client.UpdateResponse, status string, tries int) State {
	return &UpdateStatusReportRetryState{
		WaitState:    NewWaitState(MenderStatusReportRetryState),
		reportState:  reportState,
		update:       update,
		status:       status,
		triesSending: tries,
	}
}

// try to send failed report at lest 3 times or keep trying every
// 'retryPollInterval' for the duration of two 'updatePollInterval'
func maxSendingAttempts(upi, rpi time.Duration, minRetries int) int {
	if rpi == 0 {
		return minRetries
	}
	max := upi / rpi
	if max <= 3 {
		return minRetries
	}
	return int(max) * 2
}

// retry at least that many times
var minReportSendRetries = 3

func (usr *UpdateStatusReportRetryState) Handle(ctx *StateContext, c Controller) (State, bool) {
	maxTrySending :=
		maxSendingAttempts(c.GetUpdatePollInterval(),
			c.GetRetryPollInterval(), minReportSendRetries)
		// we are always initializing with triesSending = 1
	maxTrySending++

	if usr.triesSending < maxTrySending {
		return usr.Wait(usr.reportState, usr, c.GetRetryPollInterval())
	}
	return NewReportErrorState(usr.update, usr.status), false
}

type ReportErrorState struct {
	baseState
	update       client.UpdateResponse
	updateStatus string
}

func NewReportErrorState(update client.UpdateResponse, status string) State {
	return &ReportErrorState{
		baseState: baseState{
			id: MenderStateReportStatusError,
		},
		update:       update,
		updateStatus: status,
	}
}

func (res *ReportErrorState) Handle(ctx *StateContext, c Controller) (State, bool) {
	log.Errorf("handling report error state with status: %v", res.updateStatus)

	switch res.updateStatus {
	case client.StatusSuccess:
		// error while reporting success; rollback
		return NewRollbackState(res.update), false
	case client.StatusFailure:
		// error while reporting failure;
		// start from scratch as previous update was broken
		log.Errorf("error while performing update: %v (%v)", res.updateStatus, res.update)
		RemoveStateData(ctx.store)
		return initState, false
	case client.StatusAlreadyInstalled:
		// we've failed to report already-installed status, not a big
		// deal, start from scratch
		RemoveStateData(ctx.store)
		return initState, false
	default:
		// should not end up here
		return doneState, false
	}
}

type RebootState struct {
	baseState
	update client.UpdateResponse
}

func NewRebootState(update client.UpdateResponse) State {
	return &RebootState{
		baseState{
			id: MenderStateReboot,
		},
		update,
	}
}

func (e *RebootState) Handle(ctx *StateContext, c Controller) (State, bool) {

	// start deployment logging
	if err := DeploymentLogger.Enable(e.update.ID); err != nil {
		// just log error; we need to reboot anyway
		log.Errorf("failed to enable deployment logger: %s", err)
	}

	log.Debug("handling reboot state")

	if err := StoreStateData(ctx.store, StateData{
		Name:       e.Id(),
		UpdateInfo: e.update,
	}); err != nil {
		// too late to do anything now, update is installed and enabled, let's play
		// along and reboot
		log.Errorf("failed to store state data in reboot state: %v, "+
			"continuing with reboot", err)
	}

	merr := c.ReportUpdateStatus(e.update, client.StatusRebooting)
	if merr != nil && merr.IsFatal() {
		return NewUpdateErrorState(NewTransientError(merr.Cause()), e.update), false
	}

	log.Info("rebooting device")

	if err := c.Reboot(); err != nil {
		log.Errorf("error rebooting device: %v", err)
		return NewErrorState(NewFatalError(err)), false
	}

	// we can not reach this point
	return doneState, false
}

type RollbackState struct {
	baseState
	update client.UpdateResponse
}

func NewRollbackState(update client.UpdateResponse) State {
	return &RollbackState{
		baseState{
			id: MenderStateRollback,
		},
		update,
	}
}

func (rs *RollbackState) Handle(ctx *StateContext, c Controller) (State, bool) {
	// start deployment logging
	if err := DeploymentLogger.Enable(rs.update.ID); err != nil {
		// just log error; we need to reboot anyway
		log.Errorf("failed to enable deployment logger: %s", err)
	}

	log.Info("performing rollback")

	// swap active and inactive partitions
	if err := c.Rollback(); err != nil {
		log.Errorf("rollback failed: %s", err)
		return NewErrorState(NewFatalError(err)), false
	}

	return NewRollbackRebootState(rs.update), false
}

type RollbackRebootState struct {
	baseState
	update client.UpdateResponse
}

func NewRollbackRebootState(update client.UpdateResponse) State {
	return &RollbackRebootState{
		baseState{
			id: MenderStateRollbackReboot,
		},
		update,
	}
}

func (rs *RollbackRebootState) Handle(ctx *StateContext, c Controller) (State, bool) {
	// start deployment logging
	if err := DeploymentLogger.Enable(rs.update.ID); err != nil {
		// just log error; we need to reboot anyway
		log.Errorf("failed to enable deployment logger: %s", err)
	}

	log.Info("rebooting device after rollback")

	if err := StoreStateData(ctx.store, StateData{
		Name:       rs.Id(),
		UpdateInfo: rs.update,
	}); err != nil {
		// too late to do anything now, let's play along and reboot
		log.Errorf("failed to store state data in reboot state: %v, "+
			"continuing with reboot", err)
	}

	if err := c.Reboot(); err != nil {
		log.Errorf("error rebooting device: %v", err)
		return NewErrorState(NewFatalError(err)), false
	}

	// we can not reach this point
	return doneState, false
}

type AfterRollbackRebootState struct {
	baseState
	update client.UpdateResponse
}

func NewAfterRollbackRebootState(update client.UpdateResponse) State {
	return &AfterRollbackRebootState{
		baseState{
			id: MenderStateAfterRollbackReboot,
		},
		update,
	}
}

func (rs *AfterRollbackRebootState) Handle(ctx *StateContext,
	c Controller) (State, bool) {
	// this state is needed to satisfy ToRollbackReboot
	// transition Leave() action
	log.Debug("handling state after rollback reboot")

	return NewUpdateStatusReportState(rs.update, client.StatusFailure), false
}

type FinalState struct {
	baseState
}

func (f *FinalState) Handle(ctx *StateContext, c Controller) (State, bool) {
	panic("reached final state")
}

// current version of the format of StateData;
// incerease the version number once the format of StateData is changed
const stateDataVersion = 1

func StoreStateData(store store.Store, sd StateData) error {
	// if the verions is not filled in, use the current one
	if sd.Version == 0 {
		sd.Version = stateDataVersion
	}
	data, _ := json.Marshal(sd)

	return store.WriteAll(stateDataKey, data)
}

func LoadStateData(store store.Store) (StateData, error) {
	data, err := store.ReadAll(stateDataKey)
	if err != nil {
		return StateData{}, err
	}

	var sd StateData
	// we are relying on the fact that Unmarshal will decode all and only the fields
	// that it can find in the destination type.
	err = json.Unmarshal(data, &sd)
	if err != nil {
		return StateData{}, err
	}

	switch sd.Version {
	case 0, 1:
		return sd, nil
	default:
		return StateData{}, errors.New("unsupported state data version")
	}
}

func RemoveStateData(store store.Store) error {
	return store.Remove(stateDataKey)
}
