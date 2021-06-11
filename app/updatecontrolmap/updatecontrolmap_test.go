// Copyright 2021 Northern.tech AS
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

package updatecontrolmap

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
)

const TEST_UUID = "3380e4f2-c913-11eb-9119-c39aba66b261"
const TEST_UUID2 = "68711312-c913-11eb-a0ab-1ba9e86afdfd"

func TestUpdateControlMapStateValidation(t *testing.T) {
	// Empty values, shall validate
	stateEmpty := UpdateControlMapState{}
	assert.NoError(t, stateEmpty.Validate())

	// Legal values, shall validate
	for _, value := range []string{"continue", "force_continue", "pause", "fail"} {
		stateAction := UpdateControlMapState{
			Action: value,
		}
		assert.NoError(t, stateAction.Validate())

		stateOnMapExpire := UpdateControlMapState{
			OnMapExpire: value,
		}
		if value == "pause" {
			// Except for "OnMapExpire": "pause", which is not allowed
			assert.Error(t, stateOnMapExpire.Validate())
		} else {
			assert.NoError(t, stateOnMapExpire.Validate())
		}

		stateOnActionExecuted := UpdateControlMapState{
			OnActionExecuted: value,
		}
		assert.NoError(t, stateOnActionExecuted.Validate())
	}

	// Any other string, shall invalidate
	stateActionFoo := UpdateControlMapState{
		Action: "foo",
	}
	assert.Error(t, stateActionFoo.Validate())
	stateOnMapExpireFoo := UpdateControlMapState{
		OnMapExpire: "bar",
	}
	assert.Error(t, stateOnMapExpireFoo.Validate())
	stateOnActionExecutedFoo := UpdateControlMapState{
		OnActionExecuted: "baz",
	}
	assert.Error(t, stateOnActionExecutedFoo.Validate())
}

func TestUpdateControlMapValidation(t *testing.T) {
	// Empty, shall invalidate
	mapEmpty := UpdateControlMap{}
	assert.Error(t, mapEmpty.Validate())

	// Only ID, shall validate
	mapOnlyID := UpdateControlMap{
		ID: TEST_UUID,
	}
	assert.NoError(t, mapOnlyID.Validate())

	// Non-UUIDs, shall invalidate
	mapOnlyID = UpdateControlMap{
		ID: TEST_UUID + "a",
	}
	assert.Error(t, mapOnlyID.Validate())
	// Wrong hyphen positions.
	mapOnlyID = UpdateControlMap{
		ID: "3380e4f2-c913-11eb-91199-c39aba66b26",
	}
	assert.Error(t, mapOnlyID.Validate())
	// Illegal characters.
	mapOnlyID = UpdateControlMap{
		ID: "3380e4f2-c913-11eb-9119-c39aba66b26g",
	}
	assert.Error(t, mapOnlyID.Validate())

	// Legal values, shall validate
	for _, value := range []int{
		-10, -9, 0, 1, 9, 10,
	} {
		mapValid := UpdateControlMap{
			ID:       TEST_UUID,
			Priority: value,
		}
		assert.NoError(t, mapValid.Validate())
	}

	// Illegal values, shall not validate
	for _, value := range []int{
		-11, 11, 999, -999,
	} {
		mapValid := UpdateControlMap{
			ID:       TEST_UUID,
			Priority: value,
		}
		assert.Error(t, mapValid.Validate())
	}

	// Legal values, shall validate
	for _, value := range []string{
		"ArtifactInstall_Enter",
		"ArtifactReboot_Enter",
		"ArtifactCommit_Enter",
	} {
		mapValid := UpdateControlMap{
			ID:     TEST_UUID,
			States: map[string]UpdateControlMapState{value: {}},
		}
		assert.NoError(t, mapValid.Validate())
	}

	// Illegal values, shall not validate
	for _, value := range []string{
		"ArtifactInstall_Enter0",
		"0ArtifactReboot_Enter",
		"ArtifactCommit_Leave",
	} {
		mapValid := UpdateControlMap{
			ID:     TEST_UUID,
			States: map[string]UpdateControlMapState{value: {}},
		}
		assert.Error(t, mapValid.Validate())
	}
}

func TestUpdateControlMapValidationFromJSON(t *testing.T) {
	jsonString := `{
	"priority": 0,
	"states": {
		"ArtifactInstall_Enter": {
			"action": "continue",
			"on_map_expire": "force_continue",
			"on_action_executed": "pause"
		},
		"ArtifactReboot_Enter": {
			"action": "pause",
			"on_map_expire": "fail",
			"on_action_executed": "continue"
		}
	},
	"id": "01234567-89ab-cdef-0123-456789abcdef"
}`

	controlMap := UpdateControlMap{}
	err := json.Unmarshal([]byte(jsonString), &controlMap)
	assert.NoError(t, err)
	assert.NoError(t, controlMap.Validate())

	assert.Equal(t, 2, len(controlMap.States))
	state1 := controlMap.States["ArtifactInstall_Enter"]
	assert.Equal(t, "continue", state1.Action)
	state2 := controlMap.States["ArtifactReboot_Enter"]
	assert.Equal(t, "fail", state2.OnMapExpire)
}

func TestUpdateControlMapStateSanitize(t *testing.T) {

	tc := []struct {
		controlMapState          UpdateControlMapState
		controlMapStateSanitized UpdateControlMapState
	}{
		{
			controlMapState: UpdateControlMapState{
				Action:           "pause",
				OnMapExpire:      "force_continue",
				OnActionExecuted: "fail",
			},
			controlMapStateSanitized: UpdateControlMapState{
				Action:           "pause",
				OnMapExpire:      "force_continue",
				OnActionExecuted: "fail",
			},
		},
		{
			controlMapState: UpdateControlMapState{},
			controlMapStateSanitized: UpdateControlMapState{
				Action:           "continue",
				OnMapExpire:      "continue",
				OnActionExecuted: "continue",
			},
		},
		{
			controlMapState: UpdateControlMapState{
				Action: "force_continue",
			},
			controlMapStateSanitized: UpdateControlMapState{
				Action:           "force_continue",
				OnMapExpire:      "force_continue",
				OnActionExecuted: "force_continue",
			},
		},
		{
			controlMapState: UpdateControlMapState{
				OnMapExpire: "force_continue",
			},
			controlMapStateSanitized: UpdateControlMapState{
				Action:           "continue",
				OnMapExpire:      "force_continue",
				OnActionExecuted: "continue",
			},
		},
		{
			controlMapState: UpdateControlMapState{
				OnActionExecuted: "force_continue",
			},
			controlMapStateSanitized: UpdateControlMapState{
				Action:           "continue",
				OnMapExpire:      "continue",
				OnActionExecuted: "force_continue",
			},
		},
		{
			controlMapState: UpdateControlMapState{
				Action: "fail",
			},
			controlMapStateSanitized: UpdateControlMapState{
				Action:           "fail",
				OnMapExpire:      "fail",
				OnActionExecuted: "fail",
			},
		},
		{
			controlMapState: UpdateControlMapState{
				Action: "pause",
			},
			controlMapStateSanitized: UpdateControlMapState{
				Action:           "pause",
				OnMapExpire:      "fail",
				OnActionExecuted: "pause",
			},
		},
		{
			controlMapState: UpdateControlMapState{
				OnMapExpire:      "fail",
				OnActionExecuted: "fail",
			},
			controlMapStateSanitized: UpdateControlMapState{
				Action:           "continue",
				OnMapExpire:      "fail",
				OnActionExecuted: "fail",
			},
		},
	}

	for n, tt := range tc {
		caseName := strconv.Itoa(n)
		t.Run(caseName, func(t *testing.T) {
			tt.controlMapState.Sanitize()
			assert.Equal(t, tt.controlMapStateSanitized, tt.controlMapState)

		})
	}
}

func TestUpdateControlMapSanitize(t *testing.T) {
	mapDefault := UpdateControlMap{
		ID:       TEST_UUID,
		Priority: 10,
		States: map[string]UpdateControlMapState{
			"ArtifactInstall_Enter": {
				Action:           "continue",
				OnMapExpire:      "continue",
				OnActionExecuted: "continue",
			},
			"ArtifactReboot_Enter": {
				Action:           "continue",
				OnMapExpire:      "continue",
				OnActionExecuted: "continue",
			},
			"ArtifactCommit_Enter": {
				Action:           "continue",
				OnMapExpire:      "continue",
				OnActionExecuted: "continue",
			},
		},
	}
	mapDefault.Sanitize()
	assert.Equal(t, 0, len(mapDefault.States))

	mapOneState := UpdateControlMap{
		ID:       TEST_UUID,
		Priority: 10,
		States: map[string]UpdateControlMapState{
			"ArtifactInstall_Enter": {
				Action:           "continue",
				OnMapExpire:      "continue",
				OnActionExecuted: "continue",
			},
			"ArtifactReboot_Enter": {
				Action:           "fail",
				OnMapExpire:      "continue",
				OnActionExecuted: "continue",
			},
			"ArtifactCommit_Enter": {
				Action:           "continue",
				OnMapExpire:      "continue",
				OnActionExecuted: "continue",
			},
		},
	}
	mapOneState.Sanitize()
	assert.Equal(t, 1, len(mapOneState.States))
	_, ok := mapOneState.States["ArtifactReboot_Enter"]
	assert.True(t, ok)
}

func TestUpdateControlMapEqual(t *testing.T) {
	tests := map[string]struct {
		base     *UpdateControlMap
		other    *UpdateControlMap
		expected bool
	}{
		"equal IDs": {
			base: &UpdateControlMap{
				ID: TEST_UUID,
			},
			other: &UpdateControlMap{
				ID: TEST_UUID,
			},
			expected: true,
		},
		"unequal IDs": {
			base: &UpdateControlMap{
				ID: TEST_UUID,
			},
			other: &UpdateControlMap{
				ID: TEST_UUID2,
			},
			expected: false,
		},
		"equal IDs and Prioritys": {
			base: &UpdateControlMap{
				ID:       TEST_UUID,
				Priority: 1,
			},
			other: &UpdateControlMap{
				ID:       TEST_UUID,
				Priority: 1,
			},
			expected: true,
		},
		"equal IDs and unequal Prioritys": {
			base: &UpdateControlMap{
				ID:       TEST_UUID,
				Priority: 1,
			},
			other: &UpdateControlMap{
				ID:       TEST_UUID,
				Priority: 2,
			},
			expected: false,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, test.expected, test.base.Equal(test.other))
		})
	}
}
