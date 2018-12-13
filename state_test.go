// Copyright 2018 Northern.tech AS
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

	"github.com/mendersoftware/log"
	"github.com/mendersoftware/mender/client"
	"github.com/mendersoftware/mender/store"
	"github.com/stretchr/testify/assert"
)

type stateTestController struct {
	fakeDevice
	updater         fakeUpdater
	artifactName    string
	pollIntvl       time.Duration
	retryIntvl      time.Duration
	hasUpgrade      bool
	hasUpgradeErr   menderError
	state           State
	updateResp      *client.UpdateResponse
	updateRespErr   menderError
	authorized      bool
	authorizeErr    menderError
	reportError     menderError
	logSendingError menderError
	reportStatus    string
	reportUpdate    client.UpdateResponse
	logUpdate       client.UpdateResponse
	logs            []byte
	inventoryErr    error
}

func (s *stateTestController) GetCurrentArtifactName() (string, error) {
	if s.artifactName == "" {
		return "", errors.New("open ..., no such file or directory")
	}
	return s.artifactName, nil
}

func (s *stateTestController) GetUpdatePollInterval() time.Duration {
	return s.pollIntvl
}

func (s *stateTestController) GetInventoryPollInterval() time.Duration {
	return s.pollIntvl
}

func (s *stateTestController) GetRetryPollInterval() time.Duration {
	return s.retryIntvl
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

func (s *stateTestController) GetCurrentState() State {
	return s.state
}

func (s *stateTestController) SetNextState(state State) {
	s.state = state
}

func (s *stateTestController) TransitionState(next State, ctx *StateContext) (State, bool) {
	next, cancel := s.state.Handle(ctx, s)
	s.state = next
	return next, cancel
}

func (s *stateTestController) Authorize() menderError {
	return s.authorizeErr
}

func (s *stateTestController) IsAuthorized() bool {
	return s.authorized
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

func (s *stateTestController) CheckScriptsCompatibility() error {
	return nil
}

type waitStateTest struct {
	baseState
}

func (c *waitStateTest) Wait(next, same State, wait time.Duration) (State, bool) {
	log.Debugf("Fake waiting for %f seconds, going from state %s to state %s",
		wait.Seconds(), same.Id(), next.Id())
	return next, false
}

func (c *waitStateTest) Wake() bool {
	return true // Dummy.
}

func (c *waitStateTest) Stop() {
	// Noop for now.
}

func (c *waitStateTest) Handle(*StateContext, Controller) (State, bool) {
	return c, false
}

func TestStateBase(t *testing.T) {
	bs := baseState{
		id: MenderStateInit,
	}

	assert.Equal(t, MenderStateInit, bs.Id())
	assert.False(t, bs.Cancel())
}

func TestStateWait(t *testing.T) {
	cs := NewWaitState(MenderStateAuthorizeWait, ToNone)

	assert.Equal(t, MenderStateAuthorizeWait, cs.Id())

	var s State
	var c bool

	// no update
	var tstart, tend time.Time

	tstart = time.Now()
	s, c = cs.Wait(authorizeState, authorizeWaitState, 100*time.Millisecond)
	tend = time.Now()
	// not cancelled should return the 'next' state
	assert.Equal(t, authorizeState, s)
	assert.False(t, c)
	assert.WithinDuration(t, tend, tstart, 105*time.Millisecond)

	// asynchronously cancel state operation
	go func() {
		ch := cs.Cancel()
		assert.True(t, ch)
	}()
	// should finish right away
	tstart = time.Now()
	s, c = cs.Wait(authorizeState, authorizeWaitState, 100*time.Millisecond)
	tend = time.Now()
	// canceled should return the same state
	assert.Equal(t, authorizeWaitState, s)
	assert.True(t, c)
	assert.WithinDuration(t, tend, tstart, 5*time.Millisecond)
	// Force wake from sleep and continue execution.
	s, c = cs.Wait(authorizeState, authorizeWaitState, 10*time.Second)
	go func() {
		assert.False(t, cs.Wake())
	}()
	// Wake should return the next state
	assert.Equal(t, authorizeState, s)
	assert.False(t, c)
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
	assert.IsType(t, &IdleState{}, s)
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

	ms := store.NewMemStore()
	ctx := StateContext{
		store: ms,
	}
	sc := &stateTestController{}
	es = NewUpdateErrorState(fooerr, update)
	s, _ := es.Handle(&ctx, sc)
	assert.IsType(t, &UpdateStatusReportState{}, s)
	// verify that update status report state data is correct
	usr, _ := s.(*UpdateStatusReportState)
	assert.Equal(t, client.StatusFailure, usr.status)
	assert.Equal(t, update, usr.Update())
}

func TestStateUpdateReportStatus(t *testing.T) {
	update := client.UpdateResponse{
		ID: "foobar",
	}

	ms := store.NewMemStore()
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

	sc = &stateTestController{}
	usr = NewUpdateStatusReportState(update, client.StatusSuccess)
	usr.Handle(&ctx, sc)
	assert.Equal(t, client.StatusSuccess, sc.reportStatus)
	assert.Equal(t, update, sc.reportUpdate)

	// cancelled state should not wipe state data, for this pretend the reporting
	// fails and cancel
	sc = &stateTestController{
		reportError: NewTransientError(errors.New("report failed")),
	}
	usr = NewUpdateStatusReportState(update, client.StatusSuccess)
	s, c := usr.Handle(&ctx, sc)
	// there was an error while reporting status; we should retry
	assert.IsType(t, s, &UpdateStatusReportRetryState{})
	assert.False(t, c)
	sd, err := LoadStateData(ms)
	assert.NoError(t, err)
	assert.Equal(t, update, sd.UpdateInfo)
	assert.Equal(t, client.StatusSuccess, sd.UpdateStatus)

	poll := 5 * time.Millisecond
	retry := 1 * time.Millisecond
	// error sending status
	sc = &stateTestController{
		pollIntvl:   poll,
		retryIntvl:  retry,
		reportError: NewTransientError(errors.New("test error sending status")),
	}

	shouldTry := maxSendingAttempts(poll, retry, minReportSendRetries)
	s = NewUpdateStatusReportState(update, client.StatusSuccess)

	now := time.Now()

	for i := 0; i < shouldTry; i++ {
		s, c = s.Handle(&ctx, sc)
		assert.IsType(t, &UpdateStatusReportRetryState{}, s)
		assert.False(t, c)

		s, c = s.Handle(&ctx, sc)
		assert.IsType(t, &UpdateStatusReportState{}, s)
		assert.False(t, c)
	}
	assert.WithinDuration(t, now, time.Now(), time.Duration(int64(shouldTry)*int64(retry))+time.Millisecond*10)

	// next attempt should return an error
	s, c = s.Handle(&ctx, sc)
	assert.IsType(t, &UpdateStatusReportRetryState{}, s)
	s, c = s.Handle(&ctx, sc)
	assert.IsType(t, &ReportErrorState{}, s)
	assert.False(t, c)

	// error sending logs
	sc = &stateTestController{
		pollIntvl:       poll,
		retryIntvl:      retry,
		logSendingError: NewTransientError(errors.New("test error sending logs")),
	}
	s = NewUpdateStatusReportState(update, client.StatusFailure)

	now = time.Now()
	for i := 0; i < shouldTry; i++ {
		s, c = s.Handle(&ctx, sc)
		assert.IsType(t, &UpdateStatusReportRetryState{}, s)
		assert.False(t, c)

		s, c = s.Handle(&ctx, sc)
		assert.IsType(t, &UpdateStatusReportState{}, s)
		assert.True(t, s.(*UpdateStatusReportState).reportSent)
		assert.False(t, c)
	}
	assert.WithinDuration(t, now, time.Now(), time.Duration(int64(shouldTry)*int64(retry))+time.Millisecond*10)

	s, c = s.Handle(&ctx, sc)
	assert.IsType(t, &UpdateStatusReportRetryState{}, s)
	s, c = s.Handle(&ctx, sc)
	assert.IsType(t, s, &ReportErrorState{})
	assert.False(t, c)

	// pretend update was aborted at the backend, but was applied
	// successfully on the device
	usr = NewUpdateStatusReportState(update, client.StatusSuccess)
	sc = &stateTestController{
		reportError: NewFatalError(client.ErrDeploymentAborted),
	}
	s, c = usr.Handle(&ctx, sc)
	assert.IsType(t, &ReportErrorState{}, s)

	// pretend update was aborted at the backend, along with local failure
	usr = NewUpdateStatusReportState(update, client.StatusFailure)
	sc = &stateTestController{
		reportError: NewFatalError(client.ErrDeploymentAborted),
	}
	s, c = usr.Handle(&ctx, sc)
	assert.IsType(t, &ReportErrorState{}, s)
}

func TestStateIdle(t *testing.T) {
	i := IdleState{}

	s, c := i.Handle(&StateContext{}, &stateTestController{
		authorized: false,
	})
	assert.IsType(t, &AuthorizeState{}, s)
	assert.False(t, c)

	s, c = i.Handle(&StateContext{}, &stateTestController{
		authorized: true,
	})
	assert.IsType(t, &CheckWaitState{}, s)
	assert.False(t, c)
}

func TestStateInit(t *testing.T) {
	// create directory for storing deployments logs
	tempDir, _ := ioutil.TempDir("", "logs")
	defer os.RemoveAll(tempDir)
	DeploymentLogger = NewDeploymentLogManager(tempDir)

	i := InitState{}

	var s State
	var c bool

	ms := store.NewMemStore()
	ctx := StateContext{
		store: ms,
	}
	s, c = i.Handle(&ctx, &stateTestController{
		hasUpgrade: false,
	})
	assert.IsType(t, &IdleState{}, s)
	assert.False(t, c)

	// pretend we have state data
	update := client.UpdateResponse{
		ID: "foobar",
	}
	update.Artifact.ArtifactName = "fakeid"

	StoreStateData(ms, StateData{
		Name:       MenderStateReboot,
		UpdateInfo: update,
	})
	// have state data and have correct artifact name
	s, c = i.Handle(&ctx, &stateTestController{
		artifactName: "fakeid",
		hasUpgrade:   true,
	})
	assert.IsType(t, &AfterRebootState{}, s)
	uvs := s.(*AfterRebootState)
	assert.Equal(t, update, uvs.Update())
	assert.False(t, c)

	// error restoring state data
	ms.Disable(true)
	s, c = i.Handle(&ctx, &stateTestController{})
	assert.IsType(t, &UpdateErrorState{}, s)
	assert.False(t, c)
	ms.Disable(false)

	// pretend reading invalid state
	StoreStateData(ms, StateData{
		UpdateInfo: update,
	})
	s, c = i.Handle(&ctx, &stateTestController{hasUpgrade: false})
	assert.IsType(t, &UpdateErrorState{}, s)
	use, _ := s.(*UpdateErrorState)
	assert.Equal(t, update, use.update)

	// update-commit-leave behaviour
	StoreStateData(ms, StateData{
		UpdateInfo: update,
		Name:       MenderStateUpdateCommit,
	})
	ctrl := &stateTestController{hasUpgrade: false}
	s, c = i.Handle(&ctx, ctrl)
	assert.IsType(t, &IdleState{}, s)
	assert.False(t, c)
	assert.IsType(t, ToArtifactCommit, ctrl.GetCurrentState().Transition())

}

func TestStateAuthorize(t *testing.T) {
	a := AuthorizeState{}
	s, c := a.Handle(nil, &stateTestController{})
	assert.IsType(t, &CheckWaitState{}, s)
	assert.False(t, c)

	s, c = a.Handle(nil, &stateTestController{
		authorizeErr: NewTransientError(errors.New("auth fail temp")),
	})
	assert.IsType(t, &AuthorizeWaitState{}, s)
	assert.False(t, c)

	s, c = a.Handle(nil, &stateTestController{
		authorizeErr: NewFatalError(errors.New("auth error")),
	})
	assert.IsType(t, &ErrorState{}, s)
	assert.False(t, c)
}

func TestStateInvetoryUpdate(t *testing.T) {
	ius := inventoryUpdateState
	ctx := new(StateContext)

	s, _ := ius.Handle(ctx, &stateTestController{
		inventoryErr: errors.New("some err"),
	})
	assert.IsType(t, &CheckWaitState{}, s)

	s, _ = ius.Handle(ctx, &stateTestController{})
	assert.IsType(t, &CheckWaitState{}, s)

	// no artifact name should fail
	s, _ = ius.Handle(ctx, &stateTestController{
		inventoryErr: errNoArtifactName,
	})

	assert.IsType(t, &ErrorState{}, s)

}

func TestStateAuthorizeWait(t *testing.T) {
	cws := NewAuthorizeWaitState()

	var s State
	var c bool
	ctx := new(StateContext)

	// no update
	var tstart, tend time.Time

	tstart = time.Now()
	s, c = cws.Handle(ctx, &stateTestController{
		retryIntvl: 100 * time.Millisecond,
	})
	tend = time.Now()
	assert.IsType(t, &AuthorizeState{}, s)
	assert.False(t, c)
	assert.WithinDuration(t, tend, tstart, 105*time.Millisecond)

	// asynchronously cancel state operation
	go func() {
		ch := cws.Cancel()
		assert.True(t, ch)
	}()
	// should finish right away
	tstart = time.Now()
	s, c = cws.Handle(ctx, &stateTestController{
		retryIntvl: 100 * time.Millisecond,
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
	update.Artifact.ArtifactName = "fakeid"

	s := NewUpdateVerifyState(update)
	_, ok := s.(UpdateState)
	assert.True(t, ok)

	uvs := UpdateVerifyState{
		UpdateState: NewUpdateState(MenderStateUpdateVerify, ToNone, update),
	}

	// HasUpgrade() failed
	s, c := uvs.Handle(nil, &stateTestController{
		hasUpgradeErr: NewFatalError(errors.New("upgrade err")),
	})
	assert.IsType(t, &UpdateErrorState{}, s)
	ues := s.(*UpdateErrorState)
	assert.Equal(t, update, ues.update)
	assert.False(t, c)

	// pretend artifact name is different from expected; rollback happened
	s, c = uvs.Handle(nil, &stateTestController{
		hasUpgrade:   true,
		artifactName: "not-fakeid",
	})
	assert.IsType(t, &UpdateCommitState{}, s)
	assert.False(t, c)

	// Test upgrade available and no artifact-file found
	s, c = uvs.Handle(nil, &stateTestController{
		hasUpgrade: true,
	})
	assert.IsType(t, &UpdateCommitState{}, s)
	assert.False(t, c)

	// artifact name is as expected; update was successful
	s, c = uvs.Handle(nil, &stateTestController{
		hasUpgrade:   true,
		artifactName: "fakeid",
	})
	assert.IsType(t, &UpdateCommitState{}, s)
	assert.False(t, c)

	// we should continue reporting have upgrade flag is not set
	s, _ = uvs.Handle(nil, &stateTestController{
		hasUpgrade:   false,
		artifactName: "fakeid",
	})
	assert.IsType(t, &RollbackState{}, s)
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

	ms := store.NewMemStore()
	ctx := StateContext{
		store: ms,
	}
	StoreStateData(ms, StateData{
		UpdateInfo: update,
	})
	// commit without errors
	sc := &stateTestController{}
	s, c = cs.Handle(&ctx, sc)
	assert.IsType(t, &RollbackState{}, s)
	assert.False(t, c)
	usr, _ := s.(*RollbackState)
	assert.Equal(t, update, usr.Update())

	s, c = cs.Handle(&ctx, &stateTestController{
		fakeDevice: fakeDevice{
			retCommit: NewFatalError(errors.New("commit fail")),
		},
	})
	assert.IsType(t, s, &RollbackState{})
	assert.False(t, c)
	rs, _ := s.(*RollbackState)
	assert.Equal(t, update, rs.Update())
}

func TestStateUpdateCheckWait(t *testing.T) {
	cws := NewCheckWaitState()
	ctx := new(StateContext)

	// no iventory was sent; we should first send inventory
	var tstart, tend time.Time
	tstart = time.Now()
	s, c := cws.Handle(ctx, &stateTestController{
		pollIntvl: 10 * time.Millisecond,
	})

	tend = time.Now()
	assert.IsType(t, &InventoryUpdateState{}, s)
	assert.False(t, c)
	assert.WithinDuration(t, tend, tstart, 15*time.Millisecond)

	// now we have inventory sent; should send update request
	ctx.lastInventoryUpdateAttempt = tend
	ctx.lastUpdateCheckAttempt = tend
	tstart = time.Now()
	s, c = cws.Handle(ctx, &stateTestController{
		pollIntvl: 10 * time.Millisecond,
	})
	tend = time.Now()
	assert.IsType(t, &UpdateCheckState{}, s)
	assert.False(t, c)
	assert.WithinDuration(t, tend, tstart, 15*time.Millisecond)

	// asynchronously cancel state operation
	go func() {
		ch := cws.Cancel()
		assert.True(t, ch)
	}()
	// should finish right away
	tstart = time.Now()
	s, c = cws.Handle(ctx, &stateTestController{
		pollIntvl: 100 * time.Millisecond,
	})
	tend = time.Now()
	// canceled state should return itself
	assert.IsType(t, &CheckWaitState{}, s)
	assert.True(t, c)
	assert.WithinDuration(t, tend, tstart, 5*time.Millisecond)
}

func TestStateUpdateCheck(t *testing.T) {
	cs := UpdateCheckState{}
	ctx := new(StateContext)

	var s State
	var c bool

	// no update
	s, c = cs.Handle(ctx, &stateTestController{})
	assert.IsType(t, &CheckWaitState{}, s)
	assert.False(t, c)

	// pretend update check failed
	s, c = cs.Handle(ctx, &stateTestController{
		updateRespErr: NewTransientError(errors.New("check failed")),
	})
	assert.IsType(t, &ErrorState{}, s)
	assert.False(t, c)

	// pretend we have an update
	update := &client.UpdateResponse{}

	s, c = cs.Handle(ctx, &stateTestController{
		updateResp: update,
	})
	assert.IsType(t, &UpdateFetchState{}, s)
	assert.False(t, c)
	ufs, _ := s.(*UpdateFetchState)
	assert.Equal(t, *update, ufs.update)
}

func TestUpdateCheckSameImage(t *testing.T) {
	cs := UpdateCheckState{}
	ctx := new(StateContext)

	var s State
	var c bool

	// pretend we have an update
	update := &client.UpdateResponse{
		ID: "my-id",
	}

	s, c = cs.Handle(ctx, &stateTestController{
		updateResp:    update,
		updateRespErr: NewTransientError(os.ErrExist),
	})
	assert.IsType(t, &UpdateStatusReportState{}, s)
	assert.False(t, c)
	urs, _ := s.(*UpdateStatusReportState)
	assert.Equal(t, *update, urs.Update())
	assert.Equal(t, client.StatusAlreadyInstalled, urs.status)
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

	ms := store.NewMemStore()
	ctx := StateContext{
		store: ms,
	}

	// can not store state data
	ms.ReadOnly(true)
	s, c := cs.Handle(&ctx, &stateTestController{})
	assert.IsType(t, &UpdateStatusReportState{}, s)
	assert.False(t, c)
	ms.ReadOnly(false)

	data := "test"
	stream := ioutil.NopCloser(bytes.NewBufferString(data))

	sc := &stateTestController{
		updater: fakeUpdater{
			fetchUpdateReturnReadCloser: stream,
			fetchUpdateReturnSize:       int64(len(data)),
		},
	}
	s, c = cs.Handle(&ctx, sc)
	assert.IsType(t, &UpdateStoreState{}, s)
	assert.False(t, c)
	assert.Equal(t, client.StatusDownloading, sc.reportStatus)
	assert.Equal(t, update, sc.reportUpdate)

	ud, err := LoadStateData(ms)
	assert.NoError(t, err)
	assert.Equal(t, StateData{
		Version:    stateDataVersion,
		UpdateInfo: update,
		Name:       MenderStateUpdateFetch,
	}, ud)

	uis, _ := s.(*UpdateStoreState)
	assert.Equal(t, stream, uis.imagein)
	assert.Equal(t, int64(len(data)), uis.size)

	ms.ReadOnly(true)
	// pretend writing update state data fails
	sc = &stateTestController{}
	s, c = uis.Handle(&ctx, sc)
	assert.IsType(t, &UpdateStatusReportState{}, s)
	ms.ReadOnly(false)

	// pretend update was aborted
	sc = &stateTestController{
		reportError: NewFatalError(client.ErrDeploymentAborted),
	}
	s, c = uis.Handle(&ctx, sc)
	assert.IsType(t, &UpdateStatusReportState{}, s)
}

func TestStateUpdateFetchRetry(t *testing.T) {
	// pretend we have an update
	update := client.UpdateResponse{
		ID: "foobar",
	}
	cs := NewUpdateFetchState(update)
	ms := store.NewMemStore()
	ctx := StateContext{
		store: ms,
	}
	stc := stateTestController{
		updater: fakeUpdater{
			fetchUpdateReturnError: NewTransientError(errors.New("fetch failed")),
		},
		pollIntvl: 5 * time.Minute,
	}

	// pretend update check failed
	s, c := cs.Handle(&ctx, &stc)
	assert.IsType(t, &FetchStoreRetryState{}, s)
	assert.False(t, c)

	// Test for the twelve expected attempts:
	// (1m*3) + (2m*3) + (4m*3) + (5m*3)
	for i := 0; i < 12; i++ {
		s.(*FetchStoreRetryState).WaitState = &waitStateTest{}

		s, c = s.Handle(&ctx, &stc)
		assert.IsType(t, &UpdateFetchState{}, s)
		assert.False(t, c)

		s, c = s.Handle(&ctx, &stc)
		assert.IsType(t, &FetchStoreRetryState{}, s)
		assert.False(t, c)
	}

	// Final attempt should fail completely.
	s.(*FetchStoreRetryState).WaitState = &waitStateTest{baseState{
		id: MenderStateCheckWait,
	}}

	s, c = s.Handle(&ctx, &stc)
	assert.IsType(t, &UpdateStatusReportState{}, s)
	assert.False(t, c)
}

func TestStateUpdateStore(t *testing.T) {
	// create directory for storing deployments logs
	tempDir, _ := ioutil.TempDir("", "logs")
	defer os.RemoveAll(tempDir)
	DeploymentLogger = NewDeploymentLogManager(tempDir)

	data := "test"
	stream := ioutil.NopCloser(bytes.NewBufferString(data))

	update := client.UpdateResponse{
		ID: "foo",
	}
	uis := NewUpdateStoreState(stream, int64(len(data)), update)

	ms := store.NewMemStore()
	ctx := StateContext{
		store: ms,
	}

	ms.ReadOnly(true)
	// pretend writing update state data fails
	sc := &stateTestController{}
	s, c := uis.Handle(&ctx, sc)
	assert.IsType(t, &UpdateStatusReportState{}, s)
	ms.ReadOnly(false)

	sc = &stateTestController{}
	s, c = uis.Handle(&ctx, sc)
	assert.IsType(t, &UpdateInstallState{}, s)
	assert.False(t, c)
	assert.Equal(t, client.StatusDownloading, sc.reportStatus)

	ud, err := LoadStateData(ms)
	assert.NoError(t, err)
	assert.Equal(t, StateData{
		Version:    stateDataVersion,
		UpdateInfo: update,
		Name:       MenderStateUpdateStore,
	}, ud)

	// pretend update was aborted
	sc = &stateTestController{
		reportError: NewFatalError(client.ErrDeploymentAborted),
	}
	s, c = uis.Handle(&ctx, sc)
	assert.IsType(t, &UpdateStatusReportState{}, s)
}

func TestStateUpdateInstallRetry(t *testing.T) {
	// create directory for storing deployments logs
	tempDir, _ := ioutil.TempDir("", "logs")
	defer os.RemoveAll(tempDir)
	DeploymentLogger = NewDeploymentLogManager(tempDir)

	update := client.UpdateResponse{
		ID: "foo",
	}
	data := "test"
	stream := ioutil.NopCloser(bytes.NewBufferString(data))
	uis := NewUpdateStoreState(stream, int64(len(data)), update)
	ms := store.NewMemStore()
	ctx := StateContext{
		store: ms,
	}
	stc := stateTestController{
		fakeDevice: fakeDevice{
			retInstallUpdate: NewFatalError(errors.New("install failed")),
		},
		pollIntvl: 5 * time.Minute,
	}

	// pretend update check failed
	s, c := uis.Handle(&ctx, &stc)
	assert.IsType(t, &FetchStoreRetryState{}, s)
	assert.False(t, c)

	// Test for the twelve expected attempts:
	// (1m*3) + (2m*3) + (4m*3) + (5m*3)
	for i := 0; i < 12; i++ {
		s.(*FetchStoreRetryState).WaitState = &waitStateTest{baseState{
			id: MenderStateCheckWait,
		}}

		s, c = s.Handle(&ctx, &stc)
		assert.IsType(t, &UpdateFetchState{}, s)
		assert.False(t, c)

		s, c = s.Handle(&ctx, &stc)
		assert.IsType(t, &UpdateStoreState{}, s)
		assert.False(t, c)

		// Reset data stream to something that can be closed.
		stream = ioutil.NopCloser(bytes.NewBufferString(data))
		s.(*UpdateStoreState).imagein = stream

		s, c = s.Handle(&ctx, &stc)
		assert.IsType(t, &FetchStoreRetryState{}, s)
		assert.False(t, c)
	}

	// Final attempt should fail completely.
	s.(*FetchStoreRetryState).WaitState = &waitStateTest{baseState{
		id: MenderStateCheckWait,
	}}

	s, c = s.Handle(&ctx, &stc)
	assert.IsType(t, &UpdateStatusReportState{}, s)
	assert.False(t, c)
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

	ctx := StateContext{}
	s, c := rs.Handle(&ctx, &stateTestController{
		fakeDevice: fakeDevice{
			retReboot: NewFatalError(errors.New("reboot failed")),
		}})
	assert.IsType(t, &RollbackState{}, s)
	assert.False(t, c)

	sc := &stateTestController{}
	s, c = rs.Handle(&ctx, sc)
	assert.IsType(t, &FinalState{}, s)
	assert.False(t, c)
	assert.Equal(t, client.StatusRebooting, sc.reportStatus)
	// reboot will be performed regardless of failures to write update state data
	s, c = rs.Handle(&ctx, sc)
	assert.IsType(t, &FinalState{}, s)
	assert.False(t, c)

	// pretend update was aborted
	sc = &stateTestController{
		reportError: NewFatalError(client.ErrDeploymentAborted),
	}
	s, c = rs.Handle(&ctx, sc)
	assert.IsType(t, &RollbackState{}, s)
}

func TestStateRollback(t *testing.T) {
	update := client.UpdateResponse{
		ID: "foo",
	}
	rs := NewRollbackState(update, true, false)

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
	assert.IsType(t, &UpdateErrorState{}, s)
	assert.False(t, c)
}

func TestStateFinal(t *testing.T) {
	rs := FinalState{}

	assert.Panics(t, func() {
		rs.Handle(nil, &stateTestController{})
	}, "final state Handle() should panic")
}

func TestStateData(t *testing.T) {
	ms := store.NewMemStore()
	sd := StateData{
		Version: stateDataVersion,
		Name:    MenderStateInit,
		UpdateInfo: client.UpdateResponse{
			ID: "foobar",
		},
	}
	err := StoreStateData(ms, sd)
	assert.NoError(t, err)
	rsd, err := LoadStateData(ms)
	assert.NoError(t, err)
	assert.Equal(t, sd, rsd)

	// test if data marshalling works fine
	data, err := ms.ReadAll(stateDataKey)
	assert.NoError(t, err)
	assert.Contains(t, string(data), `"Name":"init"`)

	sd.Version = 999
	err = StoreStateData(ms, sd)
	assert.NoError(t, err)
	rsd, err = LoadStateData(ms)
	assert.Error(t, err)
	assert.Equal(t, StateData{}, rsd)
	assert.Equal(t, sd.Version, 999)

	ms.Remove(stateDataKey)
	_, err = LoadStateData(ms)
	assert.Error(t, err)
	assert.True(t, os.IsNotExist(err))
}

func TestStateReportError(t *testing.T) {
	update := client.UpdateResponse{
		ID: "foobar",
	}

	ms := store.NewMemStore()
	ctx := &StateContext{
		store: ms,
	}
	sc := &stateTestController{}

	// update succeeded, but we failed to report the status to the server,
	// rollback happens next
	res := NewReportErrorState(update, client.StatusSuccess)
	s, c := res.Handle(ctx, sc)
	assert.IsType(t, &RollbackState{}, s)
	assert.False(t, c)

	// store some state data, failing to report status with a failed update
	// will just clean that up and
	StoreStateData(ms, StateData{
		Name:       MenderStateReportStatusError,
		UpdateInfo: update,
	})
	// update failed and we failed to report that status to the server,
	// state data should be removed and we should go back to init
	res = NewReportErrorState(update, client.StatusFailure)
	s, c = res.Handle(ctx, sc)
	assert.IsType(t, &IdleState{}, s)
	assert.False(t, c)

	_, err := LoadStateData(ms)
	assert.Equal(t, err, nil)

	// store some state data, failing to report status with an update that
	// is already installed will also clean it up
	StoreStateData(ms, StateData{
		Name:       MenderStateReportStatusError,
		UpdateInfo: update,
	})
	// update is already installed and we failed to report that status to
	// the server, state data should be removed and we should go back to
	// init
	res = NewReportErrorState(update, client.StatusAlreadyInstalled)
	s, c = res.Handle(ctx, sc)
	assert.IsType(t, &IdleState{}, s)
	assert.False(t, c)

	_, err = LoadStateData(ms)
	assert.Equal(t, err, nil)
}

func TestMaxSendingAttempts(t *testing.T) {
	assert.Equal(t, minReportSendRetries,
		maxSendingAttempts(time.Second, 0*time.Second, minReportSendRetries))
	assert.Equal(t, minReportSendRetries,
		maxSendingAttempts(time.Second, time.Minute, minReportSendRetries))
	assert.Equal(t, 10, maxSendingAttempts(5*time.Second, time.Second, 3))
	assert.Equal(t, minReportSendRetries,
		maxSendingAttempts(time.Second, time.Second, minReportSendRetries))
}
