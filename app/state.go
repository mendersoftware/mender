// Copyright 2020 Northern.tech AS
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
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/mendersoftware/mender/client"
	"github.com/mendersoftware/mender/conf"
	"github.com/mendersoftware/mender/datastore"
	"github.com/mendersoftware/mender/installer"
	"github.com/mendersoftware/mender/store"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

const (
	errMsgDependencyNotSatisfiedF = "Artifact dependency %q not satisfied " +
		"by currently installed artifact (%v != %v)."
)

// StateContext carrying over data that may be used by all state handlers
type StateContext struct {
	// data store access
	Rebooter                   installer.Rebooter
	Store                      store.Store
	WakeupChan                 chan bool
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

type StateCollection struct {
	Authorize       *authorizeState
	AuthorizeWait   *authorizeWaitState
	CheckWait       *checkWaitState
	Final           *finalState
	Idle            *idleState
	Init            *initState
	InventoryUpdate *inventoryUpdateState
	UpdateCheck     *updateCheckState
}

// Exposed state variables.
var States = StateCollection{
	Authorize: &authorizeState{
		baseState{
			id: datastore.MenderStateAuthorize,
			t:  ToSync,
		},
	},
	AuthorizeWait: NewAuthorizeWaitState().(*authorizeWaitState),
	CheckWait:     NewCheckWaitState().(*checkWaitState),
	Final: &finalState{
		baseState{
			id: datastore.MenderStateDone,
			t:  ToNone,
		},
	},
	Init: &initState{
		baseState{
			id: datastore.MenderStateInit,
			t:  ToNone,
		},
	},
	Idle: &idleState{
		baseState{
			id: datastore.MenderStateIdle,
			t:  ToIdle,
		},
	},
	InventoryUpdate: &inventoryUpdateState{
		baseState{
			id: datastore.MenderStateInventoryUpdate,
			t:  ToSync,
		},
	},
	UpdateCheck: &updateCheckState{
		baseState{
			id: datastore.MenderStateUpdateCheck,
			t:  ToSync,
		},
	},
}

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
	Wait(next, same State, wait time.Duration, wakeup chan bool) (State, bool)
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
		// wakeup: is a global channel shared between all states through the `StateContext`.
	}
}

// Wait performs wait for time `wait` and return state (`next`, false) after the wait
// has completed. If wait was interrupted returns (`same`, true)
func (ws *waitState) Wait(next, same State,
	wait time.Duration, wakeup chan bool) (State, bool) {
	ticker := time.NewTicker(wait)
	ws.wakeup = wakeup

	defer ticker.Stop()
	select {
	case <-ticker.C:
		log.Debugf("Wait complete")
		return next, false
	case <-ws.wakeup:
		log.Info("Forced wake-up from sleep")
		return next, false
	case <-ws.cancel:
		log.Infof("Wait canceled")
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
		setBrokenArtifactFlag(ctx.Store, us.Update().ArtifactName())
		return NewUpdateErrorState(err, us.Update()), false
	}
}

type idleState struct {
	baseState
}

func (i *idleState) Handle(ctx *StateContext, c Controller) (State, bool) {
	log.Info("idleState.Handle starting")
	// stop deployment logging
	DeploymentLogger.Disable()

	// cleanup state-data if any data is still present after an update
	RemoveStateData(ctx.Store)

	// check if client is authorized
	log.Info("    idleState.Handle calling c.IsAuthorized")
	if c.IsAuthorized() {
		log.Infof("        idleState.Handle returning %v,%v.", States.CheckWait, false)
		return States.CheckWait, false
	}
	log.Infof("    idleState.Handle returning %v,%v.", States.AuthorizeWait, false)
	return States.AuthorizeWait, false
}

type initState struct {
	baseState
}

func (i *initState) Handle(ctx *StateContext, c Controller) (State, bool) {
	// restore previous state information
	sd, sdErr := datastore.LoadStateData(ctx.Store)

	// handle easy case first: no previous state stored,
	// means no update was in progress; we should continue from idle
	if sdErr != nil && os.IsNotExist(sdErr) {
		log.Debug("No state data stored")
		return States.Idle, false
	}

	if sdErr != nil && sdErr != datastore.MaximumStateDataStoreCountExceeded {
		log.Errorf("Failed to restore state data: %v", sdErr)
		me := NewFatalError(errors.Wrapf(sdErr, "failed to restore state data"))
		return NewUpdateErrorState(me, &datastore.UpdateInfo{
			ID: "unknown",
		}), false
	}

	log.Infof("Handling loaded state: %s", sd.Name)

	if err := DeploymentLogger.Enable(sd.UpdateInfo.ID); err != nil {
		// just log error
		log.Errorf("Failed to enable deployment logger: %s", err)
	}

	if sdErr == datastore.MaximumStateDataStoreCountExceeded {
		// State argument not needed since we already know that maximum
		// count was exceeded and a different state will be returned.
		return handleStateDataError(ctx, nil, false, sd.Name, &sd.UpdateInfo, sdErr)
	}

	msg := fmt.Sprintf("Mender shut down in state: %s", sd.Name)
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

func (i *initState) getNextState(ctx *StateContext, sd *datastore.StateData,
	maybeErr menderError) (State, bool) {

	// check last known state
	switch sd.Name {

	// The update never got a chance to even start. Go straight to Idle
	// state, and it will be picked up again at the next polling interval.
	case datastore.MenderStateUpdateFetch:
		err := RemoveStateData(ctx.Store)
		if err != nil {
			return NewErrorState(NewFatalError(err)), false
		}
		return States.Idle, false

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
			setBrokenArtifactFlag(ctx.Store, sd.UpdateInfo.ArtifactName())
			return NewUpdateErrorState(maybeErr, &sd.UpdateInfo), false
		}
	}
}

type authorizeWaitState struct {
	baseState
	WaitState
}

func NewAuthorizeWaitState() State {
	return &authorizeWaitState{
		baseState: baseState{
			id: datastore.MenderStateAuthorizeWait,
			t:  ToIdle,
		},
		WaitState: NewWaitState(datastore.MenderStateAuthorizeWait, ToIdle),
	}
}

func (a *authorizeWaitState) Cancel() bool {
	return a.WaitState.Cancel()
}

func (a *authorizeWaitState) Handle(ctx *StateContext, c Controller) (State, bool) {
	log.Debugf("Handle authorize wait state")

	attempt := ctx.lastAuthorizeAttempt.Add(c.GetRetryPollInterval())

	now := time.Now()
	var wait time.Duration
	if attempt.After(now) {
		wait = attempt.Sub(now)
	}

	log.Debugf("Wait %v before next authorization attempt", wait)

	if wait == 0 {
		ctx.lastAuthorizeAttempt = now
		return States.Authorize, false
	}

	ctx.lastAuthorizeAttempt = attempt
	return a.Wait(States.Authorize, a, wait, ctx.WakeupChan)
}

type authorizeState struct {
	baseState
}

func (a *authorizeState) Handle(ctx *StateContext, c Controller) (State, bool) {
	// stop deployment logging
	DeploymentLogger.Disable()

	log.Debugf("Handle authorize state")
	if err := c.Authorize(); err != nil {
		log.Errorf("Authorize failed: %v", err)
		if !err.IsFatal() {
			return States.AuthorizeWait, false
		}
		return NewErrorState(err), false
	}
	// if everything is OK we should let Mender figure out what to do
	// in MenderStateCheckWait state
	return States.CheckWait, false
}

type updateCommitState struct {
	*updateState
	reportTries int
}

func NewUpdateCommitState(update *datastore.UpdateInfo) State {
	return &updateCommitState{
		updateState: NewUpdateState(datastore.MenderStateUpdateCommit,
			ToArtifactCommit_Enter, update),
	}
}

func (uc *updateCommitState) Handle(ctx *StateContext, c Controller) (State, bool) {
	var err error

	// start deployment logging
	if err = DeploymentLogger.Enable(uc.Update().ID); err != nil {
		log.Errorf("Can not enable deployment logger: %s", err)
	}

	log.Debug("Handle update commit state")

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

	// If the client migrated the database, we still need the old database
	// information if we are to roll back. However, after the commit above,
	// it is too late to roll back, so indidate that DB schema migration is
	// now permanent, if there was one.
	uc.Update().HasDBSchemaUpdate = false

	// And then store the data together with the new artifact name,
	// indicating that we have now migrated to a new artifact!
	err = datastore.StoreStateDataAndTransaction(ctx.Store,
		datastore.StateData{
			Name:       uc.Id(),
			UpdateInfo: *uc.Update(),
		}, func(txn store.Transaction) error {
			log.Debugf("Committing new artifact name: %s",
				uc.Update().ArtifactName())
			if err := txn.WriteAll(datastore.ArtifactNameKey,
				[]byte(uc.Update().ArtifactName())); err != nil {
				return err
			}
			log.Debugf("Committing new artifact group name: %s",
				uc.Update().ArtifactGroup())
			if err := txn.WriteAll(datastore.ArtifactGroupKey,
				[]byte(uc.Update().ArtifactGroup())); err != nil {
				return err
			}
			log.Debug("Committing new artifact type-info provides")
			providesBuf, err := json.Marshal(
				uc.Update().ArtifactTypeInfoProvides())
			if err != nil {
				return errors.Wrapf(err,
					"Error encoding ArtifactTypeInfoProvides to JSON.")
			}
			if err = txn.WriteAll(datastore.ArtifactTypeInfoProvidesKey,
				providesBuf); err != nil {
				return err
			}
			return nil
		})
	if err != nil {
		log.Error("Could not write state data to persistent storage: ", err.Error())
		return handleStateDataError(ctx, NewUpdateErrorState(NewTransientError(err), uc.Update()),
			false, datastore.MenderStateUpdateInstall, uc.Update(), err)
	}

	// Do rest of update commits now; then post commit-tasks
	return NewUpdateAfterFirstCommitState(uc.Update()), false
}

type updatePreCommitStatusReportRetryState struct {
	waitState
	returnToState State
	reportTries   int
}

func NewUpdatePreCommitStatusReportRetryState(returnToState State, reportTries int) State {
	return &updatePreCommitStatusReportRetryState{
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

func (usr *updatePreCommitStatusReportRetryState) Handle(ctx *StateContext, c Controller) (State, bool) {
	maxTrySending :=
		maxSendingAttempts(c.GetUpdatePollInterval(),
			c.GetRetryPollInterval(), minReportSendRetries)
	// we are always initializing with triesSending = 1
	maxTrySending++

	if usr.reportTries < maxTrySending {
		return usr.Wait(usr.returnToState, usr, c.GetRetryPollInterval(), ctx.WakeupChan)
	}
	return usr.returnToState.HandleError(ctx, c,
		NewTransientError(errors.New("Tried sending status report maximum number of times.")))
}

type updateAfterFirstCommitState struct {
	*updateState
}

func NewUpdateAfterFirstCommitState(update *datastore.UpdateInfo) State {
	return &updateAfterFirstCommitState{
		updateState: NewUpdateState(datastore.MenderStateUpdateAfterFirstCommit,
			ToNone, update),
	}
}

func (uc *updateAfterFirstCommitState) Handle(ctx *StateContext, c Controller) (State, bool) {
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

func (uc *updateAfterFirstCommitState) HandleError(ctx *StateContext, c Controller, merr menderError) (State, bool) {
	log.Error(merr.Error())

	// Too late to back out now. Just report the error, but do not try to roll back.
	setBrokenArtifactFlag(ctx.Store, uc.Update().ArtifactName())
	return NewUpdateCleanupState(uc.Update(), client.StatusFailure), false
}

type updateAfterCommitState struct {
	*updateState
}

func NewUpdateAfterCommitState(update *datastore.UpdateInfo) State {
	return &updateAfterCommitState{
		updateState: NewUpdateState(datastore.MenderStateUpdateAfterCommit,
			ToArtifactCommit_Leave, update),
	}
}

func (uc *updateAfterCommitState) Handle(ctx *StateContext, c Controller) (State, bool) {
	// This state only exists to rerun Commit_Leave scripts in the event of
	// spontaneous shutdowns, so there is nothing else to do in this state.

	// update is committed; clean up
	return NewUpdateCleanupState(uc.Update(), client.StatusSuccess), false
}

func (uc *updateAfterCommitState) HandleError(ctx *StateContext, c Controller, merr menderError) (State, bool) {
	log.Error(merr.Error())

	// Too late to back out now. Just report the error, but do not try to roll back.
	setBrokenArtifactFlag(ctx.Store, uc.Update().ArtifactName())
	return NewUpdateCleanupState(uc.Update(), client.StatusFailure), false
}

type updateCheckState struct {
	baseState
}

func (u *updateCheckState) Handle(ctx *StateContext, c Controller) (State, bool) {
	log.Infof("Handle update check state")

	update, err := c.CheckUpdate()

	if err != nil {
		if err.Cause() == os.ErrExist {
			// We are already running image which we are supposed to install.
			// Just report successful update and return to normal operations.
			return NewUpdateStatusReportState(update, client.StatusAlreadyInstalled), false
		}

		log.Errorf("Update check failed: %s", err)
		return NewErrorState(err), false
	}

	if update != nil {
		return NewUpdateFetchState(update), false
	}
	return States.CheckWait, false
}

type updateFetchState struct {
	baseState
	update datastore.UpdateInfo
}

func NewUpdateFetchState(update *datastore.UpdateInfo) State {
	return &updateFetchState{
		baseState: baseState{
			id: datastore.MenderStateUpdateFetch,
			t:  ToDownload_Enter,
		},
		update: *update,
	}
}

func (u *updateFetchState) Handle(ctx *StateContext, c Controller) (State, bool) {
	// start deployment logging
	if err := DeploymentLogger.Enable(u.update.ID); err != nil {
		return NewUpdateStatusReportState(&u.update, client.StatusFailure), false
	}

	log.Debugf("Handling update fetch state")

	merr := c.ReportUpdateStatus(&u.update, client.StatusDownloading)
	if merr != nil && merr.IsFatal() {
		return NewUpdateStatusReportState(&u.update, client.StatusFailure), false
	}

	in, _, err := c.FetchUpdate(u.update.URI())
	if err != nil {
		log.Errorf("Update fetch failed: %s", err)
		return NewFetchStoreRetryState(u, &u.update, err), false
	}

	return NewUpdateStoreState(in, &u.update), false
}

func (uf *updateFetchState) Update() *datastore.UpdateInfo {
	return &uf.update
}

type updateStoreState struct {
	*updateState
	// reader for obtaining image data
	imagein io.ReadCloser
}

func NewUpdateStoreState(in io.ReadCloser, update *datastore.UpdateInfo) State {
	return &updateStoreState{
		NewUpdateState(datastore.MenderStateUpdateStore,
			ToDownload_Enter, update),
		in,
	}
}

// Checks that the artifact name and compatible devices matches between the
// artifact header and the response. The installer argument holds a reference
// to the artifact reader for the update stream.
func (u *updateStoreState) verifyUpdateResponseAndHeader(
	installer *installer.Installer) error {
	if installer.GetArtifactName() != u.Update().ArtifactName() {
		return errors.Errorf("Artifact name in artifact is not what "+
			"the server claims (%s != %s).",
			installer.GetArtifactName(), u.Update().ArtifactName())
	}

	for _, rspDev := range u.Update().CompatibleDevices() {
		isEqual := false
		for _, artDev := range installer.GetCompatibleDevices() {
			if artDev == rspDev {
				isEqual = true
				break
			}
		}
		if !isEqual {
			return errors.Errorf("Compatible devices in artifact "+
				"is not what the server claims (%v != %v).",
				installer.GetCompatibleDevices(),
				u.Update().CompatibleDevices())
		}
	}
	return nil
}

func (u *updateStoreState) Handle(ctx *StateContext, c Controller) (State, bool) {

	// make sure to close the stream with image data
	defer u.imagein.Close()

	// start deployment logging
	if err := DeploymentLogger.Enable(u.update.ID); err != nil {
		return NewUpdateStatusReportState(&u.update, client.StatusFailure), false
	}

	log.Debugf("Handling update install state")

	merr := c.ReportUpdateStatus(&u.update, client.StatusDownloading)
	if merr != nil && merr.IsFatal() {
		return NewUpdateStatusReportState(&u.update, client.StatusFailure), false
	}

	installer, err := c.ReadArtifactHeaders(u.imagein)
	if err != nil {
		log.Errorf("Fetching Artifact headers failed: %s", err)
		return NewFetchStoreRetryState(u, &u.update, err), false
	}

	installers := c.GetInstallers()
	u.update.Artifact.PayloadTypes = make([]string, len(installers))
	for n, i := range installers {
		u.update.Artifact.PayloadTypes[n] = i.GetType()
	}

	// Verify that response from update request matches artifact header.
	if err := u.verifyUpdateResponseAndHeader(installer); err != nil {
		log.Error(err.Error())
		return NewUpdateStatusReportState(u.Update(),
			client.StatusFailure), false
	}

	err = u.maybeVerifyArtifactDependsAndProvides(ctx, installer)
	if err != nil {
		return NewUpdateStatusReportState(u.Update(),
			client.StatusFailure), false
	}

	// Store state so that all the payload handlers are recorded there. This
	// is important since they need to call their Cleanup functions after we
	// have started the download.
	err = datastore.StoreStateData(ctx.Store, datastore.StateData{
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

func (u *updateStoreState) maybeVerifyArtifactDependsAndProvides(
	ctx *StateContext, installer *installer.Installer) error {
	// For artifact version >= 3 we need to fetch the artifact provides of
	// the previous artifact from the datastore, and verify that the
	// provides match artifact dependencies.
	if depends, err := installer.GetArtifactDepends(); err != nil {
		log.Error("Failed to extract artifact dependencies from " +
			"header: " + err.Error())
	} else if depends != nil {
		var provides map[string]string
		// load header-info provides
		provides, err := datastore.LoadProvides(ctx.Store)
		if err != nil {
			log.Error(err.Error())
			return err
		}
		if err = verifyArtifactDependencies(
			depends, provides); err != nil {
			log.Error(err.Error())
			return err
		}
	}

	// Update the UpdateInfo provides info if artifact v3.
	if provides, err := installer.GetArtifactProvides(); err != nil {
		log.Error("Failed to extract artifact provides from " +
			"header: " + err.Error())
		return err
	} else if provides != nil {
		if _, ok := provides["artifact_name"]; !ok {
			log.Error("Missing required \"ArtifactName\" from " +
				"artifact dependencies")
			return err
		}
		delete(provides, "artifact_name")
		if grp, ok := provides["artifact_group"]; ok {
			u.update.Artifact.ArtifactGroup = grp
			// remove duplication
			delete(provides, "artifact_group")
		}
		u.update.Artifact.TypeInfoProvides = provides
	}
	return nil
}

func (u *updateStoreState) handleSupportsRollback(ctx *StateContext, c Controller) (bool, State, bool) {
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
	err := datastore.StoreStateData(ctx.Store, datastore.StateData{
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

func (is *updateStoreState) HandleError(ctx *StateContext, c Controller, merr menderError) (State, bool) {
	log.Error(merr.Error())
	return NewUpdateCleanupState(is.Update(), client.StatusFailure), false
}

type updateAfterStoreState struct {
	*updateState
}

func NewUpdateAfterStoreState(update *datastore.UpdateInfo) State {
	return &updateAfterStoreState{
		updateState: NewUpdateState(datastore.MenderStateUpdateAfterStore,
			ToDownload_Leave, update),
	}
}

func (s *updateAfterStoreState) Handle(ctx *StateContext, c Controller) (State, bool) {
	// This state only exists to run Download_Leave.
	return NewUpdateInstallState(s.Update()), false
}

func (s *updateAfterStoreState) HandleError(ctx *StateContext, c Controller, merr menderError) (State, bool) {
	log.Error(merr.Error())
	return NewUpdateCleanupState(s.Update(), client.StatusFailure), false
}

type updateInstallState struct {
	*updateState
}

func NewUpdateInstallState(update *datastore.UpdateInfo) State {
	return &updateInstallState{
		updateState: NewUpdateState(datastore.MenderStateUpdateInstall,
			ToArtifactInstall, update),
	}
}

func (is *updateInstallState) Handle(ctx *StateContext, c Controller) (State, bool) {
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

		case datastore.RebootTypeCustom, datastore.RebootTypeAutomatic:
			// Go to reboot state if at least one payload requested it.
			return NewUpdateRebootState(is.Update()), false

		default:
			return is.HandleError(ctx, c, NewTransientError(errors.New(
				"Unknown reboot type stored in database. Not continuing")))
		}
	}

	// No reboot requests, go to commit state.
	return NewUpdateCommitState(is.Update()), false
}

func (is *updateInstallState) handleRebootType(ctx *StateContext, c Controller) (bool, State, bool) {
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
	err := datastore.StoreStateData(ctx.Store, datastore.StateData{
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

type fetchStoreRetryState struct {
	baseState
	WaitState
	from   State
	update datastore.UpdateInfo
	err    error
}

func NewFetchStoreRetryState(from State, update *datastore.UpdateInfo,
	err error) State {
	return &fetchStoreRetryState{
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

func (fir *fetchStoreRetryState) Cancel() bool {
	return fir.WaitState.Cancel()
}
func (fir *fetchStoreRetryState) Handle(ctx *StateContext, c Controller) (State, bool) {
	log.Debugf("Handle fetch install retry state")

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

	log.Debugf("Wait %v before next fetch/install attempt", intvl)
	return fir.Wait(NewUpdateFetchState(&fir.update), fir, intvl, ctx.WakeupChan)
}

type checkWaitState struct {
	baseState
	WaitState
}

func NewCheckWaitState() State {
	return &checkWaitState{
		baseState: baseState{
			id: datastore.MenderStateCheckWait,
			t:  ToIdle,
		},
		WaitState: NewWaitState(datastore.MenderStateCheckWait, ToIdle),
	}
}

func (cw *checkWaitState) Cancel() bool {
	return cw.WaitState.Cancel()
}

func (cw *checkWaitState) Handle(ctx *StateContext, c Controller) (State, bool) {

	log.Info("Handle check wait state")

	// calculate next interval
	update := ctx.lastUpdateCheckAttempt.Add(c.GetUpdatePollInterval())
	inventory := ctx.lastInventoryUpdateAttempt.Add(c.GetInventoryPollInterval())

	// if we haven't sent inventory so far
	if ctx.lastInventoryUpdateAttempt.IsZero() {
		inventory = ctx.lastInventoryUpdateAttempt
	}

	log.Debugf("Check wait state; next checks: (update: %v) (inventory: %v)",
		update, inventory)

	next := struct {
		when  time.Time
		state State
	}{
		// assume update will be the next state
		when:  update,
		state: States.UpdateCheck,
	}

	if inventory.Before(update) {
		next.when = inventory
		next.state = States.InventoryUpdate
	}

	now := time.Now()
	log.Debugf("Next check: %v:%v, (%v)", next.when, next.state, now)

	// check if we should wait for the next state or we should return
	// immediately
	var wait time.Duration
	if next.when.After(now) {
		wait = next.when.Sub(now)
	}

	// (MEN-2195): Set the last update/inventory check time to now, as an error in an enter script will
	// hinder these states from ever running, and thus causing an infinite loop if the script
	// keeps returning the same error.
	switch (next.state).(type) {
	case *inventoryUpdateState:
		if wait == 0 {
			ctx.lastInventoryUpdateAttempt = now
		} else {
			ctx.lastInventoryUpdateAttempt = next.when
		}
	case *updateCheckState:
		if wait == 0 {
			ctx.lastUpdateCheckAttempt = now
		} else {
			ctx.lastUpdateCheckAttempt = next.when
		}
	}

	if wait != 0 {
		log.Debugf("Waiting %s for the next state", wait)
		return cw.Wait(next.state, cw, wait, ctx.WakeupChan)
	}

	log.Debugf("Check wait returned: %v", next.state)
	return next.state, false
}

type inventoryUpdateState struct {
	baseState
}

func (iu *inventoryUpdateState) Handle(ctx *StateContext, c Controller) (State, bool) {

	err := c.InventoryRefresh()
	if err != nil {
		log.Warnf("Failed to refresh inventory: %v", err)
		if errors.Cause(err) == errNoArtifactName {
			return NewErrorState(NewTransientError(err)), false
		}
	} else {
		log.Debugf("Inventory refresh complete")
	}
	return States.CheckWait, false
}

type errorState struct {
	baseState
	cause menderError
}

func NewErrorState(err menderError) State {
	if err == nil {
		err = NewFatalError(errors.New("general error"))
	}

	return &errorState{
		baseState{
			id: datastore.MenderStateError,
			t:  ToError,
		},
		err,
	}
}

func (e *errorState) Handle(ctx *StateContext, c Controller) (State, bool) {
	// stop deployment logging
	DeploymentLogger.Disable()

	log.Infof("Handling error state, current error: %v", e.cause.Error())
	// decide if error is transient, exit for now
	if e.cause.IsFatal() {
		return States.Final, false
	}
	return States.Idle, false
}

func (e *errorState) IsFatal() bool {
	return e.cause.IsFatal()
}

type updateErrorState struct {
	errorState
	update datastore.UpdateInfo
}

func NewUpdateErrorState(err menderError, update *datastore.UpdateInfo) State {
	return &updateErrorState{
		errorState{
			baseState{id: datastore.MenderStateUpdateError, t: ToArtifactFailure},
			err,
		},
		*update,
	}
}

func (ue *updateErrorState) Handle(ctx *StateContext, c Controller) (State, bool) {

	log.Debug("Handle update error state")

	for _, i := range c.GetInstallers() {
		err := i.Failure()
		if err != nil {
			log.Errorf("ArtifactFailure failed: %s", err.Error())
		}
	}

	return NewUpdateCleanupState(&ue.update, client.StatusFailure), false
}

func (ue *updateErrorState) Update() *datastore.UpdateInfo {
	return &ue.update
}

type updateCleanupState struct {
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
	return &updateCleanupState{
		updateState: NewUpdateState(datastore.MenderStateUpdateCleanup,
			transition, update),
		status: status,
	}
}

func (s *updateCleanupState) Handle(ctx *StateContext, c Controller) (State, bool) {
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
type updateStatusReportState struct {
	*updateState
	status             string
	triesSendingReport int
	triesSendingLogs   int
	logs               []byte
}

func NewUpdateStatusReportState(update *datastore.UpdateInfo, status string) State {
	return &updateStatusReportState{
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
		log.Errorf("Failed to report deployment logs: %v", err)
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

func (usr *updateStatusReportState) Handle(ctx *StateContext, c Controller) (State, bool) {

	// start deployment logging; no error checking
	// we can do nothing here; either we will have the logs or not...
	DeploymentLogger.Enable(usr.Update().ID)

	log.Debug("Handling update status report state")

	if err := sendDeploymentStatus(usr.Update(), usr.status,
		&usr.triesSendingReport, c); err != nil {

		log.Errorf("Failed to send status to server: %v", err)
		if err.IsFatal() {
			// there is no point in retrying
			return NewReportErrorState(usr.Update(), usr.status), false
		}
		return NewUpdateStatusReportRetryState(usr, usr.Update(), usr.status,
			usr.triesSendingReport), false
	}

	if usr.status == client.StatusFailure {
		log.Debugf("Attempting to upload deployment logs for failed update")
		if err := sendDeploymentLogs(usr.Update(),
			&usr.triesSendingLogs, usr.logs, c); err != nil {

			log.Errorf("Failed to send deployment logs to server: %v", err)
			if err.IsFatal() {
				// there is no point in retrying
				return NewReportErrorState(usr.Update(), usr.status), false
			}
			return NewUpdateStatusReportRetryState(usr, usr.Update(), usr.status,
				usr.triesSendingLogs), false
		}
	}

	log.Debug("Reporting complete")
	// stop deployment logging as the update is completed at this point
	DeploymentLogger.Disable()

	return States.Idle, false
}

type updateStatusReportRetryState struct {
	baseState
	WaitState
	reportState  State
	update       datastore.UpdateInfo
	status       string
	triesSending int
}

func NewUpdateStatusReportRetryState(reportState State,
	update *datastore.UpdateInfo, status string, tries int) State {
	return &updateStatusReportRetryState{
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

func (usr *updateStatusReportRetryState) Cancel() bool {
	return usr.WaitState.Cancel()
}

// retry no more than 10 times
var maxSendingAttemptsRoof = 10

func Min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// try to send failed report at least minRetries times or keep trying every
// 'retryPollInterval' for the duration of two 'updatePollInterval', or a
// maximum of 10 times
func maxSendingAttempts(upi, rpi time.Duration, minRetries int) int {
	if rpi == 0 {
		return minRetries
	}
	max := int(upi / rpi)
	if max <= minRetries {
		return minRetries
	}
	return Min(max*2, maxSendingAttemptsRoof)
}

// retry at least that many times
var minReportSendRetries = 3

func (usr *updateStatusReportRetryState) Handle(ctx *StateContext, c Controller) (State, bool) {
	maxTrySending :=
		maxSendingAttempts(c.GetUpdatePollInterval(),
			c.GetRetryPollInterval(), minReportSendRetries)
	// we are always initializing with triesSending = 1
	maxTrySending++

	if usr.triesSending < maxTrySending {
		return usr.Wait(usr.reportState, usr, c.GetRetryPollInterval(), ctx.WakeupChan)
	}
	return NewReportErrorState(&usr.update, usr.status), false
}

func (usr *updateStatusReportRetryState) Update() *datastore.UpdateInfo {
	return &usr.update
}

type reportErrorState struct {
	*updateState
	updateStatus string
}

func NewReportErrorState(update *datastore.UpdateInfo, status string) State {
	return &reportErrorState{
		updateState: NewUpdateState(datastore.MenderStateReportStatusError,
			ToArtifactFailure, update),
		updateStatus: status,
	}
}

func (res *reportErrorState) Handle(ctx *StateContext, c Controller) (State, bool) {
	// start deployment logging; no error checking
	// we can do nothing here; either we will have the logs or not...
	DeploymentLogger.Enable(res.Update().ID)

	log.Errorf("Handling report error state with status: %v", res.updateStatus)

	switch res.updateStatus {
	case client.StatusSuccess:
		// error while reporting success; rollback
		return NewUpdateRollbackState(res.Update()), false
	case client.StatusFailure:
		// error while reporting failure;
		// start from scratch as previous update was broken
		log.Errorf("Error while performing update: %v (%v)", res.updateStatus, *res.Update())
		return States.Idle, false
	case client.StatusAlreadyInstalled:
		// we've failed to report already-installed status, not a big
		// deal, start from scratch
		return States.Idle, false
	default:
		// should not end up here
		return States.Final, false
	}
}

func (res *reportErrorState) HandleError(ctx *StateContext, c Controller, merr menderError) (State, bool) {
	log.Errorf("Reached final error state: %s", merr.Error())
	return States.Idle, false
}

type updateRebootState struct {
	*updateState
}

func NewUpdateRebootState(update *datastore.UpdateInfo) State {
	return &updateRebootState{
		updateState: NewUpdateState(datastore.MenderStateReboot,
			ToArtifactReboot_Enter, update),
	}
}

func (e *updateRebootState) Handle(ctx *StateContext, c Controller) (State, bool) {

	// start deployment logging
	if err := DeploymentLogger.Enable(e.Update().ID); err != nil {
		// just log error; we need to reboot anyway
		log.Errorf("Failed to enable deployment logger: %s", err)
	}

	log.Debug("Handling reboot state")

	merr := c.ReportUpdateStatus(e.Update(), client.StatusRebooting)
	if merr != nil && merr.IsFatal() {
		return NewUpdateRollbackState(e.Update()), false
	}

	log.Info("Rebooting device(s)")

	systemRebootRequested := false
	for n, i := range c.GetInstallers() {
		rebootRequested, err := e.Update().RebootRequested.Get(n)
		if err != nil {
			return e.HandleError(ctx, c, NewTransientError(errors.Wrap(
				err, "Unable to get requested reboot type")))
		}
		switch rebootRequested {
		case datastore.RebootTypeCustom:
			if err := i.Reboot(); err != nil {
				log.Errorf("Error rebooting device: %v", err)
				return NewUpdateRollbackState(e.Update()), false
			}

		case datastore.RebootTypeAutomatic:
			systemRebootRequested = true
		}
	}

	if systemRebootRequested {
		// Final system reboot after reboot scripts have run.
		err := ctx.Rebooter.Reboot()
		// Should never return from Reboot().
		return e.HandleError(ctx, c, NewTransientError(errors.Wrap(err, "Could not reboot host")))
	}

	// We may never get here, if the machine we're on rebooted. However, if
	// we rebooted a peripheral device, we will get here.
	return NewUpdateVerifyRebootState(e.Update()), false
}

type updateVerifyRebootState struct {
	*updateState
}

func NewUpdateVerifyRebootState(update *datastore.UpdateInfo) State {
	return &updateVerifyRebootState{
		updateState: NewUpdateState(datastore.MenderStateAfterReboot,
			ToArtifactReboot_Leave, update),
	}
}

func (rs *updateVerifyRebootState) Handle(ctx *StateContext, c Controller) (State, bool) {
	for _, i := range c.GetInstallers() {
		err := i.VerifyReboot()
		if err != nil {
			return rs.HandleError(ctx, c, NewTransientError(err))
		}
	}

	return NewUpdateAfterRebootState(rs.Update()), false
}

type updateAfterRebootState struct {
	*updateState
}

func NewUpdateAfterRebootState(update *datastore.UpdateInfo) State {
	return &updateAfterRebootState{
		updateState: NewUpdateState(datastore.MenderStateAfterReboot,
			ToArtifactReboot_Leave, update),
	}
}

func (rs *updateAfterRebootState) Handle(ctx *StateContext,
	c Controller) (State, bool) {
	// start deployment logging; no error checking
	// we can do nothing here; either we will have the logs or not...
	DeploymentLogger.Enable(rs.Update().ID)

	// this state is needed to satisfy ToReboot transition Leave() action
	log.Debug("Handling state after reboot")

	return NewUpdateCommitState(rs.Update()), false
}

type updateRollbackState struct {
	*updateState
}

func NewUpdateRollbackState(update *datastore.UpdateInfo) State {
	return &updateRollbackState{
		updateState: NewUpdateState(datastore.MenderStateRollback, ToArtifactRollback, update),
	}
}

func (rs *updateRollbackState) Handle(ctx *StateContext, c Controller) (State, bool) {
	// start deployment logging
	if err := DeploymentLogger.Enable(rs.Update().ID); err != nil {
		// just log error; we need to reboot anyway
		log.Errorf("Failed to enable deployment logger: %s", err)
	}

	log.Info("Performing rollback")

	// Roll back to original partition and perform reboot
	for _, i := range c.GetInstallers() {
		if err := i.Rollback(); err != nil {
			log.Errorf("Rollback failed: %s", err)
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

		case datastore.RebootTypeCustom, datastore.RebootTypeAutomatic:
			// Enter rollback reboot state if at least one payload
			// asked for it.
			log.Debug("Will try to rollback reboot the device")
			return NewUpdateRollbackRebootState(rs.Update()), false

		default:
			return rs.HandleError(ctx, c, NewTransientError(errors.New(
				"Unknown reboot type stored in database. Not continuing")))
		}
	}

	// if no reboot is needed, just return the error and start over
	return NewUpdateErrorState(NewTransientError(errors.New("update error")),
		rs.Update()), false
}

func (rs *updateRollbackState) HandleError(ctx *StateContext, c Controller, merr menderError) (State, bool) {
	log.Error(merr.Error())
	setBrokenArtifactFlag(ctx.Store, rs.Update().ArtifactName())
	return NewUpdateErrorState(merr, rs.Update()), false
}

type updateRollbackRebootState struct {
	*updateState
}

func NewUpdateRollbackRebootState(update *datastore.UpdateInfo) State {
	return &updateRollbackRebootState{
		updateState: NewUpdateState(datastore.MenderStateRollbackReboot,
			ToArtifactRollbackReboot_Enter, update),
	}
}

func (rs *updateRollbackRebootState) Handle(ctx *StateContext, c Controller) (State, bool) {
	// start deployment logging
	if err := DeploymentLogger.Enable(rs.Update().ID); err != nil {
		// just log error; we need to reboot anyway
		log.Errorf("Failed to enable deployment logger: %s", err)
	}

	log.Info("Rebooting device(s) after rollback")

	systemRebootRequested := false
	for n, i := range c.GetInstallers() {
		rebootRequested, err := rs.Update().RebootRequested.Get(n)
		if err != nil {
			return rs.HandleError(ctx, c, NewTransientError(errors.Wrap(
				err, "Unable to get requested reboot type")))
		}
		switch rebootRequested {
		case datastore.RebootTypeCustom:
			if err := i.RollbackReboot(); err != nil {
				log.Errorf("Error rebooting device: %v", err)
				// Outcome is irrelevant, we will go to the
				// VerifyRollbackReboot state regardless.
			}

		case datastore.RebootTypeAutomatic:
			systemRebootRequested = true
		}
	}

	if systemRebootRequested {
		// Final system reboot after reboot scripts have run.
		err := ctx.Rebooter.Reboot()
		// Should never return from Reboot().
		return rs.HandleError(ctx, c, NewTransientError(errors.Wrap(err, "Could not reboot host")))
	}

	// We may never get here, if the machine we're on rebooted. However, if
	// we rebooted a peripheral device, we will get here.
	return NewUpdateVerifyRollbackRebootState(rs.Update()), false
}

func (rs *updateRollbackRebootState) HandleError(ctx *StateContext,
	c Controller, merr menderError) (State, bool) {

	// We don't really handle errors here, but instead in the verify state.
	return NewUpdateVerifyRollbackRebootState(rs.Update()), false
}

type updateVerifyRollbackRebootState struct {
	*updateState
}

func NewUpdateVerifyRollbackRebootState(update *datastore.UpdateInfo) State {
	return &updateVerifyRollbackRebootState{
		updateState: NewUpdateState(datastore.MenderStateVerifyRollbackReboot,
			ToArtifactRollbackReboot_Leave, update),
	}
}

func (rs *updateVerifyRollbackRebootState) Handle(ctx *StateContext, c Controller) (State, bool) {
	for _, i := range c.GetInstallers() {
		err := i.VerifyRollbackReboot()
		if err != nil {
			return rs.HandleError(ctx, c, NewTransientError(err))
		}
	}

	return NewUpdateAfterRollbackRebootState(rs.Update()), false
}

func (rs *updateVerifyRollbackRebootState) HandleError(ctx *StateContext,
	c Controller, merr menderError) (State, bool) {

	log.Errorf("Rollback reboot failed, will retry. Cause: %s", merr.Error())

	return NewUpdateRollbackRebootState(rs.Update()), false
}

type updateAfterRollbackRebootState struct {
	*updateState
}

func NewUpdateAfterRollbackRebootState(update *datastore.UpdateInfo) State {
	return &updateAfterRollbackRebootState{
		updateState: NewUpdateState(datastore.MenderStateAfterRollbackReboot,
			ToArtifactRollbackReboot_Leave, update),
	}
}

func (rs *updateAfterRollbackRebootState) Handle(ctx *StateContext,
	c Controller) (State, bool) {
	// start deployment logging
	if err := DeploymentLogger.Enable(rs.Update().ID); err != nil {
		// just log error; we need to reboot anyway
		log.Errorf("Failed to enable deployment logger: %s", err)
	}

	// this state is needed to satisfy ToRollbackReboot
	// transition Leave() action
	log.Debug("Handling state after rollback reboot")

	return NewUpdateErrorState(NewTransientError(errors.New("update error")),
		rs.Update()), false
}

type finalState struct {
	baseState
}

func (f *finalState) Handle(ctx *StateContext, c Controller) (State, bool) {
	panic("reached final state")
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

	if err == datastore.MaximumStateDataStoreCountExceeded {
		log.Errorf("State transition loop detected in state %s: Forcefully aborting "+
			"update. The system is likely to be in an inconsistent "+
			"state after this.", stateName)
		setBrokenArtifactFlag(ctx.Store, update.ArtifactName())
		return NewUpdateStatusReportState(update, client.StatusFailure), false
	}

	return newState, cancelled
}

func setBrokenArtifactFlag(store store.Store, artName string) {
	newName := artName + conf.BrokenArtifactSuffix
	log.Debugf("Setting artifact name to %s", newName)
	err := store.WriteAll(datastore.ArtifactNameKey, []byte(newName))
	if err != nil {
		log.Errorf("Could not write artifact name \"%s\": %s", artName, err.Error())
		// No error return, because everyone who calls this function is
		// already in an error path.
	}
}
