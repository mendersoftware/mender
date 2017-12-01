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
	"strings"
	"testing"

	"github.com/mendersoftware/mender/client"
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

type testExecutor struct {
	executed   []stateScript
	execErrors map[stateScript]bool
}

func (te *testExecutor) ExecuteAll(state, action string, ignoreError bool, report *client.StatusReportWrapper) error {
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

func (e *checkIgnoreErrorsExecutor) ExecuteAll(state, action string, ignoreError bool, report *client.StatusReportWrapper) error {
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
	err := tr.Leave(&e, nil)
	assert.NoError(t, err)

	e = checkIgnoreErrorsExecutor{false}
	tr = ToArtifactCommit
	err = tr.Enter(&e, nil)
	assert.NoError(t, err)

	e = checkIgnoreErrorsExecutor{true}
	tr = ToIdle
	err = tr.Enter(&e, nil)
	assert.NoError(t, err)
}

// stateScriptReportExecutor implements Executor
type stateScriptReportExecutor struct {
}

func (sexec *stateScriptReportExecutor) ExecuteAll(state, action string, ignoreError bool, report *client.StatusReportWrapper) error {

	return nil
}

func (sexec *stateScriptReportExecutor) CheckRootfsScriptsVersion() error {
	return nil
}

func TestTransitionReporting(t *testing.T) {

	update := client.UpdateResponse{
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
				URI: strings.Join([]string{"www.example.com", "test"}, "/"),
			},
			CompatibleDevices: []string{"vexpress"},
			ArtifactName:      "foo",
		},
		ID: "foo",
	}

	tc := []struct {
		state    State
		expected bool
	}{
		{
			state:    initState,
			expected: false,
		},
		{
			state:    idleState,
			expected: false,
		},
		{
			state:    authorizeState,
			expected: false,
		},
		{
			state:    authorizeWaitState,
			expected: false,
		},
		{
			state:    checkWaitState,
			expected: false,
		},
		{
			state:    &UpdateCheckState{},
			expected: false,
		},
		{
			state:    NewUpdateFetchState(update),
			expected: true,
		},
		{
			state:    NewUpdateStoreState(nil, 0, update),
			expected: true,
		},
		{
			state:    NewUpdateInstallState(update),
			expected: true,
		},
		{
			state:    NewRebootState(update),
			expected: true,
		},
		{
			state:    NewAfterRebootState(update),
			expected: true,
		},
		{
			state:    NewUpdateVerifyState(update),
			expected: true,
		},
		{
			state:    NewUpdateCommitState(update),
			expected: true,
		},
		{
			state:    NewRollbackState(update, false, false),
			expected: true,
		},
		{
			state:    NewAfterRollbackRebootState(update),
			expected: true,
		},
		{
			state:    NewUpdateErrorState(nil, update),
			expected: true,
		},
	}

	for _, test := range tc {
		t.Logf("Running state: %s", test.state.Id())
		res := shouldReportUpdateStatus(test.state.Id())
		assert.Equal(t, test.expected, res, "ShouldReportUpdateStatus returns the wrong value for state %s", test.state.Id().String())
		if res {
			_, err := getUpdateFromState(test.state)
			assert.NoError(t, err, "received error in: %s", test.state.Id())
		}
	}
}
