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
	"encoding/json"
	"io"
	"os"
	"time"

	"github.com/mendersoftware/log"
	"github.com/mendersoftware/mender/client"
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
//                                  v
//
//                             update fetch
//
//                                  |
//                                  | (update fetched)
//                                  v
//
//                            update install
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
	store               Store
	lastUpdateCheck     time.Time
	lastInventoryUpdate time.Time
}

type State interface {
	// Perform state action, returns next state and boolean flag indicating if
	// execution was cancelled or not
	Handle(ctx *StateContext, c Controller) (State, bool)
	// Cancel state action, returns true if action was cancelled
	Cancel() bool
	// Return numeric state ID
	Id() MenderState
}

type StateRunner interface {
	// Set runner's state to 's'
	SetState(s State)
	// Obtain runner's state
	GetState() State
	// Run the currently set state with this context
	RunState(ctx *StateContext) (State, bool)
}

// state information that can be used for restring state from storage
type StateData struct {
	// update reponse data for the update that was in progress
	UpdateInfo client.UpdateResponse
	// string representing the id of the last state to execute
	Name string
	// update status
	UpdateStatus string
}

const (
	// name of file where state data is stored across reboots
	stateDataFileName = "state"
)

var (
	initState = &InitState{
		BaseState{
			id: MenderStateInit,
		},
	}

	bootstrappedState = &BootstrappedState{
		BaseState{
			id: MenderStateBootstrapped,
		},
	}

	authorizeWaitState = NewAuthorizeWaitState()

	authorizedState = &AuthorizedState{
		BaseState{
			id: MenderStateAuthorized,
		},
	}

	inventoryUpdateState = &InventoryUpdateState{
		BaseState{
			id: MenderStateInventoryUpdate,
		},
	}

	checkWaitState = NewCheckWaitState()

	updateCheckState = &UpdateCheckState{
		BaseState{
			id: MenderStateUpdateCheck,
		},
	}

	doneState = &FinalState{
		BaseState{
			id: MenderStateDone,
		},
	}
)

// Helper base state with some convenience methods
type BaseState struct {
	id MenderState
}

func (b *BaseState) Id() MenderState {
	return b.id
}

func (b *BaseState) Cancel() bool {
	return false
}

type CancellableState struct {
	BaseState
	cancel chan bool
}

func NewCancellableState(base BaseState) CancellableState {
	return CancellableState{
		base,
		make(chan bool),
	}
}

// Perform wait for time `wait` and return state (`next`, false) after the wait
// has completed. If wait was interrupted returns (`same`, true)
func (cs *CancellableState) StateAfterWait(next, same State, wait time.Duration) (State, bool) {
	if cs.Wait(wait) {
		// wait complete
		return next, false
	}
	return same, true
}

// wait and return true if wait was completed (false if canceled)
func (cs *CancellableState) Wait(wait time.Duration) bool {
	ticker := time.NewTicker(wait)

	defer ticker.Stop()
	select {
	case <-ticker.C:
		log.Debugf("wait complete")
		return true
	case <-cs.cancel:
		log.Infof("wait canceled")
	}

	return false
}

func (cs *CancellableState) Cancel() bool {
	cs.cancel <- true
	return true
}

func (cs *CancellableState) Stop() {
	close(cs.cancel)
}

type InitState struct {
	BaseState
}

func (i *InitState) Handle(ctx *StateContext, c Controller) (State, bool) {

	// make sure that deployment logging is disabled
	DeploymentLogger.Disable()

	log.Debugf("handle init state")
	if err := c.Bootstrap(); err != nil {
		log.Errorf("bootstrap failed: %s", err)
		return NewErrorState(err), false
	}
	return bootstrappedState, false
}

type BootstrappedState struct {
	BaseState
}

func (b *BootstrappedState) Handle(ctx *StateContext, c Controller) (State, bool) {
	log.Debugf("handle bootstrapped state")
	if err := c.Authorize(); err != nil {
		log.Errorf("authorize failed: %v", err)
		if !err.IsFatal() {
			return authorizeWaitState, false
		} else {
			return NewErrorState(err), false
		}
	}

	return authorizedState, false
}

type UpdateVerifyState struct {
	BaseState
	update client.UpdateResponse
}

func NewUpdateVerifyState(update client.UpdateResponse) State {
	return &UpdateVerifyState{
		BaseState{
			id: MenderStateUpdateVerify,
		},
		update,
	}
}

func (uv *UpdateVerifyState) Handle(ctx *StateContext, c Controller) (State, bool) {

	// start deployment logging
	if err := DeploymentLogger.Enable(uv.update.ID); err != nil {
		return NewUpdateErrorState(NewTransientError(err), uv.update), false
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
		if uv.update.ArtifactName() == c.GetCurrentArtifactName() {
			log.Infof("successfully running with new image %v", c.GetCurrentArtifactName())
			// update info and has upgrade flag are there, we're running the new
			// update, everything looks good, proceed with committing
			return NewUpdateCommitState(uv.update), false
		} else {
			// seems like we're running in a different image than expected from update
			// information, best report an error
			log.Errorf("running with image %v, expected updated image %v",
				c.GetCurrentArtifactName(), uv.update.ArtifactName())
			return NewUpdateStatusReportState(uv.update, client.StatusFailure), false
		}
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
	BaseState
	update client.UpdateResponse
}

func NewUpdateCommitState(update client.UpdateResponse) State {
	return &UpdateCommitState{
		BaseState{
			id: MenderStateUpdateCommit,
		},
		update,
	}
}

func (uc *UpdateCommitState) Handle(ctx *StateContext, c Controller) (State, bool) {

	// start deployment logging
	if err := DeploymentLogger.Enable(uc.update.ID); err != nil {
		return NewUpdateErrorState(NewTransientError(err), uc.update), false
	}

	// reset inventory sending timer
	var zeroTime time.Time
	ctx.lastInventoryUpdate = zeroTime

	log.Debugf("handle update commit state")

	err := c.CommitUpdate()
	if err != nil {
		log.Errorf("update commit failed: %s", err)
		// TODO: should we rollback?
		return NewUpdateStatusReportState(uc.update, client.StatusFailure), false
	}

	// update is commited now; report status
	return NewUpdateStatusReportState(uc.update, client.StatusSuccess), false
}

type UpdateCheckState struct {
	BaseState
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
		// custom state data?
		return NewUpdateFetchState(*update), false
	}

	return checkWaitState, false
}

type UpdateFetchState struct {
	BaseState
	update client.UpdateResponse
}

func NewUpdateFetchState(update client.UpdateResponse) State {
	return &UpdateFetchState{
		BaseState{
			id: MenderStateUpdateFetch,
		},
		update,
	}
}

func (u *UpdateFetchState) Handle(ctx *StateContext, c Controller) (State, bool) {
	if err := StoreStateData(ctx.store, StateData{
		Name:       u.Id().String(),
		UpdateInfo: u.update,
	}); err != nil {
		log.Errorf("failed to store state data in fetch state: %v", err)
		return NewUpdateErrorState(NewTransientError(err), u.update), false
	}

	merr := c.ReportUpdateStatus(u.update, client.StatusDownloading)
	if merr != nil && merr.IsFatal() {
		return NewUpdateErrorState(NewTransientError(merr.Cause()), u.update), false
	}

	log.Debugf("handle update fetch state")
	in, size, err := c.FetchUpdate(u.update.URI())
	if err != nil {
		log.Errorf("update fetch failed: %s", err)
		return NewUpdateErrorState(NewTransientError(err), u.update), false
	}

	return NewUpdateInstallState(in, size, u.update), false
}

type UpdateInstallState struct {
	BaseState
	// reader for obtaining image data
	imagein io.ReadCloser
	// expected image size
	size   int64
	update client.UpdateResponse
}

func NewUpdateInstallState(in io.ReadCloser, size int64, update client.UpdateResponse) State {
	return &UpdateInstallState{
		BaseState{
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

	if err := StoreStateData(ctx.store, StateData{
		Name:       u.Id().String(),
		UpdateInfo: u.update,
	}); err != nil {
		log.Errorf("failed to store state data in install state: %v", err)
		return NewUpdateErrorState(NewTransientError(err), u.update), false
	}

	merr := c.ReportUpdateStatus(u.update, client.StatusInstalling)
	if merr != nil && merr.IsFatal() {
		return NewUpdateErrorState(NewTransientError(merr.Cause()), u.update), false
	}

	log.Debugf("handle update install state")

	if err := c.InstallUpdate(u.imagein, u.size); err != nil {
		log.Errorf("update install failed: %s", err)
		return NewUpdateErrorState(NewTransientError(err), u.update), false
	}

	return NewRebootState(u.update), false
}

type CheckWaitState struct {
	CancellableState
}

func NewCheckWaitState() State {
	return &CheckWaitState{
		NewCancellableState(BaseState{
			id: MenderStateCheckWait,
		}),
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

	if next.when.After(time.Now()) {
		wait := next.when.Sub(now)

		log.Debugf("waiting %s for the next state", wait)

		completed := cw.Wait(wait)
		if !completed {
			log.Info("waiting cancelled")
			return cw, true
		}
	}

	log.Debugf("check wait returned: %v", next.state)
	return next.state, false
}

type AuthorizeWaitState struct {
	CancellableState
}

func NewAuthorizeWaitState() State {
	return &AuthorizeWaitState{
		NewCancellableState(BaseState{
			id: MenderStateAuthorizeWait,
		}),
	}
}

func (a *AuthorizeWaitState) Handle(ctx *StateContext, c Controller) (State, bool) {
	log.Debugf("handle authorize wait state")
	intvl := c.GetRetryPollInterval()

	log.Debugf("wait %v before next authorization attempt", intvl)
	return a.StateAfterWait(bootstrappedState, a, intvl)
}

type AuthorizedState struct {
	BaseState
}

func (a *AuthorizedState) Handle(ctx *StateContext, c Controller) (State, bool) {
	// restore previous state information
	sd, err := LoadStateData(ctx.store)

	// tricky part - try to figure out if there's an update in progress, if so
	// proceed to UpdateCommitState; in case of errors that occur either now or
	// when the update was being feched/installed previously, try to handle them
	// gracefully

	// handle easy case first, no update info present, means no update in progress
	if err != nil && os.IsNotExist(err) {
		log.Debug("no update in progress, proceed")
		return inventoryUpdateState, false
	}

	if err != nil {
		log.Errorf("failed to restore update information: %v", err)
		me := NewFatalError(errors.Wrapf(err, "failed to restore update information"))

		// report update error with unknown deployment ID
		// TODO: fill current artifact name?
		return NewUpdateErrorState(me, client.UpdateResponse{
			ID: "unknown",
		}), false
	}

	log.Infof("handling state: %s", sd.Name)

	// chack last known status
	switch StateID(sd.Name) {
	// update process was finished; check what is the status of update
	case MenderStateReboot:
		return NewUpdateVerifyState(sd.UpdateInfo), false

		// update prosess was initialized but stopped in the middle
	case MenderStateUpdateFetch, MenderStateUpdateInstall:
		// TODO: for now we just continue sending error report to the server
		// in future we might want to have some recovery option here
		me := NewFatalError(errors.New("update process was interrupted"))
		return NewUpdateErrorState(me, sd.UpdateInfo), false

		// there was some error while reporting update status
	case MenderStateUpdateStatusReport:
		log.Infof("restoring update status report state")
		if sd.UpdateStatus != client.StatusFailure &&
			sd.UpdateStatus != client.StatusSuccess {
			return NewUpdateStatusReportState(sd.UpdateInfo, client.StatusFailure), false
		}
		// check what is exact state of update before reporting anything
		return NewUpdateVerifyState(sd.UpdateInfo), false

		// this should not happen
	default:
		log.Errorf("got invalid update state: %s", sd.Name)
		me := NewFatalError(errors.New("got invalid update state"))
		return NewUpdateErrorState(me, sd.UpdateInfo), false
	}
}

type InventoryUpdateState struct {
	BaseState
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
	BaseState
	cause menderError
}

func NewErrorState(err menderError) State {
	if err == nil {
		err = NewFatalError(errors.New("general error"))
	}

	return &ErrorState{
		BaseState{
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
			BaseState{
				id: MenderStateUpdateError,
			},
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
	CancellableState
	update             client.UpdateResponse
	status             string
	triesSendingReport int
}

func NewUpdateStatusReportState(update client.UpdateResponse, status string) State {
	return &UpdateStatusReportState{
		CancellableState: NewCancellableState(BaseState{
			id: MenderStateUpdateStatusReport,
		}),
		update: update,
		status: status,
	}
}

type SendData func(updResp client.UpdateResponse, status string, c Controller) menderError

func sendDeploymentLogs(update client.UpdateResponse, status string, c Controller) menderError {
	logs, err := DeploymentLogger.GetLogs(update.ID)
	if err != nil {
		log.Errorf("Failed to get deployment logs for deployment [%v]: %v",
			update.ID, err)
		// there is nothing more we can do here
		return NewFatalError(errors.New("can not get deployment logs from file"))
	}

	if err = c.UploadLog(update, logs); err != nil {
		// we got error while sending deployment logs to server;
		log.Errorf("failed to report deployment logs: %v", err)
		return NewFatalError(errors.Wrapf(err, "failed to send deployment logs"))
	}
	return nil
}

// wrapper for report sending
func sendStatus(update client.UpdateResponse, status string, c Controller) menderError {
	return c.ReportUpdateStatus(update, status)
}

// retry at least that many times
var minReportSendRetries = 3

// try to send failed report at lest 3 times or keep trying every
// 'retryPollInterval' for the duration of two 'updatePollInterval'
func maxSendingAttempts(upi, rpi time.Duration) int {
	if rpi == 0 {
		return minReportSendRetries
	}
	max := upi / rpi
	if max <= 3 {
		return minReportSendRetries
	}
	return int(max) * 2
}

func (usr *UpdateStatusReportState) trySend(send SendData, c Controller) (error, bool) {

	maxTrySending :=
		maxSendingAttempts(c.GetUpdatePollInterval(), c.GetRetryPollInterval())
	for usr.triesSendingReport < maxTrySending {

		log.Infof("attempting to report data of deployment [%v] to the backend;"+
			" deployment status [%v], try %d",
			usr.update.ID, usr.status, usr.triesSendingReport)
		if err := send(usr.update, usr.status, c); err != nil {
			log.Errorf("failed to report data %v: %v", usr.status, err.Cause())
			// fatal error means that the cause is not likely to go
			// away with subsequent retries, just stop at once
			if err.IsFatal() {
				return err, false
			}

			// error reporting status or sending logs;
			// wait for some time before trying again
			if wc := usr.Wait(c.GetRetryPollInterval()); wc == false {
				// if the waiting was interrupted don't increase triesSendingReport
				return nil, true
			}
			usr.triesSendingReport++
			continue
		}
		// reset counter
		usr.triesSendingReport = 0
		return nil, false
	}
	return NewFatalError(errors.New("error sending data to server")), false
}

func (usr *UpdateStatusReportState) Handle(ctx *StateContext, c Controller) (State, bool) {

	// start deployment logging; no error checking
	// we can do nothing here; either we will have the logs or not...
	DeploymentLogger.Enable(usr.update.ID)

	if err := StoreStateData(ctx.store, StateData{
		Name:         usr.Id().String(),
		UpdateInfo:   usr.update,
		UpdateStatus: usr.status,
	}); err != nil {
		log.Errorf("failed to store state data in update status report state: %v",
			err)
		return NewReportErrorState(usr.update, usr.status), false
	}

	err, wasInterupted := usr.trySend(sendStatus, c)
	if wasInterupted {
		return usr, false
	}
	if err != nil {
		log.Errorf("failed to send status to server: %v", err)
		return NewReportErrorState(usr.update, usr.status), false
	}

	if usr.status == client.StatusFailure {
		log.Debugf("attempting to upload deployment logs for failed update")
		err, wasInterupted = usr.trySend(sendDeploymentLogs, c)
		if wasInterupted {
			return usr, false
		}
		if err != nil {
			log.Errorf("failed to send deployment logs to server: %v", err)
			return NewReportErrorState(usr.update, usr.status), false
		}
	}

	log.Debug("reporting complete")
	// stop deployment logging as the update is completed at this point
	DeploymentLogger.Disable()
	// status reported, logs uploaded if needed, remove state data
	RemoveStateData(ctx.store)

	return initState, false
}

type ReportErrorState struct {
	BaseState
	update       client.UpdateResponse
	updateStatus string
}

func NewReportErrorState(update client.UpdateResponse, status string) State {
	return &ReportErrorState{
		BaseState: BaseState{
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
	BaseState
	update client.UpdateResponse
}

func NewRebootState(update client.UpdateResponse) State {
	return &RebootState{
		BaseState{
			id: MenderStateReboot,
		},
		update,
	}
}

func (e *RebootState) Handle(ctx *StateContext, c Controller) (State, bool) {

	// start deployment logging
	if err := DeploymentLogger.Enable(e.update.ID); err != nil {
		return NewUpdateErrorState(NewTransientError(err), e.update), false
	}

	if err := StoreStateData(ctx.store, StateData{
		Name:       e.Id().String(),
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

	// stop deployment logging
	DeploymentLogger.Disable()

	return doneState, false
}

type RollbackState struct {
	BaseState
	update client.UpdateResponse
}

func NewRollbackState(update client.UpdateResponse) State {
	return &RollbackState{
		BaseState{
			id: MenderStateRollback,
		},
		update,
	}
}

func (rs *RollbackState) Handle(ctx *StateContext, c Controller) (State, bool) {
	DeploymentLogger.Enable(rs.update.ID)
	log.Info("performing rollback")
	// swap active and inactive partitions
	if err := c.Rollback(); err != nil {
		log.Errorf("swapping active and inactive partitions failed: %s", err)
		// TODO: what can we do here
		return NewErrorState(NewFatalError(err)), false
	}
	DeploymentLogger.Disable()
	return NewRebootState(rs.update), false
}

type FinalState struct {
	BaseState
}

func (f *FinalState) Handle(ctx *StateContext, c Controller) (State, bool) {
	panic("reached final state")
}

func StoreStateData(store Store, sd StateData) error {
	data, _ := json.Marshal(sd)

	return store.WriteAll(stateDataFileName, data)
}

func LoadStateData(store Store) (StateData, error) {
	data, err := store.ReadAll(stateDataFileName)
	if err != nil {
		return StateData{}, err
	}

	var sd StateData
	err = json.Unmarshal(data, &sd)
	return sd, err
}

func RemoveStateData(store Store) error {
	return store.Remove(stateDataFileName)
}
