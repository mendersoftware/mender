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
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/mendersoftware/log"
	"github.com/mendersoftware/mender/client"
	"github.com/mendersoftware/mender/datastore"
	"github.com/mendersoftware/mender/installer"
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

// StateContext carrying over data that may be used by all state handlers
type StateContext struct {
	// data store access
	store                      store.Store
	lastUpdateCheckAttempt     time.Time
	lastInventoryUpdateAttempt time.Time
	lastAuthorizeAttempt       time.Time
	fetchInstallAttempts       int
}

type StateRunner interface {
	// Set runner's state to 's'
	SetNextState(s State)
	// Obtain runner's state
	GetCurrentState() State
	// Run the currently set state with this context
	TransitionState(next State, ctx *StateContext) (State, bool)
}

var (
	initState = &InitState{
		baseState{
			id: datastore.MenderStateInit,
			t:  ToNone,
		},
	}

	idleState = &IdleState{
		baseState{
			id: datastore.MenderStateIdle,
			t:  ToIdle,
		},
	}

	authorizeWaitState = NewAuthorizeWaitState()

	authorizeState = &AuthorizeState{
		baseState{
			id: datastore.MenderStateAuthorize,
			t:  ToSync,
		},
	}

	inventoryUpdateState = &InventoryUpdateState{
		baseState{
			id: datastore.MenderStateInventoryUpdate,
			t:  ToSync,
		},
	}

	checkWaitState = NewCheckWaitState()

	updateCheckState = &UpdateCheckState{
		baseState{
			id: datastore.MenderStateUpdateCheck,
			t:  ToSync,
		},
	}

	doneState = &FinalState{
		baseState{
			id: datastore.MenderStateDone,
			t:  ToNone,
		},
	}
)

type State interface {
	// Perform state action, returns next state and boolean flag indicating if
	// execution was cancelled or not
	Handle(ctx *StateContext, c Controller) (State, bool)
	HandleError(ctx *StateContext, c Controller, err menderError) (State, bool)
	// Cancel state action, returns true if action was cancelled
	Cancel() bool
	// Return numeric state ID
	Id() datastore.MenderState
	// Return transition
	Transition() Transition
	SetTransition(t Transition)
}

type WaitState interface {
	Cancel() bool
	Wake() bool
	Wait(next, same State, wait time.Duration) (State, bool)
}

type UpdateState interface {
	State
	Update() *datastore.UpdateInfo
}

// baseState is a helper state with some convenience methods
type baseState struct {
	id datastore.MenderState
	t  Transition
}

func (b *baseState) Id() datastore.MenderState {
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

func (b *baseState) String() string {
	return b.id.String()
}

func (b *baseState) HandleError(ctx *StateContext, c Controller, err menderError) (State, bool) {
	log.Error(err.Error())
	return NewErrorState(err), false
}

type waitState struct {
	baseState
	cancel chan bool
	wakeup chan bool
}

func NewWaitState(id datastore.MenderState, t Transition) *waitState {
	return &waitState{
		baseState: baseState{id: id, t: t},
		cancel:    make(chan bool),
		wakeup:    make(chan bool),
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
	case <-ws.wakeup:
		log.Info("forced wake-up from sleep")
		return next, false
	case <-ws.cancel:
		log.Infof("wait canceled")
	}
	return same, true
}

func (ws *waitState) Wake() bool {
	ws.wakeup <- true
	return true
}

func (ws *waitState) Cancel() bool {
	ws.cancel <- true
	return true
}

type updateState struct {
	baseState
	update datastore.UpdateInfo
}

func NewUpdateState(id datastore.MenderState, t Transition, u *datastore.UpdateInfo) *updateState {
	return &updateState{
		baseState: baseState{id: id, t: t},
		update:    *u,
	}
}

func (us *updateState) Update() *datastore.UpdateInfo {
	return &us.update
}

func (us *updateState) HandleError(ctx *StateContext, c Controller, err menderError) (State, bool) {
	log.Error(err.Error())

	// Default for most update states. Some states will override this.
	if us.Update().SupportsRollback == datastore.RollbackSupported {
		return NewUpdateRollbackState(us.Update()), false
	} else {
		setBrokenArtifactFlag(ctx.store, us.Update().ArtifactName())
		return NewUpdateErrorState(err, us.Update()), false
	}
}

type IdleState struct {
	baseState
}

func (i *IdleState) Handle(ctx *StateContext, c Controller) (State, bool) {
	// stop deployment logging
	DeploymentLogger.Disable()

	// cleanup state-data if any data is still present after an update
	RemoveStateData(ctx.store)

	// check if client is authorized
	if c.IsAuthorized() {
		return checkWaitState, false
	}
	return authorizeWaitState, false
}

type InitState struct {
	baseState
}

func (i *InitState) Handle(ctx *StateContext, c Controller) (State, bool) {
	// restore previous state information
	sd, sdErr := LoadStateData(ctx.store)

	// handle easy case first: no previous state stored,
	// means no update was in progress; we should continue from idle
	if sdErr != nil && os.IsNotExist(sdErr) {
		log.Debug("no state data stored")
		return idleState, false
	}

	if sdErr != nil && sdErr != maximumStateDataStoreCountExceeded {
		log.Errorf("failed to restore state data: %v", sdErr)
		me := NewFatalError(errors.Wrapf(sdErr, "failed to restore state data"))
		return NewUpdateErrorState(me, &datastore.UpdateInfo{
			ID: "unknown",
		}), false
	}

	log.Infof("handling loaded state: %s", sd.Name)

	if err := DeploymentLogger.Enable(sd.UpdateInfo.ID); err != nil {
		// just log error
		log.Errorf("failed to enable deployment logger: %s", err)
	}

	if sdErr == maximumStateDataStoreCountExceeded {
		// State argument not needed since we already know that maximum
		// count was exceeded and a different state will be returned.
		return handleStateDataError(ctx, nil, false, sd.Name, &sd.UpdateInfo, sdErr)
	}

	msg := fmt.Sprintf("Update was interrupted in state: %s", sd.Name)
	switch sd.Name {
	case datastore.MenderStateReboot:
	case datastore.MenderStateRollbackReboot:
		// Interruption is expected in these, don't produce error.
		log.Info(msg)
	default:
		log.Error(msg)
	}

	// Used in some cases below. Doesn't mean that there must be an error.
	me := NewFatalError(errors.New(msg))

	// We need to restore our payload handlers.
	err := c.RestoreInstallersFromTypeList(sd.UpdateInfo.Artifact.PayloadTypes)
	if err != nil {
		// Getting an error here is *really* bad. It means that we
		// cannot recover *anything*. Report big bad failure.
		return NewUpdateStatusReportState(&sd.UpdateInfo, client.StatusFailure), false
	}

	return i.getNextState(ctx, &sd, me)
}

func (i *InitState) getNextState(ctx *StateContext, sd *datastore.StateData,
	maybeErr menderError) (State, bool) {

	// check last known state
	switch sd.Name {

	// The update never got a chance to even start. Go straight to Idle
	// state, and it will be picked up again at the next polling interval.
	case datastore.MenderStateUpdateFetch:
		err := RemoveStateData(ctx.store)
		if err != nil {
			return NewErrorState(NewFatalError(err)), false
		}
		return idleState, false

	// Go straight to cleanup if we rebooted from Download state. This is
	// important so that artifact scripts from that state do not get to run,
	// since they have not yet been signature checked.
	case datastore.MenderStateUpdateStore,
		datastore.MenderStateUpdateAfterStore:

		return NewUpdateCleanupState(&sd.UpdateInfo, client.StatusFailure), false

	// After reboot into new update.
	case datastore.MenderStateReboot:
		return NewUpdateVerifyRebootState(&sd.UpdateInfo), false

	// VerifyRollbackReboot must be retried if interrupted, in order to
	// possibly go back and RollbackReboot again.
	case datastore.MenderStateRollbackReboot,
		datastore.MenderStateVerifyRollbackReboot,
		datastore.MenderStateAfterRollbackReboot:

		return NewUpdateVerifyRollbackRebootState(&sd.UpdateInfo), false

	// Rerun commits in subsequent payloads
	case datastore.MenderStateUpdateAfterFirstCommit:
		return NewUpdateAfterFirstCommitState(&sd.UpdateInfo), false

	// Rerun commit-leave
	case datastore.MenderStateUpdateAfterCommit:
		return NewUpdateAfterCommitState(&sd.UpdateInfo), false

	// Error state (ArtifactFailure) should be retried.
	case datastore.MenderStateUpdateError:
		return NewUpdateErrorState(maybeErr, &sd.UpdateInfo), false

	// Cleanup state should be retried.
	case datastore.MenderStateUpdateCleanup:
		return NewUpdateCleanupState(&sd.UpdateInfo, client.StatusFailure), false

	// All other states go to either error or rollback state, depending on
	// what's supported.
	default:
		if sd.UpdateInfo.SupportsRollback == datastore.RollbackSupported {
			return NewUpdateRollbackState(&sd.UpdateInfo), false
		} else {
			setBrokenArtifactFlag(ctx.store, sd.UpdateInfo.ArtifactName())
			return NewUpdateErrorState(maybeErr, &sd.UpdateInfo), false
		}
	}
}

type AuthorizeWaitState struct {
	baseState
	WaitState
}

func NewAuthorizeWaitState() State {
	return &AuthorizeWaitState{
		baseState: baseState{
			id: datastore.MenderStateAuthorizeWait,
			t:  ToIdle,
		},
		WaitState: NewWaitState(datastore.MenderStateAuthorizeWait, ToIdle),
	}
}

func (a *AuthorizeWaitState) Cancel() bool {
	return a.WaitState.Cancel()
}

func (a *AuthorizeWaitState) Handle(ctx *StateContext, c Controller) (State, bool) {
	log.Debugf("handle authorize wait state")

	attempt := ctx.lastAuthorizeAttempt.Add(c.GetRetryPollInterval())

	now := time.Now()
	var wait time.Duration
	if attempt.After(now) {
		wait = attempt.Sub(now)
	}

	log.Debugf("wait %v before next authorization attempt", wait)

	if wait == 0 {
		ctx.lastAuthorizeAttempt = now
		return authorizeState, false
	}

	ctx.lastAuthorizeAttempt = attempt
	return a.Wait(authorizeState, a, wait)
}

type AuthorizeState struct {
	baseState
}

func (a *AuthorizeState) Handle(ctx *StateContext, c Controller) (State, bool) {
	// stop deployment logging
	DeploymentLogger.Disable()

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

type UpdateCommitState struct {
	*updateState
	reportTries int
}

func NewUpdateCommitState(update *datastore.UpdateInfo) State {
	return &UpdateCommitState{
		updateState: NewUpdateState(datastore.MenderStateUpdateCommit,
			ToArtifactCommit_Enter, update),
	}
}

func (uc *UpdateCommitState) Handle(ctx *StateContext, c Controller) (State, bool) {
	var err error

	// start deployment logging
	if err = DeploymentLogger.Enable(uc.Update().ID); err != nil {
		log.Errorf("Can not enable deployment logger: %s", err)
	}

	log.Debugf("handle update commit state")

	// check if state scripts version is supported
	if err = c.CheckScriptsCompatibility(); err != nil {
		merr := NewTransientError(errors.Errorf("update commit failed: %s", err.Error()))
		return uc.HandleError(ctx, c, merr)
	}

	// A last status report to the server before committing. This is most
	// likely a repeat of the previous status, but the real motivation
	// behind it is to find out whether the server cancelled the deployment
	// while we were installing/rebooting, and whether network is working.
	merr := sendDeploymentStatus(uc.Update(), client.StatusInstalling, &uc.reportTries, c)
	if merr != nil {
		log.Errorf("Failed to send status report to server: %s", merr.Error())
		if merr.IsFatal() {
			return uc.HandleError(ctx, c, merr)
		} else {
			return NewUpdatePreCommitStatusReportRetryState(uc, uc.reportTries), false
		}
	}

	// Commit first payload only. After this commit it is no longer possible
	// to roll back, so the rest (if any) will be committed in the next
	// state.
	installers := c.GetInstallers()
	if len(installers) < 1 {
		return uc.HandleError(ctx, c, NewTransientError(
			errors.New("GetInstallers() returned empty list? Should not happen")))
	}
	err = installers[0].CommitUpdate()
	if err != nil {
		// we need to perform roll-back here; one scenario is when
		// u-boot fw utils won't work after update; at this point
		// without rolling-back it won't be possible to perform new
		// update
		merr := NewTransientError(errors.Errorf("update commit failed: %s", err.Error()))
		return uc.HandleError(ctx, c, merr)
	}

	// After this it is too late to roll back, so indidate that DB schema
	// migration is now permanent, if there was one.
	uc.Update().HasDBSchemaUpdate = false

	// And then store the data together with the new artifact name,
	// indicating that we have now migrated to a new artifact!
	err = StoreStateDataAndTransaction(ctx.store, datastore.StateData{
		Name:       uc.Id(),
		UpdateInfo: *uc.Update(),
	}, func(txn store.Transaction) error {
		log.Debugf("Committing new artifact name: %s", uc.Update().ArtifactName())
		return txn.WriteAll(datastore.ArtifactNameKey, []byte(uc.Update().ArtifactName()))
	})
	if err != nil {
		log.Error("Could not write state data to persistent storage: ", err.Error())
		return handleStateDataError(ctx, NewUpdateErrorState(NewTransientError(err), uc.Update()),
			false, datastore.MenderStateUpdateInstall, uc.Update(), err)
	}

	// Do rest of update commits now; then post commit-tasks
	return NewUpdateAfterFirstCommitState(uc.Update()), false
}

type UpdatePreCommitStatusReportRetryState struct {
	waitState
	returnToState State
	reportTries   int
}

func NewUpdatePreCommitStatusReportRetryState(returnToState State, reportTries int) State {
	return &UpdatePreCommitStatusReportRetryState{
		waitState: waitState{
			baseState: baseState{
				id: datastore.MenderStateUpdatePreCommitStatusReportRetry,
				t:  ToArtifactCommit_Enter,
			},
		},
		returnToState: returnToState,
		reportTries:   reportTries,
	}
}

func (usr *UpdatePreCommitStatusReportRetryState) Handle(ctx *StateContext, c Controller) (State, bool) {
	maxTrySending :=
		maxSendingAttempts(c.GetUpdatePollInterval(),
			c.GetRetryPollInterval(), minReportSendRetries)
	// we are always initializing with triesSending = 1
	maxTrySending++

	if usr.reportTries < maxTrySending {
		return usr.Wait(usr.returnToState, usr, c.GetRetryPollInterval())
	}
	return usr.returnToState.HandleError(ctx, c,
		NewTransientError(errors.New("Tried sending status report maximum number of times.")))
}

type UpdateAfterFirstCommitState struct {
	*updateState
}

func NewUpdateAfterFirstCommitState(update *datastore.UpdateInfo) State {
	return &UpdateAfterFirstCommitState{
		updateState: NewUpdateState(datastore.MenderStateUpdateAfterFirstCommit,
			ToNone, update),
	}
}

func (uc *UpdateAfterFirstCommitState) Handle(ctx *StateContext, c Controller) (State, bool) {
	// This state exists to run Commit for payloads after the first
	// one. After the first commit it is too late to roll back, which means
	// that this state has both different error handling and different
	// spontaneous reboot handling than the first commit state.

	var firstErr error

	for _, i := range c.GetInstallers()[1:] {
		err := i.CommitUpdate()
		if err != nil {
			log.Errorf("Error committing %s payload: %s", i.GetType(), err.Error())
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	if firstErr != nil {
		merr := NewTransientError(errors.Errorf("update commit failed: %s", firstErr.Error()))
		return uc.HandleError(ctx, c, merr)
	}

	// Move on to post-commit tasks.
	return NewUpdateAfterCommitState(uc.Update()), false
}

func (uc *UpdateAfterFirstCommitState) HandleError(ctx *StateContext, c Controller, merr menderError) (State, bool) {
	log.Error(merr.Error())

	// Too late to back out now. Just report the error, but do not try to roll back.
	setBrokenArtifactFlag(ctx.store, uc.Update().ArtifactName())
	return NewUpdateCleanupState(uc.Update(), client.StatusFailure), false
}

type UpdateAfterCommitState struct {
	*updateState
}

func NewUpdateAfterCommitState(update *datastore.UpdateInfo) State {
	return &UpdateAfterCommitState{
		updateState: NewUpdateState(datastore.MenderStateUpdateAfterCommit,
			ToArtifactCommit_Leave, update),
	}
}

func (uc *UpdateAfterCommitState) Handle(ctx *StateContext, c Controller) (State, bool) {
	// This state only exists to rerun Commit_Leave scripts in the event of
	// spontaneous shutdowns, so there is nothing else to do in this state.

	// update is commited; clean up
	return NewUpdateCleanupState(uc.Update(), client.StatusSuccess), false
}

func (uc *UpdateAfterCommitState) HandleError(ctx *StateContext, c Controller, merr menderError) (State, bool) {
	log.Error(merr.Error())

	// Too late to back out now. Just report the error, but do not try to roll back.
	setBrokenArtifactFlag(ctx.store, uc.Update().ArtifactName())
	return NewUpdateCleanupState(uc.Update(), client.StatusFailure), false
}

type UpdateCheckState struct {
	baseState
}

func (u *UpdateCheckState) Handle(ctx *StateContext, c Controller) (State, bool) {
	log.Debugf("handle update check state")

	update, err := c.CheckUpdate()

	if err != nil {
		if err.Cause() == os.ErrExist {
			// We are already running image which we are supposed to install.
			// Just report successful update and return to normal operations.
			return NewUpdateStatusReportState(update, client.StatusAlreadyInstalled), false
		}

		log.Errorf("update check failed: %s", err)
		return NewErrorState(err), false
	}

	if update != nil {
		return NewUpdateFetchState(update), false
	}
	return checkWaitState, false
}

type UpdateFetchState struct {
	baseState
	update datastore.UpdateInfo
}

func NewUpdateFetchState(update *datastore.UpdateInfo) State {
	return &UpdateFetchState{
		baseState: baseState{
			id: datastore.MenderStateUpdateFetch,
			t:  ToDownload_Enter,
		},
		update: *update,
	}
}

func (u *UpdateFetchState) Handle(ctx *StateContext, c Controller) (State, bool) {
	// start deployment logging
	if err := DeploymentLogger.Enable(u.update.ID); err != nil {
		return NewUpdateStatusReportState(&u.update, client.StatusFailure), false
	}

	log.Debugf("handle update fetch state")

	merr := c.ReportUpdateStatus(&u.update, client.StatusDownloading)
	if merr != nil && merr.IsFatal() {
		return NewUpdateStatusReportState(&u.update, client.StatusFailure), false
	}

	in, _, err := c.FetchUpdate(u.update.URI())
	if err != nil {
		log.Errorf("update fetch failed: %s", err)
		return NewFetchStoreRetryState(u, &u.update, err), false
	}

	return NewUpdateStoreState(in, &u.update), false
}

func (uf *UpdateFetchState) Update() *datastore.UpdateInfo {
	return &uf.update
}

type UpdateStoreState struct {
	*updateState
	// reader for obtaining image data
	imagein io.ReadCloser
}

func NewUpdateStoreState(in io.ReadCloser, update *datastore.UpdateInfo) State {
	return &UpdateStoreState{
		NewUpdateState(datastore.MenderStateUpdateStore,
			ToDownload_Enter, update),
		in,
	}
}

func (u *UpdateStoreState) Handle(ctx *StateContext, c Controller) (State, bool) {

	// make sure to close the stream with image data
	defer u.imagein.Close()

	// start deployment logging
	if err := DeploymentLogger.Enable(u.update.ID); err != nil {
		return NewUpdateStatusReportState(&u.update, client.StatusFailure), false
	}

	log.Debugf("handle update install state")

	merr := c.ReportUpdateStatus(&u.update, client.StatusDownloading)
	if merr != nil && merr.IsFatal() {
		return NewUpdateStatusReportState(&u.update, client.StatusFailure), false
	}

	installer, err := c.ReadArtifactHeaders(u.imagein)
	if err != nil {
		log.Errorf("Fetching Artifact headers failed: %s", err)
		return NewFetchStoreRetryState(u, &u.update, err), false
	}

	if installer.GetArtifactName() != u.Update().ArtifactName() {
		log.Errorf("Artifact name in artifact is not what the server claims (%s != %s).",
			installer.GetArtifactName(), u.Update().ArtifactName())
		return NewUpdateStatusReportState(u.Update(), client.StatusFailure), false
	}

	installers := c.GetInstallers()
	u.update.Artifact.PayloadTypes = make([]string, len(installers))
	for n, i := range installers {
		u.update.Artifact.PayloadTypes[n] = i.GetType()
	}

	// Store state so that all the payload handlers are recorded there. This
	// is important since they need to call their Cleanup functions after we
	// have started the download.
	err = StoreStateData(ctx.store, datastore.StateData{
		Name:       u.Id(),
		UpdateInfo: u.update,
	})
	if err != nil {
		log.Error("Could not write state data to persistent storage: ", err.Error())
		return handleStateDataError(ctx, NewUpdateCleanupState(&u.update, client.StatusFailure),
			false, u.Id(), &u.update, err)
	}

	err = installer.StorePayloads()
	if err != nil {
		log.Errorf("Artifact install failed: %s", err)
		return NewUpdateCleanupState(&u.update, client.StatusFailure), false
	}

	ok, state, cancelled := u.handleSupportsRollback(ctx, c)
	if !ok {
		return state, cancelled
	}

	// restart counter so that we are able to retry next time
	ctx.fetchInstallAttempts = 0

	// check if update is not aborted
	// this step is needed as installing might take a while and we might end up with
	// proceeding with already cancelled update
	merr = c.ReportUpdateStatus(&u.update, client.StatusDownloading)
	if merr != nil && merr.IsFatal() {
		return NewUpdateErrorState(merr, &u.update), false
	}

	return NewUpdateAfterStoreState(&u.update), false
}

func (u *UpdateStoreState) handleSupportsRollback(ctx *StateContext, c Controller) (bool, State, bool) {
	for _, i := range c.GetInstallers() {
		supportsRollback, err := i.SupportsRollback()
		if err != nil {
			log.Errorf("Could not determine if module supports rollback: %s", err.Error())
			state, cancelled := NewUpdateErrorState(NewTransientError(err), &u.update), false
			return false, state, cancelled
		}
		if supportsRollback {
			err = u.update.SupportsRollback.Set(datastore.RollbackSupported)
		} else {
			err = u.update.SupportsRollback.Set(datastore.RollbackNotSupported)
		}
		if err != nil {
			log.Errorf("Could update module rollback support status: %s", err.Error())
			state, cancelled := NewUpdateErrorState(NewTransientError(err), &u.update), false
			return false, state, cancelled
		}
	}

	// Make sure SupportsRollback status is stored
	err := StoreStateData(ctx.store, datastore.StateData{
		Name:       u.Id(),
		UpdateInfo: u.update,
	})
	if err != nil {
		log.Error("Could not write state data to persistent storage: ", err.Error())
		state, cancelled := handleStateDataError(ctx, NewUpdateErrorState(NewTransientError(err), &u.update),
			false, u.Id(), &u.update, err)
		return false, state, cancelled
	}

	return true, nil, false
}

func (is *UpdateStoreState) HandleError(ctx *StateContext, c Controller, merr menderError) (State, bool) {
	log.Error(merr.Error())
	return NewUpdateCleanupState(is.Update(), client.StatusFailure), false
}

type UpdateAfterStoreState struct {
	*updateState
}

func NewUpdateAfterStoreState(update *datastore.UpdateInfo) State {
	return &UpdateAfterStoreState{
		updateState: NewUpdateState(datastore.MenderStateUpdateAfterStore,
			ToDownload_Leave, update),
	}
}

func (s *UpdateAfterStoreState) Handle(ctx *StateContext, c Controller) (State, bool) {
	// This state only exists to run Download_Leave.
	return NewUpdateInstallState(s.Update()), false
}

func (s *UpdateAfterStoreState) HandleError(ctx *StateContext, c Controller, merr menderError) (State, bool) {
	log.Error(merr.Error())
	return NewUpdateCleanupState(s.Update(), client.StatusFailure), false
}

type UpdateInstallState struct {
	*updateState
}

func NewUpdateInstallState(update *datastore.UpdateInfo) State {
	return &UpdateInstallState{
		updateState: NewUpdateState(datastore.MenderStateUpdateInstall,
			ToArtifactInstall, update),
	}
}

func (is *UpdateInstallState) Handle(ctx *StateContext, c Controller) (State, bool) {
	// start deployment logging
	if err := DeploymentLogger.Enable(is.Update().ID); err != nil {
		return NewUpdateErrorState(NewTransientError(err), is.Update()), false
	}

	merr := c.ReportUpdateStatus(is.Update(), client.StatusInstalling)
	if merr != nil && merr.IsFatal() {
		return is.HandleError(ctx, c, merr)
	}

	// If download was successful, install update, which for dual rootfs
	// means marking inactive partition as the active one.
	for _, i := range c.GetInstallers() {
		if err := i.InstallUpdate(); err != nil {
			return is.HandleError(ctx, c, NewTransientError(err))
		}
	}

	ok, state, cancelled := is.handleRebootType(ctx, c)
	if !ok {
		return state, cancelled
	}

	for n := range c.GetInstallers() {
		rebootRequested, err := is.Update().RebootRequested.Get(n)
		if err != nil {
			return is.HandleError(ctx, c, NewTransientError(errors.Wrap(
				err, "Unable to get requested reboot type")))
		}
		switch rebootRequested {

		case datastore.RebootTypeNone:
			// Do nothing.

		case datastore.RebootTypeCustom:
			// Go to reboot state if at least one payload requested it.
			return NewUpdateRebootState(is.Update()), false

		case datastore.RebootTypeAutomatic:
			return is.HandleError(ctx, c, NewTransientError(errors.New(
				"Update module automatic reboots are not supported by this client")))

		default:
			return is.HandleError(ctx, c, NewTransientError(errors.New(
				"Unknown reboot type stored in database. Not continuing")))
		}
	}

	// No reboot requests, go to commit state.
	return NewUpdateCommitState(is.Update()), false
}

func (is *UpdateInstallState) handleRebootType(ctx *StateContext, c Controller) (bool, State, bool) {
	for n, i := range c.GetInstallers() {
		needsReboot, err := i.NeedsReboot()
		if err != nil {
			state, cancelled := is.HandleError(ctx, c, NewTransientError(err))
			return false, state, cancelled
		}
		switch needsReboot {
		case installer.NoReboot:
			err = is.Update().RebootRequested.Set(n, datastore.RebootTypeNone)
		case installer.RebootRequired:
			err = is.Update().RebootRequested.Set(n, datastore.RebootTypeCustom)
		case installer.AutomaticReboot:
			err = is.Update().RebootRequested.Set(n, datastore.RebootTypeAutomatic)
		default:
			state, cancelled := is.HandleError(ctx, c, NewFatalError(errors.New(
				"Unknown reply from NeedsReboot. Should not happen")))
			return false, state, cancelled
		}
		if err != nil {
			state, cancelled := is.HandleError(ctx, c, NewTransientError(errors.Wrap(
				err, "Unable to store requested reboot type")))
			return false, state, cancelled
		}
	}
	// Make sure RebootRequested status is stored
	err := StoreStateData(ctx.store, datastore.StateData{
		Name:       datastore.MenderStateUpdateInstall,
		UpdateInfo: *is.Update(),
	})
	if err != nil {
		log.Error("Could not write state data to persistent storage: ", err.Error())
		state, cancelled := is.HandleError(ctx, c, NewTransientError(err))
		state, cancelled = handleStateDataError(ctx, state, cancelled,
			datastore.MenderStateUpdateInstall, is.Update(), err)
		return false, state, cancelled
	}

	return true, nil, false
}

type FetchStoreRetryState struct {
	baseState
	WaitState
	from   State
	update datastore.UpdateInfo
	err    error
}

func NewFetchStoreRetryState(from State, update *datastore.UpdateInfo,
	err error) State {
	return &FetchStoreRetryState{
		baseState: baseState{
			id: datastore.MenderStateFetchStoreRetryWait,
			t:  ToDownload_Enter,
		},
		WaitState: NewWaitState(datastore.MenderStateFetchStoreRetryWait, ToDownload_Enter),
		from:      from,
		update:    *update,
		err:       err,
	}
}

func (fir *FetchStoreRetryState) Cancel() bool {
	return fir.WaitState.Cancel()
}
func (fir *FetchStoreRetryState) Handle(ctx *StateContext, c Controller) (State, bool) {
	log.Debugf("handle fetch install retry state")

	intvl, err := client.GetExponentialBackoffTime(ctx.fetchInstallAttempts, c.GetUpdatePollInterval())
	if err != nil {
		if fir.err != nil {
			return NewUpdateErrorState(
				NewTransientError(errors.Wrap(fir.err, err.Error())),
				&fir.update), false
		}
		return NewUpdateErrorState(
			NewTransientError(err), &fir.update), false
	}

	ctx.fetchInstallAttempts++

	log.Debugf("wait %v before next fetch/install attempt", intvl)
	return fir.Wait(NewUpdateFetchState(&fir.update), fir, intvl)
}

type CheckWaitState struct {
	baseState
	WaitState
}

func NewCheckWaitState() State {
	return &CheckWaitState{
		baseState: baseState{
			id: datastore.MenderStateCheckWait,
			t:  ToIdle,
		},
		WaitState: NewWaitState(datastore.MenderStateCheckWait, ToIdle),
	}
}

func (cw *CheckWaitState) Cancel() bool {
	return cw.WaitState.Cancel()
}

func (cw *CheckWaitState) Handle(ctx *StateContext, c Controller) (State, bool) {

	log.Debugf("handle check wait state")

	// calculate next interval
	update := ctx.lastUpdateCheckAttempt.Add(c.GetUpdatePollInterval())
	inventory := ctx.lastInventoryUpdateAttempt.Add(c.GetInventoryPollInterval())

	// if we haven't sent inventory so far
	if ctx.lastInventoryUpdateAttempt.IsZero() {
		inventory = ctx.lastInventoryUpdateAttempt
	}

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
	var wait time.Duration
	if next.when.After(now) {
		wait = next.when.Sub(now)
	}

	// (MEN-2195): Set the last udpate/inventory check time to now, as an error in an enter script will
	// hinder these states from ever running, and thus causing an infinite loop if the script
	// keeps returning the same error.
	switch (next.state).(type) {
	case *InventoryUpdateState:
		if wait == 0 {
			ctx.lastInventoryUpdateAttempt = now
		} else {
			ctx.lastInventoryUpdateAttempt = next.when
		}
	case *UpdateCheckState:
		if wait == 0 {
			ctx.lastUpdateCheckAttempt = now
		} else {
			ctx.lastUpdateCheckAttempt = next.when
		}
	}

	if wait != 0 {
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

	err := c.InventoryRefresh()
	if err != nil {
		log.Warnf("failed to refresh inventory: %v", err)
		if errors.Cause(err) == errNoArtifactName {
			return NewErrorState(NewTransientError(err)), false
		}
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
			id: datastore.MenderStateError,
			t:  ToError,
		},
		err,
	}
}

func (e *ErrorState) Handle(ctx *StateContext, c Controller) (State, bool) {
	// stop deployment logging
	DeploymentLogger.Disable()

	log.Infof("handling error state, current error: %v", e.cause.Error())
	// decide if error is transient, exit for now
	if e.cause.IsFatal() {
		return doneState, false
	}
	return idleState, false
}

func (e *ErrorState) IsFatal() bool {
	return e.cause.IsFatal()
}

type UpdateErrorState struct {
	ErrorState
	update datastore.UpdateInfo
}

func NewUpdateErrorState(err menderError, update *datastore.UpdateInfo) State {
	return &UpdateErrorState{
		ErrorState{
			baseState{id: datastore.MenderStateUpdateError, t: ToArtifactFailure},
			err,
		},
		*update,
	}
}

func (ue *UpdateErrorState) Handle(ctx *StateContext, c Controller) (State, bool) {

	log.Debug("handle update error state")

	for _, i := range c.GetInstallers() {
		err := i.Failure()
		if err != nil {
			log.Errorf("ArtifactFailure failed: %s", err.Error())
		}
	}

	return NewUpdateCleanupState(&ue.update, client.StatusFailure), false
}

func (ue *UpdateErrorState) Update() *datastore.UpdateInfo {
	return &ue.update
}

type UpdateCleanupState struct {
	*updateState
	status string
}

func NewUpdateCleanupState(update *datastore.UpdateInfo, status string) State {
	var transition Transition
	if status == client.StatusSuccess {
		transition = ToNone
	} else {
		transition = ToError
	}
	return &UpdateCleanupState{
		updateState: NewUpdateState(datastore.MenderStateUpdateCleanup,
			transition, update),
		status: status,
	}
}

func (s *UpdateCleanupState) Handle(ctx *StateContext, c Controller) (State, bool) {
	if err := DeploymentLogger.Enable(s.Update().ID); err != nil {
		log.Errorf("Can not enable deployment logger: %s", err)
	}

	log.Debug("Handling Cleanup state")

	var lastError error
	for _, i := range c.GetInstallers() {
		err := i.Cleanup()
		if err != nil {
			log.Errorf("Cleanup failed: %s", err.Error())
			lastError = err
			// Nothing we can do about it though. Just continue.
		}
	}

	if lastError != nil {
		s.status = client.StatusFailure
	}

	// Cleanup is done, report outcome.
	return NewUpdateStatusReportState(s.Update(), s.status), false
}

// Wrapper for mandatory update state reporting. The state handler will attempt
// to report state for a number of times. In case of recurring failure, the
// update is deemed as failed.
type UpdateStatusReportState struct {
	*updateState
	status             string
	triesSendingReport int
	triesSendingLogs   int
	logs               []byte
}

func NewUpdateStatusReportState(update *datastore.UpdateInfo, status string) State {
	return &UpdateStatusReportState{
		updateState: NewUpdateState(datastore.MenderStateUpdateStatusReport,
			ToNone, update),
		status: status,
	}
}

func sendDeploymentLogs(update *datastore.UpdateInfo, sentTries *int,
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

func sendDeploymentStatus(update *datastore.UpdateInfo, status string,
	tries *int, c Controller) menderError {
	// check if the report was already sent
	*tries++
	if err := c.ReportUpdateStatus(update, status); err != nil {
		return err
	}
	return nil
}

func (usr *UpdateStatusReportState) Handle(ctx *StateContext, c Controller) (State, bool) {

	// start deployment logging; no error checking
	// we can do nothing here; either we will have the logs or not...
	DeploymentLogger.Enable(usr.Update().ID)

	log.Debug("handle update status report state")

	if err := sendDeploymentStatus(usr.Update(), usr.status,
		&usr.triesSendingReport, c); err != nil {

		log.Errorf("failed to send status to server: %v", err)
		if err.IsFatal() {
			// there is no point in retrying
			return NewReportErrorState(usr.Update(), usr.status), false
		}
		return NewUpdateStatusReportRetryState(usr, usr.Update(), usr.status,
			usr.triesSendingReport), false
	}

	if usr.status == client.StatusFailure {
		log.Debugf("attempting to upload deployment logs for failed update")
		if err := sendDeploymentLogs(usr.Update(),
			&usr.triesSendingLogs, usr.logs, c); err != nil {

			log.Errorf("failed to send deployment logs to server: %v", err)
			if err.IsFatal() {
				// there is no point in retrying
				return NewReportErrorState(usr.Update(), usr.status), false
			}
			return NewUpdateStatusReportRetryState(usr, usr.Update(), usr.status,
				usr.triesSendingLogs), false
		}
	}

	log.Debug("reporting complete")
	// stop deployment logging as the update is completed at this point
	DeploymentLogger.Disable()

	return idleState, false
}

type UpdateStatusReportRetryState struct {
	baseState
	WaitState
	reportState  State
	update       datastore.UpdateInfo
	status       string
	triesSending int
}

func NewUpdateStatusReportRetryState(reportState State,
	update *datastore.UpdateInfo, status string, tries int) State {
	return &UpdateStatusReportRetryState{
		baseState: baseState{
			id: datastore.MenderStatusReportRetryState,
			t:  ToNone,
		},
		WaitState:    NewWaitState(datastore.MenderStatusReportRetryState, ToNone),
		reportState:  reportState,
		update:       *update,
		status:       status,
		triesSending: tries,
	}
}

func (usr *UpdateStatusReportRetryState) Cancel() bool {
	return usr.WaitState.Cancel()
}

// try to send failed report at least minRetries times or keep trying every
// 'retryPollInterval' for the duration of two 'updatePollInterval'
func maxSendingAttempts(upi, rpi time.Duration, minRetries int) int {
	if rpi == 0 {
		return minRetries
	}
	max := int(upi / rpi)
	if max <= minRetries {
		return minRetries
	}
	return max * 2
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
	return NewReportErrorState(&usr.update, usr.status), false
}

func (usr *UpdateStatusReportRetryState) Update() *datastore.UpdateInfo {
	return &usr.update
}

type ReportErrorState struct {
	*updateState
	updateStatus string
}

func NewReportErrorState(update *datastore.UpdateInfo, status string) State {
	return &ReportErrorState{
		updateState: NewUpdateState(datastore.MenderStateReportStatusError,
			ToArtifactFailure, update),
		updateStatus: status,
	}
}

func (res *ReportErrorState) Handle(ctx *StateContext, c Controller) (State, bool) {
	// start deployment logging; no error checking
	// we can do nothing here; either we will have the logs or not...
	DeploymentLogger.Enable(res.Update().ID)

	log.Errorf("handling report error state with status: %v", res.updateStatus)

	switch res.updateStatus {
	case client.StatusSuccess:
		// error while reporting success; rollback
		return NewUpdateRollbackState(res.Update()), false
	case client.StatusFailure:
		// error while reporting failure;
		// start from scratch as previous update was broken
		log.Errorf("error while performing update: %v (%v)", res.updateStatus, *res.Update())
		return idleState, false
	case client.StatusAlreadyInstalled:
		// we've failed to report already-installed status, not a big
		// deal, start from scratch
		return idleState, false
	default:
		// should not end up here
		return doneState, false
	}
}

func (res *ReportErrorState) HandleError(ctx *StateContext, c Controller, merr menderError) (State, bool) {
	log.Errorf("Reached final error state: %s", merr.Error())
	return idleState, false
}

type UpdateRebootState struct {
	*updateState
}

func NewUpdateRebootState(update *datastore.UpdateInfo) State {
	return &UpdateRebootState{
		updateState: NewUpdateState(datastore.MenderStateReboot,
			ToArtifactReboot_Enter, update),
	}
}

func (e *UpdateRebootState) Handle(ctx *StateContext, c Controller) (State, bool) {

	// start deployment logging
	if err := DeploymentLogger.Enable(e.Update().ID); err != nil {
		// just log error; we need to reboot anyway
		log.Errorf("failed to enable deployment logger: %s", err)
	}

	log.Debug("handling reboot state")

	merr := c.ReportUpdateStatus(e.Update(), client.StatusRebooting)
	if merr != nil && merr.IsFatal() {
		return NewUpdateRollbackState(e.Update()), false
	}

	log.Info("rebooting device")

	for n, i := range c.GetInstallers() {
		rebootRequested, err := e.Update().RebootRequested.Get(n)
		if err != nil {
			return e.HandleError(ctx, c, NewTransientError(errors.Wrap(
				err, "Unable to get requested reboot type")))
		}
		if rebootRequested == datastore.RebootTypeCustom {
			if err := i.Reboot(); err != nil {
				log.Errorf("error rebooting device: %v", err)
				return NewUpdateRollbackState(e.Update()), false
			}
		}
	}

	// We may never get here, if the machine we're on rebooted. However, if
	// we rebooted a peripheral device, we will get here.
	return NewUpdateVerifyRebootState(e.Update()), false
}

type UpdateVerifyRebootState struct {
	*updateState
}

func NewUpdateVerifyRebootState(update *datastore.UpdateInfo) State {
	return &UpdateVerifyRebootState{
		updateState: NewUpdateState(datastore.MenderStateAfterReboot,
			ToArtifactReboot_Leave, update),
	}
}

func (rs *UpdateVerifyRebootState) Handle(ctx *StateContext, c Controller) (State, bool) {
	for _, i := range c.GetInstallers() {
		err := i.VerifyReboot()
		if err != nil {
			return rs.HandleError(ctx, c, NewTransientError(err))
		}
	}

	return NewUpdateAfterRebootState(rs.Update()), false
}

type UpdateAfterRebootState struct {
	*updateState
}

func NewUpdateAfterRebootState(update *datastore.UpdateInfo) State {
	return &UpdateAfterRebootState{
		updateState: NewUpdateState(datastore.MenderStateAfterReboot,
			ToArtifactReboot_Leave, update),
	}
}

func (rs *UpdateAfterRebootState) Handle(ctx *StateContext,
	c Controller) (State, bool) {
	// start deployment logging; no error checking
	// we can do nothing here; either we will have the logs or not...
	DeploymentLogger.Enable(rs.Update().ID)

	// this state is needed to satisfy ToReboot transition Leave() action
	log.Debug("handling state after reboot")

	return NewUpdateCommitState(rs.Update()), false
}

type UpdateRollbackState struct {
	*updateState
}

func NewUpdateRollbackState(update *datastore.UpdateInfo) State {
	return &UpdateRollbackState{
		updateState: NewUpdateState(datastore.MenderStateRollback, ToArtifactRollback, update),
	}
}

func (rs *UpdateRollbackState) Handle(ctx *StateContext, c Controller) (State, bool) {
	// start deployment logging
	if err := DeploymentLogger.Enable(rs.Update().ID); err != nil {
		// just log error; we need to reboot anyway
		log.Errorf("failed to enable deployment logger: %s", err)
	}

	log.Info("performing rollback")

	// Roll back to original partition and perform reboot
	for _, i := range c.GetInstallers() {
		if err := i.Rollback(); err != nil {
			log.Errorf("rollback failed: %s", err)
			return rs.HandleError(ctx, c, NewFatalError(err))
		}
	}
	for n := range c.GetInstallers() {
		rebootRequested, err := rs.Update().RebootRequested.Get(n)
		if err != nil {
			// We treat error as RebootTypeNone, since it's possible
			// the modules have not been queried yet.
			rebootRequested = datastore.RebootTypeNone
		}
		switch rebootRequested {

		case datastore.RebootTypeNone:
			// Do nothing.

		case datastore.RebootTypeCustom:
			// Enter rollback reboot state if at least one payload
			// asked for it.
			log.Debug("will try to rollback reboot the device")
			return NewUpdateRollbackRebootState(rs.Update()), false

		case datastore.RebootTypeAutomatic:
			return rs.HandleError(ctx, c, NewTransientError(errors.New(
				"Update module automatic reboots are not supported by this client")))

		default:
			return rs.HandleError(ctx, c, NewTransientError(errors.New(
				"Unknown reboot type stored in database. Not continuing")))
		}
	}

	// if no reboot is needed, just return the error and start over
	return NewUpdateErrorState(NewTransientError(errors.New("update error")),
		rs.Update()), false
}

func (rs *UpdateRollbackState) HandleError(ctx *StateContext, c Controller, merr menderError) (State, bool) {
	log.Error(merr.Error())
	setBrokenArtifactFlag(ctx.store, rs.Update().ArtifactName())
	return NewUpdateErrorState(merr, rs.Update()), false
}

type UpdateRollbackRebootState struct {
	*updateState
}

func NewUpdateRollbackRebootState(update *datastore.UpdateInfo) State {
	return &UpdateRollbackRebootState{
		updateState: NewUpdateState(datastore.MenderStateRollbackReboot,
			ToArtifactRollbackReboot_Enter, update),
	}
}

func (rs *UpdateRollbackRebootState) Handle(ctx *StateContext, c Controller) (State, bool) {
	// start deployment logging
	if err := DeploymentLogger.Enable(rs.Update().ID); err != nil {
		// just log error; we need to reboot anyway
		log.Errorf("failed to enable deployment logger: %s", err)
	}

	log.Info("rebooting device after rollback")

	for n, i := range c.GetInstallers() {
		rebootRequested, err := rs.Update().RebootRequested.Get(n)
		if err != nil {
			return rs.HandleError(ctx, c, NewTransientError(errors.Wrap(
				err, "Unable to get requested reboot type")))
		}
		if rebootRequested == datastore.RebootTypeCustom {
			if err := i.RollbackReboot(); err != nil {
				log.Errorf("error rebooting device: %v", err)
				// Outcome is irrelevant, we will go to the
				// VerifyRollbackReboot state regardless.
			}
		}
	}

	// We may never get here, if the machine we're on rebooted. However, if
	// we rebooted a peripheral device, we will get here.
	return NewUpdateVerifyRollbackRebootState(rs.Update()), false
}

func (rs *UpdateRollbackRebootState) HandleError(ctx *StateContext,
	c Controller, merr menderError) (State, bool) {

	// We don't really handle errors here, but instead in the verify state.
	return NewUpdateVerifyRollbackRebootState(rs.Update()), false
}

type UpdateVerifyRollbackRebootState struct {
	*updateState
}

func NewUpdateVerifyRollbackRebootState(update *datastore.UpdateInfo) State {
	return &UpdateVerifyRollbackRebootState{
		updateState: NewUpdateState(datastore.MenderStateVerifyRollbackReboot,
			ToArtifactRollbackReboot_Leave, update),
	}
}

func (rs *UpdateVerifyRollbackRebootState) Handle(ctx *StateContext, c Controller) (State, bool) {
	for _, i := range c.GetInstallers() {
		err := i.VerifyRollbackReboot()
		if err != nil {
			return rs.HandleError(ctx, c, NewTransientError(err))
		}
	}

	return NewUpdateAfterRollbackRebootState(rs.Update()), false
}

func (rs *UpdateVerifyRollbackRebootState) HandleError(ctx *StateContext,
	c Controller, merr menderError) (State, bool) {

	log.Errorf("Rollback reboot failed, will retry. Cause: %s", merr.Error())

	return NewUpdateRollbackRebootState(rs.Update()), false
}

type UpdateAfterRollbackRebootState struct {
	*updateState
}

func NewUpdateAfterRollbackRebootState(update *datastore.UpdateInfo) State {
	return &UpdateAfterRollbackRebootState{
		updateState: NewUpdateState(datastore.MenderStateAfterRollbackReboot,
			ToArtifactRollbackReboot_Leave, update),
	}
}

func (rs *UpdateAfterRollbackRebootState) Handle(ctx *StateContext,
	c Controller) (State, bool) {
	// start deployment logging
	if err := DeploymentLogger.Enable(rs.Update().ID); err != nil {
		// just log error; we need to reboot anyway
		log.Errorf("failed to enable deployment logger: %s", err)
	}

	// this state is needed to satisfy ToRollbackReboot
	// transition Leave() action
	log.Debug("handling state after rollback reboot")

	return NewUpdateErrorState(NewTransientError(errors.New("update error")),
		rs.Update()), false
}

type FinalState struct {
	baseState
}

func (f *FinalState) Handle(ctx *StateContext, c Controller) (State, bool) {
	panic("reached final state")
}

func StoreStateData(dbStore store.Store, sd datastore.StateData) error {
	return StoreStateDataAndTransaction(dbStore, sd, nil)
}

// Execute storing the state and a custom transaction function atomically.
func StoreStateDataAndTransaction(dbStore store.Store, sd datastore.StateData,
	txnFunc func(txn store.Transaction) error) error {

	// if the verions is not filled in, use the current one
	if sd.Version == 0 {
		sd.Version = datastore.StateDataVersion
	}

	var storeCountExceeded bool

	err := dbStore.WriteTransaction(func(txn store.Transaction) error {
		// Perform custom transaction operations, if any.
		if txnFunc != nil {
			err := txnFunc(txn)
			if err != nil {
				return err
			}
		}

		var key string
		if sd.UpdateInfo.HasDBSchemaUpdate {
			key = datastore.StateDataKeyUncommitted
		} else {
			key = datastore.StateDataKey
		}

		// See if there is an existing entry and update the store count.
		existingData, err := txn.ReadAll(key)
		if err == nil {
			var existing datastore.StateData
			err := json.Unmarshal(existingData, &existing)
			if err == nil {
				sd.UpdateInfo.StateDataStoreCount = existing.UpdateInfo.StateDataStoreCount
			}
		}

		if sd.UpdateInfo.StateDataStoreCount >= maximumStateDataStoreCount {
			// Reset store count to prevent subsequent states from
			// hitting the same error.
			sd.UpdateInfo.StateDataStoreCount = 0
			storeCountExceeded = true
		}

		sd.UpdateInfo.StateDataStoreCount++
		data, err := json.Marshal(sd)
		if err != nil {
			return err
		}

		if !sd.UpdateInfo.HasDBSchemaUpdate {
			err = txn.Remove(datastore.StateDataKeyUncommitted)
			if err != nil {
				return err
			}
		}
		return txn.WriteAll(key, data)
	})

	if storeCountExceeded {
		return maximumStateDataStoreCountExceeded
	}

	return err
}

// This number should be kept quite a lot higher than the number of expected
// state storage operations, which is usually roughly equivalent to the number
// of state transitions.
const maximumStateDataStoreCount int = 30

// Special kind of error: When this error is returned by LoadStateData, the
// StateData will also be valid, and can be used to handle the error.
var maximumStateDataStoreCountExceeded error = errors.New("State data stored and retrieved maximum number of times")

func loadStateData(txn store.Transaction, key string) (datastore.StateData, error) {
	var sd datastore.StateData

	data, err := txn.ReadAll(key)
	if err != nil {
		return sd, err
	}

	// we are relying on the fact that Unmarshal will decode all and only the fields
	// that it can find in the destination type.
	err = json.Unmarshal(data, &sd)
	if err != nil {
		return sd, err
	}

	return sd, nil
}

func LoadStateData(dbStore store.Store) (datastore.StateData, error) {
	var sd datastore.StateData
	var storeCountExceeded bool

	// We do the state data loading in a write transaction so that we can
	// update the StateDataStoreCount.
	err := dbStore.WriteTransaction(func(txn store.Transaction) error {
		var err error
		sd, err = loadStateData(txn, datastore.StateDataKey)
		if err != nil {
			return err
		}

		switch sd.Version {
		case 0, 1:
			// We need to upgrade the schema. Check if we have
			// already written an updated one.
			uncommSd, err := loadStateData(txn, datastore.StateDataKeyUncommitted)
			if err == nil {
				// Verify that the update IDs are equal,
				// otherwise this may be a leftover remnant from
				// an earlier update.
				if sd.UpdateInfo.ID == uncommSd.UpdateInfo.ID {
					// Use the uncommitted one instead.
					sd = uncommSd
				}
			} else if err != os.ErrNotExist {
				return err
			}

			// If we are upgrading the schema, we know for a fact
			// that we came from a rootfs-image update, because it
			// was the only thing that was supported there. Store
			// this, since this information will be missing in
			// databases before version 2.
			sd.UpdateInfo.Artifact.PayloadTypes = []string{"rootfs-image"}
			sd.UpdateInfo.RebootRequested = []datastore.RebootType{datastore.RebootTypeCustom}
			sd.UpdateInfo.SupportsRollback = datastore.RollbackSupported

			sd.UpdateInfo.HasDBSchemaUpdate = true

		case 2:
			sd.UpdateInfo.HasDBSchemaUpdate = false

		default:
			return errors.New("unsupported state data version")
		}

		sd.Version = datastore.StateDataVersion

		if sd.UpdateInfo.StateDataStoreCount >= maximumStateDataStoreCount {
			// Reset store count to prevent subsequent states from
			// hitting the same error.
			sd.UpdateInfo.StateDataStoreCount = 0
			storeCountExceeded = true
		}

		sd.UpdateInfo.StateDataStoreCount++
		data, err := json.Marshal(sd)
		if err != nil {
			return err
		}

		// Store the updated count back in the database.
		if sd.UpdateInfo.HasDBSchemaUpdate {
			return txn.WriteAll(datastore.StateDataKeyUncommitted, data)
		} else {
			return txn.WriteAll(datastore.StateDataKey, data)
		}

	})

	if storeCountExceeded {
		return sd, maximumStateDataStoreCountExceeded
	}

	return sd, err
}

func RemoveStateData(store store.Store) error {
	if store == nil {
		return nil
	}
	return store.Remove(datastore.StateDataKey)
}

// Returns newState, unless store count is exceeded.
func handleStateDataError(ctx *StateContext, newState State, cancelled bool,
	stateName datastore.MenderState,
	update *datastore.UpdateInfo, err error) (State, bool) {

	if err == maximumStateDataStoreCountExceeded {
		log.Errorf("State transition loop detected in state %s: Forcefully aborting "+
			"update. The system is likely to be in an inconsistent "+
			"state after this.", stateName)
		setBrokenArtifactFlag(ctx.store, update.ArtifactName())
		return NewUpdateStatusReportState(update, client.StatusFailure), false
	}

	return newState, cancelled
}

func setBrokenArtifactFlag(store store.Store, artName string) {
	newName := artName + brokenArtifactSuffix
	log.Debugf("Setting artifact name to %s", newName)
	err := store.WriteAll(datastore.ArtifactNameKey, []byte(newName))
	if err != nil {
		log.Errorf("Could not write artifact name \"%s\": %s", artName, err.Error())
		// No error return, because everyone who calls this function is
		// already in an error path.
	}
}
