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
//                             init
//
//                               |        (wait timeout expired)
//                               |   +---------------------------------+
//                               |   |                                 |
//                               v   v                                 |
//                                         (auth req. failed)
//                          bootstrapped ----------------------> authorize wait
//
//                                |
//                                |
//                                |  (auth data avail.)
//                                |
//                                v
//
//                           authorized
//
//          (update needs     |   |
//           commit)          |   |
//         +------------------+   |
//         |                      |
//         v                      |          (wait timeout expired)
//                                |    +-----------------------------+
//   update commit                |    |                             |
//                                v    v                             |
//         |                                (no update)
//         +---------------> update check ---------------->  update check wait
//
//                                |
//                                | (update ready)
//                                v
//
//                           update fetch
//
//                                |
//                                | (update fetched)
//                                v
//
//                          update install
//
//                                |
//                                | (update installed,
//                                |  enabled)
//                                |
//                                v
//
//                              reboot
//
//                                |
//                                v
//
//                              final (daemon exit)
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
//        |      (commit )
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
	store Store
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
	UpdateInfo UpdateResponse
	// id of the last state to execute
	Id MenderState
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

	updateCheckWaitState = NewUpdateCheckWaitState()

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

type UpdateCommitState struct {
	BaseState
	update UpdateResponse
}

func NewUpdateCommitState(update UpdateResponse) State {
	return &UpdateCommitState{
		BaseState{
			id: MenderStateUpdateCommit,
		},
		update,
	}
}

func (uc *UpdateCommitState) Handle(ctx *StateContext, c Controller) (State, bool) {

	// start deployment logging
	DeploymentLogger.Enable(uc.update.ID)

	log.Debugf("handle update commit state")
	err := c.CommitUpdate()
	if err != nil {
		log.Errorf("update commit failed: %s", err)
		return NewUpdateErrorState(NewFatalError(err), uc.update), false
	}

	// done?
	return NewUpdateStatusReportState(uc.update, statusSuccess), false
}

type UpdateCheckState struct {
	BaseState
}

func (u *UpdateCheckState) Handle(ctx *StateContext, c Controller) (State, bool) {
	log.Debugf("handle update check state")
	update, err := c.CheckUpdate()
	if err != nil {
		log.Errorf("update check failed: %s", err)
		// maybe transient error?
		return NewErrorState(err), false
	}

	if update != nil {
		// custom state data?
		return NewUpdateFetchState(*update), false
	}

	return updateCheckWaitState, false
}

type UpdateFetchState struct {
	BaseState
	update UpdateResponse
}

func NewUpdateFetchState(update UpdateResponse) State {
	return &UpdateFetchState{
		BaseState{
			id: MenderStateUpdateFetch,
		},
		update,
	}
}

func (u *UpdateFetchState) Handle(ctx *StateContext, c Controller) (State, bool) {

	// start logging as we are having new update to be installed
	DeploymentLogger.Enable(u.update.ID)

	if err := StoreStateData(ctx.store, StateData{
		Id:         u.Id(),
		UpdateInfo: u.update,
	}); err != nil {
		log.Errorf("failed to store state data in fetch state: %v", err)
		return NewUpdateErrorState(NewTransientError(err), u.update), false
	}

	// report downloading, don't care about errors
	c.ReportUpdateStatus(u.update, statusDownloading)

	log.Debugf("handle update fetch state")
	in, size, err := c.FetchUpdate(u.update.Image.URI)
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
	update UpdateResponse
}

func NewUpdateInstallState(in io.ReadCloser, size int64, update UpdateResponse) State {
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

	// start deployment logging
	DeploymentLogger.Enable(u.update.ID)

	if err := StoreStateData(ctx.store, StateData{
		Id:         u.Id(),
		UpdateInfo: u.update,
	}); err != nil {
		log.Errorf("failed to store state data in install state: %v", err)
		return NewUpdateErrorState(NewTransientError(err), u.update), false
	}

	// report installing, don't care about errors
	c.ReportUpdateStatus(u.update, statusInstalling)

	log.Debugf("handle update install state")
	if err := c.InstallUpdate(u.imagein, u.size); err != nil {
		log.Errorf("update install failed: %s", err)
		return NewUpdateErrorState(NewTransientError(err), u.update), false
	}

	if err := c.EnableUpdatedPartition(); err != nil {
		log.Errorf("enabling updated partition failed: %s", err)
		return NewUpdateErrorState(NewTransientError(err), u.update), false
	}

	return NewRebootState(u.update), false
}

type UpdateCheckWaitState struct {
	CancellableState
}

func NewUpdateCheckWaitState() State {
	return &UpdateCheckWaitState{
		NewCancellableState(BaseState{
			id: MenderStateUpdateCheckWait,
		}),
	}
}

func (u *UpdateCheckWaitState) Handle(ctx *StateContext, c Controller) (State, bool) {
	log.Debugf("handle update check wait state")

	intvl := c.GetUpdatePollInterval()

	log.Debugf("wait %v before next poll", intvl)
	return u.StateAfterWait(updateCheckState, u, intvl)
}

// Cancel wait state
func (u *UpdateCheckWaitState) Cancel() bool {
	u.cancel <- true
	return true
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
	intvl := c.GetUpdatePollInterval()

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
		return updateCheckWaitState, false
	}

	if err != nil {
		log.Errorf("failed to restore update information: %v", err)
		me := NewFatalError(errors.Wrapf(err, "failed to restore update information"))

		// report update error with unknown deployment ID
		// TODO: fill current image ID?
		return NewUpdateErrorState(me, UpdateResponse{
			ID: "unknown",
		}), false
	}

	// were we reporting update status before?
	if sd.Id == MenderStateUpdateStatusReport {
		if sd.UpdateStatus != statusSuccess && sd.UpdateStatus != statusFailure {
			log.Errorf("unexpected deployment %s status %s, overriding with %s",
				sd.UpdateInfo.ID, sd.UpdateStatus, statusFailure)
			sd.UpdateStatus = statusFailure
		}
		log.Infof("restoring update status report state")
		return NewUpdateStatusReportState(sd.UpdateInfo, sd.UpdateStatus), false
	}

	// look at the update flag
	has, haserr := c.HasUpgrade()
	if haserr != nil {
		log.Errorf("has upgrade check failed: %v", haserr)
		me := NewFatalError(errors.Wrapf(err, "failed to perform 'has upgrade' check"))
		return NewUpdateErrorState(me, sd.UpdateInfo), false
	}

	if has {
		// start logging as we might need to store some error logs
		DeploymentLogger.Enable(sd.UpdateInfo.ID)

		if sd.UpdateInfo.Image.YoctoID == c.GetCurrentImageID() {
			log.Infof("successfully running with new image %v", c.GetCurrentImageID())
			// update info and has upgrade flag are there, we're running the new
			// update, everything looks good, proceed with committing
			return NewUpdateCommitState(sd.UpdateInfo), false
		} else {
			// seems like we're running in a different image than expected from update
			// information, best report an error
			log.Errorf("running with image %v, expected updated image %v",
				c.GetCurrentImageID(), sd.UpdateInfo.Image.YoctoID)
			me := NewFatalError(errors.Errorf("restarted with old image %v, "+
				"expected updated image %v",
				c.GetCurrentImageID(), sd.UpdateInfo.Image.YoctoID))
			return NewUpdateErrorState(me, sd.UpdateInfo), false
		}
	}

	// we have upgrade info but has flag is not set
	log.Infof("update info for deployment %v present, but update flag is not set",
		sd.UpdateInfo.ID)

	// starting from scratch
	log.Debugf("starting from scratch")
	RemoveStateData(ctx.store)

	return updateCheckWaitState, false
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
	update UpdateResponse
}

func NewUpdateErrorState(err menderError, update UpdateResponse) State {
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
<<<<<<< fddaceb41c71707716d65da675649cc042a37427
	return NewUpdateStatusReportState(ue.update, statusFailure), false
}

// Wrapper for mandatory update state reporting. The state handler will attempt
// to report state for a number of times. In case of recurring failure, the
// update is deemed as failed.
type UpdateStatusReportState struct {
	CancellableState
	update UpdateResponse
	status string
}

func NewUpdateStatusReportState(update UpdateResponse, status string) State {
	return &UpdateStatusReportState{
		CancellableState: NewCancellableState(BaseState{
			id: MenderStateUpdateStatusReport,
		}),
		update: update,
		status: status,
	}
}

func (usr *UpdateStatusReportState) Handle(ctx *StateContext, c Controller) (State, bool) {
	if err := StoreStateData(ctx.store, StateData{
		Id:           usr.Id(),
		UpdateInfo:   usr.update,
		UpdateStatus: usr.status,
	}); err != nil {
		log.Errorf("failed to store state data in update status report state: %v",
			err)
		// TODO: update should fail and rollback should be triggered at this point
	}

	try := 0

	for {
		try += 1

		log.Infof("attempting to report status %v of deployment to the backend, try %v",
			usr.status, usr.update.ID, try)
		if merr := c.ReportUpdateStatus(usr.update, usr.status); merr != nil {
			log.Errorf("failed to report status %v: %v", usr.status, merr.Cause())
			// TODO: until backend has implemented status reporting, this error cannot
			// result in update being aborted. Once required API endpoint is available
			// revisit the code below. See https://tracker.mender.io/browse/MEN-536
			// for details about backend implementation.

			// TODO: we should fail the update if status cannot be reported for a
			// number of times. However we cannot really enabled this right now due to
			// missing pieces in the backend.

			// wait for some time before trying again
			// if wc := usr.Wait(c.GetUpdatePollInterval()); wc == false {
			// 	return usr, true
			// }
		} else {
			if usr.status == statusFailure {
				log.Debugf("update failed, attempt log upload")
				// TODO upload logs from the failed update, see
				// https://tracker.mender.io/browse/MEN-437 for details
				c.UploadLog(usr.update, []LogEntry{
					LogEntry{
						Timestamp: time.Now().Format(time.RFC3339),
						Level:     "error",
						Message:   "update failed",
					},
				})
			}
		}

		log.Debug("reporting complete")
		break
	}

	// stop deployment logging as the update is completed at this point
  DeploymentLogger.Disable()

	// status reported, logs uploaded if needed, remove state data
	RemoveStateData(ctx.store)

	return initState, false
}

type RebootState struct {
	BaseState
	update UpdateResponse
}

func NewRebootState(update UpdateResponse) State {
	return &RebootState{
		BaseState{
			id: MenderStateReboot,
		},
		update,
	}
}

func (e *RebootState) Handle(ctx *StateContext, c Controller) (State, bool) {

	// start deployment logging
	DeploymentLogger.Enable(e.update.ID)

	if err := StoreStateData(ctx.store, StateData{
		Id:         e.Id(),
		UpdateInfo: e.update,
	}); err != nil {
		// too late to do anything now, update is installed and enabled, let's play
		// along and reboot
		log.Errorf("failed to store state data in reboot state: %v, "+
			"continuing with reboot", err)
	}

	c.ReportUpdateStatus(e.update, statusRebooting)

	log.Debugf("handle reboot state")

	if err := c.Reboot(); err != nil {
		return NewErrorState(NewFatalError(err)), false
	}

	// stop deployment logging
	DeploymentLogger.Disable()

	return doneState, false
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
