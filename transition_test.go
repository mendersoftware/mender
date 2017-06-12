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
	"errors"
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

func (te *testExecutor) ExecuteAll(state, action string, ignoreError bool) error {
	te.executed = append(te.executed, stateScript{state, action})

	if _, ok := te.execErrors[stateScript{state, action}]; ok {
		return errors.New("error executing script")
	}
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
		{from: &testState{t: ToIdle, shouldErrorLeave: true},
			to:        &testState{t: ToSync, next: initState},
			expectedT: []stateScript{{"Idle", "Leave"}},
			expectedS: &ErrorState{},
		},
		{from: &testState{t: ToIdle},
			to:        &testState{t: ToSync, shouldErrorEnter: true, next: initState},
			expectedT: []stateScript{{"Idle", "Leave"}, {"Sync", "Enter"}, {"Sync", "Error"}},
			expectedS: &ErrorState{},
		},
		{from: &testState{t: ToArtifactInstall},
			to:        &testState{t: ToArtifactFailure, next: initState},
			expectedT: []stateScript{{"ArtifactFailure", "Enter"}},
			expectedS: &InitState{},
		},
		{from: &testState{t: ToArtifactInstall},
			to:        &testState{t: ToArtifactFailure, shouldErrorEnter: true, next: initState},
			expectedT: []stateScript{{"ArtifactFailure", "Enter"}},
			expectedS: &InitState{},
		},
		{from: &testState{t: ToArtifactInstall},
			to:        &testState{t: ToArtifactCommit, next: NewUpdateErrorState(nil, client.UpdateResponse{})},
			expectedT: []stateScript{{"ArtifactInstall", "Leave"}, {"ArtifactCommit", "Enter"}, {"ArtifactCommit", "Error"}},
			expectedS: &UpdateErrorState{},
		},
		{from: &testState{t: ToArtifactInstall},
			to:        &testState{t: ToArtifactFailure, next: NewUpdateErrorState(nil, client.UpdateResponse{})},
			expectedT: []stateScript{{"ArtifactFailure", "Enter"}},
			expectedS: &UpdateErrorState{},
		},
		{from: &testState{t: ToArtifactFailure},
			to:        &testState{t: ToIdle, next: checkWaitState},
			expectedT: []stateScript{{"ArtifactFailure", "Leave"}, {"Idle", "Enter"}},
			expectedS: &CheckWaitState{},
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
	assert.Equal(t, "",
		getName(ToArtifactRollbackReboot_Enter, "Error"))
	assert.Equal(t, "ArtifactRollbackReboot",
		getName(ToArtifactRollbackReboot_Enter, "Enter"))
	assert.Equal(t, "ArtifactRollbackReboot",
		getName(ToArtifactRollbackReboot_Leave, "Leave"))
	assert.Equal(t, "",
		getName(ToArtifactRollbackReboot_Leave, "Error"))
}
