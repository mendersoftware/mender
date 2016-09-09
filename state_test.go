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
	"path"
	"testing"
	"time"

	"github.com/mendersoftware/mender/client"
	"github.com/stretchr/testify/assert"
)

type stateTestController struct {
	fakeDevice
	updater         fakeUpdater
	bootstrapErr    menderError
	imageID         string
	pollIntvl       time.Duration
	hasUpgrade      bool
	hasUpgradeErr   menderError
	state           State
	updateResp      *client.UpdateResponse
	updateRespErr   menderError
	authorize       menderError
	reportError     menderError
	logSendingError menderError
	reportStatus    string
	reportUpdate    client.UpdateResponse
	logUpdate       client.UpdateResponse
	logs            []byte
	inventoryErr    error
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

func (s *stateTestController) CheckUpdate() (*client.UpdateResponse, menderError) {
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

func (s *stateTestController) ReportUpdateStatus(update client.UpdateResponse, status string) menderError {
	s.reportUpdate = update
	s.reportStatus = status
	return s.reportError
}

func (s *stateTestController) UploadLog(update client.UpdateResponse, logs []byte) menderError {
	s.logUpdate = update
	s.logs = logs
	return s.logSendingError
}

func (s *stateTestController) InventoryRefresh() error {
	return s.inventoryErr
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
	s, c := es.Handle(nil, &stateTestController{})
	assert.IsType(t, &InitState{}, s)
	assert.False(t, c)

	es = NewErrorState(nil)
	errstate, _ = es.(*ErrorState)
	assert.NotNil(t, errstate)
	assert.Contains(t, errstate.cause.Error(), "general error")
	s, c = es.Handle(nil, &stateTestController{})
	assert.IsType(t, &FinalState{}, s)
	assert.False(t, c)
}

func TestStateUpdateError(t *testing.T) {

	update := client.UpdateResponse{
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
	assert.Equal(t, client.StatusError, usr.status)
	assert.Equal(t, update, usr.update)
}

func TestStateUpdateReportStatus(t *testing.T) {
	update := client.UpdateResponse{
		ID: "foobar",
	}

	ms := NewMemStore()
	ctx := StateContext{
		store: ms,
	}
	sc := &stateTestController{}

	// create directory for storing deployments logs
	tempDir, _ := ioutil.TempDir("", "logs")
	defer os.RemoveAll(tempDir)

	openLogFileWithContent(path.Join(tempDir, "deployments.0001.foobar.log"),
		`{ "time": "12:12:12", "level": "error", "msg": "log foo" }`)
	DeploymentLogger = NewDeploymentLogManager(tempDir)

	usr := NewUpdateStatusReportState(update, client.StatusFailure)
	usr.Handle(&ctx, sc)
	assert.Equal(t, client.StatusFailure, sc.reportStatus)
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

	// once error has been reported, state data should be wiped out
	_, err := ms.ReadAll(stateDataFileName)
	assert.True(t, os.IsNotExist(err))

	sc = &stateTestController{}
	usr = NewUpdateStatusReportState(update, client.StatusSuccess)
	usr.Handle(&ctx, sc)
	assert.Equal(t, client.StatusSuccess, sc.reportStatus)
	assert.Equal(t, update, sc.reportUpdate)
	// once error has been reported, state data should be wiped
	_, err = ms.ReadAll(stateDataFileName)
	assert.True(t, os.IsNotExist(err))

	// cancelled state should not wipe state data, for this pretend the reporting
	// fails and cancel
	sc = &stateTestController{
		pollIntvl:   5 * time.Second,
		reportError: NewTransientError(errors.New("report failed")),
	}
	usr = NewUpdateStatusReportState(update, client.StatusSuccess)
	go func() {
		usr.Cancel()
	}()
	s, c := usr.Handle(&ctx, sc)
	// the state was canceled
	assert.IsType(t, s, &UpdateStatusReportState{})
	assert.False(t, c)
	// once error has been reported, state data should be wiped
	sd, err := LoadStateData(ms)
	assert.NoError(t, err)
	assert.Equal(t, update, sd.UpdateInfo)
	assert.Equal(t, client.StatusSuccess, sd.UpdateStatus)

	old := maxReportSendingTime
	maxReportSendingTime = 2 * time.Second

	poll := 1 * time.Millisecond
	now1 := time.Now()
	// error sending status
	sc = &stateTestController{
		pollIntvl:   poll,
		reportError: NewTransientError(errors.New("test error sending status")),
	}
	s, c = usr.Handle(&ctx, sc)
	assert.IsType(t, s, &ReportErrorState{})
	assert.False(t, c)
	assert.WithinDuration(t, time.Now(), now1, 3*time.Second)
	assert.InDelta(t, int(maxReportSendingTime/poll),
		usr.(*UpdateStatusReportState).triesSendingReport, 100)

	// error sending logs
	now2 := time.Now()
	usr = NewUpdateStatusReportState(update, client.StatusFailure)
	sc = &stateTestController{
		pollIntvl:       poll,
		logSendingError: NewTransientError(errors.New("test error sending logs")),
	}
	s, c = usr.Handle(&ctx, sc)
	assert.IsType(t, s, &ReportErrorState{})
	assert.False(t, c)
	assert.WithinDuration(t, now2, time.Now(), 3*time.Second)

	maxReportSendingTime = old
}

func TestStateInit(t *testing.T) {
	i := InitState{}

	s, c := i.Handle(nil, &stateTestController{
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
	// create directory for storing deployments logs
	tempDir, _ := ioutil.TempDir("", "logs")
	defer os.RemoveAll(tempDir)
	DeploymentLogger = NewDeploymentLogManager(tempDir)

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
	assert.IsType(t, &InventoryUpdateState{}, s)
	assert.False(t, c)

	// pretend we have state data
	update := client.UpdateResponse{
		ID: "foobar",
	}
	update.Image.YoctoID = "fakeid"

	StoreStateData(ms, StateData{
		Id:         MenderStateReboot,
		UpdateInfo: update,
	})
	// have state data and have correct image ID
	s, c = b.Handle(&ctx, &stateTestController{
		imageID: "fakeid",
	})
	assert.IsType(t, &UpdateVerifyState{}, s)
	uvs := s.(*UpdateVerifyState)
	assert.Equal(t, update, uvs.update)
	assert.False(t, c)

	// error restoring state data
	ms.Disable(true)
	s, c = b.Handle(&ctx, &stateTestController{})
	assert.IsType(t, &UpdateErrorState{}, s)
	assert.False(t, c)
	ms.Disable(false)

	// pretend we were trying to report status the last time, first check that
	// status is failure if UpdateStatus was not set when saving
	StoreStateData(ms, StateData{
		Id:         MenderStateUpdateStatusReport,
		UpdateInfo: update,
	})
	s, c = b.Handle(&ctx, &stateTestController{})
	assert.IsType(t, &UpdateStatusReportState{}, s)
	usr := s.(*UpdateStatusReportState)
	assert.Equal(t, client.StatusError, usr.status)
	assert.Equal(t, update, usr.update)

	// now pretend we were trying to report success
	StoreStateData(ms, StateData{
		Id:           MenderStateUpdateStatusReport,
		UpdateInfo:   update,
		UpdateStatus: client.StatusSuccess,
	})
	s, c = b.Handle(&ctx, &stateTestController{})
	assert.IsType(t, &UpdateVerifyState{}, s)
	ver, _ := s.(*UpdateVerifyState)
	assert.Equal(t, update, ver.update)

	// pretend last update was interrupted
	StoreStateData(ms, StateData{
		Id:         MenderStateUpdateFetch,
		UpdateInfo: update,
	})
	s, c = b.Handle(&ctx, &stateTestController{})
	assert.IsType(t, &UpdateErrorState{}, s)
	use, _ := s.(*UpdateErrorState)
	assert.Equal(t, update, use.update)

	// pretend reading invalid state
	StoreStateData(ms, StateData{
		UpdateInfo: update,
	})
	s, c = b.Handle(&ctx, &stateTestController{})
	assert.IsType(t, &UpdateErrorState{}, s)
	use, _ = s.(*UpdateErrorState)
	assert.Equal(t, update, use.update)
}

func TestStateInvetoryUpdate(t *testing.T) {
	ius := inventoryUpdateState

	s, _ := ius.Handle(nil, &stateTestController{
		inventoryErr: errors.New("some err"),
	})
	assert.IsType(t, &UpdateCheckWaitState{}, s)

	s, _ = ius.Handle(nil, &stateTestController{})
	assert.IsType(t, &UpdateCheckWaitState{}, s)
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

func TestUpdateVerifyState(t *testing.T) {

	// create directory for storing deployments logs
	tempDir, _ := ioutil.TempDir("", "logs")
	defer os.RemoveAll(tempDir)
	DeploymentLogger = NewDeploymentLogManager(tempDir)

	// pretend we have state data
	update := client.UpdateResponse{
		ID: "foobar",
	}
	update.Image.YoctoID = "fakeid"

	uvs := UpdateVerifyState{
		update: update,
	}

	// HasUpgrade() failed
	s, c := uvs.Handle(nil, &stateTestController{
		hasUpgradeErr: NewFatalError(errors.New("upgrade err")),
	})
	assert.IsType(t, &UpdateErrorState{}, s)
	ues := s.(*UpdateErrorState)
	assert.Equal(t, update, ues.update)
	assert.False(t, c)

	// pretend image id is different from expected; rollback happened
	s, c = uvs.Handle(nil, &stateTestController{
		hasUpgrade: true,
		imageID:    "not-fakeid",
	})
	assert.IsType(t, &UpdateStatusReportState{}, s)
	assert.False(t, c)

	// image id is as expected; update was successful
	s, c = uvs.Handle(nil, &stateTestController{
		hasUpgrade: true,
		imageID:    "fakeid",
	})
	assert.IsType(t, &UpdateCommitState{}, s)
	assert.False(t, c)

	// we should continue reporting have upgrade flag is not set
	s, c = uvs.Handle(nil, &stateTestController{
		hasUpgrade: false,
		imageID:    "fakeid",
	})
	assert.IsType(t, &UpdateStatusReportState{}, s)
}

func TestStateUpdateCommit(t *testing.T) {
	// create directory for storing deployments logs
	tempDir, _ := ioutil.TempDir("", "logs")
	defer os.RemoveAll(tempDir)
	DeploymentLogger = NewDeploymentLogManager(tempDir)

	update := client.UpdateResponse{
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
	assert.Equal(t, update, usr.update)
	assert.Equal(t, client.StatusSuccess, usr.status)

	s, c = cs.Handle(&ctx, &stateTestController{
		fakeDevice: fakeDevice{
			retCommit: NewFatalError(errors.New("commit fail")),
		},
	})
	assert.IsType(t, s, &UpdateStatusReportState{})
	assert.False(t, c)
	usr, _ = s.(*UpdateStatusReportState)
	assert.Equal(t, update, usr.update)
	assert.Equal(t, client.StatusFailure, usr.status)
}

func TestStateUpdateCheckWait(t *testing.T) {
	cws := NewUpdateCheckWaitState()

	// no update
	var tstart, tend time.Time

	tstart = time.Now()
	s, c := cws.Handle(nil, &stateTestController{
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
	assert.IsType(t, &InventoryUpdateState{}, s)
	assert.False(t, c)

	// pretend update check failed
	s, c = cs.Handle(nil, &stateTestController{
		updateRespErr: NewTransientError(errors.New("check failed")),
	})
	assert.IsType(t, &ErrorState{}, s)
	assert.False(t, c)

	// pretend we have an update
	update := &client.UpdateResponse{}

	s, c = cs.Handle(nil, &stateTestController{
		updateResp: update,
	})
	assert.IsType(t, &UpdateFetchState{}, s)
	assert.False(t, c)
	ufs, _ := s.(*UpdateFetchState)
	assert.Equal(t, *update, ufs.update)
}

func TestUpdateCheckSameImage(t *testing.T) {
	cs := UpdateCheckState{}

	var s State
	var c bool

	// pretend we have an update
	update := &client.UpdateResponse{
		ID: "my-id",
	}

	s, c = cs.Handle(nil, &stateTestController{
		updateResp:    update,
		updateRespErr: NewTransientError(os.ErrExist),
	})
	assert.IsType(t, &UpdateStatusReportState{}, s)
	assert.False(t, c)
	urs, _ := s.(*UpdateStatusReportState)
	assert.Equal(t, *update, urs.update)
	assert.Equal(t, client.StatusSuccess, urs.status)
}

func TestStateUpdateFetch(t *testing.T) {
	// create directory for storing deployments logs
	tempDir, _ := ioutil.TempDir("", "logs")
	defer os.RemoveAll(tempDir)
	DeploymentLogger = NewDeploymentLogManager(tempDir)

	// pretend we have an update
	update := client.UpdateResponse{
		ID: "foobar",
	}
	cs := NewUpdateFetchState(update)

	ms := NewMemStore()
	ctx := StateContext{
		store: ms,
	}

	// can not store state data
	ms.ReadOnly(true)
	s, c := cs.Handle(&ctx, &stateTestController{})
	assert.IsType(t, &UpdateErrorState{}, s)
	assert.False(t, c)
	ms.ReadOnly(false)

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
	assert.Equal(t, client.StatusDownloading, sc.reportStatus)
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

	update := client.UpdateResponse{
		ID: "foo",
	}
	uis := NewUpdateInstallState(stream, int64(len(data)), update)

	ms := NewMemStore()
	ctx := StateContext{
		store: ms,
	}
	// pretend update check failed
	s, c := uis.Handle(&ctx, &stateTestController{
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
	assert.Equal(t, client.StatusInstalling, sc.reportStatus)

	ud, err := LoadStateData(ms)
	assert.NoError(t, err)
	assert.Equal(t, StateData{
		UpdateInfo: update,
		Id:         MenderStateUpdateInstall,
	}, ud)
}

func TestStateReboot(t *testing.T) {
	update := client.UpdateResponse{
		ID: "foo",
	}
	rs := NewRebootState(update)

	// create directory for storing deployments logs
	tempDir, _ := ioutil.TempDir("", "logs")
	defer os.RemoveAll(tempDir)
	DeploymentLogger = NewDeploymentLogManager(tempDir)

	ms := NewMemStore()
	ctx := StateContext{
		store: ms,
	}
	s, c := rs.Handle(&ctx, &stateTestController{
		fakeDevice: fakeDevice{
			retReboot: NewFatalError(errors.New("reboot failed")),
		}})
	assert.IsType(t, &ErrorState{}, s)
	assert.False(t, c)

	sc := &stateTestController{}
	s, c = rs.Handle(&ctx, sc)
	assert.IsType(t, &FinalState{}, s)
	assert.False(t, c)
	assert.Equal(t, client.StatusRebooting, sc.reportStatus)
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
func TestStateRollback(t *testing.T) {
	update := client.UpdateResponse{
		ID: "foo",
	}
	rs := NewRollbackState(update)

	// create directory for storing deployments logs
	tempDir, _ := ioutil.TempDir("", "logs")
	defer os.RemoveAll(tempDir)
	DeploymentLogger = NewDeploymentLogManager(tempDir)

	s, c := rs.Handle(nil, &stateTestController{
		fakeDevice: fakeDevice{
			retRollback: NewFatalError(errors.New("rollback failed")),
		}})
	assert.IsType(t, &ErrorState{}, s)
	assert.False(t, c)

	s, c = rs.Handle(nil, &stateTestController{})
	assert.IsType(t, &RebootState{}, s)
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
		UpdateInfo: client.UpdateResponse{
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
