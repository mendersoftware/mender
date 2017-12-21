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
	"github.com/mendersoftware/log"
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

func (d *menderDaemon) Run() error {
	// set the first state transition
	var toState State = d.mender.GetCurrentState()
	cancelled := false

	var from, to State
	for {
		to = toState
		from = d.mender.GetCurrentState()
		toState, cancelled = d.mender.TransitionState(toState, &d.sctx)

		_, ok := toState.(Recover)
		log.Infof("Transitioning: oldfrom: %s, oldto: %s, nxt: %s - is nxt recover-state: %t", from.Id(), to.Id(), toState.Id(), ok)
		if err := handleRecoveryStates(toState, from, to, d.sctx.store); err != nil {
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

func handleRecoveryStates(nxt, from, to State, store store.Store) error {
	us, ok := nxt.(Recover)
	// Do not store recovery-data when doing the init -> init transition
	if ok && !(from.Id() == MenderStateInit && to.Id() == MenderStateInit) {
		log.Debugf("Storing recovery data for: %s", nxt.Id())
		rd := us.RecoveryData(to)
		rd.TransitionStatus = NoStatus // transition done
		log.Debugf("Storing state-data: %v", rd)
		log.Debugf("UpdateStore leaveTransition: %s", rd.LeaveTransition)
		if err := StoreStateData(store, rd); err != nil {
			log.Errorf("Failed to write recovery data to memory")
			// return TransitionError(from, "TODO"), false
		}
		return nil
	}
	return nil
}
