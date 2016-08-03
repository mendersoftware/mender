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
	"os"
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
	logs          []byte
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

func (s *stateTestController) RunState(ctx *StateContext) (State, bool) {
	return s.state.Handle(ctx, s)
}

func (s *stateTestController) Authorize() menderError {
	return s.authorize
}

func (s *stateTestController) ReportUpdateStatus(update UpdateResponse, status string) menderError {
	s.reportUpdate = update
	s.reportStatus = status
	return s.reportError
}

func (s *stateTestController) UploadLog(update UpdateResponse, logs []byte) menderError {
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

	// same thing again, but calling Wait() now
	go func() {
		c := cs.Cancel()
		assert.True(t, c)
	}()
	// should finish right away
	tstart = time.Now()
	wc := cs.Wait(100 * time.Millisecond)
	tend = time.Now()
	assert.False(t, wc)
	assert.WithinDuration(t, tend, tstart, 5*time.Millisecond)

	// let wait finish
	tstart = time.Now()
	wc = cs.Wait(100 * time.Millisecond)
	tend = time.Now()
	assert.True(t, wc)
	assert.WithinDuration(t, tend, tstart, 105*time.Millisecond)
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

	update := UpdateResponse{
		ID: "foobar",
	}
	fooerr := NewTransientError(errors.New("foo"))

	es := NewUpdateErrorState(fooerr, update)
	assert.Equal(t, MenderStateUpdateError, es.Id())
	assert.IsType(t, &UpdateErrorState{}, es)
	errstate, _ := es.(*UpdateErrorState)
	assert.NotNil(t, errstate)
	assert.Equal(t, fooerr, errstate.cause)

	ms := NewMemStore()
	ctx := StateContext{
		store: ms,
	}
	sc := &stateTestController{}
	es = NewUpdateErrorState(fooerr, update)
	s, _ := es.Handle(&ctx, sc)
	assert.IsType(t, &UpdateStatusReportState{}, s)
	// verify that update status report state data is correct
	usr, _ := s.(*UpdateStatusReportState)
	assert.Equal(t, statusFailure, usr.status)
	assert.Equal(t, update, usr.update)
}

func TestStateUpdateReportStatus(t *testing.T) {
	update := UpdateResponse{
		ID: "foobar",
	}

	ms := NewMemStore()
	ctx := StateContext{
		store: ms,
	}
	sc := &stateTestController{}

	openLogFileWithContent("deployments.0001.foobar.log", `{ "time": "12:12:12", "level": "error", "msg": "log foo" }`)
	DeploymentLogger = NewDeploymentLogManager("")
	defer os.Remove("deployments.0001.foobar.log")

	usr := NewUpdateStatusReportState(update, statusFailure)
	usr.Handle(&ctx, sc)
	assert.Equal(t, statusFailure, sc.reportStatus)
	assert.Equal(t, update, sc.reportUpdate)

	assert.NotEmpty(t, sc.logs)
	assert.JSONEq(t, `{
	  "messages": [
	      {
	          "time": "12:12:12",
	          "level": "error",
	          "msg": "log foo"
	      }
	   ]}`, string(sc.logs))

	// once error has been reported, state data should be wiped
	_, err := ms.ReadAll(stateDataFileName)
	assert.True(t, os.IsNotExist(err))

	sc = &stateTestController{}
	usr = NewUpdateStatusReportState(update, statusSuccess)
	usr.Handle(&ctx, sc)
	assert.Equal(t, statusSuccess, sc.reportStatus)
	assert.Equal(t, update, sc.reportUpdate)
	// once error has been reported, state data should be wiped
	_, err = ms.ReadAll(stateDataFileName)
	assert.True(t, os.IsNotExist(err))

	// further tests are skipped as backend support is not present at the moment,
	// hence client implementation ignores reporting errors
	t.Skipf("skipping test due to workaround for missing backend functionality")

	// cancelled state should not wipe state data, for this pretend the reporting
	// fails and cancel
	sc = &stateTestController{
		pollIntvl:   5 * time.Second,
		reportError: NewTransientError(errors.New("report failed")),
	}
	usr = NewUpdateStatusReportState(update, statusSuccess)
	go func() {
		usr.Cancel()
	}()
	_, c := usr.Handle(&ctx, sc)
	// the state was canceled
	assert.True(t, c)
	// once error has been reported, state data should be wiped
	sd, err := LoadStateData(ms)
	assert.NoError(t, err)
	assert.Equal(t, update, sd.UpdateInfo)
	assert.Equal(t, statusSuccess, sd.UpdateStatus)
}

func TestStateInit(t *testing.T) {
	i := InitState{}

	var s State
	var c bool
	s, c = i.Handle(nil, &stateTestController{
		bootstrapErr: NewFatalError(errors.New("fake err")),
	})
	assert.IsType(t, &ErrorState{}, s)
	assert.False(t, c)

	s, c = i.Handle(nil, &stateTestController{})
	assert.IsType(t, &BootstrappedState{}, s)
	assert.False(t, c)
}

func TestStateBootstrapped(t *testing.T) {
	b := BootstrappedState{}

	var s State
	var c bool

	s, c = b.Handle(nil, &stateTestController{})
	assert.IsType(t, &AuthorizedState{}, s)
	assert.False(t, c)

	s, c = b.Handle(nil, &stateTestController{
		authorize: NewTransientError(errors.New("auth fail temp")),
	})
	assert.IsType(t, &AuthorizeWaitState{}, s)
	assert.False(t, c)

	s, c = b.Handle(nil, &stateTestController{
		authorize: NewFatalError(errors.New("upgrade err")),
	})
	assert.IsType(t, &ErrorState{}, s)
	assert.False(t, c)
}

func TestStateAuthorized(t *testing.T) {
	b := AuthorizedState{}

	var s State
	var c bool

	ms := NewMemStore()
	ctx := StateContext{
		store: ms,
	}
	s, c = b.Handle(&ctx, &stateTestController{
		hasUpgrade: false,
	})
	assert.IsType(t, &UpdateCheckWaitState{}, s)
	assert.False(t, c)

	// pretend we have state data
	update := UpdateResponse{
		ID: "foobar",
	}
	update.Image.YoctoID = "fakeid"

	StoreStateData(ms, StateData{
		Id:         MenderStateUpdateInstall,
		UpdateInfo: update,
	})
	// have state data and HasUpgrade() is true, have correct image ID
	s, c = b.Handle(&ctx, &stateTestController{
		hasUpgrade: true,
		imageID:    "fakeid",
	})
	assert.IsType(t, &UpdateCommitState{}, s)
	ucs := s.(*UpdateCommitState)
	assert.Equal(t, update, ucs.update)
	assert.False(t, c)

	// have state data and HasUpgrade() failed
	s, c = b.Handle(&ctx, &stateTestController{
		hasUpgradeErr: NewFatalError(errors.New("upgrade err")),
	})
	assert.IsType(t, &UpdateErrorState{}, s)
	ues := s.(*UpdateErrorState)
	assert.Equal(t, update, ues.update)
	assert.False(t, c)

	// error restoring state data
	ms.Disable(true)
	s, c = b.Handle(&ctx, &stateTestController{
		hasUpgrade: true,
	})
	assert.IsType(t, &UpdateErrorState{}, s)
	assert.False(t, c)
	ms.Disable(false)

	// pretend image id is different from expected
	s, c = b.Handle(&ctx, &stateTestController{
		hasUpgrade: true,
		imageID:    "not-fakeid",
	})
	assert.IsType(t, &UpdateErrorState{}, s)
	assert.False(t, c)

	// pretend we were trying to report status the last time, first check that
	// status is failure if UpdateStatus was not set when saving
	StoreStateData(ms, StateData{
		Id:         MenderStateUpdateStatusReport,
		UpdateInfo: update,
	})
	s, c = b.Handle(&ctx, &stateTestController{})
	assert.IsType(t, &UpdateStatusReportState{}, s)
	usr, _ := s.(*UpdateStatusReportState)
	assert.Equal(t, statusFailure, usr.status)
	assert.Equal(t, update, usr.update)

	// now pretend we were trying to report success
	StoreStateData(ms, StateData{
		Id:           MenderStateUpdateStatusReport,
		UpdateInfo:   update,
		UpdateStatus: statusSuccess,
	})
	s, c = b.Handle(&ctx, &stateTestController{})
	assert.IsType(t, &UpdateStatusReportState{}, s)
	usr, _ = s.(*UpdateStatusReportState)
	assert.Equal(t, statusSuccess, usr.status)
	assert.Equal(t, update, usr.update)

	// we should continue reporting even if have upgrade flag is set
	StoreStateData(ms, StateData{
		Id:           MenderStateUpdateStatusReport,
		UpdateInfo:   update,
		UpdateStatus: statusFailure,
	})
	s, c = b.Handle(&ctx, &stateTestController{
		hasUpgrade: true,
		imageID:    "fakeid",
	})
	assert.IsType(t, &UpdateStatusReportState{}, s)
}

func TestStateAuthorizeWait(t *testing.T) {
	cws := NewAuthorizeWaitState()

	var s State
	var c bool

	// no update
	var tstart, tend time.Time

	tstart = time.Now()
	s, c = cws.Handle(nil, &stateTestController{
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
	s, c = cws.Handle(nil, &stateTestController{
		pollIntvl: 100 * time.Millisecond,
	})
	tend = time.Now()
	// canceled state should return itself
	assert.IsType(t, &AuthorizeWaitState{}, s)
	assert.True(t, c)
	assert.WithinDuration(t, tend, tstart, 5*time.Millisecond)
}

func TestStateUpdateCommit(t *testing.T) {
	update := UpdateResponse{
		ID: "foobar",
	}
	cs := NewUpdateCommitState(update)

	var s State
	var c bool

	ms := NewMemStore()
	ctx := StateContext{
		store: ms,
	}
	StoreStateData(ms, StateData{
		UpdateInfo: update,
	})
	// commit without errors
	sc := &stateTestController{}
	s, c = cs.Handle(&ctx, sc)
	assert.IsType(t, &UpdateStatusReportState{}, s)
	assert.False(t, c)
	usr, _ := s.(*UpdateStatusReportState)
	assert.Equal(t, statusSuccess, usr.status)
	assert.Equal(t, update, usr.update)

	s, c = cs.Handle(&ctx, &stateTestController{
		fakeDevice: fakeDevice{
			retCommit: NewFatalError(errors.New("commit fail")),
		},
	})
	assert.IsType(t, s, &UpdateErrorState{})
	assert.False(t, c)
}

func TestStateUpdateCheckWait(t *testing.T) {
	cws := NewUpdateCheckWaitState()

	var s State
	var c bool

	// no update
	var tstart, tend time.Time

	tstart = time.Now()
	s, c = cws.Handle(nil, &stateTestController{
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
	s, c = cws.Handle(nil, &stateTestController{
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
	s, c = cs.Handle(nil, &stateTestController{})
	assert.IsType(t, &UpdateCheckWaitState{}, s)
	assert.False(t, c)

	// pretend update check failed
	s, c = cs.Handle(nil, &stateTestController{
		updateRespErr: NewTransientError(errors.New("check failed")),
	})
	assert.IsType(t, &ErrorState{}, s)
	assert.False(t, c)

	// pretend we have an update
	update := &UpdateResponse{}

	s, c = cs.Handle(nil, &stateTestController{
		updateResp: update,
	})
	assert.IsType(t, &UpdateFetchState{}, s)
	assert.False(t, c)
	ufs, _ := s.(*UpdateFetchState)
	assert.Equal(t, *update, ufs.update)
}

func TestStateUpdateFetch(t *testing.T) {
	// pretend we have an update
	update := UpdateResponse{
		ID: "foobar",
	}
	cs := NewUpdateFetchState(update)

	var s State
	var c bool

	ms := NewMemStore()
	ctx := StateContext{
		store: ms,
	}
	// pretend update check failed
	s, c = cs.Handle(&ctx, &stateTestController{
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
	s, c = cs.Handle(&ctx, sc)
	assert.IsType(t, &UpdateInstallState{}, s)
	assert.False(t, c)
	assert.Equal(t, statusDownloading, sc.reportStatus)
	assert.Equal(t, update, sc.reportUpdate)

	ud, err := LoadStateData(ms)
	assert.NoError(t, err)
	assert.Equal(t, StateData{
		UpdateInfo: update,
		Id:         MenderStateUpdateFetch,
	}, ud)

	uis, _ := s.(*UpdateInstallState)
	assert.Equal(t, stream, uis.imagein)
	assert.Equal(t, int64(len(data)), uis.size)

	ms.ReadOnly(true)
	// pretend writing update state data fails
	sc = &stateTestController{}
	s, c = uis.Handle(&ctx, sc)
	assert.IsType(t, &UpdateErrorState{}, s)
	ms.ReadOnly(false)

}

func TestStateUpdateInstall(t *testing.T) {
	data := "test"
	stream := ioutil.NopCloser(bytes.NewBufferString(data))

	update := UpdateResponse{
		ID: "foo",
	}
	uis := NewUpdateInstallState(stream, int64(len(data)), update)

	var s State
	var c bool

	ms := NewMemStore()
	ctx := StateContext{
		store: ms,
	}
	// pretend update check failed
	s, c = uis.Handle(&ctx, &stateTestController{
		fakeDevice: fakeDevice{
			retInstallUpdate: NewFatalError(errors.New("install failed")),
		},
	})
	assert.IsType(t, &UpdateErrorState{}, s)
	assert.False(t, c)

	s, c = uis.Handle(&ctx, &stateTestController{
		fakeDevice: fakeDevice{
			retEnablePart: NewFatalError(errors.New("enable failed")),
		},
	})
	assert.IsType(t, &UpdateErrorState{}, s)
	assert.False(t, c)

	s, c = uis.Handle(&ctx, &stateTestController{
		fakeDevice: fakeDevice{
			retEnablePart: NewFatalError(errors.New("enable failed")),
		},
	})
	assert.IsType(t, &UpdateErrorState{}, s)
	assert.False(t, c)

	ms.ReadOnly(true)
	// pretend writing update state data fails
	sc := &stateTestController{}
	s, c = uis.Handle(&ctx, sc)
	assert.IsType(t, &UpdateErrorState{}, s)
	ms.ReadOnly(false)

	sc = &stateTestController{}
	s, c = uis.Handle(&ctx, sc)
	assert.IsType(t, &RebootState{}, s)
	assert.False(t, c)
	assert.Equal(t, statusInstalling, sc.reportStatus)

	ud, err := LoadStateData(ms)
	assert.NoError(t, err)
	assert.Equal(t, StateData{
		UpdateInfo: update,
		Id:         MenderStateUpdateInstall,
	}, ud)
}

func TestStateReboot(t *testing.T) {
	update := UpdateResponse{
		ID: "foo",
	}
	rs := NewRebootState(update)

	var s State
	var c bool

	ms := NewMemStore()
	ctx := StateContext{
		store: ms,
	}
	s, c = rs.Handle(&ctx, &stateTestController{
		fakeDevice: fakeDevice{
			retReboot: NewFatalError(errors.New("reboot failed")),
		}})
	assert.IsType(t, &ErrorState{}, s)
	assert.False(t, c)

	sc := &stateTestController{}
	s, c = rs.Handle(&ctx, sc)
	assert.IsType(t, &FinalState{}, s)
	assert.False(t, c)
	assert.Equal(t, statusRebooting, sc.reportStatus)
	ud, err := LoadStateData(ms)
	assert.NoError(t, err)
	assert.Equal(t, StateData{
		UpdateInfo: update,
		Id:         MenderStateReboot,
	}, ud)

	ms.ReadOnly(true)
	// reboot will be performed regardless of failures to write update state data
	s, c = rs.Handle(&ctx, sc)
	assert.IsType(t, &FinalState{}, s)
	assert.False(t, c)
}

func TestStateFinal(t *testing.T) {
	rs := FinalState{}

	assert.Panics(t, func() {
		rs.Handle(nil, &stateTestController{})
	}, "final state Handle() should panic")
}

func TestStateData(t *testing.T) {
	ms := NewMemStore()
	sd := StateData{
		UpdateInfo: UpdateResponse{
			ID: "foobar",
		},
	}
	err := StoreStateData(ms, sd)
	assert.NoError(t, err)
	rsd, err := LoadStateData(ms)
	assert.NoError(t, err)
	assert.Equal(t, sd, rsd)

	ms.Remove(stateDataFileName)
	rsd, err = LoadStateData(ms)
	assert.Error(t, err)
	assert.True(t, os.IsNotExist(err))
}
