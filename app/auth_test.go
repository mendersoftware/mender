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
	"github.com/mendersoftware/mender/datastore"
	dev "github.com/mendersoftware/mender/device"
	"github.com/mendersoftware/mender/store"
	stest "github.com/mendersoftware/mender/system/testing"
	"github.com/stretchr/testify/assert"
)

func TestNewAuthManager(t *testing.T) {
	ms := store.NewMemStore()
	cmdr := stest.NewTestOSCalls("", 0)
	idrunner := &dev.IdentityDataRunner{
		Cmdr: cmdr,
	}
	ks := store.NewKeystore(ms, "key")

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
		KeyStore: store.NewKeystore(ms, "key"),
	})
	assert.NotNil(t, am)
	assert.IsType(t, &MenderAuthManager{}, am)

	assert.False(t, am.HasKey())
	assert.NoError(t, am.GenerateKey())
	assert.True(t, am.HasKey())

	assert.False(t, am.IsAuthorized())

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
		KeyStore:    store.NewKeystore(ms, "key"),
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
		KeyStore:    store.NewKeystore(ms, "key"),
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
		KeyStore: store.NewKeystore(ms, "key"),
	})
	assert.NotNil(t, am)

	var err error
	var tenantToken string
	var serverURL string
	err = am.RecvAuthResponse([]byte{}, tenantToken, serverURL)
	// should fail with empty response
	assert.Error(t, err)

	// make storage RO
	ms.ReadOnly(true)
	err = am.RecvAuthResponse([]byte("fooresp"), tenantToken, serverURL)
	assert.Error(t, err)

	ms.ReadOnly(false)
	err = am.RecvAuthResponse([]byte("fooresp"), tenantToken, serverURL)
	assert.NoError(t, err)
	tokdata, err := ms.ReadAll(datastore.AuthTokenName)
	assert.NoError(t, err)
	assert.Equal(t, []byte("fooresp"), tokdata)
	assert.True(t, am.IsAuthorized())
}
