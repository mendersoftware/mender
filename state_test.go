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
	"io"
	"io/ioutil"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type stateTestController struct {
	fakeDevice
	updater       fakeUpdater
	bootstrapErr  menderError
	imageID       string
	pollIntvl     time.Duration
	hasUpgrade    bool
	hasUpgradeErr menderError
	state         State
	updateResp    *UpdateResponse
	updateRespErr menderError
	authorize     menderError
	reportError   menderError
	reportStatus  string
	reportUpdate  UpdateResponse
	logUpdate     UpdateResponse
	logs          []LogEntry
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

func (s *stateTestController) FetchUpdate(url string) (io.ReadCloser, int64, error) {
	return s.updater.FetchUpdate(nil, url)
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

func (s *stateTestController) ReportUpdateStatus(update UpdateResponse, status string) menderError {
	s.reportUpdate = update
	s.reportStatus = status
	return s.reportError
}

func (s *stateTestController) UploadLog(update UpdateResponse, logs []LogEntry) menderError {
	s.logUpdate = update
	s.logs = logs
	return s.reportError
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

func TestStateUpdateError(t *testing.T) {

	fooerr := NewTransientError(errors.New("foo"))

	es := NewUpdateErrorState(fooerr, UpdateResponse{})
	assert.Equal(t, MenderStateUpdateError, es.Id())
	assert.IsType(t, &UpdateErrorState{}, es)
	errstate, _ := es.(*UpdateErrorState)
	assert.NotNil(t, errstate)
	assert.Equal(t, fooerr, errstate.cause)

	sc := &stateTestController{}
	es = NewUpdateErrorState(fooerr, UpdateResponse{})
	es.Handle(sc)
	assert.Equal(t, statusFailure, sc.reportStatus)
	assert.NotEmpty(t, sc.logs)
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
	// TODO verify that update information was restored
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
	cs := NewUpdateCommitState(UpdateResponse{})

	var s State
	var c bool

	// commit without errors
	sc := &stateTestController{}
	s, c = cs.Handle(sc)
	assert.IsType(t, &UpdateCheckWaitState{}, s)
	assert.False(t, c)
	assert.Equal(t, statusSuccess, sc.reportStatus)

	s, c = cs.Handle(&stateTestController{
		fakeDevice: fakeDevice{
			retCommit: NewFatalError(errors.New("commit fail")),
		},
	})
	assert.IsType(t, s, &ErrorState{})
	assert.False(t, c)

	// pretend device commit is success, but reporting failed
	s, c = cs.Handle(&stateTestController{
		reportError: NewFatalError(errors.New("report failed")),
	})
	// TODO: until backend has implemented status reporting, commit error cannot
	// result in update being aborted. Once required API endpoint is available,
	// the state should return an instance of UpdateErrorState
	assert.IsType(t, s, &UpdateCheckWaitState{})
	// assert.IsType(t, s, &UpdateErrorState{})
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
	assert.Equal(t, *update, ufs.update)
}

func TestStateUpdateFetch(t *testing.T) {
	// pretend we have an update
	update := &UpdateResponse{}
	cs := NewUpdateFetchState(*update)

	var s State
	var c bool

	// pretend update check failed
	s, c = cs.Handle(&stateTestController{
		updater: fakeUpdater{
			fetchUpdateReturnError: NewTransientError(errors.New("fetch failed")),
		},
	})
	assert.IsType(t, &UpdateErrorState{}, s)
	assert.False(t, c)

	data := "test"
	stream := ioutil.NopCloser(bytes.NewBufferString(data))

	sc := &stateTestController{
		updater: fakeUpdater{
			fetchUpdateReturnReadCloser: stream,
			fetchUpdateReturnSize:       int64(len(data)),
		},
	}
	s, c = cs.Handle(sc)
	assert.IsType(t, &UpdateInstallState{}, s)
	assert.False(t, c)
	assert.Equal(t, statusDownloading, sc.reportStatus)

	uis, _ := s.(*UpdateInstallState)
	assert.Equal(t, stream, uis.imagein)
	assert.Equal(t, int64(len(data)), uis.size)
}

func TestStateUpdateInstall(t *testing.T) {
	data := "test"
	stream := ioutil.NopCloser(bytes.NewBufferString(data))

	uis := NewUpdateInstallState(stream, int64(len(data)), UpdateResponse{})

	var s State
	var c bool

	// pretend update check failed
	s, c = uis.Handle(&stateTestController{
		fakeDevice: fakeDevice{
			retInstallUpdate: NewFatalError(errors.New("install failed")),
		},
	})
	assert.IsType(t, &UpdateErrorState{}, s)
	assert.False(t, c)

	s, c = uis.Handle(&stateTestController{
		fakeDevice: fakeDevice{
			retEnablePart: NewFatalError(errors.New("enable failed")),
		},
	})
	assert.IsType(t, &UpdateErrorState{}, s)
	assert.False(t, c)

	sc := &stateTestController{}
	s, c = uis.Handle(sc)
	assert.IsType(t, &RebootState{}, s)
	assert.False(t, c)
	assert.Equal(t, statusInstalling, sc.reportStatus)
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

	sc := &stateTestController{}
	s, c = rs.Handle(sc)
	assert.IsType(t, &FinalState{}, s)
	assert.False(t, c)
	assert.Equal(t, statusRebooting, sc.reportStatus)
}

func TestStateFinal(t *testing.T) {
	rs := FinalState{}

	assert.Panics(t, func() {
		rs.Handle(&stateTestController{})
	}, "final state Handle() should panic")
}
