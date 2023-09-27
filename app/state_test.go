// Copyright 2023 Northern.tech AS
//
//	Licensed under the Apache License, Version 2.0 (the "License");
//	you may not use this file except in compliance with the License.
//	You may obtain a copy of the License at
//
//	    http://www.apache.org/licenses/LICENSE-2.0
//
//	Unless required by applicable law or agreed to in writing, software
//	distributed under the License is distributed on an "AS IS" BASIS,
//	WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//	See the License for the specific language governing permissions and
//	limitations under the License.
package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mendersoftware/mender/app/updatecontrolmap"
	"github.com/mendersoftware/mender/client"
	"github.com/mendersoftware/mender/conf"
	"github.com/mendersoftware/mender/datastore"
	dev "github.com/mendersoftware/mender/device"
	"github.com/mendersoftware/mender/installer"
	"github.com/mendersoftware/mender/statescript"
	"github.com/mendersoftware/mender/store"
	"github.com/mendersoftware/mender/system"
	stest "github.com/mendersoftware/mender/system/testing"
	"github.com/mendersoftware/mender/tests"
)

type stateTestController struct {
	FakeDevice
	updater                fakeUpdater
	artifactName           string
	updatePollIntvl        time.Duration
	inventPollIntvl        time.Duration
	retryIntvl             time.Duration
	retryPollCount         int
	state                  State
	updateResp             *datastore.UpdateInfo
	updateRespErr          menderError
	authorized             bool
	authorizeErr           menderError
	reportError            menderError
	logSendingError        menderError
	reportStatus           string
	reportUpdate           datastore.UpdateInfo
	logUpdate              datastore.UpdateInfo
	logs                   []byte
	inventoryErr           error
	controlMap             *ControlMapPool
	installers             []installer.PayloadUpdatePerformer
	refreshControlMapError error
}

func (s *stateTestController) GetCurrentArtifactName() (string, error) {
	if s.artifactName == "" {
		return "", errors.New("open ..., no such file or directory")
	}
	return s.artifactName, nil
}

func (s *stateTestController) GetUpdatePollInterval() time.Duration {
	return s.updatePollIntvl
}

func (s *stateTestController) GetInventoryPollInterval() time.Duration {
	return s.inventPollIntvl
}

func (s *stateTestController) GetRetryPollInterval() time.Duration {
	return s.retryIntvl
}

func (s *stateTestController) GetRetryPollCount() int {
	return s.retryPollCount
}

func (s *stateTestController) CheckUpdate() (*datastore.UpdateInfo, menderError) {
	return s.updateResp, s.updateRespErr
}

func (s *stateTestController) FetchUpdate(url string) (io.ReadCloser, int64, error) {
	return s.updater.FetchUpdate(nil, url)
}

func (s *stateTestController) RefreshServerUpdateControlMap(deploymentID string) error {
	return s.refreshControlMapError
}

func (s *stateTestController) GetCurrentState() State {
	return s.state
}

func (s *stateTestController) SetNextState(state State) {
	s.state = state
}

func (s *stateTestController) TransitionState(_ State, ctx *StateContext) (State, bool) {
	next, cancel := s.state.Handle(ctx, s)
	s.state = next
	return next, cancel
}

func (s *stateTestController) Authorize() (client.AuthToken, client.ServerURL, error) {
	return "", "", s.authorizeErr
}

func (s *stateTestController) ClearAuthorization() {
}

func (s *stateTestController) GetControlMapPool() *ControlMapPool {
	if s.controlMap == nil {
		return NewControlMap(
			store.NewMemStore(),
			conf.DefaultUpdateControlMapBootExpirationTimeSeconds,
			conf.DefaultUpdateControlMapBootExpirationTimeSeconds,
		)
	}
	return s.controlMap
}

func (s *stateTestController) ReportUpdateStatus(
	update *datastore.UpdateInfo,
	status string,
) menderError {
	s.reportUpdate = *update
	s.reportStatus = status
	return s.reportError
}

func (s *stateTestController) UploadLog(update *datastore.UpdateInfo, logs []byte) menderError {
	s.logUpdate = *update
	s.logs = logs
	return s.logSendingError
}

func (s *stateTestController) InventoryRefresh() error {
	return s.inventoryErr
}

func (s *stateTestController) CheckScriptsCompatibility() error {
	return nil
}

func (s *stateTestController) ReadArtifactHeaders(
	from io.ReadCloser,
) (*installer.Installer, error) {
	installerFactories := installer.AllModules{
		DualRootfs: s.FakeDevice,
	}

	installer, _, err := installer.ReadHeaders(from,
		"vexpress-qemu",
		nil,
		"",
		&installerFactories)
	return installer, err
}

func (s *stateTestController) GetInstallers() []installer.PayloadUpdatePerformer {
	if s.installers != nil {
		return s.installers
	}
	return []installer.PayloadUpdatePerformer{s.FakeDevice}
}

func (s *stateTestController) RestoreInstallersFromTypeList(payloadTypes []string) error {
	return nil
}

func (s *stateTestController) NewStatusReportWrapper(updateId string,
	stateId datastore.MenderState) *client.StatusReportWrapper {

	return nil
}

func (s *stateTestController) GetScriptExecutor() statescript.Executor {
	return nil
}

func (s *stateTestController) HandleBootstrapArtifact(_ store.Store) error {
	return nil
}

type waitStateTest struct {
	baseState
}

func (c *waitStateTest) Wait(next, same State, wait time.Duration, wake chan bool) (State, bool) {
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
		id: datastore.MenderStateInit,
	}

	assert.Equal(t, datastore.MenderStateInit, bs.Id())
	assert.False(t, bs.Cancel())
}

func TestStateWait(t *testing.T) {
	cs := NewWaitState(datastore.MenderStateAuthorizeWait, ToNone)

	assert.Equal(t, datastore.MenderStateAuthorizeWait, cs.Id())

	var s State
	var c bool

	// no update
	var tstart, tend time.Time
	ctx := StateContext{
		WakeupChan: make(chan bool, 1),
	}

	tstart = time.Now()
	s, c = cs.Wait(States.UpdateCheck, States.CheckWait,
		100*time.Millisecond, ctx.WakeupChan)
	tend = time.Now()
	// not cancelled should return the 'next' state
	assert.Equal(t, States.UpdateCheck, s)
	assert.False(t, c)
	assert.WithinDuration(t, tend, tstart, 105*time.Millisecond)

	// asynchronously cancel state operation
	go func() {
		ch := cs.Cancel()
		assert.True(t, ch)
	}()
	// should finish right away
	tstart = time.Now()
	s, c = cs.Wait(States.UpdateCheck, States.CheckWait,
		100*time.Millisecond, ctx.WakeupChan)
	tend = time.Now()
	// canceled should return the same state
	assert.Equal(t, States.CheckWait, s)
	assert.True(t, c)
	assert.WithinDuration(t, tend, tstart, 5*time.Millisecond)
	// Force wake from sleep and continue execution.
	go func() {
		assert.True(t, cs.Wake())
	}()
	s, c = cs.Wait(States.UpdateCheck, States.CheckWait, 10*time.Second, ctx.WakeupChan)
	// Wake should return the next state
	assert.Equal(t, States.UpdateCheck, s)
	assert.False(t, c)
}

func TestStateError(t *testing.T) {

	fooerr := NewTransientError(errors.New("foo"))

	es := NewErrorState(fooerr)
	assert.Equal(t, datastore.MenderStateError, es.Id())
	assert.IsType(t, &errorState{}, es)
	errstate, _ := es.(*errorState)
	assert.NotNil(t, errstate)
	assert.Equal(t, fooerr, errstate.cause)
	s, c := es.Handle(nil, &stateTestController{})
	assert.IsType(t, &idleState{}, s)
	assert.False(t, c)

	es = NewErrorState(nil)
	errstate, _ = es.(*errorState)
	assert.NotNil(t, errstate)
	assert.Contains(t, errstate.cause.Error(), "general error")
	s, c = es.Handle(nil, &stateTestController{})
	assert.IsType(t, &finalState{}, s)
	assert.False(t, c)
}

func TestStateUpdateReportStatus(t *testing.T) {
	update := &datastore.UpdateInfo{
		ID: "foobar",
	}

	ms := store.NewMemStore()
	ctx := StateContext{
		Store: ms,
	}
	sc := &stateTestController{}

	// create directory for storing deployments logs
	tempDir, _ := ioutil.TempDir("", "logs")
	openLogFileWithContent(path.Join(tempDir, "deployments.0001.foobar.log"),
		`{ "time": "12:12:12", "level": "error", "msg": "log foo" }`)
	DeploymentLogger = NewDeploymentLogManager(tempDir)
	defer func() {
		DeploymentLogger = nil
		os.RemoveAll(tempDir)
	}()

	usr := NewUpdateStatusReportState(update, client.StatusFailure)
	usr.Handle(&ctx, sc)
	assert.Equal(t, client.StatusFailure, sc.reportStatus)
	assert.Equal(t, *update, sc.reportUpdate)

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
	assert.Equal(t, *update, sc.reportUpdate)

	// cancelled state should not wipe state data, for this pretend the reporting
	// fails and cancel
	sc = &stateTestController{
		reportError: NewTransientError(errors.New("report failed")),
	}
	usr = NewUpdateStatusReportState(update, client.StatusSuccess)
	s, c := usr.Handle(&ctx, sc)
	// there was an error while reporting status; we should retry
	assert.IsType(t, s, &updateStatusReportRetryState{})
	assert.False(t, c)

	poll := 5 * time.Millisecond
	retry := 1 * time.Millisecond
	// error sending status
	sc = &stateTestController{
		updatePollIntvl: poll,
		retryIntvl:      retry,
		reportError:     NewTransientError(errors.New("test error sending status")),
	}

	shouldTry := maxSendingAttempts(poll, retry, minReportSendRetries)
	s = NewUpdateStatusReportState(update, client.StatusSuccess)

	now := time.Now()

	for i := 0; i < shouldTry; i++ {
		s, c = s.Handle(&ctx, sc)
		assert.IsType(t, &updateStatusReportRetryState{}, s)
		assert.False(t, c)

		s, c = s.Handle(&ctx, sc)
		assert.IsType(t, &updateStatusReportState{}, s)
		assert.False(t, c)
	}
	assert.WithinDuration(
		t,
		now,
		time.Now(),
		time.Duration(int64(shouldTry)*int64(retry))+time.Millisecond*10,
	)

	// next attempt should return an error, and therefore go back to idle.
	s, _ = s.Handle(&ctx, sc)
	assert.IsType(t, &updateStatusReportRetryState{}, s)
	s, c = s.Handle(&ctx, sc)
	assert.IsType(t, States.Idle, s)
	assert.False(t, c)

	// error sending logs
	sc = &stateTestController{
		updatePollIntvl: poll,
		retryIntvl:      retry,
		logSendingError: NewTransientError(errors.New("test error sending logs")),
	}
	s = NewUpdateStatusReportState(update, client.StatusFailure)

	now = time.Now()
	for i := 0; i < shouldTry; i++ {
		s, c = s.Handle(&ctx, sc)
		assert.IsType(t, &updateStatusReportRetryState{}, s)
		assert.False(t, c)

		s, c = s.Handle(&ctx, sc)
		assert.IsType(t, &updateStatusReportState{}, s)
		assert.False(t, c)
	}
	assert.WithinDuration(
		t,
		now,
		time.Now(),
		time.Duration(int64(shouldTry)*int64(retry))+time.Millisecond*15,
	)

	s, _ = s.Handle(&ctx, sc)
	assert.IsType(t, &updateStatusReportRetryState{}, s)
	s, c = s.Handle(&ctx, sc)
	assert.IsType(t, s, States.Idle)
	assert.False(t, c)

	// pretend update was aborted at the backend, but was applied
	// successfully on the device
	usr = NewUpdateStatusReportState(update, client.StatusSuccess)
	sc = &stateTestController{
		reportError: NewFatalError(client.ErrDeploymentAborted),
	}
	s, _ = usr.Handle(&ctx, sc)
	assert.IsType(t, States.Idle, s)

	// pretend update was aborted at the backend, along with local failure
	usr = NewUpdateStatusReportState(update, client.StatusFailure)
	sc = &stateTestController{
		reportError: NewFatalError(client.ErrDeploymentAborted),
	}
	s, _ = usr.Handle(&ctx, sc)
	assert.IsType(t, States.Idle, s)
}

func TestStateIdle(t *testing.T) {
	i := idleState{}

	s, c := i.Handle(&StateContext{}, &stateTestController{})
	assert.IsType(t, &checkWaitState{}, s)
	assert.False(t, c)
}

func TestStateUpdateCommit(t *testing.T) {
	tempDir, _ := ioutil.TempDir("", "logs")
	DeploymentLogger = NewDeploymentLogManager(tempDir)
	defer func() {
		DeploymentLogger = nil
		os.RemoveAll(tempDir)
	}()

	artifactTypeInfoProvides := map[string]string{
		"test-kwrd": "test-value",
	}

	update := &datastore.UpdateInfo{
		ID: "foo",
		Artifact: datastore.Artifact{
			ArtifactName:      "TestName",
			ArtifactGroup:     "TestGroup",
			CompatibleDevices: []string{"vexpress-qemu"},
			PayloadTypes:      []string{"rootfs-image"},
			TypeInfoProvides:  artifactTypeInfoProvides,
		},
		SupportsRollback: datastore.RollbackSupported,
	}

	ms := store.NewMemStore()
	ctx := &StateContext{
		Store: ms,
	}
	controller := &stateTestController{
		FakeDevice: FakeDevice{
			ConsumeUpdate: true,
		},
	}
	ucs := NewUpdateCommitState(update)

	// Report fails, for example because of failed authorization. This
	// causes retry.
	controller.reportError = NewTransientError(errors.New("Report failed"))
	state, cancelled := ucs.Handle(ctx, controller)
	assert.False(t, cancelled)
	assert.IsType(t, &updatePreCommitStatusReportRetryState{}, state)

	// Report fails, because deployment is aborted. This causes immediate
	// rollback.
	controller.reportError = NewFatalError(client.ErrDeploymentAborted)
	state, cancelled = ucs.Handle(ctx, controller)
	assert.False(t, cancelled)
	assert.IsType(t, &updateRollbackState{}, state)

	// Successful update.
	controller.reportError = nil
	state, cancelled = ucs.Handle(ctx, controller)
	assert.False(t, cancelled)
	assert.IsType(t, &updateAfterFirstCommitState{}, state)
	storeBuf, err := ms.ReadAll(datastore.ArtifactNameKey)
	assert.NoError(t, err)
	artifactName := string(storeBuf)
	assert.Equal(t, artifactName, "TestName")
	storeBuf, err = ms.ReadAll(datastore.ArtifactGroupKey)
	assert.NoError(t, err)
	artifactGroup := string(storeBuf)
	assert.Equal(t, artifactGroup, "TestGroup")
	storeBuf, err = ms.ReadAll(datastore.ArtifactTypeInfoProvidesKey)
	assert.NoError(t, err)
	var typeProvides map[string]string
	err = json.Unmarshal(storeBuf, &typeProvides)
	assert.NoError(t, err)
	assert.Equal(t, typeProvides, artifactTypeInfoProvides)
}

func TestStateInventoryUpdate(t *testing.T) {
	ius := States.InventoryUpdate
	ctx := new(StateContext)

	s, _ := ius.Handle(ctx, &stateTestController{
		inventoryErr: errors.New("some err"),
	})
	assert.IsType(t, &inventoryUpdateRetry{}, s)

	s, _ = ius.Handle(ctx, &stateTestController{})
	assert.IsType(t, &checkWaitState{}, s)

	// no artifact name should fail
	s, _ = ius.Handle(ctx, &stateTestController{
		inventoryErr: errNoArtifactName,
	})

	assert.IsType(t, &errorState{}, s)
}

func TestStateInventoryUpdateRetry(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping long running test")
	}

	t.Parallel()

	ius := States.InventoryUpdate
	iur := NewInventoryUpdateRetryState(ius, nil)
	ctx := new(StateContext)
	now := time.Now()
	ctx.nextAttemptAt = now.Add(8 * time.Second)
	ctx.WakeupChan = make(chan bool, 1)
	retryPollCount := 9
	for i := 0; i < retryPollCount-1; i++ {
		ctx.inventoryUpdateAttempts = i
		s, _ := iur.Handle(ctx, &stateTestController{
			inventoryErr:    errors.New("some err"),
			inventPollIntvl: 5 * time.Second,
			retryPollCount:  retryPollCount,
			updatePollIntvl: 32 * time.Second,
			retryIntvl:      16 * time.Second,
		})
		assert.IsType(t, &inventoryUpdateState{}, s)
	}
	s, _ := ius.Handle(ctx, &stateTestController{
		inventoryErr:    nil,
		inventPollIntvl: 5 * time.Second,
		retryPollCount:  retryPollCount,
		updatePollIntvl: 32 * time.Second,
		retryIntvl:      16 * time.Second,
	})
	assert.IsType(t, States.CheckWait, s)
}

func TestStateUpdateCheckWait(t *testing.T) {
	cws := NewCheckWaitState()
	ctx := new(StateContext)

	// no iventory was sent; we should first send inventory
	var tstart, tend time.Time
	tstart = time.Now()
	s, c := cws.Handle(ctx, &stateTestController{
		updatePollIntvl: 10 * time.Millisecond,
		inventPollIntvl: 20 * time.Millisecond,
	})

	tend = time.Now()
	assert.IsType(t, &inventoryUpdateState{}, s)
	assert.False(t, c)
	assert.WithinDuration(t, tend, tstart, 15*time.Millisecond)

	// now we have inventory sent; should send update request
	ctx.lastInventoryUpdateAttempt = tend
	tstart = time.Now()
	s, c = cws.Handle(ctx, &stateTestController{
		updatePollIntvl: 10 * time.Millisecond,
		inventPollIntvl: 20 * time.Millisecond,
	})
	tend = time.Now()
	assert.IsType(t, &updateCheckState{}, s)
	assert.False(t, c)
	assert.WithinDuration(t, tend, tstart, 15*time.Millisecond)

	// next time should still send an update request
	// it is time for both, but update req has preference
	ctx.lastUpdateCheckAttempt = tend
	tstart = time.Now()
	s, c = cws.Handle(ctx, &stateTestController{
		updatePollIntvl: 10 * time.Millisecond,
		inventPollIntvl: 20 * time.Millisecond,
	})
	tend = time.Now()
	assert.IsType(t, &updateCheckState{}, s)
	assert.False(t, c)
	assert.WithinDuration(t, tend, tstart, 15*time.Millisecond)

	// finally it should send inventory update
	ctx.lastUpdateCheckAttempt = tend
	tstart = time.Now()
	s, c = cws.Handle(ctx, &stateTestController{
		updatePollIntvl: 10 * time.Millisecond,
		inventPollIntvl: 20 * time.Millisecond,
	})
	tend = time.Now()
	assert.IsType(t, &inventoryUpdateState{}, s)
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
		updatePollIntvl: 100 * time.Millisecond,
		inventPollIntvl: 100 * time.Millisecond,
	})
	tend = time.Now()
	// canceled state should return itself
	assert.IsType(t, &checkWaitState{}, s)
	assert.True(t, c)
	assert.WithinDuration(t, tend, tstart, 5*time.Millisecond)
}

func TestStateUpdateCheck(t *testing.T) {
	cs := updateCheckState{}
	ctx := new(StateContext)

	var s State
	var c bool

	// no update
	s, c = cs.Handle(ctx, &stateTestController{})
	assert.IsType(t, &checkWaitState{}, s)
	assert.False(t, c)

	// pretend update check failed
	s, c = cs.Handle(ctx, &stateTestController{
		updateRespErr: NewTransientError(errors.New("check failed")),
	})
	assert.IsType(t, &errorState{}, s)
	assert.False(t, c)

	// pretend we have an update
	update := &datastore.UpdateInfo{}

	s, c = cs.Handle(ctx, &stateTestController{
		updateResp: update,
	})
	assert.IsType(t, &updateFetchState{}, s)
	assert.False(t, c)
	ufs, _ := s.(*updateFetchState)
	assert.Equal(t, *update, ufs.update)

	// pretend we have an update
	s, c = cs.Handle(ctx, &stateTestController{
		updateRespErr: NewTransientError(client.ErrNoDeploymentAvailable),
	})
	assert.IsType(t, &checkWaitState{}, s)
	assert.False(t, c)
}

func TestStateUpdateFetch(t *testing.T) {
	// create directory for storing deployments logs
	tempDir, _ := ioutil.TempDir("", "logs")
	DeploymentLogger = NewDeploymentLogManager(tempDir)
	defer func() {
		DeploymentLogger = nil
		os.RemoveAll(tempDir)
	}()

	// pretend we have an update
	update := &datastore.UpdateInfo{
		ID: "foobar",
	}
	cs := NewUpdateFetchState(update)

	ms := store.NewMemStore()
	ctx := StateContext{
		Store: ms,
	}

	// can not store state data
	ms.ReadOnly(true)
	s, c := cs.Handle(&ctx, &stateTestController{})
	assert.IsType(t, &updateStoreState{}, s)
	assert.False(t, c)
	s, c = transitionState(s, &ctx, &stateTestController{state: cs})
	assert.IsType(t, &updateCleanupState{}, s)
	assert.False(t, c)
	ms.ReadOnly(false)

	data := "test"
	stream := ioutil.NopCloser(bytes.NewBufferString(data))

	sc := &stateTestController{
		updater: fakeUpdater{
			fetchUpdateReturnReadCloser: stream,
			fetchUpdateReturnSize:       int64(len(data)),
		},
		state: cs,
	}
	s, c = cs.Handle(&ctx, sc)
	assert.IsType(t, &updateStoreState{}, s)
	assert.False(t, c)
	assert.Equal(t, client.StatusDownloading, sc.reportStatus)
	assert.Equal(t, *update, sc.reportUpdate)
	uis := s.(*updateStoreState)
	assert.Equal(t, stream, uis.imagein)
	s, c = transitionState(s, &ctx, sc)
	assert.IsType(t, &updateStatusReportState{}, s)
	assert.False(t, c)

	ud, err := datastore.LoadStateData(ms)
	assert.NoError(t, err)
	// Ignore state data store count.
	ud.UpdateInfo.StateDataStoreCount = 0
	assert.Equal(t, datastore.StateData{
		Version:    datastore.StateDataVersion,
		UpdateInfo: *update,
		Name:       datastore.MenderStateUpdateStore,
	}, ud)
}

func TestStateUpdateFetchRetry(t *testing.T) {
	// create directory for storing deployments logs
	tempDir, _ := ioutil.TempDir("", "logs")
	DeploymentLogger = NewDeploymentLogManager(tempDir)
	defer func() {
		DeploymentLogger = nil
		os.RemoveAll(tempDir)
	}()

	// pretend we have an update
	update := &datastore.UpdateInfo{
		ID: "foobar",
	}
	cs := NewUpdateFetchState(update)
	ms := store.NewMemStore()
	ctx := StateContext{
		Store: ms,
	}
	stc := stateTestController{
		updater: fakeUpdater{
			fetchUpdateReturnError: NewTransientError(errors.New("fetch failed")),
		},
		updatePollIntvl: 5 * time.Minute,
	}

	// pretend update check failed
	s, c := cs.Handle(&ctx, &stc)
	assert.IsType(t, &fetchStoreRetryState{}, s)
	assert.False(t, c)

	// Test for the twelve expected attempts:
	// (1m*3) + (2m*3) + (4m*3) + (5m*3)
	for i := 0; i < 12; i++ {
		s.(*fetchStoreRetryState).WaitState = &waitStateTest{}

		s, c = s.Handle(&ctx, &stc)
		assert.IsType(t, &updateFetchState{}, s)
		assert.False(t, c)

		s, c = s.Handle(&ctx, &stc)
		assert.IsType(t, &fetchStoreRetryState{}, s)
		assert.False(t, c)
	}

	// Final attempt should fail completely.
	s.(*fetchStoreRetryState).WaitState = &waitStateTest{baseState{
		id: datastore.MenderStateCheckWait,
	}}

	s, c = s.Handle(&ctx, &stc)
	assert.IsType(t, &updateErrorState{}, s)
	assert.False(t, c)
}

func TestStateUpdateStore(t *testing.T) {
	// create directory for storing deployments logs
	tempDir, _ := ioutil.TempDir("", "logs")
	DeploymentLogger = NewDeploymentLogManager(tempDir)
	defer func() {
		DeploymentLogger = nil
		os.RemoveAll(tempDir)
	}()

	artifactProvides := &tests.ArtifactProvides{
		ArtifactName:  "TestName",
		ArtifactGroup: "TestGroup",
	}
	artifactDepends := &tests.ArtifactDepends{
		ArtifactName:      []string{"OtherArtifact"},
		ArtifactGroup:     []string{"TestGroup"},
		CompatibleDevices: []string{"vexpress-qemu"},
	}
	artifactTypeInfoProvides := map[string]string{
		"test": "moar-test",
	}

	stream, err := tests.CreateTestArtifactV3("test", "gzip",
		artifactProvides, artifactDepends, artifactTypeInfoProvides, nil)
	require.NoError(t, err)

	update := &datastore.UpdateInfo{
		ID: "foo",
		Artifact: datastore.Artifact{
			ArtifactName:      "TestName",
			ArtifactGroup:     "TestGroup",
			CompatibleDevices: []string{"vexpress-qemu"},
			PayloadTypes:      []string{"rootfs-image"},
			TypeInfoProvides:  artifactTypeInfoProvides,
		},
		SupportsRollback: datastore.RollbackSupported,
	}
	uis := NewUpdateStoreState(stream, update)

	ms := store.NewMemStore()
	ctx := StateContext{
		Store: ms,
	}
	ms.WriteAll(datastore.ArtifactNameKey, []byte("OtherArtifact"))
	ms.WriteAll(datastore.ArtifactGroupKey, []byte("TestGroup"))

	sc := &stateTestController{
		FakeDevice: FakeDevice{
			ConsumeUpdate: true,
		},
	}

	// pretend fail
	s, _ := uis.HandleError(&ctx, sc, NewFatalError(errors.New("test failure")))
	assert.IsType(t, &updateCleanupState{}, s)

	s, c := uis.Handle(&ctx, sc)
	assert.IsType(t, &updateAfterStoreState{}, s)
	assert.False(t, c)
	assert.Equal(t, client.StatusDownloading, sc.reportStatus)

	ud, err := datastore.LoadStateData(ms)
	assert.NoError(t, err)
	newUpdate := datastore.StateData{
		Version:    datastore.StateDataVersion,
		UpdateInfo: *update,
		Name:       datastore.MenderStateUpdateStore,
	}
	newUpdate.UpdateInfo.StateDataStoreCount = 3
	assert.Equal(t, newUpdate, ud)

	// pretend update was aborted
	sc = &stateTestController{
		reportError: NewFatalError(client.ErrDeploymentAborted),
	}
	s, _ = uis.Handle(&ctx, sc)
	assert.IsType(t, &updateStatusReportState{}, s)
}

// Tests various cases of missing dependencies, and a final case with all
// dependencies satisfied.
func TestUpdateStoreDependencies(t *testing.T) {
	// create directory for storing deployments logs
	tempDir, _ := ioutil.TempDir("", "logs")
	DeploymentLogger = NewDeploymentLogManager(tempDir)
	defer func() {
		DeploymentLogger = nil
		os.RemoveAll(tempDir)
	}()

	artifactProvides := &tests.ArtifactProvides{
		ArtifactName:  "TestName",
		ArtifactGroup: "TestGroup",
	}
	artifactDepends := &tests.ArtifactDepends{
		ArtifactName:      []string{"OtherArtifact"},
		ArtifactGroup:     []string{"TestGroup"},
		CompatibleDevices: []string{"vexpress-qemu"},
	}

	stream, err := tests.CreateTestArtifactV3("test-image", "gzip",
		artifactProvides, artifactDepends, nil, nil)
	require.NoError(t, err)

	update := &datastore.UpdateInfo{
		ID: "foo",
		Artifact: datastore.Artifact{
			ArtifactName:      "TestName",
			ArtifactGroup:     "TestGroup",
			CompatibleDevices: []string{"vexpress-qemu"},
			PayloadTypes:      []string{"rootfs-image"},
		},
		SupportsRollback: datastore.RollbackSupported,
	}
	uis := NewUpdateStoreState(stream, update)

	ms := store.NewMemStore()
	ctx := StateContext{
		Store: ms,
	}
	sc := &stateTestController{
		FakeDevice: FakeDevice{
			ConsumeUpdate: true,
		},
	}

	s, c := uis.Handle(&ctx, sc)
	assert.IsType(t, &updateStatusReportState{}, s)
	assert.False(t, c)
	ms.WriteAll(datastore.ArtifactNameKey, []byte("NonExistingArtie"))
	stream.Seek(0, io.SeekStart)
	s, c = uis.Handle(&ctx, sc)
	assert.IsType(t, &updateStatusReportState{}, s)
	assert.False(t, c)
	ms.WriteAll(datastore.ArtifactNameKey, []byte("OtherArtifact"))
	stream.Seek(0, io.SeekStart)
	s, c = uis.Handle(&ctx, sc)
	assert.IsType(t, &updateStatusReportState{}, s)
	assert.False(t, c)
	ms.WriteAll(datastore.ArtifactGroupKey, []byte("WrongGroup"))
	stream.Seek(0, io.SeekStart)
	s, c = uis.Handle(&ctx, sc)
	assert.IsType(t, &updateStatusReportState{}, s)
	assert.False(t, c)
	ms.WriteAll(datastore.ArtifactGroupKey, []byte("TestGroup"))
	stream.Seek(0, io.SeekStart)
	s, c = uis.Handle(&ctx, sc)
	assert.IsType(t, &updateAfterStoreState{}, s)
	assert.False(t, c)
}

func TestStateWrongArtifactNameFromServer(t *testing.T) {
	// create directory for storing deployments logs
	tempDir, _ := ioutil.TempDir("", "logs")
	DeploymentLogger = NewDeploymentLogManager(tempDir)
	defer func() {
		DeploymentLogger = nil
		os.RemoveAll(tempDir)
	}()

	artifactProvides := &tests.ArtifactProvides{
		ArtifactGroup: "TestGroup",
		ArtifactName:  "TestName",
	}
	artifactDepends := &tests.ArtifactDepends{
		CompatibleDevices: []string{"vexpress-qemu"},
	}

	stream, err := tests.CreateTestArtifactV3("test-image", "gzip",
		artifactProvides, artifactDepends, nil, nil)
	require.NoError(t, err)

	update := &datastore.UpdateInfo{
		ID: "foo",
		Artifact: datastore.Artifact{
			ArtifactName:      "WrongName",
			PayloadTypes:      []string{"rootfs-image"},
			CompatibleDevices: []string{"vexpress-qemu"},
		},
		SupportsRollback: datastore.RollbackSupported,
	}
	uis := NewUpdateStoreState(stream, update)

	ms := store.NewMemStore()
	ctx := StateContext{
		Store: ms,
	}

	sc := &stateTestController{
		FakeDevice: FakeDevice{
			ConsumeUpdate: true,
		},
	}

	s, c := uis.Handle(&ctx, sc)
	// Straight to status report, failure.
	assert.IsType(t, &updateStatusReportState{}, s)
	assert.False(t, c)
}

func TestStateUpdateInstallFailed(t *testing.T) {
	// create directory for storing deployments logs
	tempDir, _ := ioutil.TempDir("", "logs")
	DeploymentLogger = NewDeploymentLogManager(tempDir)
	defer func() {
		DeploymentLogger = nil
		os.RemoveAll(tempDir)
	}()

	stream, err := MakeRootfsImageArtifact(3, false)
	require.NoError(t, err)
	update := &datastore.UpdateInfo{
		ID: "foo",
		Artifact: datastore.Artifact{
			ArtifactName: "TestName",
		},
	}
	uis := NewUpdateStoreState(stream, update)
	ms := store.NewMemStore()
	ms.WriteAll(datastore.ArtifactNameKey, []byte("preexisting-name"))
	ctx := StateContext{
		Store: ms,
	}
	stc := stateTestController{
		FakeDevice: FakeDevice{
			RetStoreUpdate: NewFatalError(errors.New("install failed")),
		},
		updatePollIntvl: 5 * time.Minute,
	}

	// pretend update check failed
	s, c := uis.Handle(&ctx, &stc)
	assert.IsType(t, &updateCleanupState{}, s)
	assert.False(t, c)
}

func TestStateFinal(t *testing.T) {
	rs := finalState{}

	assert.Panics(t, func() {
		rs.Handle(nil, &stateTestController{})
	}, "final state Handle() should panic")
}

func TestStateData(t *testing.T) {
	ms := store.NewMemStore()
	sd := datastore.StateData{
		Version: datastore.StateDataVersion,
		Name:    datastore.MenderStateInit,
		UpdateInfo: datastore.UpdateInfo{
			ID: "foobar",
		},
	}
	err := datastore.StoreStateData(ms, sd, true)
	assert.NoError(t, err)
	rsd, err := datastore.LoadStateData(ms)
	assert.NoError(t, err)
	modSd := sd
	modSd.UpdateInfo.StateDataStoreCount = 2
	assert.Equal(t, modSd, rsd)

	// test if data marshalling works fine
	data, err := ms.ReadAll(datastore.StateDataKey)
	assert.NoError(t, err)
	assert.Contains(t, string(data), `"Name":"init"`)

	sd.Version = 999
	err = datastore.StoreStateData(ms, sd, true)
	assert.NoError(t, err)
	_, err = datastore.LoadStateData(ms)
	assert.Error(t, err)

	ms.Remove(datastore.StateDataKey)
	_, err = datastore.LoadStateData(ms)
	assert.Error(t, err)
	assert.True(t, os.IsNotExist(err))
}

func TestMaxSendingAttempts(t *testing.T) {
	assert.Equal(t, minReportSendRetries,
		maxSendingAttempts(time.Second, 0*time.Second, minReportSendRetries))
	assert.Equal(t, minReportSendRetries,
		maxSendingAttempts(time.Second, time.Minute, minReportSendRetries))
	assert.Equal(t, 10, maxSendingAttempts(5*time.Second, time.Second, 3))
	assert.Equal(t, minReportSendRetries,
		maxSendingAttempts(time.Second, time.Second, minReportSendRetries))
	// Make sure the global maximum (10) is returned when max > 10
	assert.Equal(t, 10,
		maxSendingAttempts(time.Second*30,
			time.Second, minReportSendRetries))
	// Make sure the proper max retry attempt is returned when
	// minRetries < upi/rpi < globalMaximum
	assert.Equal(t, 8,
		maxSendingAttempts(time.Second*40,
			time.Second*10, minReportSendRetries))
}

type menderWithCustomUpdater struct {
	*Mender
	updater                fakeUpdater
	reportWriter           io.Writer
	lastReport             string
	failStatusReportCount  int
	failStatusReportStatus string
}

func (m *menderWithCustomUpdater) CheckUpdate() (*datastore.UpdateInfo, menderError) {
	update := datastore.UpdateInfo{}
	update.Artifact.CompatibleDevices = []string{"test-device"}
	update.Artifact.ArtifactName = "artifact-name"
	update.ID = "abcdefg"
	return &update, nil
}

func (m *menderWithCustomUpdater) ReportUpdateStatus(
	update *datastore.UpdateInfo,
	status string,
) menderError {
	if m.failStatusReportStatus == status && m.failStatusReportCount > 0 {
		m.failStatusReportCount--
		return NewTransientError(errors.New("Failing status report as instructed by test"))
	}

	// Don't rereport already existing status.
	if status != m.lastReport {
		_, err := m.reportWriter.Write([]byte(fmt.Sprintf("%s\n", status)))
		if err != nil {
			return NewTransientError(err)
		}
		m.lastReport = status
	}
	return nil
}

func (m *menderWithCustomUpdater) FetchUpdate(url string) (io.ReadCloser, int64, error) {
	return m.updater.FetchUpdate(nil, url)
}

func (m *menderWithCustomUpdater) UploadLog(update *datastore.UpdateInfo, logs []byte) menderError {
	return nil
}

type stateTransitionsWithUpdateModulesTestCase struct {
	caseName               string
	stateChain             []State
	artifactStateChain     []string
	reportsLog             []string
	installOutcome         tests.InstallOutcome
	failStatusReportCount  int
	failStatusReportStatus string

	tests.TestModuleAttr
}

var stateTransitionsWithUpdateModulesTestCases []stateTransitionsWithUpdateModulesTestCase = []stateTransitionsWithUpdateModulesTestCase{
	{
		caseName: "Normal install, no reboot, no rollback",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateCommitState{},
			&updateAfterFirstCommitState{},
			&updateAfterCommitState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"NeedsArtifactReboot",
			"ArtifactInstall_Leave_00",
			"ArtifactCommit_Enter_00",
			"ArtifactCommit",
			"ArtifactCommit_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"success",
		},
		TestModuleAttr: tests.TestModuleAttr{
			RollbackDisabled: true,
			RebootDisabled:   true,
		},
		installOutcome: tests.SuccessfulInstall,
	},

	{
		caseName: "Normal install, no rollback",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateRebootState{},
			&updateVerifyRebootState{},
			&updateAfterRebootState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateCommitState{},
			&updateAfterFirstCommitState{},
			&updateAfterCommitState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"NeedsArtifactReboot",
			"ArtifactInstall_Leave_00",
			"ArtifactReboot_Enter_00",
			"ArtifactReboot",
			"ArtifactVerifyReboot",
			"ArtifactReboot_Leave_00",
			"ArtifactCommit_Enter_00",
			"ArtifactCommit",
			"ArtifactCommit_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"rebooting",
			"installing",
			"success",
		},
		TestModuleAttr: tests.TestModuleAttr{
			RollbackDisabled: true,
		},
		installOutcome: tests.SuccessfulInstall,
	},

	{
		caseName: "Normal install",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateRebootState{},
			&updateVerifyRebootState{},
			&updateAfterRebootState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateCommitState{},
			&updateAfterFirstCommitState{},
			&updateAfterCommitState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"NeedsArtifactReboot",
			"ArtifactInstall_Leave_00",
			"ArtifactReboot_Enter_00",
			"ArtifactReboot",
			"ArtifactVerifyReboot",
			"ArtifactReboot_Leave_00",
			"ArtifactCommit_Enter_00",
			"ArtifactCommit",
			"ArtifactCommit_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"rebooting",
			"installing",
			"success",
		},
		installOutcome: tests.SuccessfulInstall,
	},

	{
		caseName: "Error in Download state, no rollback",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"Download_Error_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			ErrorStates:      []string{"Download"},
			RollbackDisabled: true,
		},
		installOutcome: tests.SuccessfulRollback,
	},

	{
		caseName: "Killed in Download state, no rollback",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			SpontRebootStates: []string{"Download"},
			RollbackDisabled:  true,
		},
		installOutcome: tests.SuccessfulRollback,
	},

	{
		caseName: "Error in ArtifactInstall state, no rollback",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&updateErrorState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"ArtifactInstall_Error_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			ErrorStates:      []string{"ArtifactInstall"},
			RollbackDisabled: true,
		},
		installOutcome: tests.UnsuccessfulInstall,
	},

	{
		caseName: "Killed in ArtifactInstall state, no rollback",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&updateErrorState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			SpontRebootStates: []string{"ArtifactInstall"},
			RollbackDisabled:  true,
		},
		installOutcome: tests.UnsuccessfulInstall,
	},

	{
		caseName: "Error in ArtifactInstall",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&updateRollbackState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateAfterRollbackRebootState{},
			&updateErrorState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"ArtifactInstall_Error_00",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"NeedsArtifactReboot",
			"ArtifactRollback_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			ErrorStates: []string{"ArtifactInstall"},
		},
		installOutcome: tests.SuccessfulRollback,
	},

	{
		caseName: "Killed in ArtifactInstall",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&updateRollbackState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateAfterRollbackRebootState{},
			&updateErrorState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"NeedsArtifactReboot",
			"ArtifactRollback_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			SpontRebootStates: []string{"ArtifactInstall"},
		},
		installOutcome: tests.SuccessfulRollback,
	},

	{
		caseName: "Error in ArtifactReboot",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateRebootState{},
			&updateRollbackState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateAfterRollbackRebootState{},
			&updateErrorState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"NeedsArtifactReboot",
			"ArtifactInstall_Leave_00",
			"ArtifactReboot_Enter_00",
			"ArtifactReboot",
			"ArtifactReboot_Error_00",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"ArtifactRollback_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"rebooting",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			ErrorStates: []string{"ArtifactReboot"},
		},
		installOutcome: tests.SuccessfulRollback,
	},

	{
		caseName: "Killed in ArtifactReboot",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateRebootState{},
			&updateVerifyRebootState{},
			&updateAfterRebootState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateCommitState{},
			&updateAfterFirstCommitState{},
			&updateAfterCommitState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"NeedsArtifactReboot",
			"ArtifactInstall_Leave_00",
			"ArtifactReboot_Enter_00",
			"ArtifactReboot",
			"ArtifactVerifyReboot",
			"ArtifactReboot_Leave_00",
			"ArtifactCommit_Enter_00",
			"ArtifactCommit",
			"ArtifactCommit_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"rebooting",
			"installing",
			"success",
		},
		TestModuleAttr: tests.TestModuleAttr{
			SpontRebootStates: []string{"ArtifactReboot"},
		},
		installOutcome: tests.SuccessfulInstall,
	},

	{
		caseName: "Error in ArtifactVerifyReboot",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateRebootState{},
			&updateVerifyRebootState{},
			&updateRollbackState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateAfterRollbackRebootState{},
			&updateErrorState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"NeedsArtifactReboot",
			"ArtifactInstall_Leave_00",
			"ArtifactReboot_Enter_00",
			"ArtifactReboot",
			"ArtifactVerifyReboot",
			"ArtifactReboot_Error_00",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"ArtifactRollback_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"rebooting",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			ErrorStates: []string{"ArtifactVerifyReboot"},
		},
		installOutcome: tests.SuccessfulRollback,
	},

	{
		caseName: "Killed in ArtifactVerifyReboot",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateRebootState{},
			&updateVerifyRebootState{},
			&updateRollbackState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateAfterRollbackRebootState{},
			&updateErrorState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"NeedsArtifactReboot",
			"ArtifactInstall_Leave_00",
			"ArtifactReboot_Enter_00",
			"ArtifactReboot",
			"ArtifactVerifyReboot",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"ArtifactRollback_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"rebooting",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			SpontRebootStates: []string{"ArtifactVerifyReboot"},
		},
		installOutcome: tests.SuccessfulRollback,
	},

	{
		caseName: "Error in ArtifactRollback",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateRebootState{},
			&updateVerifyRebootState{},
			&updateRollbackState{},
			&updateErrorState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"NeedsArtifactReboot",
			"ArtifactInstall_Leave_00",
			"ArtifactReboot_Enter_00",
			"ArtifactReboot",
			"ArtifactVerifyReboot",
			"ArtifactReboot_Error_00",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"ArtifactRollback_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"rebooting",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			ErrorStates: []string{"ArtifactVerifyReboot", "ArtifactRollback"},
		},
		installOutcome: tests.UnsuccessfulInstall,
	},

	{
		caseName: "Killed in ArtifactRollback",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateRebootState{},
			&updateVerifyRebootState{},
			&updateRollbackState{},
			&updateRollbackState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateAfterRollbackRebootState{},
			&updateErrorState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"NeedsArtifactReboot",
			"ArtifactInstall_Leave_00",
			"ArtifactReboot_Enter_00",
			"ArtifactReboot",
			"ArtifactVerifyReboot",
			"ArtifactReboot_Error_00",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"ArtifactRollback_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"rebooting",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			ErrorStates:       []string{"ArtifactVerifyReboot"},
			SpontRebootStates: []string{"ArtifactRollback"},
		},
		installOutcome: tests.SuccessfulRollback,
	},

	{
		caseName: "Error in ArtifactRollbackReboot",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateRebootState{},
			&updateVerifyRebootState{},
			&updateRollbackState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateAfterRollbackRebootState{},
			&updateErrorState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"NeedsArtifactReboot",
			"ArtifactInstall_Leave_00",
			"ArtifactReboot_Enter_00",
			"ArtifactReboot",
			"ArtifactVerifyReboot",
			"ArtifactReboot_Error_00",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"ArtifactRollback_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"rebooting",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			ErrorStates: []string{"ArtifactVerifyReboot", "ArtifactRollbackReboot"},
		},
		installOutcome: tests.SuccessfulRollback,
	},

	{
		caseName: "Killed in ArtifactRollbackReboot",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateRebootState{},
			&updateVerifyRebootState{},
			&updateRollbackState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateAfterRollbackRebootState{},
			&updateErrorState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"NeedsArtifactReboot",
			"ArtifactInstall_Leave_00",
			"ArtifactReboot_Enter_00",
			"ArtifactReboot",
			"ArtifactVerifyReboot",
			"ArtifactReboot_Error_00",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"ArtifactRollback_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"rebooting",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			ErrorStates:       []string{"ArtifactVerifyReboot"},
			SpontRebootStates: []string{"ArtifactRollbackReboot"},
		},
		installOutcome: tests.SuccessfulRollback,
	},

	{
		caseName: "Error in ArtifactVerifyRollbackReboot",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateRebootState{},
			&updateVerifyRebootState{},
			&updateRollbackState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateAfterRollbackRebootState{},
			&updateErrorState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"NeedsArtifactReboot",
			"ArtifactInstall_Leave_00",
			"ArtifactReboot_Enter_00",
			"ArtifactReboot",
			"ArtifactVerifyReboot",
			"ArtifactReboot_Error_00",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"ArtifactRollback_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"rebooting",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			ErrorStates: []string{"ArtifactVerifyReboot", "ArtifactVerifyRollbackReboot"},
		},
		installOutcome: tests.SuccessfulRollback,
	},

	{
		caseName: "Killed in ArtifactVerifyRollbackReboot",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateRebootState{},
			&updateVerifyRebootState{},
			&updateRollbackState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateAfterRollbackRebootState{},
			&updateErrorState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"NeedsArtifactReboot",
			"ArtifactInstall_Leave_00",
			"ArtifactReboot_Enter_00",
			"ArtifactReboot",
			"ArtifactVerifyReboot",
			"ArtifactReboot_Error_00",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"ArtifactRollback_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"rebooting",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			ErrorStates:       []string{"ArtifactVerifyReboot"},
			SpontRebootStates: []string{"ArtifactVerifyRollbackReboot"},
		},
		installOutcome: tests.SuccessfulRollback,
	},

	{
		caseName: "Error in ArtifactFailure",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&updateRollbackState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateAfterRollbackRebootState{},
			&updateErrorState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"ArtifactInstall_Error_00",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"NeedsArtifactReboot",
			"ArtifactRollback_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			ErrorStates: []string{"ArtifactInstall", "ArtifactFailure"},
		},
		installOutcome: tests.SuccessfulRollback,
	},

	{
		caseName: "Killed in ArtifactFailure",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&updateRollbackState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateAfterRollbackRebootState{},
			&updateErrorState{},
			&updateErrorState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"ArtifactInstall_Error_00",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"NeedsArtifactReboot",
			"ArtifactRollback_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			ErrorStates:       []string{"ArtifactInstall"},
			SpontRebootStates: []string{"ArtifactFailure"},
		},
		installOutcome: tests.SuccessfulRollback,
	},

	{
		caseName: "Error in Cleanup",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateRebootState{},
			&updateVerifyRebootState{},
			&updateRollbackState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateAfterRollbackRebootState{},
			&updateErrorState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"NeedsArtifactReboot",
			"ArtifactInstall_Leave_00",
			"ArtifactReboot_Enter_00",
			"ArtifactReboot",
			"ArtifactVerifyReboot",
			"ArtifactReboot_Error_00",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"ArtifactRollback_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"rebooting",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			ErrorStates: []string{"ArtifactVerifyReboot", "Cleanup"},
		},
		installOutcome: tests.SuccessfulRollback,
	},

	{
		caseName: "Killed in Cleanup",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateRebootState{},
			&updateVerifyRebootState{},
			&updateRollbackState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateAfterRollbackRebootState{},
			&updateErrorState{},
			&updateCleanupState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"NeedsArtifactReboot",
			"ArtifactInstall_Leave_00",
			"ArtifactReboot_Enter_00",
			"ArtifactReboot",
			"ArtifactVerifyReboot",
			"ArtifactReboot_Error_00",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"ArtifactRollback_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"rebooting",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			ErrorStates:       []string{"ArtifactVerifyReboot"},
			SpontRebootStates: []string{"Cleanup"},
		},
		installOutcome: tests.SuccessfulRollback,
	},

	{
		caseName: "Error in ArtifactCommit",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateRebootState{},
			&updateVerifyRebootState{},
			&updateAfterRebootState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateCommitState{},
			&updateRollbackState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateAfterRollbackRebootState{},
			&updateErrorState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"NeedsArtifactReboot",
			"ArtifactInstall_Leave_00",
			"ArtifactReboot_Enter_00",
			"ArtifactReboot",
			"ArtifactVerifyReboot",
			"ArtifactReboot_Leave_00",
			"ArtifactCommit_Enter_00",
			"ArtifactCommit",
			"ArtifactCommit_Error_00",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"ArtifactRollback_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"rebooting",
			"installing",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			ErrorStates: []string{"ArtifactCommit"},
		},
		installOutcome: tests.SuccessfulRollback,
	},

	{
		caseName: "Killed in ArtifactCommit",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateRebootState{},
			&updateVerifyRebootState{},
			&updateAfterRebootState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateCommitState{},
			&updateRollbackState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateAfterRollbackRebootState{},
			&updateErrorState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"NeedsArtifactReboot",
			"ArtifactInstall_Leave_00",
			"ArtifactReboot_Enter_00",
			"ArtifactReboot",
			"ArtifactVerifyReboot",
			"ArtifactReboot_Leave_00",
			"ArtifactCommit_Enter_00",
			"ArtifactCommit",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"ArtifactRollback_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"rebooting",
			"installing",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			SpontRebootStates: []string{"ArtifactCommit"},
		},
		installOutcome: tests.SuccessfulRollback,
	},

	{
		caseName: "Error in ArtifactCommit, no reboot",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateCommitState{},
			&updateRollbackState{},
			&updateErrorState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"NeedsArtifactReboot",
			"ArtifactInstall_Leave_00",
			"ArtifactCommit_Enter_00",
			"ArtifactCommit",
			"ArtifactCommit_Error_00",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"NeedsArtifactReboot",
			"ArtifactRollback_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			ErrorStates:    []string{"ArtifactCommit"},
			RebootDisabled: true,
		},
		installOutcome: tests.SuccessfulRollback,
	},

	{
		caseName: "Killed in ArtifactCommit, no reboot",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateCommitState{},
			&updateRollbackState{},
			&updateErrorState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"NeedsArtifactReboot",
			"ArtifactInstall_Leave_00",
			"ArtifactCommit_Enter_00",
			"ArtifactCommit",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"NeedsArtifactReboot",
			"ArtifactRollback_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			SpontRebootStates: []string{"ArtifactCommit"},
			RebootDisabled:    true,
		},
		installOutcome: tests.SuccessfulRollback,
	},

	{
		caseName: "Error in Download_Enter_00 state, no rollback",
		stateChain: []State{
			&updateFetchState{},
			&errorState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download_Error_00",
		},
		reportsLog: []string{""},
		TestModuleAttr: tests.TestModuleAttr{
			ErrorStates:      []string{"Download_Enter_00"},
			RollbackDisabled: true,
		},
		installOutcome: tests.SuccessfulRollback,
	},

	{
		caseName: "Killed in Download_Enter_00 state, no rollback",
		stateChain: []State{
			&updateFetchState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
		},
		reportsLog: []string{""},
		TestModuleAttr: tests.TestModuleAttr{
			SpontRebootStates: []string{"Download_Enter_00"},
			RollbackDisabled:  true,
		},
		installOutcome: tests.SuccessfulRollback,
	},

	{
		caseName: "Error in ArtifactInstall_Enter_00 state, no rollback",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&updateErrorState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall_Error_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			ErrorStates:      []string{"ArtifactInstall_Enter_00"},
			RollbackDisabled: true,
		},
		installOutcome: tests.UnsuccessfulInstall,
	},

	{
		caseName: "Killed in ArtifactInstall_Enter_00 state, no rollback",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&updateErrorState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			SpontRebootStates: []string{"ArtifactInstall_Enter_00"},
			RollbackDisabled:  true,
		},
		installOutcome: tests.UnsuccessfulInstall,
	},

	{
		caseName: "Error in ArtifactInstall_Enter_00",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&updateRollbackState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateAfterRollbackRebootState{},
			&updateErrorState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall_Error_00",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"NeedsArtifactReboot",
			"ArtifactRollback_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			ErrorStates: []string{"ArtifactInstall_Enter_00"},
		},
		installOutcome: tests.SuccessfulRollback,
	},

	{
		caseName: "Killed in ArtifactInstall_Enter_00",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&updateRollbackState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateAfterRollbackRebootState{},
			&updateErrorState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"NeedsArtifactReboot",
			"ArtifactRollback_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			SpontRebootStates: []string{"ArtifactInstall_Enter_00"},
		},
		installOutcome: tests.SuccessfulRollback,
	},

	{
		caseName: "Error in ArtifactReboot_Enter_00",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateRebootState{},
			&updateRollbackState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateAfterRollbackRebootState{},
			&updateErrorState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"NeedsArtifactReboot",
			"ArtifactInstall_Leave_00",
			"ArtifactReboot_Enter_00",
			"ArtifactReboot_Error_00",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"ArtifactRollback_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			ErrorStates: []string{"ArtifactReboot_Enter_00"},
		},
		installOutcome: tests.SuccessfulRollback,
	},

	{
		caseName: "Killed in ArtifactReboot_Enter_00",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateRebootState{},
			&updateVerifyRebootState{},
			&updateAfterRebootState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateCommitState{},
			&updateAfterFirstCommitState{},
			&updateAfterCommitState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"NeedsArtifactReboot",
			"ArtifactInstall_Leave_00",
			"ArtifactReboot_Enter_00",
			"ArtifactVerifyReboot",
			"ArtifactReboot_Leave_00",
			"ArtifactCommit_Enter_00",
			"ArtifactCommit",
			"ArtifactCommit_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"installing",
			"success",
		},
		TestModuleAttr: tests.TestModuleAttr{
			SpontRebootStates: []string{"ArtifactReboot_Enter_00"},
		},
		installOutcome: tests.SuccessfulInstall,
	},

	{
		caseName: "Error in ArtifactRollback_Enter_00",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateRebootState{},
			&updateVerifyRebootState{},
			&updateRollbackState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateAfterRollbackRebootState{},
			&updateErrorState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"NeedsArtifactReboot",
			"ArtifactInstall_Leave_00",
			"ArtifactReboot_Enter_00",
			"ArtifactReboot",
			"ArtifactVerifyReboot",
			"ArtifactReboot_Error_00",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"ArtifactRollback_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"rebooting",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			ErrorStates: []string{"ArtifactVerifyReboot", "ArtifactRollback_Enter_00"},
		},
		installOutcome: tests.SuccessfulRollback,
	},

	{
		caseName: "Killed in ArtifactRollback_Enter_00",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateRebootState{},
			&updateVerifyRebootState{},
			&updateRollbackState{},
			&updateRollbackState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateAfterRollbackRebootState{},
			&updateErrorState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"NeedsArtifactReboot",
			"ArtifactInstall_Leave_00",
			"ArtifactReboot_Enter_00",
			"ArtifactReboot",
			"ArtifactVerifyReboot",
			"ArtifactReboot_Error_00",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"ArtifactRollback_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"rebooting",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			ErrorStates:       []string{"ArtifactVerifyReboot"},
			SpontRebootStates: []string{"ArtifactRollback_Enter_00"},
		},
		installOutcome: tests.SuccessfulRollback,
	},

	{
		caseName: "Error in ArtifactRollbackReboot_Enter_00",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateRebootState{},
			&updateVerifyRebootState{},
			&updateRollbackState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateAfterRollbackRebootState{},
			&updateErrorState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"NeedsArtifactReboot",
			"ArtifactInstall_Leave_00",
			"ArtifactReboot_Enter_00",
			"ArtifactReboot",
			"ArtifactVerifyReboot",
			"ArtifactReboot_Error_00",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"ArtifactRollback_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"rebooting",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			ErrorStates: []string{"ArtifactVerifyReboot", "ArtifactRollbackReboot_Enter_00"},
		},
		installOutcome: tests.SuccessfulRollback,
	},

	{
		caseName: "Killed in ArtifactRollbackReboot_Enter_00",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateRebootState{},
			&updateVerifyRebootState{},
			&updateRollbackState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateAfterRollbackRebootState{},
			&updateErrorState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"NeedsArtifactReboot",
			"ArtifactInstall_Leave_00",
			"ArtifactReboot_Enter_00",
			"ArtifactReboot",
			"ArtifactVerifyReboot",
			"ArtifactReboot_Error_00",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"ArtifactRollback_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"rebooting",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			ErrorStates:       []string{"ArtifactVerifyReboot"},
			SpontRebootStates: []string{"ArtifactRollbackReboot_Enter_00"},
		},
		installOutcome: tests.SuccessfulRollback,
	},

	{
		caseName: "Error in ArtifactFailure_Enter_00",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&updateRollbackState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateAfterRollbackRebootState{},
			&updateErrorState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"ArtifactInstall_Error_00",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"NeedsArtifactReboot",
			"ArtifactRollback_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			ErrorStates: []string{"ArtifactInstall", "ArtifactFailure_Enter_00"},
		},
		installOutcome: tests.SuccessfulRollback,
	},

	{
		caseName: "Killed in ArtifactFailure_Enter_00",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&updateRollbackState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateAfterRollbackRebootState{},
			&updateErrorState{},
			&updateErrorState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"ArtifactInstall_Error_00",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"NeedsArtifactReboot",
			"ArtifactRollback_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			ErrorStates:       []string{"ArtifactInstall"},
			SpontRebootStates: []string{"ArtifactFailure_Enter_00"},
		},
		installOutcome: tests.SuccessfulRollback,
	},

	{
		caseName: "Error in ArtifactCommit_Enter_00",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateRebootState{},
			&updateVerifyRebootState{},
			&updateAfterRebootState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateCommitState{},
			&updateRollbackState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateAfterRollbackRebootState{},
			&updateErrorState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"NeedsArtifactReboot",
			"ArtifactInstall_Leave_00",
			"ArtifactReboot_Enter_00",
			"ArtifactReboot",
			"ArtifactVerifyReboot",
			"ArtifactReboot_Leave_00",
			"ArtifactCommit_Enter_00",
			"ArtifactCommit_Error_00",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"ArtifactRollback_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"rebooting",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			ErrorStates: []string{"ArtifactCommit_Enter_00"},
		},
		installOutcome: tests.SuccessfulRollback,
	},

	{
		caseName: "Killed in ArtifactCommit_Enter_00",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateRebootState{},
			&updateVerifyRebootState{},
			&updateAfterRebootState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateCommitState{},
			&updateRollbackState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateAfterRollbackRebootState{},
			&updateErrorState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"NeedsArtifactReboot",
			"ArtifactInstall_Leave_00",
			"ArtifactReboot_Enter_00",
			"ArtifactReboot",
			"ArtifactVerifyReboot",
			"ArtifactReboot_Leave_00",
			"ArtifactCommit_Enter_00",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"ArtifactRollback_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"rebooting",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			SpontRebootStates: []string{"ArtifactCommit_Enter_00"},
		},
		installOutcome: tests.SuccessfulRollback,
	},

	{
		caseName: "Error in ArtifactCommit_Enter_00, no reboot",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateCommitState{},
			&updateRollbackState{},
			&updateErrorState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"NeedsArtifactReboot",
			"ArtifactInstall_Leave_00",
			"ArtifactCommit_Enter_00",
			"ArtifactCommit_Error_00",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"NeedsArtifactReboot",
			"ArtifactRollback_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			ErrorStates:    []string{"ArtifactCommit_Enter_00"},
			RebootDisabled: true,
		},
		installOutcome: tests.SuccessfulRollback,
	},

	{
		caseName: "Killed in ArtifactCommit_Enter_00, no reboot",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateCommitState{},
			&updateRollbackState{},
			&updateErrorState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"NeedsArtifactReboot",
			"ArtifactInstall_Leave_00",
			"ArtifactCommit_Enter_00",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"NeedsArtifactReboot",
			"ArtifactRollback_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			SpontRebootStates: []string{"ArtifactCommit_Enter_00"},
			RebootDisabled:    true,
		},
		installOutcome: tests.SuccessfulRollback,
	},

	{
		caseName: "Error in Download_Leave_00 state, no rollback",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"Download_Error_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			ErrorStates:      []string{"Download_Leave_00"},
			RollbackDisabled: true,
		},
		installOutcome: tests.SuccessfulRollback,
	},

	{
		caseName: "Killed in Download_Leave_00 state, no rollback",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			SpontRebootStates: []string{"Download_Leave_00"},
			RollbackDisabled:  true,
		},
		installOutcome: tests.SuccessfulRollback,
	},

	{
		caseName: "Error in ArtifactInstall_Leave_00 state, no rollback",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&fetchControlMapState{},
			&updateErrorState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"NeedsArtifactReboot",
			"ArtifactInstall_Leave_00",
			"ArtifactInstall_Error_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			ErrorStates:      []string{"ArtifactInstall_Leave_00"},
			RollbackDisabled: true,
		},
		installOutcome: tests.UnsuccessfulInstall,
	},

	{
		caseName: "Killed in ArtifactInstall_Leave_00 state, no rollback",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&fetchControlMapState{},
			&updateErrorState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"NeedsArtifactReboot",
			"ArtifactInstall_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			SpontRebootStates: []string{"ArtifactInstall_Leave_00"},
			RollbackDisabled:  true,
		},
		installOutcome: tests.UnsuccessfulInstall,
	},

	{
		caseName: "Error in ArtifactInstall_Leave_00",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&fetchControlMapState{},
			&updateRollbackState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateAfterRollbackRebootState{},
			&updateErrorState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"NeedsArtifactReboot",
			"ArtifactInstall_Leave_00",
			"ArtifactInstall_Error_00",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"ArtifactRollback_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			ErrorStates: []string{"ArtifactInstall_Leave_00"},
		},
		installOutcome: tests.SuccessfulRollback,
	},

	{
		caseName: "Killed in ArtifactInstall_Leave_00",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&fetchControlMapState{},
			&updateRollbackState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateAfterRollbackRebootState{},
			&updateErrorState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"NeedsArtifactReboot",
			"ArtifactInstall_Leave_00",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"ArtifactRollback_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			SpontRebootStates: []string{"ArtifactInstall_Leave_00"},
		},
		installOutcome: tests.SuccessfulRollback,
	},

	{
		caseName: "Error in ArtifactReboot_Leave_00",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateRebootState{},
			&updateVerifyRebootState{},
			&updateAfterRebootState{},
			&fetchControlMapState{},
			&updateRollbackState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateAfterRollbackRebootState{},
			&updateErrorState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"NeedsArtifactReboot",
			"ArtifactInstall_Leave_00",
			"ArtifactReboot_Enter_00",
			"ArtifactReboot",
			"ArtifactVerifyReboot",
			"ArtifactReboot_Leave_00",
			"ArtifactReboot_Error_00",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"ArtifactRollback_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"rebooting",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			ErrorStates: []string{"ArtifactReboot_Leave_00"},
		},
		installOutcome: tests.SuccessfulRollback,
	},

	{
		caseName: "Killed in ArtifactReboot_Leave_00",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateRebootState{},
			&updateVerifyRebootState{},
			&updateAfterRebootState{},
			&fetchControlMapState{},
			&updateRollbackState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateAfterRollbackRebootState{},
			&updateErrorState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"NeedsArtifactReboot",
			"ArtifactInstall_Leave_00",
			"ArtifactReboot_Enter_00",
			"ArtifactReboot",
			"ArtifactVerifyReboot",
			"ArtifactReboot_Leave_00",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"ArtifactRollback_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"rebooting",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			SpontRebootStates: []string{"ArtifactReboot_Leave_00"},
		},
		installOutcome: tests.SuccessfulRollback,
	},

	{
		caseName: "Error in ArtifactRollback_Leave_00",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateRebootState{},
			&updateVerifyRebootState{},
			&updateRollbackState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateAfterRollbackRebootState{},
			&updateErrorState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"NeedsArtifactReboot",
			"ArtifactInstall_Leave_00",
			"ArtifactReboot_Enter_00",
			"ArtifactReboot",
			"ArtifactVerifyReboot",
			"ArtifactReboot_Error_00",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"ArtifactRollback_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"rebooting",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			ErrorStates: []string{"ArtifactVerifyReboot", "ArtifactRollback_Leave_00"},
		},
		installOutcome: tests.SuccessfulRollback,
	},

	{
		caseName: "Killed in ArtifactRollback_Leave_00",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateRebootState{},
			&updateVerifyRebootState{},
			&updateRollbackState{},
			&updateRollbackRebootState{},
			&updateRollbackState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateAfterRollbackRebootState{},
			&updateErrorState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"NeedsArtifactReboot",
			"ArtifactInstall_Leave_00",
			"ArtifactReboot_Enter_00",
			"ArtifactReboot",
			"ArtifactVerifyReboot",
			"ArtifactReboot_Error_00",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"ArtifactRollback_Leave_00",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"ArtifactRollback_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"rebooting",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			ErrorStates:       []string{"ArtifactVerifyReboot"},
			SpontRebootStates: []string{"ArtifactRollback_Leave_00"},
		},
		installOutcome: tests.SuccessfulRollback,
	},

	{
		caseName: "Error in ArtifactRollbackReboot_Leave_00",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateRebootState{},
			&updateVerifyRebootState{},
			&updateRollbackState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateAfterRollbackRebootState{},
			&updateErrorState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"NeedsArtifactReboot",
			"ArtifactInstall_Leave_00",
			"ArtifactReboot_Enter_00",
			"ArtifactReboot",
			"ArtifactVerifyReboot",
			"ArtifactReboot_Error_00",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"ArtifactRollback_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"rebooting",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			ErrorStates: []string{"ArtifactVerifyReboot", "ArtifactRollbackReboot_Leave_00"},
		},
		installOutcome: tests.SuccessfulRollback,
	},

	{
		caseName: "Killed in ArtifactRollbackReboot_Leave_00",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateRebootState{},
			&updateVerifyRebootState{},
			&updateRollbackState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateAfterRollbackRebootState{},
			&updateErrorState{},
			&updateVerifyRollbackRebootState{},
			&updateAfterRollbackRebootState{},
			&updateErrorState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"NeedsArtifactReboot",
			"ArtifactInstall_Leave_00",
			"ArtifactReboot_Enter_00",
			"ArtifactReboot",
			"ArtifactVerifyReboot",
			"ArtifactReboot_Error_00",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"ArtifactRollback_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"rebooting",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			ErrorStates:       []string{"ArtifactVerifyReboot"},
			SpontRebootStates: []string{"ArtifactRollbackReboot_Leave_00"},
		},
		installOutcome: tests.SuccessfulRollback,
	},

	{
		caseName: "Error in ArtifactFailure_Leave_00",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&updateRollbackState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateAfterRollbackRebootState{},
			&updateErrorState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"ArtifactInstall_Error_00",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"NeedsArtifactReboot",
			"ArtifactRollback_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			ErrorStates: []string{"ArtifactInstall", "ArtifactFailure_Leave_00"},
		},
		installOutcome: tests.SuccessfulRollback,
	},

	{
		caseName: "Killed in ArtifactFailure_Leave_00",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&updateRollbackState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateAfterRollbackRebootState{},
			&updateErrorState{},
			&updateCleanupState{},
			&updateErrorState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"ArtifactInstall_Error_00",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"NeedsArtifactReboot",
			"ArtifactRollback_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			ErrorStates:       []string{"ArtifactInstall"},
			SpontRebootStates: []string{"ArtifactFailure_Leave_00"},
		},
		installOutcome: tests.SuccessfulRollback,
	},

	{
		caseName: "Error in ArtifactCommit_Leave_00",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateRebootState{},
			&updateVerifyRebootState{},
			&updateAfterRebootState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateCommitState{},
			&updateAfterFirstCommitState{},
			&updateAfterCommitState{},
			&updateCleanupState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"NeedsArtifactReboot",
			"ArtifactInstall_Leave_00",
			"ArtifactReboot_Enter_00",
			"ArtifactReboot",
			"ArtifactVerifyReboot",
			"ArtifactReboot_Leave_00",
			"ArtifactCommit_Enter_00",
			"ArtifactCommit",
			"ArtifactCommit_Leave_00",
			"ArtifactCommit_Error_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"rebooting",
			"installing",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			ErrorStates: []string{"ArtifactCommit_Leave_00"},
		},
		installOutcome: tests.UnsuccessfulInstall,
	},

	{
		caseName: "Killed in ArtifactCommit_Leave_00",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateRebootState{},
			&updateVerifyRebootState{},
			&updateAfterRebootState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateCommitState{},
			&updateAfterFirstCommitState{},
			&updateAfterCommitState{},
			&updateCleanupState{},
			&updateAfterCommitState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"NeedsArtifactReboot",
			"ArtifactInstall_Leave_00",
			"ArtifactReboot_Enter_00",
			"ArtifactReboot",
			"ArtifactVerifyReboot",
			"ArtifactReboot_Leave_00",
			"ArtifactCommit_Enter_00",
			"ArtifactCommit",
			"ArtifactCommit_Leave_00",
			"ArtifactCommit_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"rebooting",
			"installing",
			"success",
		},
		TestModuleAttr: tests.TestModuleAttr{
			SpontRebootStates: []string{"ArtifactCommit_Leave_00"},
		},
		installOutcome: tests.SuccessfulInstall,
	},

	{
		caseName: "Error in ArtifactCommit_Leave_00, no reboot",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateCommitState{},
			&updateAfterFirstCommitState{},
			&updateAfterCommitState{},
			&updateCleanupState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"NeedsArtifactReboot",
			"ArtifactInstall_Leave_00",
			"ArtifactCommit_Enter_00",
			"ArtifactCommit",
			"ArtifactCommit_Leave_00",
			"ArtifactCommit_Error_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			ErrorStates:    []string{"ArtifactCommit_Leave_00"},
			RebootDisabled: true,
		},
		installOutcome: tests.UnsuccessfulInstall,
	},

	{
		caseName: "Killed in ArtifactCommit_Leave_00, no reboot",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateCommitState{},
			&updateAfterFirstCommitState{},
			&updateAfterCommitState{},
			&updateCleanupState{},
			&updateAfterCommitState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"NeedsArtifactReboot",
			"ArtifactInstall_Leave_00",
			"ArtifactCommit_Enter_00",
			"ArtifactCommit",
			"ArtifactCommit_Leave_00",
			"ArtifactCommit_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"success",
		},
		TestModuleAttr: tests.TestModuleAttr{
			SpontRebootStates: []string{"ArtifactCommit_Leave_00"},
			RebootDisabled:    true,
		},
		installOutcome: tests.SuccessfulInstall,
	},

	{
		caseName: "Break out of error loop",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateRebootState{},
			&updateVerifyRebootState{},
			&updateRollbackState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			// Truncated after maximum number of state transitions.
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"NeedsArtifactReboot",
			"ArtifactInstall_Leave_00",
			"ArtifactReboot_Enter_00",
			"ArtifactReboot",
			"ArtifactVerifyReboot",
			"ArtifactReboot_Error_00",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"ArtifactRollback_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			// Truncated after maximum number of state transitions.
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"rebooting",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			ErrorStates:  []string{"ArtifactVerifyReboot", "ArtifactVerifyRollbackReboot"},
			ErrorForever: true,
		},
		installOutcome: tests.UnsuccessfulInstall,
	},

	{
		caseName: "Break out of spontaneous reboot loop",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateRebootState{},
			&updateVerifyRebootState{},
			&updateRollbackState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateAfterRollbackRebootState{},
			&updateErrorState{},
			&updateErrorState{},
			&updateErrorState{},
			&updateErrorState{},
			&updateErrorState{},
			&updateErrorState{},
			&updateErrorState{},
			&updateErrorState{},
			&updateErrorState{},
			&updateErrorState{},
			&updateErrorState{},
			&updateErrorState{},
			&updateErrorState{},
			&updateErrorState{},
			&updateErrorState{},
			&updateErrorState{},
			&updateErrorState{},
			&updateErrorState{},
			&updateErrorState{},
			&updateErrorState{},
			// Truncated after maximum number of state transitions.
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"NeedsArtifactReboot",
			"ArtifactInstall_Leave_00",
			"ArtifactReboot_Enter_00",
			"ArtifactReboot",
			"ArtifactVerifyReboot",
			"ArtifactReboot_Error_00",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"ArtifactRollback_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			// Truncated after maximum number of state transitions.
			"ArtifactFailure_Leave_00",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"rebooting",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			ErrorStates:        []string{"ArtifactVerifyReboot"},
			SpontRebootStates:  []string{"ArtifactFailure"},
			SpontRebootForever: true,
		},
		installOutcome: tests.UnsuccessfulInstall,
	},

	{
		caseName: "Hang in Download state",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"Download_Error_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			HangStates: []string{"Download"},
		},
		installOutcome: tests.SuccessfulRollback,
	},

	{
		caseName: "Hang in ArtifactInstall",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&updateRollbackState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateAfterRollbackRebootState{},
			&updateErrorState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"ArtifactInstall_Error_00",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"NeedsArtifactReboot",
			"ArtifactRollback_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			"installing",
			"failure",
		},
		TestModuleAttr: tests.TestModuleAttr{
			HangStates: []string{"ArtifactInstall"},
		},
		installOutcome: tests.SuccessfulRollback,
	},

	{
		caseName: "Temporary failure in report sending after reboot",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateRebootState{},
			&updateVerifyRebootState{},
			&updateAfterRebootState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateCommitState{},
			&updatePreCommitStatusReportRetryState{},
			&updateCommitState{},
			&updateAfterFirstCommitState{},
			&updateAfterCommitState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"NeedsArtifactReboot",
			"ArtifactInstall_Leave_00",
			"ArtifactReboot_Enter_00",
			"ArtifactReboot",
			"ArtifactVerifyReboot",
			"ArtifactReboot_Leave_00",
			"ArtifactCommit_Enter_00",
			"ArtifactCommit",
			"ArtifactCommit_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			// "installing", // Missing because of failStatusReportStatus below
			"rebooting",
			"installing",
			"success",
		},
		installOutcome:         tests.SuccessfulInstall,
		failStatusReportCount:  2,
		failStatusReportStatus: client.StatusInstalling,
	},

	{
		caseName: "Permanent failure in report sending after reboot",
		stateChain: []State{
			&updateFetchState{},
			&updateStoreState{},
			&updateAfterStoreState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateInstallState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateRebootState{},
			&updateVerifyRebootState{},
			&updateAfterRebootState{},
			&fetchControlMapState{},
			&controlMapState{},
			&updateCommitState{},
			&updatePreCommitStatusReportRetryState{},
			&updateCommitState{},
			&updatePreCommitStatusReportRetryState{},
			&updateCommitState{},
			&updatePreCommitStatusReportRetryState{},
			&updateCommitState{},
			&updatePreCommitStatusReportRetryState{},
			&updateRollbackState{},
			&updateRollbackRebootState{},
			&updateVerifyRollbackRebootState{},
			&updateAfterRollbackRebootState{},
			&updateErrorState{},
			&updateCleanupState{},
			&updateStatusReportState{},
			&idleState{},
		},
		artifactStateChain: []string{
			"Download_Enter_00",
			"Download",
			"SupportsRollback",
			"Download_Leave_00",
			"ArtifactInstall_Enter_00",
			"ArtifactInstall",
			"NeedsArtifactReboot",
			"ArtifactInstall_Leave_00",
			"ArtifactReboot_Enter_00",
			"ArtifactReboot",
			"ArtifactVerifyReboot",
			"ArtifactReboot_Leave_00",
			"ArtifactCommit_Enter_00",
			"ArtifactCommit_Error_00",
			"ArtifactRollback_Enter_00",
			"ArtifactRollback",
			"ArtifactRollback_Leave_00",
			"ArtifactRollbackReboot_Enter_00",
			"ArtifactRollbackReboot",
			"ArtifactVerifyRollbackReboot",
			"ArtifactRollbackReboot_Leave_00",
			"ArtifactFailure_Enter_00",
			"ArtifactFailure",
			"ArtifactFailure_Leave_00",
			"Cleanup",
		},
		reportsLog: []string{
			"downloading",
			// "installing", // Missing because of failStatusReportStatus below
			"rebooting",
			"failure",
		},
		installOutcome:         tests.SuccessfulRollback,
		failStatusReportCount:  100,
		failStatusReportStatus: client.StatusInstalling,
	},
}

// This test runs all state transitions for an update in a sub process,
// including state transitions that involve killing the process (which would
// happen in a spontaneous reboot situation), and tests that the transitions
// work all the way through.
func TestStateTransitionsWithUpdateModules(t *testing.T) {
	env, ok := os.LookupEnv("TestStateTransitionsWithUpdateModules")
	if ok && env == "subProcess" {
		// We are in the subprocess, run actual test case.
		for _, testCase := range stateTransitionsWithUpdateModulesTestCases {
			if os.Getenv("caseName") == testCase.caseName {
				subTestStateTransitionsWithUpdateModules(t,
					&testCase,
					os.Getenv("tmpdir"))
				return
			}
		}
		t.Errorf("Could not find test case \"%s\" in list", os.Getenv("caseName"))
	}

	// Each sub process will save the coverage info in the same file, overriding
	// the previous contents. Furthermore, the "main" test process will also
	// override the contents with its own coverage at the end
	// Create an independent file where to append cover results of each sub process
	coverMissingSubTestsFile, err := os.OpenFile(
		"coverage-missing-subtests.txt",
		os.O_TRUNC|os.O_CREATE|os.O_WRONLY,
		0644,
	)
	assert.Nil(t, err)
	defer coverMissingSubTestsFile.Close()

	// Write expected header
	header := "mode: set\n"
	assert.Nil(t, err)
	nBytesWritten, err := coverMissingSubTestsFile.Write([]byte(header))
	assert.Nil(t, err)
	assert.Equal(t, len(header), nBytesWritten)

	// Run test in sub command so that we can use kill during the test.
	for _, c := range stateTransitionsWithUpdateModulesTestCases {
		t.Run(c.caseName, func(t *testing.T) {
			t.Log(c.caseName)
			tmpdir, err := ioutil.TempDir("", "TestStateTransitionsWithUpdateModules")
			require.NoError(t, err)
			defer os.RemoveAll(tmpdir)

			tests.UpdateModulesSetup(
				t,
				&c.TestModuleAttr,
				tmpdir,
				tests.ArtifactAttributeOverrides{},
			)

			env := []string{}
			env = append(env, "TestStateTransitionsWithUpdateModules=subProcess")
			env = append(env, fmt.Sprintf("caseName=%s", c.caseName))
			env = append(env, fmt.Sprintf("tmpdir=%s", tmpdir))

			arg0, args := cmdLineForSubTest()

			killCount := 0
			for {
				cmd := exec.Command(arg0, args...)
				cmd.Env = env
				output, err := cmd.CombinedOutput()
				t.Log(string(output))
				if err == nil {
					break
				}

				waitStatus := cmd.ProcessState.Sys().(syscall.WaitStatus)
				if waitStatus.Signal() == syscall.SIGKILL &&
					(killCount < len(c.SpontRebootStates) || c.SpontRebootForever) {

					t.Log("Killed as expected")
					killCount++
					continue
				}

				t.Fatal(err.Error())
			}

			// Append coverage results to coverMissingSubTestsFile
			var filenameCoverProfile string
			for c := 1; c < len(os.Args); c++ {
				if strings.Contains(os.Args[c], "-test.coverprofile=") {
					filenameCoverProfile = strings.TrimPrefix(os.Args[c], "-test.coverprofile=")
					break
				}
			}
			if len(filenameCoverProfile) > 0 {
				sourceSubTest, err := os.Open(filenameCoverProfile)
				assert.Nil(t, err)

				// Discard the header of the coverage file
				header := "mode: set\n"
				buf := make([]byte, len(header))
				bytesHeader, err := io.ReadFull(sourceSubTest, buf)
				assert.Nil(t, err)
				assert.NotZero(t, bytesHeader)
				assert.Equal(t, string(buf), header)

				_, err = io.Copy(coverMissingSubTestsFile, sourceSubTest)
				assert.Nil(t, err)

				err = sourceSubTest.Close()
				assert.Nil(t, err)
			}

			logContent := make([]byte, 10000)

			log, err := os.Open(path.Join(tmpdir, "execution.log"))
			require.NoError(t, err)
			n, err := log.Read(logContent)
			require.NoError(t, err)
			assert.True(t, n > 0)
			logList := strings.Split(string(bytes.TrimRight(logContent[:n], "\n")), "\n")
			assert.Equal(t, c.artifactStateChain, logList)
			log.Close()

			log, err = os.Open(path.Join(tmpdir, "reports.log"))
			require.NoError(t, err)
			n, err = log.Read(logContent)
			if err == io.EOF {
				require.Equal(t, 0, n)
			} else {
				require.NoError(t, err)
				assert.True(t, n > 0)
			}
			logList = strings.Split(string(bytes.TrimRight(logContent[:n], "\n")), "\n")
			assert.Equal(t, c.reportsLog, logList)
			log.Close()
		})
	}
}

func cmdLineForSubTest() (string, []string) {
	args := make([]string, 0, len(os.Args)+2)
	for c := 1; c < len(os.Args); c++ {
		if os.Args[c] == "-test.run" {
			// Skip "-test.run" arguments. We will add our own.
			c += 1
			continue
		}
		args = append(args, os.Args[c])
	}
	args = append(args, "-test.run")
	args = append(args, "TestStateTransitionsWithUpdateModules")

	return os.Args[0], args
}

// This entire function is executed in a sub process, so we can freely mess with
// the client state without cleaning up, and even kill it.
func subTestStateTransitionsWithUpdateModules(t *testing.T,
	c *stateTransitionsWithUpdateModulesTestCase,
	tmpdir string) {

	ctx, mender := subProcessSetup(t, tmpdir)

	mender.failStatusReportCount = c.failStatusReportCount
	mender.failStatusReportStatus = c.failStatusReportStatus

	var state State
	var stateIndex int

	// Since we may be killed and restarted, read where we were in the state
	// indexes.
	indexText, err := ctx.Store.ReadAll("test_stateIndex")
	if os.IsNotExist(err) {
		// Shortcut into update check state: a new update.
		ucs := *States.UpdateCheck
		state = &ucs
		// Set initial Artifact name
		mender.Store.WriteAll(datastore.ArtifactNameKey, []byte("old_name"))
	} else {
		require.NoError(t, err)
		// Start in init state, which should resume the correct state
		// after a kill/reboot.
		init := *States.Init
		state = &init
		stateIndex64, err := strconv.ParseInt(string(indexText), 0, 0)
		require.NoError(t, err)
		stateIndex = int(stateIndex64)
		t.Logf("Resuming from state index %d (%T)",
			stateIndex, c.stateChain[stateIndex])
	}

	// IMPORTANT: Do not use "assert.Whatever()", but only
	// "require.Whatever()" in this function. The reason is that we may get
	// killed, and then the status from asserts is lost.
	for _, expectedState := range c.stateChain[stateIndex:] {
		// Store next state index we will enter
		indexText = []byte(fmt.Sprintf("%d", stateIndex))
		require.NoError(t, ctx.Store.WriteAll("test_stateIndex", indexText))

		// Now do state transition, which may kill us (part of testing
		// spontaneous reboot)
		var cancelled bool
		state, cancelled = transitionState(state, ctx, mender)
		require.False(t, cancelled)
		require.IsTypef(t, expectedState, state, "state index %d", stateIndex)

		stateIndex++
	}

	name, err := mender.GetCurrentArtifactName()
	require.NoError(t, err)
	switch c.installOutcome {
	case tests.SuccessfulInstall:
		require.Equal(t, "artifact-name", name)
	case tests.SuccessfulRollback:
		require.Equal(t, "old_name", name)
	case tests.UnsuccessfulInstall:
		require.Equal(t, "artifact-name"+conf.BrokenArtifactSuffix, name)
	default:
		require.True(t, false, "installOutcome must be defined for test")
	}
}

func subProcessSetup(t *testing.T,
	tmpdir string) (*StateContext, *menderWithCustomUpdater) {

	store.LmdbNoSync = true
	dbStore := store.NewDBStore(path.Join(tmpdir, "db"))

	ctx := StateContext{
		Store: dbStore,
	}

	config := conf.MenderConfig{
		MenderConfigFromFile: conf.MenderConfigFromFile{
			Servers: []conf.MenderServer{
				{
					ServerURL: "https://not-used",
				},
			},
			ModuleTimeoutSeconds:      5,
			UpdatePollIntervalSeconds: 5,
			RetryPollIntervalSeconds:  5,
		},
		ModulesPath:         path.Join(tmpdir, "modules"),
		ModulesWorkPath:     path.Join(tmpdir, "work"),
		ArtifactScriptsPath: path.Join(tmpdir, "scripts"),
		RootfsScriptsPath:   path.Join(tmpdir, "scriptdir"),
	}

	menderPieces := MenderPieces{
		Store: dbStore,
		AuthManager: NewAuthManager(AuthManagerConfig{
			AuthDataStore: dbStore,
			KeyStore: store.NewKeystore(
				dbStore,
				conf.DefaultKeyFile,
				"",
				false,
				defaultKeyPassphrase,
			),
			IdentitySource: &dev.IdentityDataRunner{
				Cmdr: stest.NewTestOSCalls("mac=foobar", 0),
			},
			Config: &config,
		}),
	}

	DeploymentLogger = NewDeploymentLogManager(path.Join(tmpdir, "logs"))
	// In most other places we need to clean up the DeploymentLogger by
	// setting it to nil, and cleaning up the tmpdir. However, we don't need
	// it here, because this is a sub process which will lose the pointer
	// anyway, and the parent process will clean up the tmpdir.

	log.SetLevel(log.DebugLevel)

	reports, err := os.OpenFile(path.Join(tmpdir, "reports.log"),
		os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
	require.NoError(t, err)

	mender, err := NewMender(&config, menderPieces)
	require.NoError(t, err)
	controller := menderWithCustomUpdater{
		Mender:       mender,
		reportWriter: reports,
	}

	controller.DeviceTypeFile = path.Join(tmpdir, "device_type")
	controller.StateScriptPath = path.Join(tmpdir, "scripts")

	artPath := path.Join(tmpdir, "artifact.mender")
	updateStream, err := os.Open(artPath)
	assert.NoError(t, err)
	controller.updater.fetchUpdateReturnReadCloser = updateStream

	// Avoid waiting by setting a short retry time.
	client.ExponentialBackoffSmallestUnit = time.Millisecond

	return &ctx, &controller
}

func TestDBSchemaUpdate(t *testing.T) {
	tmpdir, err := ioutil.TempDir("", "TestDBSchemaUpdate")
	require.NoError(t, err)
	defer os.RemoveAll(tmpdir)

	store.LmdbNoSync = true
	defer func() {
		store.LmdbNoSync = false
	}()

	db := store.NewDBStore(tmpdir)
	defer db.Close()

	origSd := datastore.StateData{}
	origSd.UpdateInfo.ID = "abc"
	origSd.UpdateInfo.Artifact.ArtifactName = "oldname"

	// Check that default is to store using StateDataKey.
	sd := datastore.StateData{
		UpdateInfo: datastore.UpdateInfo{
			ID: "abc",
			Artifact: datastore.Artifact{
				ArtifactName: "oldname",
			},
		},
	}
	require.NoError(t, datastore.StoreStateData(db, sd, true))
	sd, err = datastore.LoadStateData(db)
	require.NoError(t, err)

	_, err = db.ReadAll(datastore.StateDataKeyUncommitted)
	assert.Error(t, err)
	_, err = db.ReadAll(datastore.StateDataKey)
	assert.NoError(t, err)

	assert.Equal(t, "abc", sd.UpdateInfo.ID)
	assert.Equal(t, "oldname", sd.UpdateInfo.Artifact.ArtifactName)
	assert.Equal(t, datastore.StateDataVersion, sd.Version)
	assert.False(t, sd.UpdateInfo.HasDBSchemaUpdate)

	// Store an old version in the DB.
	sd = datastore.StateData{
		Version: 1,
		UpdateInfo: datastore.UpdateInfo{
			ID: "abc",
			Artifact: datastore.Artifact{
				ArtifactName: "oldname",
			},
		},
	}
	require.NoError(t, datastore.StoreStateData(db, sd, true))
	sd, err = datastore.LoadStateData(db)
	require.NoError(t, err)

	// Now both should be stored.
	_, err = db.ReadAll(datastore.StateDataKeyUncommitted)
	assert.NoError(t, err)
	_, err = db.ReadAll(datastore.StateDataKey)
	assert.NoError(t, err)

	assert.Equal(t, "abc", sd.UpdateInfo.ID)
	assert.Equal(t, "oldname", sd.UpdateInfo.Artifact.ArtifactName)
	assert.Equal(t, datastore.StateDataVersion, sd.Version)
	assert.True(t, sd.UpdateInfo.HasDBSchemaUpdate)

	// Check that storing a new one does not affect the committed one.
	sd = datastore.StateData{
		UpdateInfo: datastore.UpdateInfo{
			ID: "abc",
			Artifact: datastore.Artifact{
				ArtifactName: "newname",
			},
			HasDBSchemaUpdate: true,
		},
	}
	require.NoError(t, datastore.StoreStateData(db, sd, true))

	// Check manually for both.
	data, err := db.ReadAll(datastore.StateDataKeyUncommitted)
	require.NoError(t, err)
	err = json.Unmarshal(data, &sd)
	require.NoError(t, err)

	assert.Equal(t, "abc", sd.UpdateInfo.ID)
	assert.Equal(t, "newname", sd.UpdateInfo.Artifact.ArtifactName)
	assert.Equal(t, datastore.StateDataVersion, sd.Version)
	assert.True(t, sd.UpdateInfo.HasDBSchemaUpdate)

	data, err = db.ReadAll(datastore.StateDataKey)
	require.NoError(t, err)
	err = json.Unmarshal(data, &sd)
	require.NoError(t, err)

	assert.Equal(t, "abc", sd.UpdateInfo.ID)
	assert.Equal(t, "oldname", sd.UpdateInfo.Artifact.ArtifactName)
	assert.Equal(t, 1, sd.Version)
	assert.False(t, sd.UpdateInfo.HasDBSchemaUpdate)

	// Check loading.
	sd, err = datastore.LoadStateData(db)
	require.NoError(t, err)

	assert.Equal(t, "abc", sd.UpdateInfo.ID)
	assert.Equal(t, "newname", sd.UpdateInfo.Artifact.ArtifactName)
	assert.Equal(t, datastore.StateDataVersion, sd.Version)
	assert.True(t, sd.UpdateInfo.HasDBSchemaUpdate)

	// Check that storing an entry with a different update ID (stale entry)
	// is ignored.
	sd = datastore.StateData{
		UpdateInfo: datastore.UpdateInfo{
			ID: "def",
			Artifact: datastore.Artifact{
				ArtifactName: "newname",
			},
			HasDBSchemaUpdate: true,
		},
	}
	require.NoError(t, datastore.StoreStateData(db, sd, true))

	data, err = db.ReadAll(datastore.StateDataKeyUncommitted)
	require.NoError(t, err)
	err = json.Unmarshal(data, &sd)
	require.NoError(t, err)

	assert.Equal(t, "def", sd.UpdateInfo.ID)
	assert.Equal(t, "newname", sd.UpdateInfo.Artifact.ArtifactName)
	assert.Equal(t, datastore.StateDataVersion, sd.Version)
	assert.True(t, sd.UpdateInfo.HasDBSchemaUpdate)

	data, err = db.ReadAll(datastore.StateDataKey)
	require.NoError(t, err)
	err = json.Unmarshal(data, &sd)
	require.NoError(t, err)

	assert.Equal(t, "abc", sd.UpdateInfo.ID)
	assert.Equal(t, "oldname", sd.UpdateInfo.Artifact.ArtifactName)
	assert.Equal(t, 1, sd.Version)
	assert.False(t, sd.UpdateInfo.HasDBSchemaUpdate)

	sd, err = datastore.LoadStateData(db)
	require.NoError(t, err)

	assert.Equal(t, "abc", sd.UpdateInfo.ID)
	assert.Equal(t, "oldname", sd.UpdateInfo.Artifact.ArtifactName)
	assert.Equal(t, datastore.StateDataVersion, sd.Version)
	assert.True(t, sd.UpdateInfo.HasDBSchemaUpdate)

	// Check that committing the structure removes the uncommitted one.
	sd = datastore.StateData{
		UpdateInfo: datastore.UpdateInfo{
			ID: "abc",
			Artifact: datastore.Artifact{
				ArtifactName: "newname",
			},
			HasDBSchemaUpdate: false,
		},
	}
	require.NoError(t, datastore.StoreStateData(db, sd, true))

	_, err = db.ReadAll(datastore.StateDataKeyUncommitted)
	assert.Error(t, err)

	data, err = db.ReadAll(datastore.StateDataKey)
	require.NoError(t, err)
	err = json.Unmarshal(data, &sd)
	require.NoError(t, err)

	assert.Equal(t, "abc", sd.UpdateInfo.ID)
	assert.Equal(t, "newname", sd.UpdateInfo.Artifact.ArtifactName)
	assert.Equal(t, datastore.StateDataVersion, sd.Version)
	assert.False(t, sd.UpdateInfo.HasDBSchemaUpdate)

	sd, err = datastore.LoadStateData(db)
	require.NoError(t, err)

	assert.Equal(t, "abc", sd.UpdateInfo.ID)
	assert.Equal(t, "newname", sd.UpdateInfo.Artifact.ArtifactName)
	assert.Equal(t, datastore.StateDataVersion, sd.Version)
	assert.False(t, sd.UpdateInfo.HasDBSchemaUpdate)
}

func TestAutomaticReboot(t *testing.T) {
	tempDir, _ := ioutil.TempDir("", "logs")
	defer os.RemoveAll(tempDir)

	DeploymentLogger = NewDeploymentLogManager(tempDir)
	defer func() {
		DeploymentLogger.Disable()
		DeploymentLogger = nil
	}()

	log.AddHook(NewDeploymentLogHook(DeploymentLogger))

	ctx := &StateContext{
		Store:    store.NewMemStore(),
		Rebooter: system.NewSystemRebootCmd(stest.NewTestOSCalls("Called reboot", 99)),
	}
	u := &datastore.UpdateInfo{
		Artifact: datastore.Artifact{
			PayloadTypes: []string{"test-type"},
		},
		ID:              "abc",
		RebootRequested: datastore.RebootRequestedType{datastore.RebootTypeAutomatic},
	}
	c := &stateTestController{}
	rebootState := NewUpdateRebootState(u)

	state, cancelled := rebootState.Handle(ctx, c)

	assert.False(t, cancelled)
	assert.IsType(t, &updateErrorState{}, state)

	logs, err := DeploymentLogger.GetLogs("abc")
	require.NoError(t, err)
	assert.Contains(t, string(logs), "exit status 99")
}

type loopingNotPermittedUpdateState struct {
	updateState
	// Sounds counter-intuitive to have loopCount in here, but we will use
	// it to loop back to ourselves when it runs out, thereby looping
	// "illegally" and trigger the state counter overflow, hence the struct
	// name. While it's positive, we go to the loopingState instead, and
	// this looping is allowed.
	loopCount int
	loopTo    State
}
type loopingPermittedUpdateState struct {
	updateState
	loopTo State
}

// Looping in the base state is permitted simply by power of not being an update
// state.
type loopingPermittedBaseState struct {
	baseState
	loopTo State
}

func (s *loopingNotPermittedUpdateState) Handle(ctx *StateContext, c Controller) (State, bool) {
	s.loopCount--
	if s.loopCount < 0 {
		return s, false
	} else {
		return s.loopTo, false
	}
}

func (s *loopingPermittedUpdateState) Handle(ctx *StateContext, c Controller) (State, bool) {
	return s.loopTo, false
}

func (s *loopingPermittedUpdateState) PermitLooping() bool {
	return true
}

func (s *loopingPermittedBaseState) Handle(ctx *StateContext, c Controller) (State, bool) {
	return s.loopTo, false
}

const TestLoopingStatesCount = 500

// Test that the state transitions work correctly for looping states, and do not
// abruptly break out of loops.
func TestLoopingStates(t *testing.T) {
	// It should be possible to loop indefinitely when looping is permitted,
	// but as soon as the states involved do not permit looping it should
	// trigger a failure after a certain number of states.

	// -------------- updateState ------------------
	lpu := &loopingPermittedUpdateState{}
	lnp := &loopingNotPermittedUpdateState{
		loopCount: TestLoopingStatesCount,
		loopTo:    lpu,
	}
	lpu.loopTo = lnp

	ms := store.NewMemStore()
	ctx := &StateContext{
		Store: ms,
	}
	sc := &stateTestController{
		state: lpu,
	}

	var count int
	var currentState State = lpu
	// Times two because we need to go through two states to increase the
	// count in one of them.
	transitionsExpected := TestLoopingStatesCount*2 + datastore.MaximumStateDataStoreCount + 2

	for count = 0; count < transitionsExpected+50; count++ {
		currentState, _ = transitionState(currentState, ctx, sc)
		if currentState != lnp && currentState != lpu {
			break
		}
	}
	assert.Equal(t, transitionsExpected, count)
	assert.NotEqual(t, currentState, lnp)
	assert.NotEqual(t, currentState, lpu)

	// -------------- baseState ------------------
	lpb := &loopingPermittedBaseState{}
	lnp = &loopingNotPermittedUpdateState{
		loopCount: TestLoopingStatesCount,
		loopTo:    lpb,
	}
	lpb.loopTo = lnp

	ms = store.NewMemStore()
	ctx = &StateContext{
		Store: ms,
	}
	sc = &stateTestController{
		state: lpb,
	}

	currentState = lpb
	// Times two because we need to go through two states to increase the
	// count in one of them.
	transitionsExpected = TestLoopingStatesCount*2 + datastore.MaximumStateDataStoreCount + 2

	for count = 0; count < transitionsExpected+50; count++ {
		currentState, _ = transitionState(currentState, ctx, sc)
		if currentState != lnp && currentState != lpb {
			break
		}
	}
	assert.Equal(t, transitionsExpected, count)
	assert.NotEqual(t, currentState, lnp)
	assert.NotEqual(t, currentState, lpb)
}

func TestControlMapState(t *testing.T) {
	tests := map[string]struct {
		action   string
		state    string
		expected State
	}{
		"OK - Continue": {
			state:    "ArtifactInstall_Enter",
			action:   "continue",
			expected: &updateInstallState{},
		},
		"OK - Pause": {
			state:    "ArtifactInstall_Enter",
			action:   "pause",
			expected: &controlMapPauseState{},
		},
		"OK - Fail": {
			state:    "ArtifactInstall_Enter",
			action:   "fail",
			expected: &updateErrorState{},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Log(name)

			ms := store.NewMemStore()
			pool := NewControlMap(
				ms,
				conf.DefaultUpdateControlMapBootExpirationTimeSeconds,
				conf.DefaultUpdateControlMapBootExpirationTimeSeconds)

			pool.Insert((&updatecontrolmap.UpdateControlMap{
				ID: "foo",
				States: map[string]updatecontrolmap.UpdateControlMapState{
					test.state: {
						Action: test.action,
					},
				},
			}).Stamp(100))
			ctx := &StateContext{
				Store:         ms,
				pauseReported: make(map[string]bool),
			}
			c := &stateTestController{controlMap: pool}
			u := &datastore.UpdateInfo{}

			next, _ := NewControlMapState(NewUpdateInstallState(u), nil).Handle(ctx, c)
			assert.IsType(t, test.expected, next)
		})
	}
}

func TestControlMapPauseState(t *testing.T) {
	// Test that the map insertion wakes the client up from sleep
	ms := store.NewMemStore()
	pool := NewControlMap(
		ms,
		conf.DefaultUpdateControlMapBootExpirationTimeSeconds,
		conf.DefaultUpdateControlMapBootExpirationTimeSeconds)

	var serverUpdateControlMap = (&updatecontrolmap.UpdateControlMap{
		ID: "foo",
		States: map[string]updatecontrolmap.UpdateControlMapState{
			"ArtifactInstall_Enter": {
				Action: "pause",
			},
		},
	}).Stamp(2)
	pool.Insert(serverUpdateControlMap)
	ctx := &StateContext{
		Store: ms,
	}
	c := &stateTestController{
		controlMap:      pool,
		updatePollIntvl: 30 * time.Second,
	}
	u := &datastore.UpdateInfo{ID: "foo"}

	next, _ := NewControlMapPauseState(NewUpdateInstallState(u)).Handle(ctx, c)
	assert.IsType(t, &controlMapState{}, next)

	// Now that there is no more wake-ups in store from the control maps,
	// have the timer expire, and a new server check for new maps should occur
	next, _ = NewControlMapPauseState(NewUpdateInstallState(u)).Handle(ctx, c)
	assert.IsType(t, &fetchControlMapState{}, next)

	// We expire the server side map and insert a new one with a different ID
	serverUpdateControlMap.Expire()
	var localUpdateControlMap = (&updatecontrolmap.UpdateControlMap{
		ID: "bar",
		States: map[string]updatecontrolmap.UpdateControlMapState{
			"ArtifactInstall_Enter": {
				Action: "pause",
			},
		},
	}).Stamp(2)
	pool.Insert(localUpdateControlMap)

	// The returned state shall be controlMapState both when wake-up from store
	next, _ = NewControlMapPauseState(NewUpdateInstallState(u)).Handle(ctx, c)
	assert.IsType(t, &controlMapState{}, next)

	// and on ticker expiry
	next, _ = NewControlMapPauseState(NewUpdateInstallState(u)).Handle(ctx, c)
	assert.IsType(t, &controlMapState{}, next)
}

func TestControlMapFetch(t *testing.T) {

	tests := map[string]struct {
		controlMapRefreshError error
		expectedNextState      State
	}{
		"OK - no errors fetching update": {
			controlMapRefreshError: nil,
			expectedNextState:      &controlMapState{},
		},
		"Err: deployment aborted": {
			controlMapRefreshError: client.ErrNoDeploymentAvailable,
			expectedNextState:      &updateErrorState{},
		},
		"Err: generic network issue": {
			controlMapRefreshError: errors.New("Generic network error"),
			expectedNextState:      &fetchRetryControlMapState{},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			ms := store.NewMemStore()
			ctx := &StateContext{
				Store: ms,
			}
			c := &stateTestController{
				refreshControlMapError: test.controlMapRefreshError,
				controlMap: NewControlMap(
					store.NewMemStore(),
					conf.DefaultUpdateControlMapBootExpirationTimeSeconds,
					conf.DefaultUpdateControlMapBootExpirationTimeSeconds,
				),
			}
			// Add a map to the pool
			c.controlMap.insertMap(func(u *updatecontrolmap.UpdateControlMap) bool {
				return true
			}, &updatecontrolmap.UpdateControlMap{
				ID: "foobar",
			})

			next, _ := NewFetchControlMapState(
				NewUpdateInstallState(
					&datastore.UpdateInfo{
						ID: "foobar",
					}), nil).
				Handle(ctx, c)
			assert.IsType(t, test.expectedNextState, next)
		})
	}
}

func TestFetchRetryUpdateControl(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping long running test (1m wait)")
	}

	t.Parallel()

	ms := store.NewMemStore()
	ctx := &StateContext{
		Store: ms,
	}
	c := &stateTestController{
		updatePollIntvl: 1 * time.Second,
	}

	next, _ := NewFetchRetryControlMapState(
		NewUpdateInstallState(
			&datastore.UpdateInfo{}), nil).
		Handle(ctx, c)

	assert.IsType(t, &fetchControlMapState{}, next)
}

func TestArtifactRollbackRebootUpdateModuleRebootHandling(t *testing.T) {
	tempDir, _ := ioutil.TempDir("", "logs")
	DeploymentLogger = NewDeploymentLogManager(tempDir)
	defer func() {
		DeploymentLogger = nil
		os.RemoveAll(tempDir)
	}()

	tests := map[string]struct {
		setInitialValue   bool
		initialValue      datastore.RebootType
		moduleRebootReply installer.RebootAction
		expectedNextState State
	}{
		"Reboot already requested": {
			setInitialValue:   true,
			initialValue:      datastore.RebootTypeAutomatic,
			moduleRebootReply: installer.RebootRequired,
			expectedNextState: &updateRollbackRebootState{},
		},
		"Reboot not reported, but is requested - required": {
			initialValue:      datastore.RebootTypeNone,
			moduleRebootReply: installer.RebootRequired,
			expectedNextState: &updateRollbackRebootState{},
		},
		"Reboot not reported, but is requested - automatic": {
			initialValue:      datastore.RebootTypeNone,
			moduleRebootReply: installer.AutomaticReboot,
			expectedNextState: &updateRollbackRebootState{},
		},
		"Reboot not reported, and is not requested": {
			initialValue:      datastore.RebootTypeNone,
			moduleRebootReply: installer.NoReboot,
			expectedNextState: &updateErrorState{},
		},
	}

	for name, testCase := range tests {
		t.Logf("Running testcase:  %s", name)
		ms := store.NewMemStore()
		ctx := &StateContext{
			Store: ms,
		}
		c := &stateTestController{}

		c.FakeDevice.NeedsRebootReturnValue = &testCase.moduleRebootReply
		c.installers = []installer.PayloadUpdatePerformer{c.FakeDevice}
		ds := &datastore.UpdateInfo{}
		state := NewUpdateRollbackState(ds)
		if testCase.setInitialValue {
			state.Update().RebootRequested.Set(0, testCase.initialValue)
		}

		next, _ := state.Handle(ctx, c)
		assert.IsType(t, testCase.expectedNextState, next)

	}
}
