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
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"testing"

	"github.com/mendersoftware/log"
	"github.com/mendersoftware/mender/client"
	cltest "github.com/mendersoftware/mender/client/test"
	"github.com/mendersoftware/mender/store"
	"github.com/stretchr/testify/assert"
)

type testState struct {
	t                Transition
	shouldErrorEnter bool
	shouldErrorLeave bool
	shouldErrorError bool
	next             State
}

func (s *testState) Handle(ctx *StateContext, c Controller) (State, bool) {
	return s.next, false
}

func (s *testState) Cancel() bool { return true }

func (s *testState) Id() MenderState { return MenderStateInit }

func (s *testState) Transition() Transition        { return s.t }
func (s *testState) SetTransition(tran Transition) { s.t = tran }

type stateScript struct {
	state  string
	action string
}

type spontanaeousRebootExecutor struct {
	expectedActions []string // test colouring
}

var panicFlag = false

func (sre *spontanaeousRebootExecutor) ExecuteAll(state, action string, ignoreError bool) error {
	log.Info("Executing all in spont-reboot")
	sre.expectedActions = append(sre.expectedActions, action)
	panicFlag = !panicFlag // flip
	if panicFlag {
		panic(fmt.Sprintf("state: %v action: %v", state, action))
	}
	return nil
}

func (te *spontanaeousRebootExecutor) CheckRootfsScriptsVersion() error {
	return nil
}

type testExecutor struct {
	executed   []stateScript
	execErrors map[stateScript]bool
}

func (te *testExecutor) ExecuteAll(state, action string, ignoreError bool) error {
	te.executed = append(te.executed, stateScript{state, action})

	if _, ok := te.execErrors[stateScript{state, action}]; ok {
		if ignoreError {
			return nil
		}
		return errors.New("error executing script")
	}
	return nil
}

func (te *testExecutor) CheckRootfsScriptsVersion() error {
	return nil
}

func (te *testExecutor) setExecError(state *testState) {
	if state.shouldErrorEnter {
		te.execErrors[stateScript{state.Transition().String(), "Enter"}] = true
	}
	if state.shouldErrorLeave {
		te.execErrors[stateScript{state.Transition().String(), "Leave"}] = true
	}
	if state.shouldErrorError {
		te.execErrors[stateScript{state.Transition().String(), "Error"}] = true
	}
}

func (te *testExecutor) verifyExecuted(should []stateScript) bool {
	if len(should) != len(te.executed) {
		return false
	}
	for i, _ := range te.executed {
		if should[i] != te.executed[i] {
			return false
		}
	}
	return true
}

func TestSpontanaeousReboot(t *testing.T) {

	// create temp dir
	td, _ := ioutil.TempDir("", "mender-install-update-")
	defer os.RemoveAll(td)

	// prepare fake artifactInfo file
	artifactInfo := path.Join(td, "artifact_info")
	// prepare fake device type file
	deviceType := path.Join(td, "device_type")

	atok := client.AuthToken("authorized")
	authMgr := &testAuthManager{
		authorized: true,
		authtoken:  atok,
	}

	srv := cltest.NewClientTestServer()
	defer srv.Close()

	mender := newTestMender(nil,
		menderConfig{
			ServerURL: srv.URL,
		},
		testMenderPieces{
			MenderPieces: MenderPieces{
				// device: &fakedevice{}
				authMgr: authMgr,
			},
		},
	)

	ctx := StateContext{store: store.NewMemStore()}

	transitions := [][]struct {
		from              State
		to                State
		expectedStateData *StateData
		transitionStatus  TransitionStatus
		expectedActions   []string
		modifyServer      func()
	}{
		{ // The code will step through a transition stepwise as a panic in executeAll will flip
			// every time it is run
			{
				// init -> idle
				// Fail in transition enter
				from:             initState,
				to:               idleState,
				transitionStatus: NoStatus,
				expectedStateData: &StateData{
					Version:          1, // standard version atm // FIXME export the field(?)
					FromState:        MenderStateInit,
					ToState:          MenderStateIdle,
					TransitionStatus: LeaveDone,
				},
				expectedActions: []string{"Enter"},
			},
			{
				// finish enter and state
				from:             initState,
				to:               idleState,
				transitionStatus: LeaveDone,
				expectedStateData: &StateData{
					Version:          1,
					FromState:        MenderStateIdle,
					ToState:          MenderStateCheckWait,
					TransitionStatus: NoStatus,
				},
				expectedActions: []string{"Enter"},
			},
		},
		{
			{
				// no transition done here
				from:             idleState,
				to:               checkWaitState,
				transitionStatus: NoStatus,
				expectedStateData: &StateData{
					Version:          1,
					FromState:        MenderStateCheckWait,
					ToState:          MenderStateInventoryUpdate,
					TransitionStatus: NoStatus,
				},
				expectedActions: nil,
			},
			{
				// fail in idle-leave
				from:             checkWaitState,
				to:               inventoryUpdateState,
				transitionStatus: NoStatus,
				expectedStateData: &StateData{
					Version:          1,
					FromState:        MenderStateCheckWait,
					ToState:          MenderStateInventoryUpdate,
					TransitionStatus: NoStatus,
				},
				expectedActions: []string{"Leave"},
			},
			{
				// finish idle-leave, fail in sync-enter
				from:             checkWaitState,
				to:               inventoryUpdateState,
				transitionStatus: NoStatus,
				expectedStateData: &StateData{
					Version:          1,
					FromState:        MenderStateCheckWait,
					ToState:          MenderStateInventoryUpdate,
					TransitionStatus: LeaveDone,
				},
				expectedActions: []string{"Leave", "Enter"},
			},
			{
				// finish the transition
				from:             checkWaitState,
				to:               inventoryUpdateState,
				transitionStatus: LeaveDone,
				expectedStateData: &StateData{
					Version:          1,
					FromState:        MenderStateInventoryUpdate,
					ToState:          MenderStateCheckWait,
					TransitionStatus: NoStatus,
				},
				expectedActions: []string{"Enter"},
			},
			{
				// from invupdate to checkwait, fail leave
				from:             inventoryUpdateState,
				to:               checkWaitState,
				transitionStatus: NoStatus,
				expectedStateData: &StateData{
					Version:          1,
					FromState:        MenderStateInventoryUpdate,
					ToState:          MenderStateCheckWait,
					TransitionStatus: NoStatus,
				},
				expectedActions: []string{"Leave"},
			},
			{
				// from invupdate to checkwait, finish leave, fail enter
				from:             inventoryUpdateState,
				to:               checkWaitState,
				transitionStatus: NoStatus,
				expectedStateData: &StateData{
					Version:          1,
					FromState:        MenderStateInventoryUpdate,
					ToState:          MenderStateCheckWait,
					TransitionStatus: LeaveDone,
				},
				expectedActions: []string{"Leave", "Enter"},
			},
			{
				// from invupdate to checkwait, finish enter and state
				from:             inventoryUpdateState,
				to:               checkWaitState,
				transitionStatus: LeaveDone,
				expectedStateData: &StateData{
					Version:          1,
					FromState:        MenderStateCheckWait,
					ToState:          MenderStateUpdateCheck,
					TransitionStatus: NoStatus,
				},
				expectedActions: []string{"Enter"},
			},
		},
		// checkwait -> updatecheck but make an update available
		{
			{
				// fail in leave
				modifyServer: func() {
					// prepare an update
					srv.Update.Has = true
					srv.Update.Current = client.CurrentUpdate{
						Artifact:   "fake-id",
						DeviceType: "hammer",
					}
					mender.artifactInfoFile = artifactInfo
					mender.deviceTypeFile = deviceType

					// NOTE: manifest file data must match current update information expected by
					// the server
					ioutil.WriteFile(artifactInfo, []byte("artifact_name=fake-id\nDEVICE_TYPE=hammer"), 0600)
					ioutil.WriteFile(deviceType, []byte("device_type=hammer"), 0600)

					// currID, _ := mender.GetCurrentArtifactName()
					// make artifact-name different from the current one
					// in order to receive a new update
					// srv.Update.Data.Artifact.ArtifactName = currID + "-fake"
					// srv.UpdateDownload.Data = *bytes.NewBuffer([]byte("hello"))
				},
				from:             checkWaitState,
				to:               updateCheckState,
				transitionStatus: NoStatus,
				expectedStateData: &StateData{
					Version:          1,
					FromState:        MenderStateCheckWait,
					ToState:          MenderStateUpdateCheck,
					TransitionStatus: NoStatus,
				},
				expectedActions: []string{"Leave"},
			},
			{
				// Finish leave, fail enter
				from:             checkWaitState,
				to:               updateCheckState,
				transitionStatus: NoStatus,
				expectedStateData: &StateData{
					Version:          1,
					FromState:        MenderStateCheckWait,
					ToState:          MenderStateUpdateCheck,
					TransitionStatus: LeaveDone,
				},
				expectedActions: []string{"Leave", "Enter"},
			},
			{
				// finish the transition
				from:             checkWaitState,
				to:               updateCheckState,
				transitionStatus: LeaveDone,
				expectedStateData: &StateData{
					Version:          1,
					FromState:        MenderStateUpdateCheck,
					ToState:          MenderStateUpdateFetch,
					TransitionStatus: NoStatus,
				},
				expectedActions: []string{"Enter"},
			},
		},
		// update-check -> update-fetch
		// update-fetch -> update-store
	}

	log.SetLevel(log.DebugLevel)

	DeploymentLogger = NewDeploymentLogManager("testLogger")

	for _, transition := range transitions {
		for _, tc := range transition {
			if tc.modifyServer != nil {
				tc.modifyServer()
			}
			rebootExecutor := &spontanaeousRebootExecutor{}
			mender.stateScriptExecutor = rebootExecutor
			RunPanickingTransition(t, mender.TransitionState, tc.from, tc.to, &ctx, tc.transitionStatus)
			assert.Equal(t, tc.expectedActions, rebootExecutor.expectedActions)

			sData, err := LoadStateData(ctx.store)

			assert.NoError(t, err)
			if &tc.expectedStateData.UpdateInfo != nil {
				// TODO - does not compare updates atm
			} else {
				assert.Equal(t, *tc.expectedStateData, sData)
			}
			//  recreate the states that have been aborted
			fromState, toState, _ := mender.GetCurrentState(ctx.store)
			assert.Equal(t, tc.expectedStateData.FromState, fromState.Id())
			assert.Equal(t, tc.expectedStateData.ToState, toState.Id())
		}

	}
}

func RunPanickingTransition(t *testing.T, f func(from, to State, ctx *StateContext, status TransitionStatus) (State, State, bool), from, to State, ctx *StateContext, status TransitionStatus) {
	defer func() {
		if r := recover(); r == nil {
			t.Log("no panic")
		} else {
			t.Logf("Panicked! %v", r)
		}
	}()
	f(from, to, ctx, status)
}

func TestTransitions(t *testing.T) {
	mender, err := NewMender(menderConfig{}, MenderPieces{})
	assert.NoError(t, err)

	ctx := StateContext{store: store.NewMemStore()}

	tc := []struct {
		from      *testState
		to        *testState
		expectedT []stateScript
		expectedS State
	}{
		{from: &testState{t: ToIdle},
			to:        &testState{t: ToSync, next: initState},
			expectedT: []stateScript{{"Idle", "Leave"}, {"Sync", "Enter"}},
			expectedS: &InitState{},
		},
		// idle error should have no effect
		{from: &testState{t: ToIdle, shouldErrorLeave: true},
			to:        &testState{t: ToSync, next: initState},
			expectedT: []stateScript{{"Idle", "Leave"}, {"Sync", "Enter"}},
			expectedS: &InitState{},
		},
		{from: &testState{t: ToIdle},
			to:        &testState{t: ToSync, shouldErrorEnter: true, next: initState},
			expectedT: []stateScript{{"Idle", "Leave"}, {"Sync", "Enter"}},
			expectedS: &ErrorState{},
		},
		{from: &testState{t: ToSync, shouldErrorLeave: true},
			to:        &testState{t: ToDownload, next: initState},
			expectedT: []stateScript{{"Sync", "Leave"}},
			expectedS: &ErrorState{},
		},
		{from: &testState{t: ToError},
			to:        &testState{t: ToIdle, next: initState},
			expectedT: []stateScript{{"Error", "Leave"}, {"Idle", "Enter"}},
			expectedS: &InitState{},
		},
	}

	for _, tt := range tc {
		tt.from.next = tt.to

		te := &testExecutor{
			executed:   make([]stateScript, 0),
			execErrors: make(map[stateScript]bool),
		}
		te.setExecError(tt.from)
		te.setExecError(tt.to)

		mender.stateScriptExecutor = te
		mender.SetNextState(tt.from)

		p, s, c := mender.TransitionState(tt.from, tt.to, &ctx, NoStatus) // TODO - this test needs to be rewritten for spontanaeous reboots
		assert.Equal(t, p, p)                                             // TODO - not a valid test!
		assert.IsType(t, tt.expectedS, s)
		assert.False(t, c)

		t.Logf("has: %v expect: %v\n", te.executed, tt.expectedT)
		assert.True(t, te.verifyExecuted(tt.expectedT))

	}
}

func TestGetName(t *testing.T) {
	assert.Equal(t, "Sync", getName(ToSync, "Enter"))
	assert.Equal(t, "",
		getName(ToArtifactRollbackReboot_Enter, "Leave"))
	assert.Equal(t, "ArtifactRollbackReboot",
		getName(ToArtifactRollbackReboot_Enter, "Error"))
	assert.Equal(t, "ArtifactRollbackReboot",
		getName(ToArtifactRollbackReboot_Enter, "Enter"))
	assert.Equal(t, "ArtifactRollbackReboot",
		getName(ToArtifactRollbackReboot_Leave, "Leave"))
	assert.Equal(t, "ArtifactRollbackReboot",
		getName(ToArtifactRollbackReboot_Leave, "Error"))
}

type checkIgnoreErrorsExecutor struct {
	shouldIgnore bool
}

func (e *checkIgnoreErrorsExecutor) ExecuteAll(state, action string, ignoreError bool) error {
	if e.shouldIgnore == ignoreError {
		return nil
	}
	return errors.New("should ignore errors, but is not")
}

func (e *checkIgnoreErrorsExecutor) CheckRootfsScriptsVersion() error {
	return nil
}

func TestIgnoreErrors(t *testing.T) {
	e := checkIgnoreErrorsExecutor{false}
	tr := ToArtifactReboot_Leave
	err := tr.Leave(&e)
	assert.NoError(t, err)

	e = checkIgnoreErrorsExecutor{false}
	tr = ToArtifactCommit
	err = tr.Enter(&e)
	assert.NoError(t, err)

	e = checkIgnoreErrorsExecutor{true}
	tr = ToIdle
	err = tr.Enter(&e)
	assert.NoError(t, err)
}
