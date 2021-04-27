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
	"github.com/mendersoftware/mender/dbus"
	"github.com/mendersoftware/mender/dbus/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"testing"
	"time"
)

// TestControlMap tests the ControlMap structure, and verifies the thread-safety
// of the data access, and writes.
func TestControlMap(t *testing.T) {
	cm := NewControlMap()
	cm.Set(&UpdateControlMap{
		ID:       "foo",
		Priority: 0,
	})
	cm.Set(&UpdateControlMap{
		ID:       "foo",
		Priority: 1,
	})
	cm.Set(&UpdateControlMap{
		ID:       "foo",
		Priority: 1,
	})
	cm.Set(&UpdateControlMap{
		ID:       "foo",
		Priority: 0,
	})
	assert.EqualValues(t, cm.Get("foo"), []*UpdateControlMap{
		&UpdateControlMap{ID: "foo", Priority: 0},
		&UpdateControlMap{ID: "foo", Priority: 1},
	}, cm.controlMap)
	assert.Equal(t, len(cm.Get("foo")), 2, "The map has a duplicate")
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
	um := NewUpdateManager(6)
	um.EnableDBus(api)
	ctx, cancel := context.WithCancel(context.Background())
	go um.run(ctx)
	time.Sleep(3 * time.Second)
	cancel()
	// Give the defered functions some time to run
	time.Sleep(3 * time.Second)

}
