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
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

type testState struct {
	t    Transition
	next State
}

func (s *testState) Handle(ctx *StateContext, c Controller) (State, bool) {
	return s.next, false
}

func (s *testState) Cancel() bool { return true }

func (s *testState) Id() MenderState { return MenderStateIdle }

func (s *testState) Transition() Transition { return s.t }

type stateScript struct {
	state  string
	action string
}

type testExecutor struct {
	executed map[stateScript]int
}

func (te *testExecutor) ExecuteAll(state, action string) error {
	fmt.Printf("executing %s_%s\n", state, action)

	if s, ok := te.executed[stateScript{state: state, action: action}]; ok {
		s++
		te.executed[stateScript{state: state, action: action}] = s
	} else {
		te.executed[stateScript{state: state, action: action}] = 1
	}

	return nil
}

func TestTransitions(t *testing.T) {
	mender, err := NewMender(menderConfig{}, MenderPieces{})
	assert.NoError(t, err)
	mender.stateScriptExecutor = &testExecutor{executed: make(map[stateScript]int)}

	to := &testState{t: ToSync, next: initState}
	from := &testState{t: ToIdle, next: to}

	mender.SetNextState(from)
	s, c := mender.TransitionState(to, nil)

	assert.IsType(t, &InitState{}, s)
	assert.False(t, c)

	fmt.Printf("executor %v\n", mender.stateScriptExecutor)
}
