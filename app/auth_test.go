// Copyright 2020 Northern.tech AS
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
	"encoding/json"
	"testing"

	"github.com/mendersoftware/mender/client"
	cltest "github.com/mendersoftware/mender/client/test"
	"github.com/mendersoftware/mender/conf"
	"github.com/mendersoftware/mender/datastore"
	"github.com/mendersoftware/mender/dbus"
	"github.com/mendersoftware/mender/dbus/mocks"
	dev "github.com/mendersoftware/mender/device"
	"github.com/mendersoftware/mender/store"
	stest "github.com/mendersoftware/mender/system/testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
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
	ks := store.NewKeystore(ms, "key", "", false)

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

	dbus := &mocks.DBusAPI{}
	am = am.WithDBus(dbus)
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
		KeyStore: store.NewKeystore(ms, "key", "", false),
	})
	assert.NotNil(t, am)
	assert.IsType(t, &MenderAuthManager{}, am)

	assert.False(t, am.HasKey())
	assert.NoError(t, am.GenerateKey())
	assert.True(t, am.HasKey())

	code, err := am.AuthToken()
	assert.Equal(t, noAuthToken, code)
	assert.NoError(t, err)

	ms.WriteAll(datastore.AuthTokenName, []byte("footoken"))
	// disable store access
	ms.Disable(true)
	_, err = am.AuthToken()
	assert.Error(t, err)
	ms.Disable(false)

	code, err = am.AuthToken()
	assert.Equal(t, client.AuthToken("footoken"), code)
	assert.NoError(t, err)

	am = NewAuthManager(AuthManagerConfig{
		AuthDataStore: ms,
		IdentitySource: &dev.IdentityDataRunner{
			Cmdr: cmdr,
		},
		KeyStore: store.NewKeystore(ms, "key", "", true),
	})
	err = am.GenerateKey()
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
		KeyStore:    store.NewKeystore(ms, "key", "", false),
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
		KeyStore:    store.NewKeystore(ms, "key", "", false),
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

	mam := am.(*MenderAuthManager)
	pempub, _ := mam.keyStore.PublicPEM()
	assert.Equal(t, client.AuthReqData{
		IdData:      "{\"mac\":\"foobar\"}",
		TenantToken: "tenant",
		Pubkey:      pempub,
	}, ard)

	sign, err := mam.keyStore.Sign(req.Data)
	assert.NoError(t, err)
	assert.Equal(t, sign, req.Signature)
}

func TestAuthManagerResponse(t *testing.T) {
	ms := store.NewMemStore()

	cmdr := stest.NewTestOSCalls("mac=foobar", 0)
	am := NewAuthManager(AuthManagerConfig{
		AuthDataStore: ms,
		IdentitySource: dev.IdentityDataRunner{
			Cmdr: cmdr,
		},
		KeyStore: store.NewKeystore(ms, "key", "", false),
	})
	assert.NotNil(t, am)

	var err error
	err = am.RecvAuthResponse([]byte{})
	// should fail with empty response
	assert.Error(t, err)

	// make storage RO
	ms.ReadOnly(true)
	err = am.RecvAuthResponse([]byte("fooresp"))
	assert.Error(t, err)

	ms.ReadOnly(false)
	err = am.RecvAuthResponse([]byte("fooresp"))
	assert.NoError(t, err)
	tokdata, err := ms.ReadAll(datastore.AuthTokenName)
	assert.NoError(t, err)
	assert.Equal(t, []byte("fooresp"), tokdata)
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
		KeyStore: store.NewKeystore(ms, conf.DefaultKeyFile, "", false),
	})
	assert.NotNil(t, am)

	merr := am.Bootstrap()
	assert.NoError(t, merr)

	kdataold, err := ms.ReadAll(conf.DefaultKeyFile)
	assert.NoError(t, err)
	assert.NotEmpty(t, kdataold)

	am.ForceBootstrap()
	assert.True(t, am.(*MenderAuthManager).needsBootstrap())

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
		KeyStore: store.NewKeystore(ms, conf.DefaultKeyFile, "", false),
	})
	assert.NotNil(t, am)

	assert.True(t, am.(*MenderAuthManager).needsBootstrap())
	assert.NoError(t, am.Bootstrap())

	mam, _ := am.(*MenderAuthManager)
	k := store.NewKeystore(mam.store, conf.DefaultKeyFile, "", false)
	assert.NotNil(t, k)
	assert.NoError(t, k.Load())
}

func TestBootstrappedHaveKeys(t *testing.T) {
	// generate valid keys
	ms := store.NewMemStore()
	k := store.NewKeystore(ms, conf.DefaultKeyFile, "", false)
	assert.NotNil(t, k)
	assert.NoError(t, k.Generate())
	assert.NoError(t, k.Save())

	cmdr := stest.NewTestOSCalls("mac=foobar", 0)
	am := NewAuthManager(AuthManagerConfig{
		AuthDataStore: ms,
		IdentitySource: dev.IdentityDataRunner{
			Cmdr: cmdr,
		},
		KeyStore: store.NewKeystore(ms, conf.DefaultKeyFile, "", false),
	})
	assert.NotNil(t, am)
	mam, _ := am.(*MenderAuthManager)
	assert.Equal(t, ms, mam.keyStore.GetStore())
	assert.NotNil(t, mam.keyStore.Private())

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
		KeyStore: store.NewKeystore(ms, conf.DefaultKeyFile, "", false),
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
		KeyStore: store.NewKeystore(ms, conf.DefaultKeyFile, "", false),
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
		AuthManagerDBusSignalValidJwtTokenAvailable,
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
		KeyStore: store.NewKeystore(ms, conf.DefaultKeyFile, "", false),
		Config:   config,
	}).WithDBus(dbusAPI)

	go am.Run()
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

	// - broadcast, error message received
	message = <-broadcastChan
	assert.Equal(t, EventFetchAuthToken, message.Event)
	assert.Error(t, message.Error)

	// 2. successful authorization
	config.Servers = make([]client.MenderServer, 1)
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
	assert.Equal(t, EventAuthTokenAvailable, message.Event)

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
	assert.Equal(t, EventAuthTokenAvailable, message.Event)

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
	am.RemoveAuthToken()

	// - request
	inChan <- AuthManagerRequest{
		Action:          ActionFetchAuthToken,
		ResponseChannel: respChan,
	}

	// - response
	message = <-respChan
	assert.NoError(t, message.Error)
	assert.Equal(t, EventFetchAuthToken, message.Event)

	// - broadcast, error message received
	message = <-broadcastChan
	assert.Equal(t, EventFetchAuthToken, message.Event)
	assert.Error(t, message.Error)

	// - check the api has been called
	assert.True(t, srv.Auth.Called)

	// 4. authorization manager fails to parse response
	srv.Auth.Called = false
	srv.Auth.Token = []byte("")
	am.RemoveAuthToken()

	// - request
	inChan <- AuthManagerRequest{
		Action:          ActionFetchAuthToken,
		ResponseChannel: respChan,
	}

	// - response
	message = <-respChan
	assert.NoError(t, message.Error)
	assert.Equal(t, EventFetchAuthToken, message.Event)

	// - broadcast, error message received
	message = <-broadcastChan
	assert.Equal(t, EventFetchAuthToken, message.Event)
	assert.Error(t, message.Error)

	// - check the api has been called
	assert.True(t, srv.Auth.Called)

	// // 5. authorization manger throws no errors, server authorizes the client
	srv.Auth.Called = false
	srv.Auth.Authorize = true
	srv.Auth.Token = rspdata
	am.RemoveAuthToken()

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
	assert.Equal(t, EventAuthTokenAvailable, message.Event)

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
