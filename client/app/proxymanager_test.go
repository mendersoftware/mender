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

	"github.com/mendersoftware/mender/client/conf"
	"github.com/mendersoftware/mender/common/dbus"
	"github.com/mendersoftware/mender/common/dbus/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestProxyManagerRun(t *testing.T) {
	menderProxyManager, err := NewProxyManager(conf.NewMenderConfig())
	assert.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())

	go menderProxyManager.run(ctx)
	time.Sleep(3 * time.Second)
	cancel()
	// Give the deferred functions some time to run
	time.Sleep(3 * time.Second)
}

func TestProxyManagerReconfigure(t *testing.T) {
	menderProxyManager, err := NewProxyManager(conf.NewMenderConfig())
	assert.NoError(t, err)

	cancel, err := menderProxyManager.Start()
	assert.NoError(t, err)

	firstUrl := menderProxyManager.proxy.GetServerUrl()

	menderProxyManager.reconfigure("http://awesome.server", "123456")

	secondUrl := menderProxyManager.proxy.GetServerUrl()

	assert.NotEqual(t, firstUrl, secondUrl)

	cancel()
}

func setupTestProxyManager() dbus.DBusAPI {
	dbusAPI := &mocks.DBusAPI{}

	dbusConn := dbus.Handle(nil)

	dbusAPI.On("BusGet",
		mock.AnythingOfType("uint"),
	).Return(dbusConn, nil)

	dbusAPI.On("BusOwnNameOnConnection",
		dbusConn,
		ProxyDBusObjectName,
		mock.AnythingOfType("uint"),
	).Return(uint(1), nil)

	dbusAPI.On("BusRegisterInterface",
		dbusConn,
		ProxyDBusPath,
		ProxyDBusInterface,
	).Return(uint(2), nil)

	dbusAPI.On("RegisterMethodCallCallback",
		ProxyDBusPath,
		ProxyDBusInterfaceName,
		ProxyDBusSetupServerURLProxy,
		mock.Anything,
	)

	dbusAPI.On("UnregisterMethodCallCallback",
		ProxyDBusPath,
		ProxyDBusInterfaceName,
		ProxyDBusSetupServerURLProxy,
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

func TestProxyManagerDBusInterface(t *testing.T) {
	api := setupTestProxyManager()
	defer api.(*mocks.DBusAPI).AssertExpectations(t)

	menderProxyManager, err := NewProxyManager(conf.NewMenderConfig())
	assert.NoError(t, err)
	menderProxyManager.DBusAPI = api

	cancel, err := menderProxyManager.Start()
	assert.NoError(t, err)

	// Give some time for the goroutine to register the interface
	time.Sleep(2 * time.Second)

	cancel()

	// Give the deferred functions to unregister the interface
	time.Sleep(2 * time.Second)
}
