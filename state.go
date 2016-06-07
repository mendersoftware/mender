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
	"io"
	"time"

	"github.com/mendersoftware/log"
	"github.com/pkg/errors"
)

type State interface {
	// Perform state action, returns next state and boolean flag indicating if
	// execution was cancelled or not
	Handle(c Controller) (State, bool)
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
	// TODO generic state run action
}

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

	updateCheckWaitState = NewUpdateCheckWaitState()

	updateCheckState = &UpdateCheckState{
		BaseState{
			id: MenderStateUpdateCheck,
		},
	}

	updateCommitState = &UpdateCommitState{
		BaseState{
			id: MenderStateUpdateCommit,
		},
	}

	rebootState = &RebootState{
		BaseState{
			id: MenderStateReboot,
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

type InitState struct {
	BaseState
}

func (i *InitState) Handle(c Controller) (State, bool) {
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

func (b *BootstrappedState) Handle(c Controller) (State, bool) {
	log.Debugf("handle bootstrapped state")
	has, err := c.HasUpgrade()
	if err != nil {
		log.Errorf("has upgrade check failed: %s", err)
		return NewErrorState(err), false
	}
	if has {
		return updateCommitState, false
	}

	return updateCheckWaitState, false

}

type UpdateCommitState struct {
	BaseState
}

func (uc *UpdateCommitState) Handle(c Controller) (State, bool) {
	log.Debugf("handle update commit state")
	err := c.CommitUpdate()
	if err != nil {
		log.Errorf("update commit failed: %s", err)
		return NewErrorState(err), false
	}
	// done?
	return updateCheckWaitState, false
}

type UpdateCheckState struct {
	BaseState
}

func (u *UpdateCheckState) Handle(c Controller) (State, bool) {
	log.Debugf("handle update check state")
	update, err := c.CheckUpdate()
	if err != nil {
		log.Errorf("update check failed: %s", err)
		// maybe transient error?
		return NewErrorState(err), false
	}

	if update != nil {
		// custom state data?
		return NewUpdateFetchState(update), false
	}

	return updateCheckWaitState, false
}

type UpdateFetchState struct {
	BaseState
	update *UpdateResponse
}

func NewUpdateFetchState(update *UpdateResponse) State {
	return &UpdateFetchState{
		BaseState{
			id: MenderStateUpdateFetch,
		},
		update,
	}
}

func (u *UpdateFetchState) Handle(c Controller) (State, bool) {
	log.Debugf("handle update fetch state")
	in, size, err := c.FetchUpdate(u.update.Image.URI)
	if err != nil {
		log.Errorf("update fetch failed: %s", err)
		return NewErrorState(err), false
	}

	return NewUpdateInstallState(in, size), false
}

type UpdateInstallState struct {
	BaseState
	// reader for obtaining image data
	imagein io.ReadCloser
	// expected image size
	size int64
}

func NewUpdateInstallState(in io.ReadCloser, size int64) State {
	return &UpdateInstallState{
		BaseState{
			id: MenderStateUpdateInstall,
		},
		in,
		size,
	}
}

func (u *UpdateInstallState) Handle(c Controller) (State, bool) {
	log.Debugf("handle update install state")
	if err := c.InstallUpdate(u.imagein, u.size); err != nil {
		log.Errorf("update install failed: %s", err)
		return NewErrorState(err), false
	}

	if err := c.EnableUpdatedPartition(); err != nil {
		log.Errorf("enabling updated partition failed: %s", err)
		return NewErrorState(err), false
	}

	return rebootState, false
}

type UpdateCheckWaitState struct {
	BaseState
	cancel chan (bool)
}

func NewUpdateCheckWaitState() State {
	return &UpdateCheckWaitState{
		BaseState{
			id: MenderStateUpdateCheckWait,
		},
		make(chan bool),
	}
}

func (u *UpdateCheckWaitState) Handle(c Controller) (State, bool) {
	log.Debugf("handle update check wait state")

	intvl := c.GetUpdatePollInterval()
	log.Debugf("wait %v before next poll", intvl)
	ticker := time.NewTicker(intvl)

	select {
	case <-ticker.C:
		log.Debugf("poll interval complete")
		return updateCheckState, false
	case <-u.cancel:
		log.Infof("update wait canceled")
	}

	return u, true
}

// Cancel wait state
func (u *UpdateCheckWaitState) Cancel() bool {
	u.cancel <- true
	return true
}

type ErrorState struct {
	BaseState
	cause error
}

func NewErrorState(err error) State {
	if err == nil {
		err = errors.New("general error")
	}

	return &ErrorState{
		BaseState{
			id: MenderStateError,
		},
		err,
	}
}

func (e *ErrorState) Handle(c Controller) (State, bool) {
	log.Debugf("handle error state")
	// decide if error is transient, exit for now
	return doneState, false
}

type RebootState struct {
	BaseState
}

func (e *RebootState) Handle(c Controller) (State, bool) {
	log.Debugf("handle reboot state")
	if err := c.Reboot(); err != nil {
		return NewErrorState(err), false
	}
	return doneState, false
}

type FinalState struct {
	BaseState
}

func (f *FinalState) Handle(c Controller) (State, bool) {
	panic("reached final state")
}
