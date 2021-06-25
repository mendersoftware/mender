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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/mendersoftware/mender/app/updatecontrolmap"
	"github.com/mendersoftware/mender/conf"
	"github.com/mendersoftware/mender/dbus"
	"github.com/mendersoftware/mender/dbus/mocks"
	"github.com/mendersoftware/mender/store"
)

const TEST_UUID = "3380e4f2-c913-11eb-9119-c39aba66b261"
const TEST_UUID2 = "68711312-c913-11eb-a0ab-1ba9e86afdfd"
const TEST_UUID3 = "691835ca-c913-11eb-aef0-53563ab9a426"

// TestControlMap tests the ControlMap structure, and verifies the thread-safety
// of the data access, and writes.
func TestControlMap(t *testing.T) {
	cm := NewControlMap(store.NewMemStore(), conf.DefaultUpdateControlMapBootExpirationTimeSeconds, conf.DefaultUpdateControlMapBootExpirationTimeSeconds)
	cm.Insert(&updatecontrolmap.UpdateControlMap{
		ID:       TEST_UUID,
		Priority: 0,
	})
	cm.Insert(&updatecontrolmap.UpdateControlMap{
		ID:       TEST_UUID,
		Priority: 1,
	})
	cm.Insert(&updatecontrolmap.UpdateControlMap{
		ID:       TEST_UUID,
		Priority: 1,
	})
	cm.Insert(&updatecontrolmap.UpdateControlMap{
		ID:       TEST_UUID,
		Priority: 0,
	})
	active, expired := cm.Get(TEST_UUID)
	assert.Equal(t, 2, len(active), "The map has a duplicate")
	assert.Equal(t, 0, len(expired), "The expired pool was not empty")
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
	um := NewUpdateManager(NewControlMap(
		store.NewMemStore(),
		conf.DefaultUpdateControlMapBootExpirationTimeSeconds,
		conf.DefaultUpdateControlMapBootExpirationTimeSeconds,
	), 6)
	um.EnableDBus(api)
	ctx, cancel := context.WithCancel(context.Background())
	go um.run(ctx)
	time.Sleep(3 * time.Second)
	cancel()
	// Give the defered functions some time to run
	time.Sleep(3 * time.Second)

}

func TestMapExpired(t *testing.T) {
	// Insert a map with a stamp
	testMap := NewControlMap(store.NewMemStore(), conf.DefaultUpdateControlMapBootExpirationTimeSeconds,
		conf.DefaultUpdateControlMapBootExpirationTimeSeconds)
	cm := &updatecontrolmap.UpdateControlMap{
		ID:       TEST_UUID,
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
	testMapPool := NewControlMap(
		store.NewMemStore(),
		conf.DefaultUpdateControlMapBootExpirationTimeSeconds,
		conf.DefaultUpdateControlMapBootExpirationTimeSeconds,
	)
	cm := &updatecontrolmap.UpdateControlMap{
		ID:       TEST_UUID,
		Priority: 1,
		States: map[string]updatecontrolmap.UpdateControlMapState{
			"ArtifactInstall": updatecontrolmap.UpdateControlMapState{
				Action: "continue",
			},
		},
	}
	testMapPool.Insert(cm.Stamp(1))
	time.Sleep(2 * time.Second)
	active, expired := testMapPool.Get(TEST_UUID)
	require.Equal(t, 0, len(active))
	require.Equal(t, 1, len(expired))
	require.True(t, cm.Equal(expired[0]))
	// Insert a matching map
	cmn := &updatecontrolmap.UpdateControlMap{
		ID:       TEST_UUID,
		Priority: 1,
		States: map[string]updatecontrolmap.UpdateControlMapState{
			"ArtifactRebootEnter": updatecontrolmap.UpdateControlMapState{
				Action: "ArtifactRebootEnter",
			},
		},
	}
	testMapPool.Insert(cmn.Stamp(2))
	// Map should exist in the active map
	active, expired = testMapPool.Get(TEST_UUID)
	// But not in the inactive anylonger
	assert.Equal(t, 0, len(expired))
	assert.Equal(t, 1, len(active))
	assert.Contains(t, active[0].States, "ArtifactRebootEnter", active)
}

func TestCleanExpiredMaps(t *testing.T) {
	testMapPool := NewControlMap(
		store.NewMemStore(),
		conf.DefaultUpdateControlMapBootExpirationTimeSeconds,
		conf.DefaultUpdateControlMapBootExpirationTimeSeconds,
	)
	testMapPool.Insert((&updatecontrolmap.UpdateControlMap{
		ID:       TEST_UUID,
		Priority: 1,
	}).Stamp(0))
	testMapPool.Insert((&updatecontrolmap.UpdateControlMap{
		ID:       TEST_UUID,
		Priority: 2,
	}).Stamp(0))
	testMapPool.Insert((&updatecontrolmap.UpdateControlMap{
		ID:       TEST_UUID,
		Priority: 2,
		States: map[string]updatecontrolmap.UpdateControlMapState{
			"ArtifactInstall": updatecontrolmap.UpdateControlMapState{
				Action: "continue",
			},
		},
	}).Stamp(3))
	time.Sleep(1 * time.Second)
	testMapPool.ClearExpired()
	active, expired := testMapPool.Get(TEST_UUID)
	assert.Equal(t, 1, len(active), active)
	assert.Equal(t, 0, len(expired))
}

func TestDelete(t *testing.T) {
	testMapPool := NewControlMap(store.NewMemStore(),
		conf.DefaultUpdateControlMapBootExpirationTimeSeconds,
		conf.DefaultUpdateControlMapBootExpirationTimeSeconds,
	)
	testMapPool.Insert(&updatecontrolmap.UpdateControlMap{
		ID:       "foo",
		Priority: 1,
	})
	testMapPool.Insert(&updatecontrolmap.UpdateControlMap{
		ID:       "bar",
		Priority: 2,
	})
	testMapPool.Insert(&updatecontrolmap.UpdateControlMap{
		ID:       "foo",
		Priority: 3,
	})
	testMapPool.Delete(EqualIDs("foo"))
	active, expired := testMapPool.Get("foo")
	assert.Equal(t, 0, len(active))
	assert.Equal(t, 0, len(expired))
	active, expired = testMapPool.Get("bar")
	assert.Equal(t, 1, len(active))
	assert.Equal(t, 0, len(expired))
}

func TestInsertMatching(t *testing.T) {
	testMapPool := NewControlMap(
		store.NewMemStore(),
		conf.DefaultUpdateControlMapBootExpirationTimeSeconds,
		conf.DefaultUpdateControlMapBootExpirationTimeSeconds,
	)
	cm := &updatecontrolmap.UpdateControlMap{
		ID:       TEST_UUID,
		Priority: 1,
		States: map[string]updatecontrolmap.UpdateControlMapState{
			"ArtifactInstall": updatecontrolmap.UpdateControlMapState{
				Action: "continue",
			},
		},
	}
	cmn := &updatecontrolmap.UpdateControlMap{
		ID:       TEST_UUID,
		Priority: 1,
		States: map[string]updatecontrolmap.UpdateControlMapState{
			"ArtifactReboot": updatecontrolmap.UpdateControlMapState{
				Action: "continue",
			},
		},
	}
	testMapPool.Insert(cm.Stamp(60))
	testMapPool.Insert(cmn.Stamp(60))
	active, expired := testMapPool.Get(TEST_UUID)
	assert.Equal(t, 1, len(active), active)
	assert.Contains(t, active[0].States, "ArtifactReboot", active)
	assert.Equal(t, 0, len(expired))
}

func TestActiveAndExpiredPoolKeys(t *testing.T) {
	testMapPool := NewControlMap(
		store.NewMemStore(),
		conf.DefaultUpdateControlMapBootExpirationTimeSeconds,
		conf.DefaultUpdateControlMapBootExpirationTimeSeconds,
	)
	testMapPool.Insert((&updatecontrolmap.UpdateControlMap{
		ID:       TEST_UUID,
		Priority: 0,
		States: map[string]updatecontrolmap.UpdateControlMapState{
			"ArtifactInstall": updatecontrolmap.UpdateControlMapState{
				Action: "continue",
			},
		},
	}).Stamp(0))
	testMapPool.Insert((&updatecontrolmap.UpdateControlMap{
		ID:       TEST_UUID2,
		Priority: 1,
		States: map[string]updatecontrolmap.UpdateControlMapState{
			"ArtifactInstall": updatecontrolmap.UpdateControlMapState{
				Action: "continue",
			},
		},
	}).Stamp(1))
	testMapPool.Insert((&updatecontrolmap.UpdateControlMap{
		ID:       TEST_UUID3,
		Priority: 1,
		States: map[string]updatecontrolmap.UpdateControlMapState{
			"ArtifactReboot": updatecontrolmap.UpdateControlMapState{
				Action: "continue",
			},
		},
	}).Stamp(3))
	assert.Eventually(t, func() bool {
		_, expired := testMapPool.Get(TEST_UUID)
		return len(expired) == 1

	},
		1*time.Second,
		100*time.Millisecond)
	assert.Eventually(t, func() bool {
		_, expired := testMapPool.Get(TEST_UUID2)
		return len(expired) == 1

	},
		3*time.Second,
		500*time.Millisecond)
	assert.Eventually(t, func() bool {
		_, expired := testMapPool.Get(TEST_UUID3)
		return len(expired) == 1

	},
		4*time.Second,
		500*time.Millisecond)
}

func TestQueryLogic(t *testing.T) {
	active := 3
	expired := 0
	t.Run("Fail exists: Action", func(t *testing.T) {
		testMapPool := NewControlMap(
			store.NewMemStore(),
			conf.DefaultUpdateControlMapBootExpirationTimeSeconds,
			conf.DefaultUpdateControlMapBootExpirationTimeSeconds,
		)
		testMapPool.Insert((&updatecontrolmap.UpdateControlMap{
			ID:       TEST_UUID,
			Priority: 2,
			States: map[string]updatecontrolmap.UpdateControlMapState{
				"ArtifactInstall": updatecontrolmap.UpdateControlMapState{
					Action: "fail",
				},
			},
		}).Stamp(active))
		testMapPool.Insert((&updatecontrolmap.UpdateControlMap{
			ID:       TEST_UUID2,
			Priority: 1,
			States: map[string]updatecontrolmap.UpdateControlMapState{
				"ArtifactInstall": updatecontrolmap.UpdateControlMapState{
					Action: "continue",
				},
			},
		}).Stamp(active))
		testMapPool.Insert((&updatecontrolmap.UpdateControlMap{
			ID:       TEST_UUID3,
			Priority: 1,
			States: map[string]updatecontrolmap.UpdateControlMapState{
				"ArtifactInstall": updatecontrolmap.UpdateControlMapState{
					Action: "continue",
				},
			},
		}).Stamp(active))
		assert.Equal(t, "fail", testMapPool.QueryAndUpdate("ArtifactInstall"))
	})
	t.Run("Fail exists: OnMapExpire", func(t *testing.T) {
		testMapPool := NewControlMap(
			store.NewMemStore(),
			conf.DefaultUpdateControlMapBootExpirationTimeSeconds,
			conf.DefaultUpdateControlMapBootExpirationTimeSeconds,
		)
		testMapPool.Insert((&updatecontrolmap.UpdateControlMap{
			ID:       TEST_UUID,
			Priority: 2,
			States: map[string]updatecontrolmap.UpdateControlMapState{
				"ArtifactInstall": updatecontrolmap.UpdateControlMapState{
					OnMapExpire: "fail",
				},
			},
		}).Stamp(expired))
		testMapPool.Insert((&updatecontrolmap.UpdateControlMap{
			ID:       TEST_UUID2,
			Priority: 1,
			States: map[string]updatecontrolmap.UpdateControlMapState{
				"ArtifactInstall": updatecontrolmap.UpdateControlMapState{
					Action: "continue",
				},
			},
		}).Stamp(active))
		testMapPool.Insert((&updatecontrolmap.UpdateControlMap{
			ID:       TEST_UUID3,
			Priority: 1,
			States: map[string]updatecontrolmap.UpdateControlMapState{
				"ArtifactInstall": updatecontrolmap.UpdateControlMapState{
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
		testMapPool := NewControlMap(
			store.NewMemStore(),
			conf.DefaultUpdateControlMapBootExpirationTimeSeconds,
			conf.DefaultUpdateControlMapBootExpirationTimeSeconds,
		)
		testMapPool.Insert((&updatecontrolmap.UpdateControlMap{
			ID:       TEST_UUID,
			Priority: 2,
			States: map[string]updatecontrolmap.UpdateControlMapState{
				"ArtifactInstall": updatecontrolmap.UpdateControlMapState{
					Action: "pause",
				},
			},
		}).Stamp(active))
		testMapPool.Insert((&updatecontrolmap.UpdateControlMap{
			ID:       TEST_UUID2,
			Priority: 1,
			States: map[string]updatecontrolmap.UpdateControlMapState{
				"ArtifactInstall": updatecontrolmap.UpdateControlMapState{
					Action: "fail",
				},
			},
		}).Stamp(active))
		testMapPool.Insert((&updatecontrolmap.UpdateControlMap{
			ID:       TEST_UUID3,
			Priority: 1,
			States: map[string]updatecontrolmap.UpdateControlMapState{
				"ArtifactInstall": updatecontrolmap.UpdateControlMapState{
					Action: "continue",
				},
			},
		}).Stamp(active))
		assert.Equal(t, "pause", testMapPool.QueryAndUpdate("ArtifactInstall"))
	})
	t.Run("Pause exists: OnMapExpire", func(t *testing.T) {
		testMapPool := NewControlMap(
			store.NewMemStore(),
			conf.DefaultUpdateControlMapBootExpirationTimeSeconds,
			conf.DefaultUpdateControlMapBootExpirationTimeSeconds,
		)
		testMapPool.Insert((&updatecontrolmap.UpdateControlMap{
			ID:       TEST_UUID,
			Priority: 2,
			States: map[string]updatecontrolmap.UpdateControlMapState{
				"ArtifactInstall": updatecontrolmap.UpdateControlMapState{
					OnMapExpire: "pause",
				},
			},
		}).Stamp(expired))
		testMapPool.Insert((&updatecontrolmap.UpdateControlMap{
			ID:       TEST_UUID2,
			Priority: 1,
			States: map[string]updatecontrolmap.UpdateControlMapState{
				"ArtifactInstall": updatecontrolmap.UpdateControlMapState{
					Action: "fail",
				},
			},
		}).Stamp(active))
		testMapPool.Insert((&updatecontrolmap.UpdateControlMap{
			ID:       TEST_UUID3,
			Priority: 1,
			States: map[string]updatecontrolmap.UpdateControlMapState{
				"ArtifactInstall": updatecontrolmap.UpdateControlMapState{
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
		testMapPool := NewControlMap(
			store.NewMemStore(),
			conf.DefaultUpdateControlMapBootExpirationTimeSeconds,
			conf.DefaultUpdateControlMapBootExpirationTimeSeconds,
		)
		testMapPool.Insert((&updatecontrolmap.UpdateControlMap{
			ID:       TEST_UUID,
			Priority: 2,
			States: map[string]updatecontrolmap.UpdateControlMapState{
				"ArtifactInstall": updatecontrolmap.UpdateControlMapState{
					Action: "force_continue",
				},
			},
		}).Stamp(active))
		testMapPool.Insert((&updatecontrolmap.UpdateControlMap{
			ID:       TEST_UUID2,
			Priority: 1,
			States: map[string]updatecontrolmap.UpdateControlMapState{
				"ArtifactInstall": updatecontrolmap.UpdateControlMapState{
					Action: "fail",
				},
			},
		}).Stamp(active))
		testMapPool.Insert((&updatecontrolmap.UpdateControlMap{
			ID:       TEST_UUID3,
			Priority: 1,
			States: map[string]updatecontrolmap.UpdateControlMapState{
				"ArtifactInstall": updatecontrolmap.UpdateControlMapState{
					Action: "continue",
				},
			},
		}).Stamp(active))
		assert.Equal(t, "continue", testMapPool.QueryAndUpdate("ArtifactInstall"))
	})
	t.Run("force_continue exists: OnMapExpire", func(t *testing.T) {
		testMapPool := NewControlMap(
			store.NewMemStore(),
			conf.DefaultUpdateControlMapBootExpirationTimeSeconds,
			conf.DefaultUpdateControlMapBootExpirationTimeSeconds,
		)
		testMapPool.Insert((&updatecontrolmap.UpdateControlMap{
			ID:       TEST_UUID,
			Priority: 2,
			States: map[string]updatecontrolmap.UpdateControlMapState{
				"ArtifactInstall": updatecontrolmap.UpdateControlMapState{
					OnMapExpire: "force_continue",
				},
			},
		}).Stamp(expired))
		testMapPool.Insert((&updatecontrolmap.UpdateControlMap{
			ID:       TEST_UUID2,
			Priority: 1,
			States: map[string]updatecontrolmap.UpdateControlMapState{
				"ArtifactInstall": updatecontrolmap.UpdateControlMapState{
					Action: "fail",
				},
			},
		}).Stamp(active))
		testMapPool.Insert((&updatecontrolmap.UpdateControlMap{
			ID:       TEST_UUID3,
			Priority: 1,
			States: map[string]updatecontrolmap.UpdateControlMapState{
				"ArtifactInstall": updatecontrolmap.UpdateControlMapState{
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
		testMapPool := NewControlMap(
			store.NewMemStore(),
			conf.DefaultUpdateControlMapBootExpirationTimeSeconds,
			conf.DefaultUpdateControlMapBootExpirationTimeSeconds,
		)
		testMapPool.Insert((&updatecontrolmap.UpdateControlMap{
			ID:       TEST_UUID,
			Priority: 0,
		}).Stamp(active))
		testMapPool.Insert((&updatecontrolmap.UpdateControlMap{
			ID:       TEST_UUID2,
			Priority: 1,
		}).Stamp(active))
		testMapPool.Insert((&updatecontrolmap.UpdateControlMap{
			ID:       TEST_UUID3,
			Priority: 1,
		}).Stamp(active))
		assert.Eventually(t,
			func() bool { return assert.Equal(t, "continue", testMapPool.QueryAndUpdate("ArtifactInstall")) },
			1*time.Second,
			100*time.Millisecond)
	})
	t.Run("on_action_executed - overrides", func(t *testing.T) {
		testMapPool := NewControlMap(
			store.NewMemStore(),
			conf.DefaultUpdateControlMapBootExpirationTimeSeconds,
			conf.DefaultUpdateControlMapBootExpirationTimeSeconds,
		)
		testMapPool.Insert((&updatecontrolmap.UpdateControlMap{
			ID:       TEST_UUID,
			Priority: 0,
			States: map[string]updatecontrolmap.UpdateControlMapState{
				"ArtifactInstall": updatecontrolmap.UpdateControlMapState{
					Action:           "fail",
					OnActionExecuted: "pause",
				},
			},
		}).Stamp(active))
		testMapPool.Insert((&updatecontrolmap.UpdateControlMap{
			ID:       TEST_UUID2,
			Priority: 1,
			States: map[string]updatecontrolmap.UpdateControlMapState{
				"ArtifactInstall": updatecontrolmap.UpdateControlMapState{
					Action: "continue",
				},
			},
		}).Stamp(active))
		testMapPool.Insert((&updatecontrolmap.UpdateControlMap{
			ID:       TEST_UUID3,
			Priority: 1,
			States: map[string]updatecontrolmap.UpdateControlMapState{
				"ArtifactInstall": updatecontrolmap.UpdateControlMapState{
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
		testMapPool := NewControlMap(
			store.NewMemStore(),
			conf.DefaultUpdateControlMapBootExpirationTimeSeconds,
			conf.DefaultUpdateControlMapBootExpirationTimeSeconds,
		)
		testMapPool.Insert((&updatecontrolmap.UpdateControlMap{
			ID:       TEST_UUID,
			Priority: 2,
			States: map[string]updatecontrolmap.UpdateControlMapState{
				"ArtifactInstall": updatecontrolmap.UpdateControlMapState{
					Action: "fail",
				},
			},
		}).Stamp(active))
		testMapPool.Insert((&updatecontrolmap.UpdateControlMap{
			ID:       TEST_UUID2,
			Priority: 2,
			States: map[string]updatecontrolmap.UpdateControlMapState{
				"ArtifactInstall": updatecontrolmap.UpdateControlMapState{
					Action: "pause",
				},
			},
		}).Stamp(active))
		testMapPool.Insert((&updatecontrolmap.UpdateControlMap{
			ID:       TEST_UUID3,
			Priority: 1,
			States: map[string]updatecontrolmap.UpdateControlMapState{
				"ArtifactInstall": updatecontrolmap.UpdateControlMapState{
					Action: "continue",
				},
			},
		}).Stamp(active))
		assert.Equal(t, "fail", testMapPool.QueryAndUpdate("ArtifactInstall"))
	})
	t.Run("Fail overrides pause - equal priorities: OnMapExpire", func(t *testing.T) {
		testMapPool := NewControlMap(
			store.NewMemStore(),
			conf.DefaultUpdateControlMapBootExpirationTimeSeconds,
			conf.DefaultUpdateControlMapBootExpirationTimeSeconds,
		)
		testMapPool.Insert((&updatecontrolmap.UpdateControlMap{
			ID:       TEST_UUID,
			Priority: 2,
			States: map[string]updatecontrolmap.UpdateControlMapState{
				"ArtifactInstall": updatecontrolmap.UpdateControlMapState{
					OnMapExpire: "fail",
				},
			},
		}).Stamp(expired))
		testMapPool.Insert((&updatecontrolmap.UpdateControlMap{
			ID:       TEST_UUID2,
			Priority: 2,
			States: map[string]updatecontrolmap.UpdateControlMapState{
				"ArtifactInstall": updatecontrolmap.UpdateControlMapState{
					OnMapExpire: "pause",
				},
			},
		}).Stamp(expired))
		testMapPool.Insert((&updatecontrolmap.UpdateControlMap{
			ID:       TEST_UUID3,
			Priority: 1,
			States: map[string]updatecontrolmap.UpdateControlMapState{
				"ArtifactInstall": updatecontrolmap.UpdateControlMapState{
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
		testMapPool := NewControlMap(
			store.NewMemStore(),
			conf.DefaultUpdateControlMapBootExpirationTimeSeconds,
			conf.DefaultUpdateControlMapBootExpirationTimeSeconds,
		)
		testMapPool.Insert((&updatecontrolmap.UpdateControlMap{
			ID:       TEST_UUID,
			Priority: 2,
			States: map[string]updatecontrolmap.UpdateControlMapState{
				"ArtifactInstall": updatecontrolmap.UpdateControlMapState{
					Action:      "pause",
					OnMapExpire: "fail",
				},
			},
		}).Stamp(active))
		testMapPool.Insert((&updatecontrolmap.UpdateControlMap{
			ID:       TEST_UUID2,
			Priority: 1,
			States: map[string]updatecontrolmap.UpdateControlMapState{
				"ArtifactInstall": updatecontrolmap.UpdateControlMapState{
					Action: "continue",
				},
			},
		}).Stamp(active))
		testMapPool.Insert((&updatecontrolmap.UpdateControlMap{
			ID:       TEST_UUID3,
			Priority: 1,
			States: map[string]updatecontrolmap.UpdateControlMapState{
				"ArtifactInstall": updatecontrolmap.UpdateControlMapState{
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

func TestSaveAndLoadToDB(t *testing.T) {

	active := 3
	expired := 0
	boot := 100

	memStore := store.NewMemStore()

	testMapPool := NewControlMap(memStore, boot, 5)

	assert.Equal(t, 0, len(testMapPool.Pool))

	testMapPool.Insert((&updatecontrolmap.UpdateControlMap{
		ID:       TEST_UUID,
		Priority: 2,
		States: map[string]updatecontrolmap.UpdateControlMapState{
			"ArtifactInstall_Enter": updatecontrolmap.UpdateControlMapState{
				OnMapExpire: "fail",
			},
		},
	}).Stamp(expired))

	time.Sleep(1 * time.Second)

	testMapPool.Insert((&updatecontrolmap.UpdateControlMap{
		ID:       TEST_UUID2,
		Priority: 1,
		States: map[string]updatecontrolmap.UpdateControlMapState{
			"ArtifactCommit_Enter": updatecontrolmap.UpdateControlMapState{
				Action: "fail",
			},
		},
	}).Stamp(active))
	testMapPool.Insert((&updatecontrolmap.UpdateControlMap{
		ID:       TEST_UUID3,
		Priority: 1,
		States: map[string]updatecontrolmap.UpdateControlMapState{
			"ArtifactInstall_Enter": updatecontrolmap.UpdateControlMapState{
				Action: "continue",
			},
		},
	}).Stamp(active))

	loadedPool := NewControlMap(memStore, boot, 5)
	expectedTime := time.Now().Add(time.Duration(boot) * time.Second)

	expiredMap := map[string]bool{
		TEST_UUID:  true,
		TEST_UUID2: false,
		TEST_UUID3: false,
	}

	assert.Equal(t, 3, len(loadedPool.Pool))
	for _, m := range loadedPool.Pool {
		assert.WithinDuration(t, expectedTime, m.ExpiryTime, 5*time.Second)
		assert.Equalf(t, expiredMap[m.ID], m.Expired(), "Map with ID %s did not have expected expired status %v", m.ID, expiredMap[m.ID])
	}
}

func TestMapUpdates(t *testing.T) {
	t.Run("TestMapInsert updates", func(t *testing.T) {
		boot := 100
		memStore := store.NewMemStore()
		testMapPool := NewControlMap(memStore, boot, 5)
		testMapPool.Insert((&updatecontrolmap.UpdateControlMap{
			ID:       TEST_UUID,
			Priority: 2,
			States: map[string]updatecontrolmap.UpdateControlMapState{
				"ArtifactInstall": updatecontrolmap.UpdateControlMapState{
					Action: "force_continue",
				},
			},
		}).Stamp(1))
		assert.Eventually(t,
			func() bool {
				<-testMapPool.Updates
				return true
			},
			2*time.Second,
			100*time.Millisecond)
	})
	t.Run("TestMapExpiration", func(t *testing.T) {
		boot := 100
		memStore := store.NewMemStore()
		testMapPool := NewControlMap(memStore, boot, 5)
		m := &updatecontrolmap.UpdateControlMap{
			ID:       TEST_UUID,
			Priority: 2,
			States: map[string]updatecontrolmap.UpdateControlMapState{
				"ArtifactInstall": updatecontrolmap.UpdateControlMapState{
					Action: "force_continue",
				},
			},
			ExpirationChannel: testMapPool.Updates,
		}
		m.Stamp(1)
		assert.Eventually(t,
			func() bool {
				<-testMapPool.Updates
				return true
			},
			2*time.Second,
			100*time.Millisecond)
	})
}

func TestUpdateControlMapHalfTime(t *testing.T) {
	testMapPool := NewControlMap(
		store.NewMemStore(),
		conf.DefaultUpdateControlMapBootExpirationTimeSeconds,
		conf.DefaultUpdateControlMapBootExpirationTimeSeconds,
	)
	testMapPool.Insert((&updatecontrolmap.UpdateControlMap{
		ID:       "foo",
		Priority: 2,
		States: map[string]updatecontrolmap.UpdateControlMapState{
			"ArtifactInstall": updatecontrolmap.UpdateControlMapState{
				Action: "fail",
			},
		},
	}).Stamp(10))
	testMapPool.Insert((&updatecontrolmap.UpdateControlMap{
		ID:       "foo",
		Priority: 1,
		States: map[string]updatecontrolmap.UpdateControlMapState{
			"ArtifactInstall": updatecontrolmap.UpdateControlMapState{
				Action: "continue",
			},
		},
	}).Stamp(20))
	testMapPool.Insert((&updatecontrolmap.UpdateControlMap{
		ID:       "foo",
		Priority: 3,
		States: map[string]updatecontrolmap.UpdateControlMapState{
			"ArtifactInstall": updatecontrolmap.UpdateControlMapState{
				Action: "continue",
			},
		},
	}).Stamp(30))

	assert.WithinDuration(t,
		time.Now().Add(5*time.Second),
		func() time.Time {
			t, _ := testMapPool.NextControlMapHalfTime("foo")
			return t
		}(),
		1*time.Second,
		"Not within the expected duration")
}
