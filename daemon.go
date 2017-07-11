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
	"github.com/pkg/errors"
)

// Config section

type menderDaemon struct {
	mender Controller
	stop   bool
	sctx   StateContext
	store  Store
}

func NewDaemon(mender Controller, store Store) *menderDaemon {

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
	// figure out the state
	for {
		state, cancelled := d.mender.RunState(&d.sctx)
		if state.Id() == MenderStateError {
			es, ok := state.(*ErrorState)
			if ok {
				if es.IsFatal() {
					return es.cause
				}
			} else {
				return errors.New("failed")
			}
		}
		if cancelled || state.Id() == MenderStateDone {
			break
		}

		if d.shouldStop() {
			return nil
		}

		d.mender.SetState(state)
	}
	return nil
}
