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
	"strings"
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
	log.Info("Executing all in spont-reboot: %s:%s", state, action)
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

	ioutil.WriteFile(deviceType, []byte("device_type=vexpress-qemu\n"), 0644)

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
				device:  &fakeDevice{consumeUpdate: true}, // TODO - add the update in here?
				authMgr: authMgr,
			},
		},
	)
	// mender.deviceTypeFile = deviceType

	ctx := StateContext{store: store.NewMemStore()}

	updateResponse := client.UpdateResponse{
		Artifact: struct {
			Source struct {
				URI    string
				Expire string
			}
			CompatibleDevices []string `json:"device_types_compatible"`
			ArtifactName      string   `json:"artifact_name"`
		}{
			Source: struct {
				URI    string
				Expire string
			}{
				URI: strings.Join([]string{srv.URL, "download"}, "/"),
			},
			CompatibleDevices: []string{"vexpress"},
			ArtifactName:      "foo",
		},
		ID: "foo",
	}

	// needed to fake an install in updatestore-state
	updateReader, err := MakeRootfsImageArtifact(1, false)
	assert.NoError(t, err)
	assert.NotNil(t, updateReader)
	// var size int64

	transitions := [][]struct {
		from              State
		to                State
		message           string
		expectedStateData *StateData
		transitionStatus  TransitionStatus
		expectedActions   []string
		modifyServer      func()
	}{

		//
		// test the critical-path upto and including reboot-state
		//
		// The code will step through a transition stepwise as a panic in executeAll will flip
		// update-check -> update-fetch
		{
			{
				// finish updatecheck [sync] leave, fail enter fetch [download]
				message:          "fail download-enter",
				from:             updateCheckState,
				to:               NewUpdateFetchState(updateResponse),
				transitionStatus: NoStatus,
				expectedStateData: &StateData{
					Version:          1,
					Name:             MenderStateUpdateFetch,
					LeaveTransition:  ToNone,
					UpdateInfo:       updateResponse,
					TransitionStatus: LeaveDone,
				},
				expectedActions: []string{"Enter"},
			},
			{
				// finish updatefetch enter and main state
				message: "finish [download]-enter and handle updatefetch-state",
				modifyServer: func() {
					// fake an update
					mender.updater = &fakeUpdater{
						GetScheduledUpdateReturnIface: updateResponse,
					}
				},
				from:             updateCheckState,
				to:               NewUpdateFetchState(updateResponse),
				transitionStatus: LeaveDone,
				expectedStateData: &StateData{
					Version:          1,
					Name:             MenderStateUpdateStore,
					LeaveTransition:  ToDownload,
					UpdateInfo:       updateResponse,
					TransitionStatus: NoStatus,
				},
				expectedActions: []string{"Enter"},
			},
		},
		// update-fetch -> update-store
		{
			{
				message: "no transition scripts should be run",
				modifyServer: func() {
					mender.updater = &fakeUpdater{
						GetScheduledUpdateReturnIface: updateResponse,
						fetchUpdateReturnReadCloser:   updateReader,
					}
					mender.deviceTypeFile = deviceType
				},
				from:             NewUpdateFetchState(updateResponse),
				to:               NewUpdateStoreState(updateReader, 0, updateResponse),
				transitionStatus: NoStatus,
				expectedStateData: &StateData{
					Version:          1,
					Name:             MenderStateUpdateInstall,
					UpdateInfo:       updateResponse,
					LeaveTransition:  ToDownload,
					TransitionStatus: NoStatus,
				},
				expectedActions: nil,
			},
		},
		// update-store [download] -> update-install [artifact-install]
		{
			{
				message: "fail in download-leave",
				from:    NewUpdateStoreState(updateReader, 0, updateResponse),
				to:      NewUpdateInstallState(updateResponse),
				expectedStateData: &StateData{
					Version:          1,
					Name:             MenderStateUpdateInstall,
					UpdateInfo:       updateResponse,
					LeaveTransition:  ToDownload,
					TransitionStatus: NoStatus,
				},
				expectedActions: []string{"Leave"},
			},
			{
				message: "Fail in artifact_install enter",
				from:    NewUpdateStoreState(updateReader, 0, updateResponse),
				to:      NewUpdateInstallState(updateResponse),
				expectedStateData: &StateData{
					Version:          1,
					Name:             MenderStateUpdateInstall,
					UpdateInfo:       updateResponse,
					LeaveTransition:  ToDownload,
					TransitionStatus: LeaveDone,
				},
				expectedActions: []string{"Leave", "Enter"},
			},
			{
				message: "finish artifact_install enter, and handle update_install_state",
				from:    NewUpdateStoreState(updateReader, 0, updateResponse),
				to:      NewUpdateInstallState(updateResponse),
				expectedStateData: &StateData{
					Version:          1,
					Name:             MenderStateReboot,
					LeaveTransition:  ToArtifactInstall,
					TransitionStatus: NoStatus,
					UpdateInfo:       updateResponse,
				},
				expectedActions: []string{"Enter"},
			},
		},
		// update-install [artifact_install] -> reboot-state [artifact_reboot]
		{
			{
				message: "fail artifact_install leave",
				from:    NewUpdateInstallState(updateResponse),
				to:      NewRebootState(updateResponse),
				expectedStateData: &StateData{
					Version:          1,
					Name:             MenderStateReboot,
					LeaveTransition:  ToArtifactInstall,
					TransitionStatus: NoStatus,
					UpdateInfo:       updateResponse,
				},
				expectedActions: []string{"Leave"},
			},
			{
				message: "finish artifact_install leave, and fail artifact-reboot enter",
				from:    NewUpdateInstallState(updateResponse),
				to:      NewRebootState(updateResponse),
				expectedStateData: &StateData{
					Version:          1,
					Name:             MenderStateReboot,
					LeaveTransition:  ToArtifactInstall,
					TransitionStatus: LeaveDone,
					UpdateInfo:       updateResponse,
				},
				expectedActions: []string{"Leave", "Enter"},
			},
			{
				message: "finish artifact-reboot enter, and handle reboot",
				from:    NewUpdateInstallState(updateResponse),
				to:      NewRebootState(updateResponse),
				// Warning: clean state-data should be stored in reboot, prior to
				// switching to the new partition
				// however, since the fakeDevice does not actually reboot
				// rebootStateRecoveryData will be stored once again
				// thus this test will not correctly reflect real life expected data
				expectedStateData: &StateData{
					Version:          1,
					Name:             MenderStateReboot,
					LeaveTransition:  ToNone,
					TransitionStatus: NoStatus,
					UpdateInfo:       updateResponse,
				},
				expectedActions: []string{"Enter"},
			},
		},
		// after the reboot all we can guarantee is what happens after the commit
	}

	log.SetLevel(log.DebugLevel)

	// create a directory for the deployment-logs
	tempDir, _ := ioutil.TempDir("", "logs")
	defer os.RemoveAll(tempDir)

	DeploymentLogger = NewDeploymentLogManager(tempDir)

	// setup the test
	us := NewUpdateFetchState(updateResponse).(Recover)
	rd := us.RecoveryData(ToNone)
	StoreStateData(ctx.store, rd)

	for _, transition := range transitions {
		for _, tc := range transition {

			// create a new mender on every iteration to simulate a powerloss
			mender = newTestMender(nil,
				menderConfig{
					ServerURL: srv.URL,
				},
				testMenderPieces{
					MenderPieces: MenderPieces{
						device:  &fakeDevice{consumeUpdate: true},
						authMgr: authMgr,
					},
				},
			)
			mender.GetCurrentState().SetTransition(ToNone)

			rebootExecutor := &spontanaeousRebootExecutor{}
			mender.stateScriptExecutor = rebootExecutor
			mender.artifactInfoFile = artifactInfo
			// First handle the initial init -> init transition
			initState = &InitState{
				baseState{
					id: MenderStateInit,
					t:  ToNone,
				},
			}
			// modify after we have created everything else needed
			if tc.modifyServer != nil {
				tc.modifyServer()
			}
			to, _ := mender.TransitionState(initState, &ctx)
			RunPanickingTransition(t, mender.TransitionState, to, &ctx)
			assert.Equal(t, tc.expectedActions, rebootExecutor.expectedActions, "The expected actions in transition: %s -> %s does not conform, message: %s", tc.from.Id(), tc.to.Id(), tc.message)

			sData, err := LoadStateData(ctx.store)
			assert.NoError(t, err)
			assert.Equal(t, *tc.expectedStateData, sData, "The expected, and stored state data diverge in transition: %s -> %s: message: %s", tc.from.Id(), tc.to.Id(), tc.message)

			// make some space in between the transition printouts
			fmt.Println()
			fmt.Println()
		}

	}
}

// RunPanickingTransition runs the state-tranitions, and recovers from the panics
// that the rebootStateScriptExecutor utters every second run
func RunPanickingTransition(t *testing.T, f func(to State, ctx *StateContext) (State, bool), to State, ctx *StateContext) {
	defer func() {
		if r := recover(); r == nil {
			t.Log("no panic")
		} else {
			t.Logf("Panicked! %v", r)
		}
	}()
	f(to, ctx)
}

func TestTransitions(t *testing.T) {
	mender, err := NewMender(menderConfig{}, MenderPieces{})
	assert.NoError(t, err)

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

		s, c := mender.TransitionState(tt.to, nil)
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
