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
	"github.com/pkg/errors"
)

// Config section

type menderDaemon struct {
	mender      Controller
	stopChannel chan (bool)
	stop        bool
}

func NewDaemon(mender Controller) *menderDaemon {

	daemon := menderDaemon{
		mender:      mender,
		stopChannel: make(chan bool),
	}
	return &daemon
}

func (d menderDaemon) StopDaemon() {
	d.stopChannel <- true
}

func (d *menderDaemon) Run() error {
	// figure out the state
	for {
		state, cancelled := d.mender.GetState().Handle(d.mender)
		if state.Id() == MenderStateError {
			es, ok := state.(*ErrorState)
			if ok {
				return es.cause
			}
			return errors.New("failed")
		}
		if cancelled || state.Id() == MenderStateDone {
			break
		}

		if d.stop {
			return nil
		}

		d.mender.SetState(state)
	}
	return nil
}
