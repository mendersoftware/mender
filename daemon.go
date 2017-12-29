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
	"os"

	"github.com/mendersoftware/log"
	"github.com/mendersoftware/mender/client"
	"github.com/mendersoftware/mender/store"
	"github.com/pkg/errors"
)

// Config section

type menderDaemon struct {
	mender Controller
	stop   bool
	sctx   StateContext
	store  store.Store
}

func NewDaemon(mender Controller, store store.Store) *menderDaemon {

	daemon := menderDaemon{
		mender: mender,
		sctx: StateContext{
			store: store,
		},
		store: store,
	}
	return &daemon
}

func (d *menderDaemon) StopDaemon() {
	d.stop = true
}

func (d *menderDaemon) Cleanup() {
	if d.store != nil {
		if err := d.store.Close(); err != nil {
			log.Errorf("failed to close data store: %v", err)
		}
		d.store = nil
	}
}

func (d *menderDaemon) shouldStop() bool {
	return d.stop
}

func createMockFromState(sd StateData) State {

	switch sd.FromState {
	case MenderStateUpdateFetch:
		return NewUpdateFetchState(sd.UpdateInfo)
	case MenderStateUpdateStore:
		return NewUpdateStoreState(nil, 0, sd.UpdateInfo)
	case MenderStateUpdateInstall:
		return NewUpdateInstallState(sd.UpdateInfo)
	case MenderStateReboot:
		return NewRebootState(sd.UpdateInfo)
	case MenderStateUpdateError:
		return NewUpdateErrorState(&MenderError{errors.New(sd.ErrCause), sd.ErrFatal}, sd.UpdateInfo)
	case MenderStateError:
		return NewErrorState(&MenderError{errors.New(sd.ErrCause), sd.ErrFatal})
	case MenderStateRollback:
		return NewRollbackState(sd.UpdateInfo, false, false)
	case MenderStateUpdateStatusReport:
		return NewUpdateStatusReportState(sd.UpdateInfo, sd.UpdateStatus)
	case MenderStateIdle:
		return idleState
	default:
		return initState
	}
}

// InitialState returns the last stored recovery state,
// or init if no recovery data is found.
func initialState(s store.Store) State {

	sd, err := LoadStateData(s)
	if err != nil {
		log.Error("Failed to read state data")
		return initState
	}

	is := createMockFromState(sd)
	is.SetTransition(sd.LeaveTransition)
	return is
}

func setToState(s store.Store, c Controller) State {

	// restore previous state information
	sd, err := LoadStateData(s)

	// handle easy case first: no previous state stored,
	// means no update was in progress; we should continue from idle
	if err != nil && os.IsNotExist(err) {
		log.Debug("no state data stored")
		return idleState
	}

	if err != nil {
		log.Errorf("failed to restore state data: %v", err)
		me := NewFatalError(errors.Wrapf(err, "failed to restore state data"))
		return NewUpdateErrorState(me, client.UpdateResponse{
			ID: "unknown",
		})
	}

	switch sd.Name {

	// needed to satisfy artifact-failure-leave recovery
	case MenderStateIdle:
		return idleState

	case MenderStateUpdateFetch:
		return NewUpdateFetchState(sd.UpdateInfo)

	case MenderStateUpdateStore:
		// we need to get a new reader to the update-stream
		in, size, err := c.FetchUpdate(sd.UpdateInfo.URI())
		if err != nil {
			// TODO - return error, as this is after a powerloss?
			return NewFetchStoreRetryState(NewUpdateFetchState(sd.UpdateInfo), sd.UpdateInfo, err)
		}
		return NewUpdateStoreState(in, size, sd.UpdateInfo)

	case MenderStateUpdateInstall:
		return NewUpdateInstallState(sd.UpdateInfo)

	// update process was finished; check what is the status of update
	case MenderStateReboot:
		if sd.TransitionStatus == UnInitialised {
			return NewAfterRebootState(sd.UpdateInfo)
		}
		// powerloss prior to switching partition
		return NewRebootState(sd.UpdateInfo)

	case MenderStateAfterReboot, MenderStateUpdateVerify, MenderStateRollback, MenderStateRollbackReboot:
		return NewUpdateErrorState(&MenderError{errors.New("fatal error while running on the uncommitted partition, this can be due to a powerloss, or any other fatal error that causes the machine to crash"), false}, sd.UpdateInfo)

	case MenderStateUpdateStatusReport:
		ur := &UpdateStatusReportState{
			UpdateState: NewUpdateState(MenderStateUpdateStatusReport,
				ToNone, sd.UpdateInfo),
			status:             sd.UpdateStatus,
			triesSendingReport: sd.TriesSendingReport,
			triesSendingLogs:   sd.TriesSendingLogs,
			reportSent:         sd.ReportSent,
			logs:               sd.Logs,
		}
		// ur.SetTransition(sd.ErrorTransition)
		return ur

	case MenderStateUpdateError:
		// Do not rerun the error-scripts
		sd.TransitionStatus = LeaveDone
		if err := StoreStateData(s, sd); err != nil {
			log.Errorf("Failed to write state-data to storage: %s", err.Error())
		}
		return NewUpdateErrorState(&sd.MenderErr, sd.UpdateInfo)

	case MenderStateUpdateCommit:
		// We need to check if the update has been committed or not
		return nil

	case MenderStateError:
		me := MenderError{
			cause: errors.New(sd.ErrCause),
			fatal: sd.ErrFatal,
		}
		return NewErrorState(&me)

	// this should not happen
	default:
		log.Errorf("got invalid state: %v", sd.Name)
		me := NewFatalError(errors.Errorf("got invalid state stored: %v", sd.Name))

		return NewUpdateErrorState(me, sd.UpdateInfo)
	}
}

func updateFinished(store store.Store, c Controller) bool {
	up, err := c.HasUpgrade()
	log.Debugf("has Upgrade: %v, err: %v", up, err)
	if err != nil {
		return true
	}
	if up {
		return false
	}
	return true
}

func (d *menderDaemon) Run() error {
	// set the first state transition
	d.mender.SetNextState(initialState(d.sctx.store))
	var toState State = setToState(d.sctx.store, d.mender)
	log.Debugf("fromState: %s -> toState %s", d.mender.GetCurrentState().Id(), toState.Id())
	cancelled := false

	// TODO - does afterReboot get stored in recoverydata?

	var from, to State
	for {
		to = toState
		toState, cancelled = d.mender.TransitionState(toState, &d.sctx)
		from = d.mender.GetCurrentState()

		_, ok := toState.(Recover)
		log.Infof("Transitioning: oldfrom: %s, oldto: %s, nxt: %s - is nxt recover-state: %t", from.Id(), to.Id(), toState.Id(), ok)

		// if toState.Id() == MenderStateIdle && from
		if err := handleRecoveryStates(toState, from, d.sctx.store); err != nil {
			log.Errorf("Failed to write recovery-data to memory")
		}

		if toState.Id() == MenderStateError {
			es, ok := toState.(*ErrorState)
			if ok {
				if es.IsFatal() {
					return es.Cause()
				}
			} else {
				return errors.New("failed")
			}
		}
		if cancelled || toState.Id() == MenderStateDone {
			break
		}
		if d.shouldStop() {
			return nil
		}
	}
	return nil
}

func handleRecoveryStates(nxt, from State, store store.Store) error {
	log.Debug("Handle recovery states")
	us, ok := nxt.(Recover)
	log.Debugf("Is recovery: %t", ok)
	if ok {
		log.Debugf("Storing recovery data for: %s", nxt.Id())
		rd := us.RecoveryData(from)
		rd.TransitionStatus = NoStatus // transition done
		// if rd.LeaveTransition == ToNone && rd.FromState == MenderStateInit &&
		// 	rd.Name == MenderStateInit {
		// 	log.Debug("Removing state data")
		// 	RemoveStateData(store)
		// 	return nil
		// }
		log.Debugf("Storing state-data: %v", rd)
		log.Debugf("UpdateStore leaveTransition: %s", rd.LeaveTransition)
		if err := StoreStateData(store, rd); err != nil {
			log.Errorf("Failed to write recovery data to memory")
			return err
		}
		return nil
	}
	return nil
}
