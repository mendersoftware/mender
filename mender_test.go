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
	"bytes"
	"errors"
	"io/ioutil"
	"os"
	"path"
	"syscall"
	"testing"
	"time"

	"github.com/mendersoftware/artifacts/parser"
	atutils "github.com/mendersoftware/artifacts/test_utils"
	"github.com/mendersoftware/artifacts/writer"
	"github.com/mendersoftware/mender/client"
	cltest "github.com/mendersoftware/mender/client/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type testMenderPieces struct {
	MenderPieces
}

func Test_getImageId_noImageIdInFile_returnsEmptyId(t *testing.T) {
	mender := newDefaultTestMender()

	manifestFile, _ := os.Create("manifest")
	defer os.Remove("manifest")

	fileContent := "dummy_data"
	manifestFile.WriteString(fileContent)
	// rewind to the beginning of file
	//manifestFile.Seek(0, 0)

	mender.manifestFile = "manifest"

	assert.Equal(t, "", mender.GetCurrentImageID())
}

func Test_getImageId_malformedImageIdLine_returnsEmptyId(t *testing.T) {
	mender := newDefaultTestMender()

	manifestFile, _ := os.Create("manifest")
	defer os.Remove("manifest")

	fileContent := "IMAGE_ID"
	manifestFile.WriteString(fileContent)
	// rewind to the beginning of file
	//manifestFile.Seek(0, 0)

	mender.manifestFile = "manifest"

	assert.Equal(t, "", mender.GetCurrentImageID())
}

func Test_getImageId_haveImageId_returnsId(t *testing.T) {
	mender := newDefaultTestMender()

	manifestFile, _ := os.Create("manifest")
	defer os.Remove("manifest")

	fileContent := "IMAGE_ID=mender-image"
	manifestFile.WriteString(fileContent)
	mender.manifestFile = "manifest"

	assert.Equal(t, "mender-image", mender.GetCurrentImageID())
}

func newTestMender(runner *testOSCalls, config menderConfig, pieces testMenderPieces) *mender {
	// fill out missing pieces

	if pieces.store == nil {
		pieces.store = NewMemStore()
	}

	if pieces.device == nil {
		pieces.device = &fakeDevice{}
	}

	if pieces.authMgr == nil {
		if config.DeviceKey == "" {
			config.DeviceKey = "devkey"
		}

		ks := NewKeystore(pieces.store, config.DeviceKey)

		cmdr := newTestOSCalls("mac=foobar", 0)
		pieces.authMgr = NewAuthManager(AuthManagerConfig{
			AuthDataStore: pieces.store,
			KeyStore:      ks,
			IdentitySource: &IdentityDataRunner{
				cmdr: &cmdr,
			},
		})
	}

	mender, _ := NewMender(config, pieces.MenderPieces)
	return mender
}

func newDefaultTestMender() *mender {
	return newTestMender(nil, menderConfig{}, testMenderPieces{})
}

func Test_ForceBootstrap(t *testing.T) {
	// generate valid keys
	ms := NewMemStore()
	mender := newTestMender(nil,
		menderConfig{
			DeviceKey: "temp.key",
		},
		testMenderPieces{
			MenderPieces: MenderPieces{
				store: ms,
			},
		},
	)

	merr := mender.Bootstrap()
	assert.NoError(t, merr)

	kdataold, err := ms.ReadAll("temp.key")
	assert.NoError(t, err)
	assert.NotEmpty(t, kdataold)

	mender.ForceBootstrap()

	assert.True(t, mender.needsBootstrap())

	merr = mender.Bootstrap()
	assert.NoError(t, merr)

	// bootstrap should have generated a new key
	kdatanew, err := ms.ReadAll("temp.key")
	assert.NoError(t, err)
	assert.NotEmpty(t, kdatanew)
	// we should have a new key
	assert.NotEqual(t, kdatanew, kdataold)
}

func Test_Bootstrap(t *testing.T) {
	mender := newTestMender(nil,
		menderConfig{
			DeviceKey: "temp.key",
		},
		testMenderPieces{},
	)

	assert.True(t, mender.needsBootstrap())

	assert.NoError(t, mender.Bootstrap())

	mam, _ := mender.authMgr.(*MenderAuthManager)
	k := NewKeystore(mam.store, "temp.key")
	assert.NotNil(t, k)
	assert.NoError(t, k.Load())
}

func Test_BootstrappedHaveKeys(t *testing.T) {

	// generate valid keys
	ms := NewMemStore()
	k := NewKeystore(ms, "temp.key")
	assert.NotNil(t, k)
	assert.NoError(t, k.Generate())
	assert.NoError(t, k.Save())

	mender := newTestMender(nil,
		menderConfig{
			DeviceKey: "temp.key",
		},
		testMenderPieces{
			MenderPieces: MenderPieces{
				store: ms,
			},
		},
	)
	assert.NotNil(t, mender)
	mam, _ := mender.authMgr.(*MenderAuthManager)
	assert.Equal(t, ms, mam.keyStore.store)
	assert.NotNil(t, mam.keyStore.private)

	// subsequen bootstrap should not fail
	assert.NoError(t, mender.Bootstrap())
}

func Test_BootstrapError(t *testing.T) {

	ms := NewMemStore()

	ms.Disable(true)

	var mender *mender
	mender = newTestMender(nil, menderConfig{}, testMenderPieces{
		MenderPieces: MenderPieces{
			store: ms,
		},
	})
	// store is disabled, attempts to load keys when creating authMgr should have
	// failed
	assert.Nil(t, mender.authMgr)

	ms.Disable(false)
	mender = newTestMender(nil, menderConfig{}, testMenderPieces{
		MenderPieces: MenderPieces{
			store: ms,
		},
	})
	assert.NotNil(t, mender.authMgr)

	ms.ReadOnly(true)

	err := mender.Bootstrap()
	assert.Error(t, err)
}

func Test_CheckUpdateSimple(t *testing.T) {
	// create temp dir
	td, _ := ioutil.TempDir("", "mender-install-update-")
	defer os.RemoveAll(td)

	// prepare fake manifest file, with bogus
	manifest := path.Join(td, "manifest")

	var mender *mender

	mender = newTestMender(nil, menderConfig{
		ServerURL: "bogusurl",
	}, testMenderPieces{})

	up, err := mender.CheckUpdate()
	assert.Error(t, err)
	assert.Nil(t, up)

	srv := cltest.NewClientTestServer()
	defer srv.Close()

	srv.Update.Has = true

	mender = newTestMender(nil,
		menderConfig{
			ServerURL: srv.URL,
		},
		testMenderPieces{})
	mender.manifestFile = manifest

	ioutil.WriteFile(manifest, []byte("IMAGE_ID=fake-id"), 0600)

	currID := mender.GetCurrentImageID()
	assert.Equal(t, "fake-id", currID)
	// make image ID same as current, will result in no updates being available
	srv.Update.Data.Image.YoctoID = currID

	up, err = mender.CheckUpdate()
	assert.Equal(t, err, NewTransientError(os.ErrExist))
	assert.NotNil(t, up)

	// make image ID different from current
	srv.Update.Data.Image.YoctoID = currID + "-fake"
	srv.Update.Has = true
	up, err = mender.CheckUpdate()
	assert.NoError(t, err)
	assert.NotNil(t, up)
	assert.Equal(t, *up, srv.Update.Data)

	// pretend that we got 204 No Content from the server, i.e empty response body
	srv.Update.Has = false
	up, err = mender.CheckUpdate()
	assert.NoError(t, err)
	assert.Nil(t, up)
}

func TestMenderHasUpgrade(t *testing.T) {
	mender := newTestMender(nil, menderConfig{}, testMenderPieces{
		MenderPieces: MenderPieces{
			device: &fakeDevice{
				retHasUpdate: true,
			},
		},
	})

	h, err := mender.HasUpgrade()
	assert.NoError(t, err)
	assert.True(t, h)

	mender = newTestMender(nil, menderConfig{}, testMenderPieces{
		MenderPieces: MenderPieces{
			device: &fakeDevice{
				retHasUpdate: false,
			},
		},
	})

	h, err = mender.HasUpgrade()
	assert.NoError(t, err)
	assert.False(t, h)

	mender = newTestMender(nil, menderConfig{}, testMenderPieces{
		MenderPieces: MenderPieces{
			device: &fakeDevice{
				retHasUpdateError: errors.New("failed"),
			},
		},
	})
	h, err = mender.HasUpgrade()
	assert.Error(t, err)
}

func TestMenderGetPollInterval(t *testing.T) {
	mender := newTestMender(nil, menderConfig{
		PollIntervalSeconds: 20,
	}, testMenderPieces{})

	intvl := mender.GetUpdatePollInterval()
	assert.Equal(t, time.Duration(20)*time.Second, intvl)
}

type testAuthDataMessenger struct {
	reqData  []byte
	sigData  []byte
	code     client.AuthToken
	reqError error
	rspError error
	rspData  []byte
}

func (t *testAuthDataMessenger) MakeAuthRequest() (*client.AuthRequest, error) {
	return &client.AuthRequest{
		t.reqData,
		t.code,
		t.sigData,
	}, t.reqError
}

func (t *testAuthDataMessenger) RecvAuthResponse(data []byte) error {
	t.rspData = data
	return t.rspError
}

type testAuthManager struct {
	authorized     bool
	authtoken      client.AuthToken
	authtokenErr   error
	haskey         bool
	generatekeyErr error
	testAuthDataMessenger
}

func (a *testAuthManager) IsAuthorized() bool {
	return a.authorized
}

func (a *testAuthManager) AuthToken() (client.AuthToken, error) {
	return a.authtoken, a.authtokenErr
}

func (a *testAuthManager) HasKey() bool {
	return a.haskey
}

func (a *testAuthManager) GenerateKey() error {
	return a.generatekeyErr
}

func (a *testAuthManager) RemoveAuthToken() error {
	return nil
}

func TestMenderAuthorize(t *testing.T) {
	runner := newTestOSCalls("", -1)

	rspdata := []byte("foobar")

	atok := client.AuthToken("authorized")
	authMgr := &testAuthManager{
		authorized: true,
		authtoken:  atok,
	}

	srv := cltest.NewClientTestServer()
	defer srv.Close()

	mender := newTestMender(&runner,
		menderConfig{
			ServerURL: srv.URL,
		},
		testMenderPieces{
			MenderPieces: MenderPieces{
				authMgr: authMgr,
			},
		})
	// we should start with no token
	assert.Equal(t, noAuthToken, mender.authToken)

	// 1. client already authorized
	err := mender.Authorize()
	assert.NoError(t, err)
	// no need to build send request if auth data is valid
	assert.False(t, srv.Auth.Called)
	assert.Equal(t, atok, mender.authToken)

	// 2. pretend caching of authorization code fails
	authMgr.authtokenErr = errors.New("auth code load failed")
	mender.authToken = noAuthToken
	err = mender.Authorize()
	assert.Error(t, err)
	// no need to build send request if auth data is valid
	assert.False(t, srv.Auth.Called)
	assert.Equal(t, noAuthToken, mender.authToken)
	authMgr.authtokenErr = nil

	// 3. call the server, server denies authorization
	authMgr.authorized = false
	err = mender.Authorize()
	assert.Error(t, err)
	assert.False(t, err.IsFatal())
	assert.True(t, srv.Auth.Called)
	assert.Equal(t, noAuthToken, mender.authToken)

	// 4. pretend authorization manager fails to parse response
	srv.Auth.Called = false
	authMgr.testAuthDataMessenger.rspError = errors.New("response parse error")
	// we need the server authorize the client
	srv.Auth.Authorize = true
	srv.Auth.Token = rspdata
	err = mender.Authorize()
	assert.Error(t, err)
	assert.False(t, err.IsFatal())
	assert.True(t, srv.Auth.Called)
	// response data should be passed verbatim to AuthDataMessenger interface
	assert.Equal(t, rspdata, authMgr.testAuthDataMessenger.rspData)

	// 5. authorization manger throws no errors, server authorizes the client
	srv.Auth.Called = false
	authMgr.testAuthDataMessenger.rspError = nil
	// server will authorize the client
	srv.Auth.Authorize = true
	srv.Auth.Token = rspdata
	err = mender.Authorize()
	// all good
	assert.NoError(t, err)
	// Authorize() should have reloaded the cache (token comes from mock
	// auth manager)
	assert.Equal(t, atok, mender.authToken)
}

func TestMenderReportStatus(t *testing.T) {
	srv := cltest.NewClientTestServer()
	defer srv.Close()

	ms := NewMemStore()
	mender := newTestMender(nil,
		menderConfig{
			ServerURL: srv.URL,
		},
		testMenderPieces{
			MenderPieces: MenderPieces{
				store: ms,
			},
		},
	)

	ms.WriteAll(authTokenName, []byte("tokendata"))

	err := mender.Authorize()
	assert.NoError(t, err)

	srv.Auth.Verify = true
	srv.Auth.Token = []byte("tokendata")

	// 1. successful report
	err = mender.ReportUpdateStatus(
		client.UpdateResponse{
			ID: "foobar",
		},
		client.StatusSuccess,
	)
	assert.Nil(t, err)
	assert.Equal(t, client.StatusSuccess, srv.Status.Status)

	// 2. pretend authorization fails, server expects a different token
	srv.Reset()
	srv.Auth.Token = []byte("footoken")
	srv.Auth.Verify = true
	err = mender.ReportUpdateStatus(
		client.UpdateResponse{
			ID: "foobar",
		},
		client.StatusSuccess,
	)
	assert.NotNil(t, err)
}

func TestMenderLogUpload(t *testing.T) {
	srv := cltest.NewClientTestServer()
	defer srv.Close()

	ms := NewMemStore()
	mender := newTestMender(nil,
		menderConfig{
			ServerURL: srv.URL,
		},
		testMenderPieces{
			MenderPieces: MenderPieces{
				store: ms,
			},
		},
	)

	ms.WriteAll(authTokenName, []byte("tokendata"))

	err := mender.Authorize()
	assert.NoError(t, err)

	srv.Auth.Verify = true
	srv.Auth.Token = []byte("tokendata")

	// 1. log upload successful
	logs := []byte(`{ "messages":
[{ "time": "12:12:12", "level": "error", "msg": "log foo" },
{ "time": "12:12:13", "level": "debug", "msg": "log bar" }]
}`)

	err = mender.UploadLog(
		client.UpdateResponse{
			ID: "foobar",
		},
		logs,
	)
	assert.Nil(t, err)
	assert.JSONEq(t, `{
	  "messages": [
	      {
	          "time": "12:12:12",
	          "level": "error",
	          "msg": "log foo"
	      },
	      {
	          "time": "12:12:13",
	          "level": "debug",
	          "msg": "log bar"
	      }
	   ]}`, string(srv.Log.Logs))

	// 2. pretend authorization fails, server expects a different token
	srv.Auth.Token = []byte("footoken")
	err = mender.UploadLog(
		client.UpdateResponse{
			ID: "foobar",
		},
		logs,
	)
	assert.NotNil(t, err)
}

func TestMenderStateName(t *testing.T) {
	m := MenderStateInit
	assert.Equal(t, "init", m.String())

	m = MenderState(333)
	assert.Equal(t, "unknown (333)", m.String())
}

func TestAuthToken(t *testing.T) {
	ts := cltest.NewClientTestServer()
	defer ts.Close()

	ms := NewMemStore()
	mender := newTestMender(nil,
		menderConfig{
			ServerURL: ts.URL,
		},
		testMenderPieces{
			MenderPieces: MenderPieces{
				store: ms,
			},
		},
	)
	ms.WriteAll(authTokenName, []byte("tokendata"))
	token, err := ms.ReadAll(authTokenName)
	assert.NoError(t, err)
	assert.Equal(t, []byte("tokendata"), token)

	ts.Update.Unauthorized = true

	_, updErr := mender.CheckUpdate()
	assert.EqualError(t, updErr.Cause(), client.ErrNotAuthorized.Error())

	token, err = ms.ReadAll(authTokenName)
	assert.Equal(t, os.ErrNotExist, err)
	assert.Empty(t, token)
}

func TestMenderInventoryRefresh(t *testing.T) {
	// create temp dir
	td, _ := ioutil.TempDir("", "mender-install-update-")
	defer os.RemoveAll(td)

	// prepare fake manifest file, it is read when submitting inventory to
	// fill some default fields (device_type, image_id)
	manifest := path.Join(td, "manifest")
	ioutil.WriteFile(manifest, []byte("IMAGE_ID=fake-id\nDEVICE_TYPE=foo-bar"), 0600)

	srv := cltest.NewClientTestServer()
	defer srv.Close()

	ms := NewMemStore()
	mender := newTestMender(nil,
		menderConfig{
			ServerURL: srv.URL,
		},
		testMenderPieces{
			MenderPieces: MenderPieces{
				store: ms,
			},
		},
	)
	mender.manifestFile = manifest

	ms.WriteAll(authTokenName, []byte("tokendata"))

	merr := mender.Authorize()
	assert.NoError(t, merr)

	// prepare fake inventory scripts
	// 1. setup a temporary path $TMPDIR/mendertest<random>/inventory
	tdir, err := ioutil.TempDir("", "mendertest")
	assert.NoError(t, err)
	invpath := path.Join(tdir, "inventory")
	err = os.MkdirAll(invpath, os.FileMode(syscall.S_IRWXU))
	assert.NoError(t, err)
	defer os.RemoveAll(tdir)

	oldDefaultPathDataDir := defaultPathDataDir
	// override datadir path for subsequent getDataDirPath() calls
	defaultPathDataDir = tdir

	// 1a. no scripts hence no inventory data, submit should have been
	// called with default inventory attributes only
	srv.Auth.Verify = true
	srv.Auth.Token = []byte("tokendata")
	err = mender.InventoryRefresh()
	assert.Nil(t, err)

	assert.True(t, srv.Inventory.Called)
	exp := []client.InventoryAttribute{
		{"device_type", "foo-bar"},
		{"image_id", "fake-id"},
		{"client_version", "unknown"},
	}
	for _, a := range exp {
		assert.Contains(t, srv.Inventory.Attrs, a)
	}

	// 2. fake inventory script
	err = ioutil.WriteFile(path.Join(invpath, "mender-inventory-foo"),
		[]byte(`#!/bin/sh
echo foo=bar`),
		os.FileMode(syscall.S_IRWXU))
	assert.NoError(t, err)

	srv.Reset()
	srv.Auth.Verify = true
	srv.Auth.Token = []byte("tokendata")
	err = mender.InventoryRefresh()
	assert.Nil(t, err)
	exp = []client.InventoryAttribute{
		{"device_type", "foo-bar"},
		{"image_id", "fake-id"},
		{"client_version", "unknown"},
		{"foo", "bar"},
	}
	for _, a := range exp {
		assert.Contains(t, srv.Inventory.Attrs, a)
	}

	// 3. pretend client is no longer authorized
	srv.Auth.Token = []byte("footoken")
	err = mender.InventoryRefresh()
	assert.NotNil(t, err)

	// restore old datadir path
	defaultPathDataDir = oldDefaultPathDataDir
}

func makeFakeUpdate(t *testing.T, root string, valid bool) (string, error) {

	var dirStructOK = []atutils.TestDirEntry{
		{Path: "0000", IsDir: true},
		{Path: "0000/data", IsDir: true},
		{Path: "0000/data/update.ext4", Content: []byte("my first update")},
		{Path: "0000/type-info",
			Content: []byte(`{"type": "rootfs-image"}`),
		},
		{Path: "0000/meta-data",
			Content: []byte(`{"DeviceType": "vexpress-qemu", "ImageID": "core-image-minimal-201608110900"}`),
		},
		{Path: "0000/signatures", IsDir: true},
		{Path: "0000/signatures/update.sig"},
		{Path: "0000/scripts", IsDir: true},
		{Path: "0000/scripts/pre", IsDir: true},
		{Path: "0000/scripts/pre/my_script", Content: []byte("my first script")},
		{Path: "0000/scripts/post", IsDir: true},
		{Path: "0000/scripts/check", IsDir: true},
	}

	err := atutils.MakeFakeUpdateDir(root, dirStructOK)
	assert.NoError(t, err)

	aw := awriter.NewWriter("mender", 1)
	defer aw.Close()

	rp := &parser.RootfsParser{}
	aw.Register(rp)

	upath := path.Join(root, "update.tar")
	err = aw.Write(root, upath)
	assert.NoError(t, err)

	return upath, nil
}

type mockReader struct {
	mock.Mock
}

func (m *mockReader) Read(p []byte) (int, error) {
	ret := m.Called()
	return ret.Get(0).(int), ret.Error(1)
}

func TestMenderInstallUpdate(t *testing.T) {
	// create temp dir
	td, _ := ioutil.TempDir("", "mender-install-update-")
	defer os.RemoveAll(td)

	// prepare fake manifest file, with bogus
	manifest := path.Join(td, "manifest")

	mender := newTestMender(nil, menderConfig{},
		testMenderPieces{
			MenderPieces: MenderPieces{
				device: &fakeDevice{consumeUpdate: true},
			},
		},
	)
	mender.manifestFile = manifest

	// try some failure scenarios first

	// EOF
	err := mender.InstallUpdate(ioutil.NopCloser(&bytes.Buffer{}), 0)
	assert.Error(t, err)
	t.Logf("error: %v", err)

	// some error from reader
	mr := mockReader{}
	mr.On("Read").Return(0, errors.New("failed"))
	err = mender.InstallUpdate(ioutil.NopCloser(&mr), 0)
	assert.Error(t, err)
	t.Logf("error: %v", err)

	// make a fake update artifact
	upath, err := makeFakeUpdate(t, path.Join(td, "update-root"), true)
	t.Logf("temp update dir: %v update artifact: %v", td, upath)

	// open archive file
	f, err := os.Open(upath)
	defer f.Close()

	assert.NoError(t, err)
	assert.NotNil(t, f)
	// setup soem bogus DEVICE_TYPE so that we don't match the update
	ioutil.WriteFile(manifest, []byte("DEVICE_TYPE=bogusdevicetype\n"), 0644)
	err = mender.InstallUpdate(f, 0)
	assert.Error(t, err)
	f.Seek(0, 0)

	// try with a legit DEVICE_TYPE
	ioutil.WriteFile(manifest, []byte("DEVICE_TYPE=vexpress-qemu\n"), 0644)
	err = mender.InstallUpdate(f, 0)
	assert.NoError(t, err)
	f.Seek(0, 0)

	// now try with device throwing errors durin ginstall
	mender = newTestMender(nil, menderConfig{},
		testMenderPieces{
			MenderPieces: MenderPieces{
				device: &fakeDevice{retInstallUpdate: errors.New("failed")},
			},
		},
	)
	mender.manifestFile = manifest
	err = mender.InstallUpdate(f, 0)
	assert.Error(t, err)

}
