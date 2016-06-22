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
	"bytes"
	"errors"
	"io/ioutil"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type stateTestController struct {
	fakeDevice
	fakeUpdater
	bootstrapErr  menderError
	imageID       string
	pollIntvl     time.Duration
	hasUpgrade    bool
	hasUpgradeErr menderError
	state         State
	updateResp    *UpdateResponse
	updateRespErr menderError
	authorize     menderError
}

func (s *stateTestController) Bootstrap() menderError {
	return s.bootstrapErr
}

func (s *stateTestController) GetCurrentImageID() string {
	return s.imageID
}

func (s *stateTestController) GetUpdatePollInterval() time.Duration {
	return s.pollIntvl
}

func (s *stateTestController) HasUpgrade() (bool, menderError) {
	return s.hasUpgrade, s.hasUpgradeErr
}

func (s *stateTestController) CheckUpdate() (*UpdateResponse, menderError) {
	return s.updateResp, s.updateRespErr
}

func (s *stateTestController) GetState() State {
	return s.state
}

func (s *stateTestController) SetState(state State) {
	s.state = state
}

func (s *stateTestController) Authorize() menderError {
	return s.authorize
}

func TestStateBase(t *testing.T) {
	bs := BaseState{
		MenderStateInit,
	}

	assert.Equal(t, MenderStateInit, bs.Id())
	assert.False(t, bs.Cancel())
}

func TestStateCancellable(t *testing.T) {
	cs := NewCancellableState(BaseState{
		id: MenderStateAuthorizeWait,
	})

	assert.Equal(t, MenderStateAuthorizeWait, cs.Id())

	var s State
	var c bool

	// no update
	var tstart, tend time.Time

	tstart = time.Now()
	s, c = cs.StateAfterWait(bootstrappedState, initState,
		100*time.Millisecond)
	tend = time.Now()
	// not cancelled should return the 'next' state
	assert.Equal(t, bootstrappedState, s)
	assert.False(t, c)
	assert.WithinDuration(t, tend, tstart, 105*time.Millisecond)

	// asynchronously cancel state operation
	go func() {
		c := cs.Cancel()
		assert.True(t, c)
	}()
	// should finish right away
	tstart = time.Now()
	s, c = cs.StateAfterWait(bootstrappedState, initState,
		100*time.Millisecond)
	tend = time.Now()
	// canceled should return the other state
	assert.Equal(t, initState, s)
	assert.True(t, c)
	assert.WithinDuration(t, tend, tstart, 5*time.Millisecond)

}

func TestStateError(t *testing.T) {

	fooerr := NewTransientError(errors.New("foo"))

	es := NewErrorState(fooerr)
	assert.Equal(t, MenderStateError, es.Id())
	assert.IsType(t, &ErrorState{}, es)
	errstate, _ := es.(*ErrorState)
	assert.NotNil(t, errstate)
	assert.Equal(t, fooerr, errstate.cause)

	es = NewErrorState(nil)
	errstate, _ = es.(*ErrorState)
	assert.NotNil(t, errstate)
	assert.Contains(t, errstate.cause.Error(), "general error")
}

func TestStateInit(t *testing.T) {
	i := InitState{}

	var s State
	var c bool
	s, c = i.Handle(&stateTestController{
		bootstrapErr: NewFatalError(errors.New("fake err")),
	})
	assert.IsType(t, &ErrorState{}, s)
	assert.False(t, c)

	s, c = i.Handle(&stateTestController{})
	assert.IsType(t, &BootstrappedState{}, s)
	assert.False(t, c)
}

func TestStateBootstrapped(t *testing.T) {
	b := BootstrappedState{}

	var s State
	var c bool

	s, c = b.Handle(&stateTestController{})
	assert.IsType(t, &AuthorizedState{}, s)
	assert.False(t, c)

	s, c = b.Handle(&stateTestController{
		authorize: NewTransientError(errors.New("auth fail temp")),
	})
	assert.IsType(t, &AuthorizeWaitState{}, s)
	assert.False(t, c)

	s, c = b.Handle(&stateTestController{
		authorize: NewFatalError(errors.New("upgrade err")),
	})
	assert.IsType(t, &ErrorState{}, s)
	assert.False(t, c)
}

func TestStateAuthorized(t *testing.T) {
	b := AuthorizedState{}

	var s State
	var c bool

	s, c = b.Handle(&stateTestController{
		hasUpgrade: false,
	})
	assert.IsType(t, &UpdateCheckWaitState{}, s)
	assert.False(t, c)

	s, c = b.Handle(&stateTestController{
		hasUpgrade: true,
	})
	assert.IsType(t, &UpdateCommitState{}, s)
	assert.False(t, c)

	s, c = b.Handle(&stateTestController{
		hasUpgradeErr: NewFatalError(errors.New("upgrade err")),
	})
	assert.IsType(t, &ErrorState{}, s)
	assert.False(t, c)
}

func TestStateAuthorizeWait(t *testing.T) {
	cws := NewAuthorizeWaitState()

	var s State
	var c bool

	// no update
	var tstart, tend time.Time

	tstart = time.Now()
	s, c = cws.Handle(&stateTestController{
		pollIntvl: 100 * time.Millisecond,
	})
	tend = time.Now()
	assert.IsType(t, &BootstrappedState{}, s)
	assert.False(t, c)
	assert.WithinDuration(t, tend, tstart, 105*time.Millisecond)

	// asynchronously cancel state operation
	go func() {
		c := cws.Cancel()
		assert.True(t, c)
	}()
	// should finish right away
	tstart = time.Now()
	s, c = cws.Handle(&stateTestController{
		pollIntvl: 100 * time.Millisecond,
	})
	tend = time.Now()
	// canceled state should return itself
	assert.IsType(t, &AuthorizeWaitState{}, s)
	assert.True(t, c)
	assert.WithinDuration(t, tend, tstart, 5*time.Millisecond)
}

func TestStateUpdateCommit(t *testing.T) {
	cs := UpdateCommitState{}

	var s State
	var c bool

	// commit without errors
	s, c = cs.Handle(&stateTestController{})
	assert.IsType(t, &UpdateCheckWaitState{}, s)
	assert.False(t, c)

	s, c = cs.Handle(&stateTestController{
		fakeDevice: fakeDevice{
			retCommit: NewFatalError(errors.New("commit fail")),
		},
	})
	assert.IsType(t, s, &ErrorState{})
	assert.False(t, c)
}

func TestStateUpdateCheckWait(t *testing.T) {
	cws := NewUpdateCheckWaitState()

	var s State
	var c bool

	// no update
	var tstart, tend time.Time

	tstart = time.Now()
	s, c = cws.Handle(&stateTestController{
		pollIntvl: 100 * time.Millisecond,
	})
	tend = time.Now()
	assert.IsType(t, &UpdateCheckState{}, s)
	assert.False(t, c)
	assert.WithinDuration(t, tend, tstart, 105*time.Millisecond)

	// asynchronously cancel state operation
	go func() {
		c := cws.Cancel()
		assert.True(t, c)
	}()
	// should finish right away
	tstart = time.Now()
	s, c = cws.Handle(&stateTestController{
		pollIntvl: 100 * time.Millisecond,
	})
	tend = time.Now()
	// canceled state should return itself
	assert.IsType(t, &UpdateCheckWaitState{}, s)
	assert.True(t, c)
	assert.WithinDuration(t, tend, tstart, 5*time.Millisecond)
}

func TestStateUpdateCheck(t *testing.T) {
	cs := UpdateCheckState{}

	var s State
	var c bool

	// no update
	s, c = cs.Handle(&stateTestController{})
	assert.IsType(t, &UpdateCheckWaitState{}, s)
	assert.False(t, c)

	// pretend update check failed
	s, c = cs.Handle(&stateTestController{
		updateRespErr: NewTransientError(errors.New("check failed")),
	})
	assert.IsType(t, &ErrorState{}, s)
	assert.False(t, c)

	// pretend we have an update
	update := &UpdateResponse{}

	s, c = cs.Handle(&stateTestController{
		updateResp: update,
	})
	assert.IsType(t, &UpdateFetchState{}, s)
	assert.False(t, c)
	ufs, _ := s.(*UpdateFetchState)
	assert.Equal(t, update, ufs.update)
}

func TestStateUpdateFetch(t *testing.T) {
	// pretend we have an update
	update := &UpdateResponse{}
	cs := NewUpdateFetchState(update)

	var s State
	var c bool

	// pretend update check failed
	s, c = cs.Handle(&stateTestController{
		fakeUpdater: fakeUpdater{
			fetchUpdateReturnError: NewTransientError(errors.New("fetch failed")),
		},
	})
	assert.IsType(t, &ErrorState{}, s)
	assert.False(t, c)

	data := "test"
	stream := ioutil.NopCloser(bytes.NewBufferString(data))

	s, c = cs.Handle(&stateTestController{
		fakeUpdater: fakeUpdater{
			fetchUpdateReturnReadCloser: stream,
			fetchUpdateReturnSize:       int64(len(data)),
		},
	})
	assert.IsType(t, &UpdateInstallState{}, s)
	assert.False(t, c)
	uis, _ := s.(*UpdateInstallState)
	assert.Equal(t, stream, uis.imagein)
	assert.Equal(t, int64(len(data)), uis.size)
}

func TestStateUpdateInstall(t *testing.T) {
	data := "test"
	stream := ioutil.NopCloser(bytes.NewBufferString(data))

	uis := NewUpdateInstallState(stream, int64(len(data)))

	var s State
	var c bool

	// pretend update check failed
	s, c = uis.Handle(&stateTestController{
		fakeDevice: fakeDevice{
			retInstallUpdate: NewFatalError(errors.New("install failed")),
		},
	})
	assert.IsType(t, &ErrorState{}, s)
	assert.False(t, c)

	s, c = uis.Handle(&stateTestController{
		fakeDevice: fakeDevice{
			retEnablePart: NewFatalError(errors.New("enable failed")),
		},
	})
	assert.IsType(t, &ErrorState{}, s)
	assert.False(t, c)

	s, c = uis.Handle(&stateTestController{})
	assert.IsType(t, &RebootState{}, s)
	assert.False(t, c)
}

func TestStateReboot(t *testing.T) {
	rs := RebootState{}

	var s State
	var c bool

	s, c = rs.Handle(&stateTestController{
		fakeDevice: fakeDevice{
			retReboot: NewFatalError(errors.New("reboot failed")),
		}})
	assert.IsType(t, &ErrorState{}, s)
	assert.False(t, c)

	s, c = rs.Handle(&stateTestController{})
	assert.IsType(t, &FinalState{}, s)
	assert.False(t, c)
}

func TestStateFinal(t *testing.T) {
	rs := FinalState{}

	assert.Panics(t, func() {
		rs.Handle(&stateTestController{})
	}, "final state Handle() should panic")
}
