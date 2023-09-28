// Copyright 2023 Northern.tech AS
//
//	Licensed under the Apache License, Version 2.0 (the "License");
//	you may not use this file except in compliance with the License.
//	You may obtain a copy of the License at
//
//	    http://www.apache.org/licenses/LICENSE-2.0
//
//	Unless required by applicable law or agreed to in writing, software
//	distributed under the License is distributed on an "AS IS" BASIS,
//	WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//	See the License for the specific language governing permissions and
//	limitations under the License.
package app

import (
	"encoding/json"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/mendersoftware/mender/client"
	cltest "github.com/mendersoftware/mender/client/test"
	"github.com/mendersoftware/mender/conf"
	"github.com/mendersoftware/mender/dbus"
	"github.com/mendersoftware/mender/dbus/mocks"
	dev "github.com/mendersoftware/mender/device"
	"github.com/mendersoftware/mender/store"
	stest "github.com/mendersoftware/mender/system/testing"
)

const (
	authManagerTestChannelName = "test"
)

func TestNewAuthManager(t *testing.T) {
	ms := store.NewMemStore()
	cmdr := stest.NewTestOSCalls("", 0)
	idrunner := &dev.IdentityDataRunner{
		Cmdr: cmdr,
	}
	ks := store.NewKeystore(ms, "key", "", false, defaultKeyPassphrase)

	am := NewAuthManager(AuthManagerConfig{
		AuthDataStore:  nil,
		IdentitySource: nil,
		KeyStore:       nil,
	})
	assert.Nil(t, am)

	am = NewAuthManager(AuthManagerConfig{
		AuthDataStore:  ms,
		IdentitySource: nil,
		KeyStore:       nil,
	})
	assert.Nil(t, am)

	am = NewAuthManager(AuthManagerConfig{
		AuthDataStore:  ms,
		IdentitySource: idrunner,
		KeyStore:       nil,
	})
	assert.Nil(t, am)

	am = NewAuthManager(AuthManagerConfig{
		AuthDataStore:  ms,
		IdentitySource: idrunner,
		KeyStore:       ks,
	})
	assert.NotNil(t, am)
}

func TestAuthManager(t *testing.T) {
	ms := store.NewMemStore()

	cmdr := stest.NewTestOSCalls("", 0)

	am := NewAuthManager(AuthManagerConfig{
		AuthDataStore: ms,
		IdentitySource: &dev.IdentityDataRunner{
			Cmdr: cmdr,
		},
		KeyStore: store.NewKeystore(ms, "key", "", false, defaultKeyPassphrase),
	})
	assert.NotNil(t, am)
	assert.IsType(t, &MenderAuthManager{}, am)

	assert.False(t, am.HasKey())
	assert.NoError(t, am.GenerateKey())
	assert.True(t, am.HasKey())

	am = NewAuthManager(AuthManagerConfig{
		AuthDataStore: ms,
		IdentitySource: &dev.IdentityDataRunner{
			Cmdr: cmdr,
		},
		KeyStore: store.NewKeystore(ms, "key", "", true, defaultKeyPassphrase),
	})
	err := am.GenerateKey()
	if assert.Error(t, err) {
		assert.True(t, store.IsStaticKey(err))
	}

}

func TestAuthManagerRequest(t *testing.T) {
	ms := store.NewMemStore()

	var err error

	badCmdr := stest.NewTestOSCalls("mac=foobar", -1)
	am := NewAuthManager(AuthManagerConfig{
		AuthDataStore: ms,
		IdentitySource: &dev.IdentityDataRunner{
			Cmdr: badCmdr,
		},
		TenantToken: []byte("tenant"),
		KeyStore:    store.NewKeystore(ms, "key", "", false, defaultKeyPassphrase),
	})
	assert.NotNil(t, am)

	_, err = am.MakeAuthRequest()
	assert.Error(t, err, "should fail, cannot obtain identity data")
	assert.Contains(t, err.Error(), "identity data")

	cmdr := stest.NewTestOSCalls("mac=foobar", 0)
	am = NewAuthManager(AuthManagerConfig{
		AuthDataStore: ms,
		IdentitySource: dev.IdentityDataRunner{
			Cmdr: cmdr,
		},
		KeyStore:    store.NewKeystore(ms, "key", "", false, defaultKeyPassphrase),
		TenantToken: []byte("tenant"),
	})
	assert.NotNil(t, am)
	_, err = am.MakeAuthRequest()
	assert.Error(t, err, "should fail, no device keys are present")
	assert.Contains(t, err.Error(), "device public key")

	// generate key first
	assert.NoError(t, am.GenerateKey())

	req, err := am.MakeAuthRequest()
	assert.NoError(t, err)
	assert.NotEmpty(t, req.Data)
	assert.Equal(t, client.AuthToken("tenant"), req.Token)
	assert.NotEmpty(t, req.Signature)

	var ard client.AuthReqData
	err = json.Unmarshal(req.Data, &ard)
	assert.NoError(t, err)

	pempub, _ := am.keyStore.PublicPEM()
	assert.Equal(t, client.AuthReqData{
		IdData:      "{\"mac\":\"foobar\"}",
		TenantToken: "tenant",
		Pubkey:      pempub,
	}, ard)

	sign, err := am.keyStore.Sign(req.Data)
	assert.NoError(t, err)
	assert.Equal(t, sign, req.Signature)
}

func TestForceBootstrap(t *testing.T) {
	// generate valid keys
	cmdr := stest.NewTestOSCalls("mac=foobar", 0)
	ms := store.NewMemStore()
	am := NewAuthManager(AuthManagerConfig{
		AuthDataStore: ms,
		IdentitySource: dev.IdentityDataRunner{
			Cmdr: cmdr,
		},
		KeyStore: store.NewKeystore(ms, conf.DefaultKeyFile, "", false, defaultKeyPassphrase),
	})
	assert.NotNil(t, am)

	merr := am.Bootstrap()
	assert.NoError(t, merr)

	kdataold, err := ms.ReadAll(conf.DefaultKeyFile)
	assert.NoError(t, err)
	assert.NotEmpty(t, kdataold)

	am.ForceBootstrap()
	assert.True(t, am.needsBootstrap())

	merr = am.Bootstrap()
	assert.NoError(t, merr)

	// bootstrap should have generated a new key
	kdatanew, err := ms.ReadAll(conf.DefaultKeyFile)
	assert.NoError(t, err)
	assert.NotEmpty(t, kdatanew)
	// we should have a new key
	assert.NotEqual(t, kdatanew, kdataold)
}

func TestBootstrap(t *testing.T) {
	cmdr := stest.NewTestOSCalls("mac=foobar", 0)
	ms := store.NewMemStore()
	am := NewAuthManager(AuthManagerConfig{
		AuthDataStore: ms,
		IdentitySource: dev.IdentityDataRunner{
			Cmdr: cmdr,
		},
		KeyStore: store.NewKeystore(ms, conf.DefaultKeyFile, "", false, defaultKeyPassphrase),
	})
	assert.NotNil(t, am)

	assert.True(t, am.needsBootstrap())
	assert.NoError(t, am.Bootstrap())

	k := store.NewKeystore(ms, conf.DefaultKeyFile, "", false, defaultKeyPassphrase)
	assert.NotNil(t, k)
	assert.NoError(t, k.Load())
}

func TestBootstrappedHaveKeys(t *testing.T) {
	// generate valid keys
	ms := store.NewMemStore()
	k := store.NewKeystore(ms, conf.DefaultKeyFile, "", false, defaultKeyPassphrase)
	assert.NotNil(t, k)
	assert.NoError(t, k.Generate())
	assert.NoError(t, k.Save())

	cmdr := stest.NewTestOSCalls("mac=foobar", 0)
	am := NewAuthManager(AuthManagerConfig{
		AuthDataStore: ms,
		IdentitySource: dev.IdentityDataRunner{
			Cmdr: cmdr,
		},
		KeyStore: store.NewKeystore(ms, conf.DefaultKeyFile, "", false, defaultKeyPassphrase),
	})
	assert.NotNil(t, am)
	assert.Equal(t, ms, am.keyStore.GetStore())
	assert.NotNil(t, am.keyStore.Private())

	// subsequen bootstrap should not fail
	assert.NoError(t, am.Bootstrap())
}

func TestBootstrapError(t *testing.T) {
	ms := store.NewMemStore()
	ms.Disable(true)

	cmdr := stest.NewTestOSCalls("mac=foobar", 0)
	am := NewAuthManager(AuthManagerConfig{
		AuthDataStore: ms,
		IdentitySource: dev.IdentityDataRunner{
			Cmdr: cmdr,
		},
		KeyStore: store.NewKeystore(ms, conf.DefaultKeyFile, "", false, defaultKeyPassphrase),
	})

	// store is disabled, attempts to load keys when creating authManager should have
	// failed, resulting in empty keys.
	assert.False(t, am.HasKey())

	ms.Disable(false)
	am = NewAuthManager(AuthManagerConfig{
		AuthDataStore: ms,
		IdentitySource: dev.IdentityDataRunner{
			Cmdr: cmdr,
		},
		KeyStore: store.NewKeystore(ms, conf.DefaultKeyFile, "", false, defaultKeyPassphrase),
	})
	assert.NotNil(t, am)

	ms.ReadOnly(true)

	err := am.Bootstrap()
	assert.Error(t, err)
}

func TestMenderAuthorize(t *testing.T) {
	_ = stest.NewTestOSCalls("", -1)

	rspdata := []byte("authorized")
	atok := client.AuthToken("authorized")

	srv := cltest.NewClientTestServer()
	defer srv.Close()

	config := &conf.MenderConfig{}

	// mocked DBus API
	dbusAPI := &mocks.DBusAPI{}
	defer dbusAPI.AssertExpectations(t)

	dbusConn := dbus.Handle(nil)
	dbusLoop := dbus.MainLoop(nil)

	dbusAPI.On("BusGet",
		mock.AnythingOfType("uint"),
	).Return(dbusConn, nil)

	dbusAPI.On("BusOwnNameOnConnection",
		dbusConn,
		AuthManagerDBusObjectName,
		mock.AnythingOfType("uint"),
	).Return(uint(1), nil)

	dbusAPI.On("BusRegisterInterface",
		dbusConn,
		AuthManagerDBusPath,
		AuthManagerDBusInterface,
	).Return(uint(2), nil)

	dbusAPI.On("MainLoopNew").Return(dbusLoop)
	dbusAPI.On("MainLoopRun", dbusLoop).Return()

	dbusAPI.On("RegisterMethodCallCallback",
		AuthManagerDBusPath,
		AuthManagerDBusInterfaceName,
		"GetJwtToken",
		mock.Anything,
	)

	dbusAPI.On("RegisterMethodCallCallback",
		AuthManagerDBusPath,
		AuthManagerDBusInterfaceName,
		"FetchJwtToken",
		mock.Anything,
	)

	dbusAPI.On("EmitSignal",
		dbusConn,
		"",
		AuthManagerDBusPath,
		AuthManagerDBusInterfaceName,
		AuthManagerDBusSignalJwtTokenStateChange,
		mock.AnythingOfType("dbus.TokenAndServerURL"),
	).Return(nil)

	dbusAPI.On("MainLoopQuit", dbusLoop)

	dbusAPI.On("UnregisterMethodCallCallback",
		AuthManagerDBusPath,
		AuthManagerDBusInterfaceName,
		"FetchJwtToken",
	)

	dbusAPI.On("UnregisterMethodCallCallback",
		AuthManagerDBusPath,
		AuthManagerDBusInterfaceName,
		"GetJwtToken",
	)

	dbusAPI.On("BusUnregisterInterface",
		dbusConn,
		uint(2),
	).Return(true)

	dbusAPI.On("BusUnownName",
		uint(1),
	)

	ms := store.NewMemStore()
	cmdr := stest.NewTestOSCalls("mac=foobar", 0)
	am := NewAuthManager(AuthManagerConfig{
		AuthDataStore: ms,
		IdentitySource: dev.IdentityDataRunner{
			Cmdr: cmdr,
		},
		KeyStore: store.NewKeystore(ms, conf.DefaultKeyFile, "", false, defaultKeyPassphrase),
		Config:   config,
	})
	am.EnableDBus(dbusAPI)

	am.Start()
	defer am.Stop()

	// subscribe to responses
	inChan := am.GetInMessageChan()
	broadcastChan := am.GetBroadcastMessageChan(authManagerTestChannelName)
	respChan := make(chan AuthManagerResponse)

	// 1. error if server list is empty

	// - request
	inChan <- AuthManagerRequest{
		Action:          ActionFetchAuthToken,
		ResponseChannel: respChan,
	}

	// - response
	message := <-respChan
	assert.NoError(t, message.Error)
	assert.Equal(t, EventFetchAuthToken, message.Event)

	// 2. successful authorization
	config.Servers = make([]conf.MenderServer, 1)
	config.Servers[0].ServerURL = srv.URL
	srv.Auth.Called = false
	srv.Auth.Authorize = true
	srv.Auth.Token = rspdata

	// - request
	inChan <- AuthManagerRequest{
		Action:          ActionFetchAuthToken,
		ResponseChannel: respChan,
	}

	// - response
	message = <-respChan
	assert.NoError(t, message.Error)
	assert.Equal(t, EventFetchAuthToken, message.Event)

	// - broadcast
	message = <-broadcastChan
	assert.NoError(t, message.Error)
	assert.Equal(t, EventAuthTokenStateChange, message.Event)

	// - get the token
	inChan <- AuthManagerRequest{
		Action:          ActionGetAuthToken,
		ResponseChannel: respChan,
	}
	message = <-respChan
	assert.NoError(t, message.Error)
	assert.Equal(t, atok, message.AuthToken)
	assert.Equal(t, EventGetAuthToken, message.Event)

	// - check the api has been called
	assert.True(t, srv.Auth.Called)

	// 2. client is already authorized
	srv.Auth.Called = false

	// - request
	inChan <- AuthManagerRequest{
		Action:          ActionFetchAuthToken,
		ResponseChannel: respChan,
	}

	// - response
	message = <-respChan
	assert.NoError(t, message.Error)
	assert.Equal(t, EventFetchAuthToken, message.Event)

	// - broadcast
	message = <-broadcastChan
	assert.NoError(t, message.Error)
	assert.Equal(t, EventAuthTokenStateChange, message.Event)

	// - get the token
	inChan <- AuthManagerRequest{
		Action:          ActionGetAuthToken,
		ResponseChannel: respChan,
	}
	message = <-respChan
	assert.NoError(t, message.Error)
	assert.Equal(t, atok, message.AuthToken)
	assert.Equal(t, EventGetAuthToken, message.Event)

	// - check the api has been called
	assert.True(t, srv.Auth.Called)

	// 3. call the server, server denies the authorization
	srv.Auth.Called = false
	srv.Auth.Authorize = false
	am.authToken = ""

	// - request
	inChan <- AuthManagerRequest{
		Action:          ActionFetchAuthToken,
		ResponseChannel: respChan,
	}

	// - response
	message = <-respChan
	assert.NoError(t, message.Error)
	assert.Equal(t, EventFetchAuthToken, message.Event)

	// - broadcast, auth token state changed
	message = <-broadcastChan
	assert.Equal(t, EventAuthTokenStateChange, message.Event)
	assert.Equal(t, noAuthToken, message.AuthToken)

	// - check the api has been called
	assert.True(t, srv.Auth.Called)

	// 4. authorization manager fails to parse response
	srv.Auth.Called = false
	srv.Auth.Token = []byte("")
	am.authToken = ""

	// - request
	inChan <- AuthManagerRequest{
		Action:          ActionFetchAuthToken,
		ResponseChannel: respChan,
	}

	// - response
	message = <-respChan
	assert.NoError(t, message.Error)
	assert.Equal(t, EventFetchAuthToken, message.Event)

	// - broadcast, auth token state changed
	message = <-broadcastChan
	assert.Equal(t, EventAuthTokenStateChange, message.Event)
	assert.Equal(t, noAuthToken, message.AuthToken)

	// - check the api has been called
	assert.True(t, srv.Auth.Called)

	// 5. authorization manager throws no errors, server authorizes the client
	srv.Auth.Called = false
	srv.Auth.Authorize = true
	srv.Auth.Token = rspdata
	am.authToken = ""

	// - request
	inChan <- AuthManagerRequest{
		Action:          ActionFetchAuthToken,
		ResponseChannel: respChan,
	}

	// - response
	message = <-respChan
	assert.NoError(t, message.Error)
	assert.Equal(t, EventFetchAuthToken, message.Event)

	// - broadcast
	message = <-broadcastChan
	assert.NoError(t, message.Error)
	assert.Equal(t, EventAuthTokenStateChange, message.Event)

	// - get the token
	inChan <- AuthManagerRequest{
		Action:          ActionGetAuthToken,
		ResponseChannel: respChan,
	}
	message = <-respChan
	assert.NoError(t, message.Error)
	assert.Equal(t, atok, message.AuthToken)
	assert.Equal(t, EventGetAuthToken, message.Event)

	// - check the api has been called
	assert.True(t, srv.Auth.Called)
}

func TestAuthManagerFinalizer(t *testing.T) {
	config := &conf.MenderConfig{}
	ms := store.NewMemStore()
	cmdr := stest.NewTestOSCalls("mac=foobar", 0)
	am := NewAuthManager(AuthManagerConfig{
		AuthDataStore: ms,
		IdentitySource: dev.IdentityDataRunner{
			Cmdr: cmdr,
		},
		KeyStore: store.NewKeystore(ms, conf.DefaultKeyFile, "", false, defaultKeyPassphrase),
		Config:   config,
	})

	runtime.GC()
	goRoutines := runtime.NumGoroutine()

	am.Start()

	assert.Greater(t, runtime.NumGoroutine(), goRoutines)

	// This should make the server unreachable, and garbage collection
	// should invoke the finalizer which kills the go routine.
	am = nil

	runtime.GC()
	// Give the Go routine a little bit of cleanup time.
	time.Sleep(400 * time.Millisecond)
	assert.Equal(t, goRoutines, runtime.NumGoroutine())
}
