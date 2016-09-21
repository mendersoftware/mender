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
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type testMenderPieces struct {
	MenderPieces
	updater Updater
	authReq AuthRequester
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

	if pieces.env == nil {
		if runner == nil {
			testrunner := newTestOSCalls("", -1)
			runner = &testrunner
		}
		pieces.env = &uBootEnv{runner}
	}

	if pieces.updater == nil {
		pieces.updater = &fakeUpdater{}
	}

	if pieces.device == nil {
		pieces.device = &fakeDevice{}
	}

	if pieces.authMgr == nil {
		if config.DeviceKey == "" {
			config.DeviceKey = "devkey"
		}

		cmdr := newTestOSCalls("mac=foobar", 0)
		pieces.authMgr = NewAuthManager(pieces.store, config.DeviceKey,
			&IdentityDataRunner{
				cmdr: &cmdr,
			})
	}

	if pieces.authReq == nil {
		pieces.authReq = &fakeAuthorizer{}
	}

	mender, _ := NewMender(config, pieces.MenderPieces)
	if mender != nil {
		mender.updater = pieces.updater
		mender.authReq = pieces.authReq
	}
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
	k := NewKeystore(mam.store)
	assert.NotNil(t, k)
	assert.NoError(t, k.Load("temp.key"))
}

func Test_BootstrappedHaveKeys(t *testing.T) {

	// generate valid keys
	ms := NewMemStore()
	k := NewKeystore(ms)
	assert.NotNil(t, k)
	assert.NoError(t, k.Generate())
	assert.NoError(t, k.Save("temp.key"))

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

	var mender *mender

	mender = newTestMender(nil, menderConfig{}, testMenderPieces{
		updater: &fakeUpdater{
			GetScheduledUpdateReturnError: errors.New("check failed"),
		},
	})
	up, err := mender.CheckUpdate()
	assert.Error(t, err)
	assert.Nil(t, up)

	update := UpdateResponse{}
	updaterIface := &fakeUpdater{
		GetScheduledUpdateReturnIface: update,
	}
	mender = newTestMender(nil, menderConfig{}, testMenderPieces{
		updater: updaterIface,
	})

	currID := mender.GetCurrentImageID()
	// make image ID same as current, will result in no updates being available
	update.Image.YoctoID = currID
	updaterIface.GetScheduledUpdateReturnIface = update
	up, err = mender.CheckUpdate()
	assert.Equal(t, err, NewTransientError(os.ErrExist))
	assert.NotNil(t, up)

	// pretend that we got 204 No Content from the server, i.e empty response body
	updaterIface.GetScheduledUpdateReturnIface = nil
	up, err = mender.CheckUpdate()
	assert.NoError(t, err)
	assert.Nil(t, up)

	// make image ID different from current
	update.Image.YoctoID = currID + "-fake"
	updaterIface.GetScheduledUpdateReturnIface = update
	up, err = mender.CheckUpdate()
	assert.NoError(t, err)
	assert.NotNil(t, up)
	assert.Equal(t, &update, up)
}

func TestMenderHasUpgrade(t *testing.T) {
	runner := newTestOSCalls("upgrade_available=1", 0)
	mender := newTestMender(&runner, menderConfig{}, testMenderPieces{})

	h, err := mender.HasUpgrade()
	assert.NoError(t, err)
	assert.True(t, h)

	runner = newTestOSCalls("upgrade_available=0", 0)
	mender = newTestMender(&runner, menderConfig{}, testMenderPieces{})

	h, err = mender.HasUpgrade()
	assert.NoError(t, err)
	assert.False(t, h)

	runner = newTestOSCalls("", -1)
	mender = newTestMender(&runner, menderConfig{}, testMenderPieces{})
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

type testAuthManager struct {
	authorized     bool
	authtoken      AuthToken
	authtokenErr   error
	haskey         bool
	generatekeyErr error
	testAuthDataMessenger
}

func (a *testAuthManager) IsAuthorized() bool {
	return a.authorized
}

func (a *testAuthManager) AuthToken() (AuthToken, error) {
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

	authReq := &fakeAuthorizer{
		rsp: rspdata,
	}

	atok := AuthToken("authorized")
	authMgr := &testAuthManager{
		authorized: true,
		authtoken:  atok,
	}

	mender := newTestMender(&runner,
		menderConfig{
			ServerURL: "localhost:2323",
		},
		testMenderPieces{
			MenderPieces: MenderPieces{
				authMgr: authMgr,
			},
			authReq: authReq,
		})

	assert.Equal(t, noAuthToken, mender.authToken)

	err := mender.Authorize()
	assert.NoError(t, err)
	// no need to build send request if auth data is valid
	assert.False(t, authReq.reqCalled)
	assert.Equal(t, atok, mender.authToken)

	// pretend caching of authorization code fails
	authMgr.authtokenErr = errors.New("auth code load failed")
	mender.authToken = noAuthToken
	err = mender.Authorize()
	assert.Error(t, err)
	// no need to build send request if auth data is valid
	assert.Equal(t, noAuthToken, mender.authToken)
	authMgr.authtokenErr = nil

	authReq.rspErr = errors.New("request error")
	authMgr.authorized = false
	err = mender.Authorize()
	assert.Error(t, err)
	assert.False(t, err.IsFatal())
	assert.True(t, authReq.reqCalled)
	assert.Equal(t, "localhost:2323", authReq.url)
	assert.Equal(t, noAuthToken, mender.authToken)

	// clear error
	authReq.rspErr = nil
	authMgr.testAuthDataMessenger.rspError = errors.New("response parse error")
	err = mender.Authorize()
	assert.Error(t, err)
	assert.False(t, err.IsFatal())
	// response data should be passed verbatim to AuthDataMessenger interface
	assert.Equal(t, rspdata, authMgr.testAuthDataMessenger.rspData)

	authMgr.testAuthDataMessenger.rspError = nil
	err = mender.Authorize()
	assert.NoError(t, err)
	// Authorize() should have reloaded the cache
	assert.Equal(t, atok, mender.authToken)
}

func TestMenderReportStatus(t *testing.T) {
	responder := &struct {
		httpStatus int
		recdata    []byte
		headers    http.Header
	}{
		http.StatusNoContent, // 204
		[]byte{},
		http.Header{},
	}

	// Test server that always responds with 200 code, and specific payload
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(responder.httpStatus)

		responder.recdata, _ = ioutil.ReadAll(r.Body)
		responder.headers = r.Header
	}))
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

	err := mender.Authorize()
	assert.NoError(t, err)

	err = mender.ReportUpdateStatus(
		UpdateResponse{
			ID: "foobar",
		},
		statusSuccess,
	)
	assert.Nil(t, err)
	assert.JSONEq(t, `{"status": "success"}`, string(responder.recdata))
	assert.Equal(t, "Bearer tokendata", responder.headers.Get("Authorization"))

	responder.httpStatus = 401
	err = mender.ReportUpdateStatus(
		UpdateResponse{
			ID: "foobar",
		},
		statusSuccess,
	)
	assert.NotNil(t, err)
}

func TestMenderLogUpload(t *testing.T) {
	responder := &struct {
		httpStatus int
		recdata    []byte
		headers    http.Header
	}{
		http.StatusNoContent, // 204
		[]byte{},
		http.Header{},
	}

	// Test server that always responds with 200 code, and specific payload
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(responder.httpStatus)

		responder.recdata, _ = ioutil.ReadAll(r.Body)
		responder.headers = r.Header
	}))
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

	err := mender.Authorize()
	assert.NoError(t, err)

	logs := []byte(`{ "messages":
[{ "time": "12:12:12", "level": "error", "msg": "log foo" },
{ "time": "12:12:13", "level": "debug", "msg": "log bar" }]
}`)

	err = mender.UploadLog(
		UpdateResponse{
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
	   ]}`, string(responder.recdata))
	assert.Equal(t, "Bearer tokendata", responder.headers.Get("Authorization"))

	responder.httpStatus = 401
	err = mender.UploadLog(
		UpdateResponse{
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

func TestMenderInventoryRefresh(t *testing.T) {
	responder := &struct {
		httpStatus int
		recdata    []byte
		headers    http.Header
	}{
		http.StatusOK, // 200
		[]byte{},
		http.Header{},
	}

	// Test server that always responds with 200 code, and specific payload
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(responder.httpStatus)

		responder.recdata, _ = ioutil.ReadAll(r.Body)
		responder.headers = r.Header
	}))
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

	// 1a. no scripts hence no inventory data, submit should not be run at all
	responder.recdata = nil
	err = mender.InventoryRefresh()
	assert.Nil(t, err)

	exp := []InventoryAttribute{
		{"device_type", ""},
		{"image_id", ""},
		{"client_version", "unknown"},
	}
	var attrs []InventoryAttribute
	json.Unmarshal(responder.recdata, &attrs)
	for _, a := range exp {
		assert.Contains(t, attrs, a)
	}
	t.Logf("data: %s", responder.recdata)

	// 2. fake inventory script
	err = ioutil.WriteFile(path.Join(invpath, "mender-inventory-foo"),
		[]byte(`#!/bin/sh
echo foo=bar`),
		os.FileMode(syscall.S_IRWXU))
	assert.NoError(t, err)

	err = mender.InventoryRefresh()
	assert.Nil(t, err)
	json.Unmarshal(responder.recdata, &attrs)
	exp = []InventoryAttribute{
		{"device_type", ""},
		{"image_id", ""},
		{"client_version", "unknown"},
		{"foo", "bar"},
	}
	for _, a := range exp {
		assert.Contains(t, attrs, a)
	}
	t.Logf("data: %s", responder.recdata)
	assert.Equal(t, "Bearer tokendata", responder.headers.Get("Authorization"))

	responder.httpStatus = 401
	err = mender.InventoryRefresh()
	assert.NotNil(t, err)

	// restore old datadir path
	defaultPathDataDir = oldDefaultPathDataDir
}
