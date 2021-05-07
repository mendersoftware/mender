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

package app

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/mendersoftware/mender/dbus"
	"github.com/mendersoftware/mender/dbus/mocks"
	"github.com/mendersoftware/mender/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestControlMap tests the ControlMap structure, and verifies the thread-safety
// of the data access, and writes.
func TestControlMap(t *testing.T) {
	cm := NewControlMap()
	cm.Insert(&UpdateControlMap{
		ID:       "foo",
		Priority: 0,
	})
	cm.Insert(&UpdateControlMap{
		ID:       "foo",
		Priority: 1,
	})
	cm.Insert(&UpdateControlMap{
		ID:       "foo",
		Priority: 1,
	})
	cm.Insert(&UpdateControlMap{
		ID:       "foo",
		Priority: 0,
	})
	active, expired := cm.Get("foo")
	assert.Equal(t, 2, len(active), "The map has a duplicate")
	assert.Equal(t, 0, len(expired), "The expired pool was not empty")
}

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
		ID: "whatever",
	}
	assert.NoError(t, mapOnlyID.Validate())

	// Legal values, shall validate
	for _, value := range []string{
		"ArtifactInstall_Enter",
		"ArtifactReboot_Enter",
		"ArtifactCommit_Enter",
	} {
		mapValid := UpdateControlMap{
			ID:     "whatever",
			States: map[string]UpdateControlMapState{value: {}},
		}
		assert.NoError(t, mapValid.Validate())
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
				OnActionExecuted: "continue",
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
				OnActionExecuted: "continue",
			},
		},
		{
			controlMapState: UpdateControlMapState{
				Action: "pause",
			},
			controlMapStateSanitized: UpdateControlMapState{
				Action:           "pause",
				OnMapExpire:      "fail",
				OnActionExecuted: "continue",
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
		ID:       "whatever",
		Priority: 100,
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
		ID:       "whatever",
		Priority: 100,
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

func setupTestUpdateManager() dbus.DBusAPI {
	dbusAPI := &mocks.DBusAPI{}

	dbusConn := dbus.Handle(nil)

	dbusAPI.On("BusGet",
		mock.AnythingOfType("uint"),
	).Return(dbusConn, nil)

	dbusAPI.On("BusOwnNameOnConnection",
		dbusConn,
		UpdateManagerDBusObjectName,
		mock.AnythingOfType("uint"),
	).Return(uint(1), nil)

	dbusAPI.On("BusRegisterInterface",
		dbusConn,
		UpdateManagerDBusPath,
		UpdateManagerDBusInterface,
	).Return(uint(2), nil)

	dbusAPI.On("RegisterMethodCallCallback",
		UpdateManagerDBusPath,
		UpdateManagerDBusInterfaceName,
		updateManagerSetUpdateControlMap,
		mock.Anything,
	)

	dbusAPI.On("UnregisterMethodCallCallback",
		UpdateManagerDBusPath,
		UpdateManagerDBusInterfaceName,
		updateManagerSetUpdateControlMap,
	)

	dbusAPI.On("BusUnregisterInterface",
		dbusConn,
		uint(2),
	).Return(true)

	dbusAPI.On("BusUnownName",
		uint(1),
	)
	return dbusAPI

}

func TestUpdateManager(t *testing.T) {

	api := setupTestUpdateManager()
	defer api.(*mocks.DBusAPI).AssertExpectations(t)
	um := NewUpdateManager(NewControlMap(), 6)
	um.EnableDBus(api)
	ctx, cancel := context.WithCancel(context.Background())
	go um.run(ctx)
	time.Sleep(3 * time.Second)
	cancel()
	// Give the defered functions some time to run
	time.Sleep(3 * time.Second)

}

func TestUpdateControlMapEqual(t *testing.T) {
	tests := map[string]struct {
		base     *UpdateControlMap
		other    *UpdateControlMap
		expected bool
	}{
		"equal IDs": {
			base: &UpdateControlMap{
				ID: "foo",
			},
			other: &UpdateControlMap{
				ID: "foo",
			},
			expected: true,
		},
		"unequal IDs": {
			base: &UpdateControlMap{
				ID: "foo",
			},
			other: &UpdateControlMap{
				ID: "foobar",
			},
			expected: false,
		},
		"equal IDs and Prioritys": {
			base: &UpdateControlMap{
				ID:       "foo",
				Priority: 1,
			},
			other: &UpdateControlMap{
				ID:       "foo",
				Priority: 1,
			},
			expected: true,
		},
		"equal IDs and unequal Prioritys": {
			base: &UpdateControlMap{
				ID:       "foo",
				Priority: 1,
			},
			other: &UpdateControlMap{
				ID:       "foo",
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

func TestMapExpired(t *testing.T) {
	// Insert a map with a stamp
	testMap := NewControlMap()
	cm := &UpdateControlMap{
		ID:       "foo",
		Priority: 1,
	}
	testMap.Insert(cm.Stamp(1))
	// Wait for the map to expire
	time.Sleep(2 * time.Second)
	_, expired := testMap.Get(cm.ID)
	assert.True(t, cm.Equal(expired[0]))
}

// Test that inserting a map which exists in the expired pool, does replace the
// expired one in the active pool
func TestInsertMatchingMap(t *testing.T) {
	// Insert an expired map
	testMapPool := NewControlMap()
	cm := &UpdateControlMap{
		ID:       "foo",
		Priority: 1,
		States: map[string]UpdateControlMapState{
			"ArtifactInstall": UpdateControlMapState{
				Action: "continue",
			},
		},
	}
	testMapPool.Insert(cm.Stamp(1))
	time.Sleep(2 * time.Second)
	active, expired := testMapPool.Get("foo")
	require.Equal(t, 0, len(active))
	require.Equal(t, 1, len(expired))
	require.True(t, cm.Equal(expired[0]))
	// Insert a matching map
	cmn := &UpdateControlMap{
		ID:       "foo",
		Priority: 1,
		States: map[string]UpdateControlMapState{
			"ArtifactRebootEnter": UpdateControlMapState{
				Action: "ArtifactRebootEnter",
			},
		},
	}
	testMapPool.Insert(cmn.Stamp(2))
	// Map should exist in the active map
	active, expired = testMapPool.Get("foo")
	// But not in the inactive anylonger
	assert.Equal(t, 0, len(expired))
	assert.Equal(t, 1, len(active))
	assert.Contains(t, active[0].States, "ArtifactRebootEnter", active)
}

func TestCleanExpiredMaps(t *testing.T) {
	testMapPool := NewControlMap()
	testMapPool.Insert((&UpdateControlMap{
		ID:       "foo",
		Priority: 1,
	}).Stamp(0))
	testMapPool.Insert((&UpdateControlMap{
		ID:       "foo",
		Priority: 2,
	}).Stamp(0))
	testMapPool.Insert((&UpdateControlMap{
		ID:       "foo",
		Priority: 2,
		States: map[string]UpdateControlMapState{
			"ArtifactInstall": UpdateControlMapState{
				Action: "continue",
			},
		},
	}).Stamp(3))
	time.Sleep(1 * time.Second)
	testMapPool.ClearExpired()
	active, expired := testMapPool.Get("foo")
	assert.Equal(t, 1, len(active), active)
	assert.Equal(t, 0, len(expired))
}

func TestInsertMatching(t *testing.T) {
	testMapPool := NewControlMap()
	cm := &UpdateControlMap{
		ID:       "foo",
		Priority: 1,
		States: map[string]UpdateControlMapState{
			"ArtifactInstall": UpdateControlMapState{
				Action: "continue",
			},
		},
	}
	cmn := &UpdateControlMap{
		ID:       "foo",
		Priority: 1,
		States: map[string]UpdateControlMapState{
			"ArtifactReboot": UpdateControlMapState{
				Action: "continue",
			},
		},
	}
	testMapPool.Insert(cm.Stamp(0))
	testMapPool.Insert(cmn.Stamp(0))
	active, expired := testMapPool.Get("foo")
	assert.Equal(t, 1, len(active), active)
	assert.Contains(t, active[0].States, "ArtifactReboot", active)
	assert.Equal(t, 0, len(expired))
}

func TestActiveAndExpiredPoolKeys(t *testing.T) {
	testMapPool := NewControlMap()
	testMapPool.Insert((&UpdateControlMap{
		ID:       "foo",
		Priority: 0,
		States: map[string]UpdateControlMapState{
			"ArtifactInstall": UpdateControlMapState{
				Action: "continue",
			},
		},
	}).Stamp(0))
	testMapPool.Insert((&UpdateControlMap{
		ID:       "bar",
		Priority: 1,
		States: map[string]UpdateControlMapState{
			"ArtifactInstall": UpdateControlMapState{
				Action: "continue",
			},
		},
	}).Stamp(1))
	testMapPool.Insert((&UpdateControlMap{
		ID:       "baz",
		Priority: 1,
		States: map[string]UpdateControlMapState{
			"ArtifactReboot": UpdateControlMapState{
				Action: "continue",
			},
		},
	}).Stamp(3))
	assert.Eventually(t, func() bool {
		_, expired := testMapPool.Get("foo")
		return len(expired) == 1

	},
		1*time.Second,
		100*time.Millisecond)
	assert.Eventually(t, func() bool {
		_, expired := testMapPool.Get("bar")
		return len(expired) == 1

	},
		3*time.Second,
		500*time.Millisecond)
	assert.Eventually(t, func() bool {
		_, expired := testMapPool.Get("baz")
		return len(expired) == 1

	},
		4*time.Second,
		500*time.Millisecond)
}

func TestQueryLogic(t *testing.T) {
	active := 3
	expired := 0
	t.Run("Fail exists: Action", func(t *testing.T) {
		testMapPool := NewControlMap()
		testMapPool.Insert((&UpdateControlMap{
			ID:       "foo",
			Priority: 2,
			States: map[string]UpdateControlMapState{
				"ArtifactInstall": UpdateControlMapState{
					Action: "fail",
				},
			},
		}).Stamp(active))
		testMapPool.Insert((&UpdateControlMap{
			ID:       "bar",
			Priority: 1,
			States: map[string]UpdateControlMapState{
				"ArtifactInstall": UpdateControlMapState{
					Action: "continue",
				},
			},
		}).Stamp(active))
		testMapPool.Insert((&UpdateControlMap{
			ID:       "baz",
			Priority: 1,
			States: map[string]UpdateControlMapState{
				"ArtifactInstall": UpdateControlMapState{
					Action: "continue",
				},
			},
		}).Stamp(active))
		assert.Equal(t, "fail", testMapPool.QueryAndUpdate("ArtifactInstall"))
	})
	t.Run("Fail exists: OnMapExpire", func(t *testing.T) {
		testMapPool := NewControlMap()
		testMapPool.Insert((&UpdateControlMap{
			ID:       "foo",
			Priority: 2,
			States: map[string]UpdateControlMapState{
				"ArtifactInstall": UpdateControlMapState{
					OnMapExpire: "fail",
				},
			},
		}).Stamp(expired))
		testMapPool.Insert((&UpdateControlMap{
			ID:       "bar",
			Priority: 1,
			States: map[string]UpdateControlMapState{
				"ArtifactInstall": UpdateControlMapState{
					Action: "continue",
				},
			},
		}).Stamp(active))
		testMapPool.Insert((&UpdateControlMap{
			ID:       "baz",
			Priority: 1,
			States: map[string]UpdateControlMapState{
				"ArtifactInstall": UpdateControlMapState{
					Action: "continue",
				},
			},
		}).Stamp(active))
		assert.Eventually(t,
			func() bool { return assert.Equal(t, "fail", testMapPool.QueryAndUpdate("ArtifactInstall")) },
			1*time.Second,
			100*time.Millisecond)

	})
	t.Run("Pause exists: Action", func(t *testing.T) {
		testMapPool := NewControlMap()
		testMapPool.Insert((&UpdateControlMap{
			ID:       "foo",
			Priority: 2,
			States: map[string]UpdateControlMapState{
				"ArtifactInstall": UpdateControlMapState{
					Action: "pause",
				},
			},
		}).Stamp(active))
		testMapPool.Insert((&UpdateControlMap{
			ID:       "bar",
			Priority: 1,
			States: map[string]UpdateControlMapState{
				"ArtifactInstall": UpdateControlMapState{
					Action: "fail",
				},
			},
		}).Stamp(active))
		testMapPool.Insert((&UpdateControlMap{
			ID:       "baz",
			Priority: 1,
			States: map[string]UpdateControlMapState{
				"ArtifactInstall": UpdateControlMapState{
					Action: "continue",
				},
			},
		}).Stamp(active))
		assert.Equal(t, "pause", testMapPool.QueryAndUpdate("ArtifactInstall"))
	})
	t.Run("Pause exists: OnMapExpire", func(t *testing.T) {
		testMapPool := NewControlMap()
		testMapPool.Insert((&UpdateControlMap{
			ID:       "foo",
			Priority: 2,
			States: map[string]UpdateControlMapState{
				"ArtifactInstall": UpdateControlMapState{
					OnMapExpire: "pause",
				},
			},
		}).Stamp(expired))
		testMapPool.Insert((&UpdateControlMap{
			ID:       "bar",
			Priority: 1,
			States: map[string]UpdateControlMapState{
				"ArtifactInstall": UpdateControlMapState{
					Action: "fail",
				},
			},
		}).Stamp(active))
		testMapPool.Insert((&UpdateControlMap{
			ID:       "baz",
			Priority: 1,
			States: map[string]UpdateControlMapState{
				"ArtifactInstall": UpdateControlMapState{
					Action: "continue",
				},
			},
		}).Stamp(active))
		assert.Eventually(t,
			func() bool { return assert.Equal(t, "pause", testMapPool.QueryAndUpdate("ArtifactInstall")) },
			1*time.Second,
			100*time.Millisecond)
	})
	t.Run("force_continue exists: Action", func(t *testing.T) {
		testMapPool := NewControlMap()
		testMapPool.Insert((&UpdateControlMap{
			ID:       "foo",
			Priority: 2,
			States: map[string]UpdateControlMapState{
				"ArtifactInstall": UpdateControlMapState{
					Action: "force_continue",
				},
			},
		}).Stamp(active))
		testMapPool.Insert((&UpdateControlMap{
			ID:       "bar",
			Priority: 1,
			States: map[string]UpdateControlMapState{
				"ArtifactInstall": UpdateControlMapState{
					Action: "fail",
				},
			},
		}).Stamp(active))
		testMapPool.Insert((&UpdateControlMap{
			ID:       "baz",
			Priority: 1,
			States: map[string]UpdateControlMapState{
				"ArtifactInstall": UpdateControlMapState{
					Action: "continue",
				},
			},
		}).Stamp(active))
		assert.Equal(t, "continue", testMapPool.QueryAndUpdate("ArtifactInstall"))
	})
	t.Run("force_continue exists: OnMapExpire", func(t *testing.T) {
		testMapPool := NewControlMap()
		testMapPool.Insert((&UpdateControlMap{
			ID:       "foo",
			Priority: 2,
			States: map[string]UpdateControlMapState{
				"ArtifactInstall": UpdateControlMapState{
					OnMapExpire: "force_continue",
				},
			},
		}).Stamp(expired))
		testMapPool.Insert((&UpdateControlMap{
			ID:       "bar",
			Priority: 1,
			States: map[string]UpdateControlMapState{
				"ArtifactInstall": UpdateControlMapState{
					Action: "fail",
				},
			},
		}).Stamp(active))
		testMapPool.Insert((&UpdateControlMap{
			ID:       "baz",
			Priority: 1,
			States: map[string]UpdateControlMapState{
				"ArtifactInstall": UpdateControlMapState{
					Action: "continue",
				},
			},
		}).Stamp(active))
		assert.Eventually(t,
			func() bool { return assert.Equal(t, "continue", testMapPool.QueryAndUpdate("ArtifactInstall")) },
			1*time.Second,
			100*time.Millisecond)
	})
	t.Run("No value exist - return continue", func(t *testing.T) {
		testMapPool := NewControlMap()
		testMapPool.Insert((&UpdateControlMap{
			ID:       "foo",
			Priority: 0,
		}).Stamp(active))
		testMapPool.Insert((&UpdateControlMap{
			ID:       "bar",
			Priority: 1,
		}).Stamp(active))
		testMapPool.Insert((&UpdateControlMap{
			ID:       "baz",
			Priority: 1,
		}).Stamp(active))
		assert.Eventually(t,
			func() bool { return assert.Equal(t, "continue", testMapPool.QueryAndUpdate("ArtifactInstall")) },
			1*time.Second,
			100*time.Millisecond)
	})
	t.Run("on_action_executed - overrides", func(t *testing.T) {
		testMapPool := NewControlMap()
		testMapPool.Insert((&UpdateControlMap{
			ID:       "foo",
			Priority: 0,
			States: map[string]UpdateControlMapState{
				"ArtifactInstall": UpdateControlMapState{
					Action:           "fail",
					OnActionExecuted: "pause",
				},
			},
		}).Stamp(active))
		testMapPool.Insert((&UpdateControlMap{
			ID:       "bar",
			Priority: 1,
			States: map[string]UpdateControlMapState{
				"ArtifactInstall": UpdateControlMapState{
					Action: "continue",
				},
			},
		}).Stamp(active))
		testMapPool.Insert((&UpdateControlMap{
			ID:       "baz",
			Priority: 1,
			States: map[string]UpdateControlMapState{
				"ArtifactInstall": UpdateControlMapState{
					Action: "continue",
				},
			},
		}).Stamp(active))
		assert.Eventually(t,
			func() bool { return assert.Equal(t, "fail", testMapPool.QueryAndUpdate("ArtifactInstall")) },
			1*time.Second,
			100*time.Millisecond)
	})
	t.Run("Fail overrides pause - equal priorities: Action", func(t *testing.T) {
		testMapPool := NewControlMap()
		testMapPool.Insert((&UpdateControlMap{
			ID:       "foo",
			Priority: 2,
			States: map[string]UpdateControlMapState{
				"ArtifactInstall": UpdateControlMapState{
					Action: "fail",
				},
			},
		}).Stamp(active))
		testMapPool.Insert((&UpdateControlMap{
			ID:       "bar",
			Priority: 2,
			States: map[string]UpdateControlMapState{
				"ArtifactInstall": UpdateControlMapState{
					Action: "pause",
				},
			},
		}).Stamp(active))
		testMapPool.Insert((&UpdateControlMap{
			ID:       "baz",
			Priority: 1,
			States: map[string]UpdateControlMapState{
				"ArtifactInstall": UpdateControlMapState{
					Action: "continue",
				},
			},
		}).Stamp(active))
		assert.Equal(t, "fail", testMapPool.QueryAndUpdate("ArtifactInstall"))
	})
	t.Run("Fail overrides pause - equal priorities: OnMapExpire", func(t *testing.T) {
		testMapPool := NewControlMap()
		testMapPool.Insert((&UpdateControlMap{
			ID:       "foo",
			Priority: 2,
			States: map[string]UpdateControlMapState{
				"ArtifactInstall": UpdateControlMapState{
					OnMapExpire: "fail",
				},
			},
		}).Stamp(expired))
		testMapPool.Insert((&UpdateControlMap{
			ID:       "bar",
			Priority: 2,
			States: map[string]UpdateControlMapState{
				"ArtifactInstall": UpdateControlMapState{
					OnMapExpire: "pause",
				},
			},
		}).Stamp(expired))
		testMapPool.Insert((&UpdateControlMap{
			ID:       "baz",
			Priority: 1,
			States: map[string]UpdateControlMapState{
				"ArtifactInstall": UpdateControlMapState{
					Action: "continue",
				},
			},
		}).Stamp(active))
		assert.Eventually(t,
			func() bool { return assert.Equal(t, "fail", testMapPool.QueryAndUpdate("ArtifactInstall")) },
			1*time.Second,
			100*time.Millisecond)
	})
	t.Run("Check Action overrides OnMapExpire for active map", func(t *testing.T) {
		testMapPool := NewControlMap()
		testMapPool.Insert((&UpdateControlMap{
			ID:       "foo",
			Priority: 2,
			States: map[string]UpdateControlMapState{
				"ArtifactInstall": UpdateControlMapState{
					Action:      "pause",
					OnMapExpire: "fail",
				},
			},
		}).Stamp(active))
		testMapPool.Insert((&UpdateControlMap{
			ID:       "bar",
			Priority: 1,
			States: map[string]UpdateControlMapState{
				"ArtifactInstall": UpdateControlMapState{
					Action: "continue",
				},
			},
		}).Stamp(active))
		testMapPool.Insert((&UpdateControlMap{
			ID:       "baz",
			Priority: 1,
			States: map[string]UpdateControlMapState{
				"ArtifactInstall": UpdateControlMapState{
					Action: "continue",
				},
			},
		}).Stamp(active))
		assert.Eventually(t,
			func() bool { return assert.Equal(t, "pause", testMapPool.QueryAndUpdate("ArtifactInstall")) },
			1*time.Second,
			100*time.Millisecond)
	})
}

func TestSaveAndLoadToDb(t *testing.T) {
	active := 3
	expired := 0
	boot := 100

	memStore := store.NewMemStore()

	testMapPool := NewControlMap()
	testMapPool.SetStore(memStore)
	testMapPool.LoadFromStore(boot)

	assert.Equal(t, 0, len(testMapPool.Pool))

	testMapPool.Insert((&UpdateControlMap{
		ID:       "foo",
		Priority: 2,
		States: map[string]UpdateControlMapState{
			"ArtifactInstall": UpdateControlMapState{
				OnMapExpire: "pause",
			},
		},
	}).Stamp(expired))
	testMapPool.Insert((&UpdateControlMap{
		ID:       "bar",
		Priority: 1,
		States: map[string]UpdateControlMapState{
			"ArtifactCommit": UpdateControlMapState{
				Action: "fail",
			},
		},
	}).Stamp(active))
	testMapPool.Insert((&UpdateControlMap{
		ID:       "baz",
		Priority: 1,
		States: map[string]UpdateControlMapState{
			"ArtifactInstall": UpdateControlMapState{
				Action: "continue",
			},
		},
	}).Stamp(active))
	time.Sleep(1)

	loadedPool := NewControlMap()
	loadedPool.SetStore(memStore)
	loadedPool.LoadFromStore(boot)
	expectedTime := time.Now().Add(time.Duration(boot) * time.Second)

	expiredMap := map[string]bool{
		"foo": true,
		"bar": false,
		"baz": false,
	}

	assert.Equal(t, 3, len(loadedPool.Pool))
	for _, m := range loadedPool.Pool {
		assert.WithinDuration(t, expectedTime, m.expiryTime, 5*time.Second)
		assert.Equalf(t, expiredMap[m.ID], m.expired, "Map with ID %s did not have expected expired status %v", m.ID, expiredMap[m.ID])
	}
}
