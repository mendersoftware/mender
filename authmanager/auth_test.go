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

package authmanager

import (
	"encoding/json"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mendersoftware/mender/authmanager/api"
	"github.com/mendersoftware/mender/authmanager/conf"
	"github.com/mendersoftware/mender/authmanager/device"
	"github.com/mendersoftware/mender/authmanager/test"
	commonconf "github.com/mendersoftware/mender/common/conf"
	"github.com/mendersoftware/mender/common/dbus"
	dbustest "github.com/mendersoftware/mender/common/dbus/test"
	"github.com/mendersoftware/mender/common/store"
	stest "github.com/mendersoftware/mender/common/system/testing"
	"github.com/mendersoftware/mender/common/tls"
)

const (
	defaultKeyPassphrase = ""
)

func TestNewAuthManager(t *testing.T) {
	ms := store.NewMemStore()

	am, err := NewAuthManager(AuthManagerConfig{})
	assert.Nil(t, am)
	assert.Error(t, err)

	am, err = NewAuthManager(AuthManagerConfig{
		AuthDataStore: ms,
	})
	assert.Nil(t, am)
	assert.Error(t, err)

	am, err = NewAuthManager(AuthManagerConfig{
		AuthConfig:    &conf.AuthConfig{},
		AuthDataStore: ms,
		KeyDirStore:   ms,
	})
	assert.NotNil(t, am)
	assert.NoError(t, err)
}

func TestAuthManagerGenerateKey(t *testing.T) {
	ms := store.NewMemStore()

	am, _ := NewAuthManager(AuthManagerConfig{
		AuthConfig:    &conf.AuthConfig{},
		AuthDataStore: ms,
		KeyDirStore:   ms,
	})
	assert.NotNil(t, am)
	assert.IsType(t, &MenderAuthManager{}, am)

	assert.False(t, am.HasKey())
	assert.NoError(t, am.GenerateKey())
	assert.True(t, am.HasKey())

	am, _ = NewAuthManager(AuthManagerConfig{
		AuthDataStore: ms,
		KeyDirStore:   ms,
		// This triggers use of a static key.
		AuthConfig: &conf.AuthConfig{
			Config: commonconf.Config{
				HttpsClient: tls.HttpsClient{
					Key: "key",
				},
			},
		},
	})
	err := am.GenerateKey()
	require.Error(t, err)
	assert.True(t, store.IsStaticKey(err))
}

func TestAuthManagerRequest(t *testing.T) {
	ms := store.NewMemStore()

	var err error

	badCmdr := stest.NewTestOSCalls("mac=foobar", -1)
	am, _ := NewAuthManager(AuthManagerConfig{
		AuthDataStore: ms,
		AuthConfig: &conf.AuthConfig{
			TenantToken: "tenant",
		},
		KeyDirStore: ms,
	})
	assert.NotNil(t, am)

	am.idSrc = &device.IdentityDataRunner{
		Cmdr: badCmdr,
	}

	_, err = am.MakeAuthRequest()
	assert.Error(t, err, "should fail, cannot obtain identity data")
	assert.Contains(t, err.Error(), "identity data")

	cmdr := stest.NewTestOSCalls("mac=foobar", 0)
	am, _ = NewAuthManager(AuthManagerConfig{
		AuthDataStore: ms,
		AuthConfig: &conf.AuthConfig{
			TenantToken: "tenant",
		},
		KeyDirStore: ms,
	})
	assert.NotNil(t, am)

	am.idSrc = &device.IdentityDataRunner{
		Cmdr: cmdr,
	}

	_, err = am.MakeAuthRequest()
	assert.Error(t, err, "should fail, no device keys are present")
	assert.Contains(t, err.Error(), "device public key")

	// generate key first
	assert.NoError(t, am.GenerateKey())

	req, err := am.MakeAuthRequest()
	assert.NoError(t, err)
	assert.NotEmpty(t, req.Data)
	assert.Equal(t, api.AuthToken("tenant"), req.Token)
	assert.NotEmpty(t, req.Signature)

	var ard api.AuthReqData
	err = json.Unmarshal(req.Data, &ard)
	assert.NoError(t, err)

	pempub, _ := am.keyStore.PublicPEM()
	assert.Equal(t, api.AuthReqData{
		IdData:      "{\"mac\":\"foobar\"}",
		TenantToken: "tenant",
		Pubkey:      pempub,
	}, ard)

	sign, err := am.keyStore.Sign(req.Data)
	assert.NoError(t, err)
	assert.Equal(t, sign, req.Signature)
}

func TestAuthManagerResponse(t *testing.T) {
	ms := store.NewMemStore()

	cmdr := stest.NewTestOSCalls("mac=foobar", 0)
	am, _ := NewAuthManager(AuthManagerConfig{
		AuthConfig:    &conf.AuthConfig{},
		AuthDataStore: ms,
		KeyDirStore:   ms,
	})
	assert.NotNil(t, am)

	am.idSrc = device.IdentityDataRunner{
		Cmdr: cmdr,
	}

	var err error
	err = am.recvAuthResponse([]byte{})
	// should fail with empty response
	assert.Error(t, err)

	err = am.recvAuthResponse([]byte("fooresp"))
	assert.NoError(t, err)
	assert.Equal(t, api.AuthToken("fooresp"), am.authToken)
}

func TestForceBootstrap(t *testing.T) {
	// generate valid keys
	ms := store.NewMemStore()
	am, _ := NewAuthManager(AuthManagerConfig{
		AuthConfig:    &conf.AuthConfig{},
		AuthDataStore: ms,
		KeyDirStore:   ms,
	})
	assert.NotNil(t, am)

	merr := am.Bootstrap()
	assert.NoError(t, merr)

	kdataold, err := ms.ReadAll(commonconf.DefaultKeyFile)
	assert.NoError(t, err)
	assert.NotEmpty(t, kdataold)

	am.ForceBootstrap()
	assert.True(t, am.needsBootstrap())

	merr = am.Bootstrap()
	assert.NoError(t, merr)

	// bootstrap should have generated a new key
	kdatanew, err := ms.ReadAll(commonconf.DefaultKeyFile)
	assert.NoError(t, err)
	assert.NotEmpty(t, kdatanew)
	// we should have a new key
	assert.NotEqual(t, kdatanew, kdataold)
}

func TestBootstrap(t *testing.T) {
	ms := store.NewMemStore()
	am, _ := NewAuthManager(AuthManagerConfig{
		AuthConfig:    &conf.AuthConfig{},
		AuthDataStore: ms,
		KeyDirStore:   ms,
	})
	assert.NotNil(t, am)

	assert.True(t, am.needsBootstrap())
	assert.NoError(t, am.Bootstrap())

	k := store.NewKeystore(ms, commonconf.DefaultKeyFile, "", false, defaultKeyPassphrase)
	assert.NotNil(t, k)
	assert.NoError(t, k.Load())
}

func TestBootstrappedHaveKeys(t *testing.T) {
	// generate valid keys
	ms := store.NewMemStore()
	k := store.NewKeystore(ms, commonconf.DefaultKeyFile, "", false, defaultKeyPassphrase)
	assert.NotNil(t, k)
	assert.NoError(t, k.Generate())
	assert.NoError(t, k.Save())

	am, _ := NewAuthManager(AuthManagerConfig{
		AuthConfig:    &conf.AuthConfig{},
		AuthDataStore: ms,
		KeyDirStore:   ms,
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

	am, _ := NewAuthManager(AuthManagerConfig{
		AuthConfig:    &conf.AuthConfig{},
		AuthDataStore: ms,
		KeyDirStore:   ms,
	})

	// store is disabled, attempts to load keys when creating authManager should have
	// failed, resulting in empty keys.
	assert.False(t, am.HasKey())

	ms.Disable(false)
	am, _ = NewAuthManager(AuthManagerConfig{
		AuthConfig:    &conf.AuthConfig{},
		AuthDataStore: ms,
		KeyDirStore:   ms,
	})
	assert.NotNil(t, am)

	ms.ReadOnly(true)

	assert.Panics(t, func() { am.Bootstrap() })
}

func TestMenderAuthorize(t *testing.T) {
	_ = stest.NewTestOSCalls("", -1)

	rspdata := []byte("authorized")
	atok := "authorized"

	srv := test.NewAuthTestServer()
	defer srv.Close()

	dbusServer := dbustest.NewDBusTestServer()
	defer dbusServer.Close()

	config := &conf.AuthConfig{}

	// mocked DBus API
	dbusAPI := dbusServer.GetDBusAPI()
	handle, err := dbusAPI.BusGet(dbus.GBusTypeSystem)
	require.NoError(t, err)

	tokenChanged := make(dbus.SignalChannel, 10)
	dbusAPI.RegisterSignalChannel(handle, AuthManagerDBusObjectName, AuthManagerDBusPath,
		AuthManagerDBusInterfaceName, "JwtTokenStateChange", tokenChanged)
	defer dbusAPI.UnregisterSignalChannel(handle, "JwtTokenStateChange", tokenChanged)

	ms := store.NewMemStore()
	cmdr := stest.NewTestOSCalls("mac=foobar", 0)
	am, _ := NewAuthManager(AuthManagerConfig{
		AuthDataStore: ms,
		KeyDirStore:   ms,
		AuthConfig:    config,
	})
	am.idSrc = device.IdentityDataRunner{
		Cmdr: cmdr,
	}
	am.dbus = dbusAPI

	am.Start()
	defer am.Stop()

	// 1. error if server list has invalid entry
	config.Servers = make([]conf.MenderServer, 1)
	config.Servers[0].ServerURL = "http://bogusserver-no-such-thing.com.org.edu.qwerty"

	// - request
	response, err := dbusAPI.Call0(handle, AuthManagerDBusObjectName, AuthManagerDBusPath,
		AuthManagerDBusInterfaceName, "FetchJwtToken")
	assert.NoError(t, err)
	assert.True(t, response[0].(bool))

	// - Token changed signal
	tokenChangedMessage := <-tokenChanged
	assert.Equal(t, "", tokenChangedMessage[0].(string))

	// 2. successful authorization
	config.Servers[0].ServerURL = srv.Server.URL
	srv.Auth.Called = false
	srv.Auth.Authorize = true
	srv.Auth.Token = rspdata

	// - request
	response, err = dbusAPI.Call0(handle, AuthManagerDBusObjectName, AuthManagerDBusPath,
		AuthManagerDBusInterfaceName, "FetchJwtToken")
	assert.NoError(t, err)
	assert.True(t, response[0].(bool))

	// - Token changed signal
	tokenChangedMessage = <-tokenChanged
	assert.Equal(t, atok, tokenChangedMessage[0].(string))

	// - get the token
	response, err = dbusAPI.Call0(handle, AuthManagerDBusObjectName, AuthManagerDBusPath,
		AuthManagerDBusInterfaceName, "GetJwtToken")
	assert.NoError(t, err)
	assert.Equal(t, atok, response[0].(string))
	assert.Equal(t, srv.Server.URL, response[1].(string))

	// - check the api has been called
	assert.True(t, srv.Auth.Called)

	// 2. client is already authorized
	srv.Auth.Called = false

	// - request
	response, err = dbusAPI.Call0(handle, AuthManagerDBusObjectName, AuthManagerDBusPath,
		AuthManagerDBusInterfaceName, "FetchJwtToken")
	assert.NoError(t, err)
	assert.True(t, response[0].(bool))

	// - Token changed signal
	tokenChangedMessage = <-tokenChanged
	assert.Equal(t, atok, tokenChangedMessage[0].(string))

	// - get the token
	response, err = dbusAPI.Call0(handle, AuthManagerDBusObjectName, AuthManagerDBusPath,
		AuthManagerDBusInterfaceName, "GetJwtToken")
	assert.NoError(t, err)
	assert.Equal(t, atok, response[0].(string))
	assert.Equal(t, srv.Server.URL, response[1].(string))

	// - check the api has been called
	assert.True(t, srv.Auth.Called)

	// 3. call the server, server denies the authorization
	srv.Auth.Called = false
	srv.Auth.Authorize = false
	am.authToken = ""

	// - request
	response, err = dbusAPI.Call0(handle, AuthManagerDBusObjectName, AuthManagerDBusPath,
		AuthManagerDBusInterfaceName, "FetchJwtToken")
	assert.NoError(t, err)
	assert.True(t, response[0].(bool))

	// - Token changed signal
	tokenChangedMessage = <-tokenChanged
	assert.Equal(t, "", tokenChangedMessage[0].(string))

	// - check the api has been called
	assert.True(t, srv.Auth.Called)

	// 4. authorization manager fails to parse response
	srv.Auth.Called = false
	srv.Auth.Token = []byte("")
	am.authToken = ""

	// - request
	response, err = dbusAPI.Call0(handle, AuthManagerDBusObjectName, AuthManagerDBusPath,
		AuthManagerDBusInterfaceName, "FetchJwtToken")
	assert.NoError(t, err)
	assert.True(t, response[0].(bool))

	// - Token changed signal
	tokenChangedMessage = <-tokenChanged
	assert.Equal(t, "", tokenChangedMessage[0].(string))

	// - check the api has been called
	assert.True(t, srv.Auth.Called)

	// 5. authorization manager throws no errors, server authorizes the client
	srv.Auth.Called = false
	srv.Auth.Authorize = true
	srv.Auth.Token = rspdata
	am.authToken = ""

	// - request
	response, err = dbusAPI.Call0(handle, AuthManagerDBusObjectName, AuthManagerDBusPath,
		AuthManagerDBusInterfaceName, "FetchJwtToken")
	assert.NoError(t, err)
	assert.True(t, response[0].(bool))

	// - Token changed signal
	tokenChangedMessage = <-tokenChanged
	assert.Equal(t, atok, tokenChangedMessage[0].(string))

	// - get the token
	response, err = dbusAPI.Call0(handle, AuthManagerDBusObjectName, AuthManagerDBusPath,
		AuthManagerDBusInterfaceName, "GetJwtToken")
	assert.NoError(t, err)
	assert.Equal(t, atok, response[0].(string))
	assert.Equal(t, srv.Server.URL, response[1].(string))

	// - check the api has been called
	assert.True(t, srv.Auth.Called)
}

func TestAuthManagerFinalizer(t *testing.T) {
	config := &conf.AuthConfig{}
	ms := store.NewMemStore()
	am, _ := NewAuthManager(AuthManagerConfig{
		AuthDataStore: ms,
		KeyDirStore:   ms,
		AuthConfig:    config,
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
	time.Sleep(500 * time.Millisecond)
	assert.Equal(t, runtime.NumGoroutine(), goRoutines)
}
