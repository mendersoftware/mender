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
	"bytes"
	"crypto/rand"
	"io"
	"io/ioutil"
	"os"
	"path"
	"syscall"
	"testing"
	"time"

	"github.com/mendersoftware/mender-artifact/artifact"
	"github.com/mendersoftware/mender-artifact/awriter"
	"github.com/mendersoftware/mender-artifact/handlers"
	"github.com/mendersoftware/mender/client"
	cltest "github.com/mendersoftware/mender/client/test"
	"github.com/mendersoftware/mender/conf"
	"github.com/mendersoftware/mender/datastore"
	dev "github.com/mendersoftware/mender/device"
	"github.com/mendersoftware/mender/store"
	stest "github.com/mendersoftware/mender/system/testing"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type testMenderPieces struct {
	MenderPieces
}

func Test_getArtifactName_noArtifactNameInFile_returnsEmptyName(t *testing.T) {
	mender := newDefaultTestMender()

	artifactInfoFile, _ := os.Create("artifact_info")
	defer os.Remove("artifact_info")

	fileContent := "#dummy comment"
	artifactInfoFile.WriteString(fileContent)
	// rewind to the beginning of file
	//artifactInfoFile.Seek(0, 0)

	mender.ArtifactInfoFile = "artifact_info"

	artName, err := mender.GetCurrentArtifactName()
	assert.NoError(t, err)
	assert.Equal(t, "", artName)
}

func Test_getArtifactName_malformedArtifactNameLine_returnsError(t *testing.T) {
	mender := newDefaultTestMender()

	artifactInfoFile, _ := os.Create("artifact_info")
	defer os.Remove("artifact_info")

	fileContent := "artifact_name"
	artifactInfoFile.WriteString(fileContent)
	// rewind to the beginning of file
	//artifactInfoFile.Seek(0, 0)

	mender.ArtifactInfoFile = "artifact_info"

	artName, err := mender.GetCurrentArtifactName()
	assert.Error(t, err)
	assert.Equal(t, "", artName)
}

func Test_getArtifactName_haveArtifactName_returnsName(t *testing.T) {
	mender := newDefaultTestMender()

	artifactInfoFile, _ := os.Create("artifact_info")
	defer os.Remove("artifact_info")

	fileContent := "artifact_name=mender-image"
	artifactInfoFile.WriteString(fileContent)
	mender.ArtifactInfoFile = "artifact_info"

	artName, err := mender.GetCurrentArtifactName()
	assert.NoError(t, err)
	assert.Equal(t, "mender-image", artName)
}

func newTestMender(_ *stest.TestOSCalls, config conf.MenderConfig,
	pieces testMenderPieces) *Mender {
	// fill out missing pieces

	if pieces.Store == nil {
		pieces.Store = store.NewMemStore()
	}

	if pieces.DualRootfsDevice == nil {
		pieces.DualRootfsDevice = &FakeDevice{}
	}

	if pieces.AuthMgr == nil {

		ks := store.NewKeystore(pieces.Store, conf.DefaultKeyFile)

		cmdr := stest.NewTestOSCalls("mac=foobar", 0)
		pieces.AuthMgr = NewAuthManager(AuthManagerConfig{
			AuthDataStore: pieces.Store,
			KeyStore:      ks,
			IdentitySource: &dev.IdentityDataRunner{
				Cmdr: cmdr,
			},
		})
	}

	mender, _ := NewMender(&config, pieces.MenderPieces)
	mender.StateScriptPath = ""

	return mender
}

func newDefaultTestMender() *Mender {
	return newTestMender(nil, conf.MenderConfig{
		MenderConfigFromFile: conf.MenderConfigFromFile{
			Servers: []client.MenderServer{{}},
		},
	}, testMenderPieces{})
}

func Test_ForceBootstrap(t *testing.T) {
	// generate valid keys
	ms := store.NewMemStore()
	mender := newTestMender(nil,
		conf.MenderConfig{},
		testMenderPieces{
			MenderPieces: MenderPieces{
				Store: ms,
			},
		},
	)

	merr := mender.Bootstrap()
	assert.NoError(t, merr)

	kdataold, err := ms.ReadAll(conf.DefaultKeyFile)
	assert.NoError(t, err)
	assert.NotEmpty(t, kdataold)

	mender.ForceBootstrap()

	assert.True(t, mender.needsBootstrap())

	merr = mender.Bootstrap()
	assert.NoError(t, merr)

	// bootstrap should have generated a new key
	kdatanew, err := ms.ReadAll(conf.DefaultKeyFile)
	assert.NoError(t, err)
	assert.NotEmpty(t, kdatanew)
	// we should have a new key
	assert.NotEqual(t, kdatanew, kdataold)
}

func Test_Bootstrap(t *testing.T) {
	mender := newTestMender(nil,
		conf.MenderConfig{},
		testMenderPieces{},
	)

	assert.True(t, mender.needsBootstrap())

	assert.NoError(t, mender.Bootstrap())

	mam, _ := mender.authMgr.(*MenderAuthManager)
	k := store.NewKeystore(mam.store, conf.DefaultKeyFile)
	assert.NotNil(t, k)
	assert.NoError(t, k.Load())
}

func Test_BootstrappedHaveKeys(t *testing.T) {

	// generate valid keys
	ms := store.NewMemStore()
	k := store.NewKeystore(ms, conf.DefaultKeyFile)
	assert.NotNil(t, k)
	assert.NoError(t, k.Generate())
	assert.NoError(t, k.Save())

	mender := newTestMender(nil,
		conf.MenderConfig{},
		testMenderPieces{
			MenderPieces: MenderPieces{
				Store: ms,
			},
		},
	)
	assert.NotNil(t, mender)
	mam, _ := mender.authMgr.(*MenderAuthManager)
	assert.Equal(t, ms, mam.keyStore.GetStore())
	assert.NotNil(t, mam.keyStore.GetPrivateKey())

	// subsequen bootstrap should not fail
	assert.NoError(t, mender.Bootstrap())
}

func Test_BootstrapError(t *testing.T) {

	ms := store.NewMemStore()

	ms.Disable(true)

	var mender *Mender
	mender = newTestMender(nil, conf.MenderConfig{}, testMenderPieces{
		MenderPieces: MenderPieces{
			Store: ms,
		},
	})
	// store is disabled, attempts to load keys when creating authMgr should have
	// failed, resulting in empty keys.
	assert.False(t, mender.authMgr.HasKey())

	ms.Disable(false)
	mender = newTestMender(nil, conf.MenderConfig{}, testMenderPieces{
		MenderPieces: MenderPieces{
			Store: ms,
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

	// prepare fake artifactInfo file
	artifactInfo := path.Join(td, "artifact_info")
	// prepare fake device type file
	deviceType := path.Join(td, "device_type")

	var mender *Mender

	mender = newTestMender(nil, conf.MenderConfig{
		MenderConfigFromFile: conf.MenderConfigFromFile{
			Servers: []client.MenderServer{{ServerURL: "bogusurl"}},
		},
	}, testMenderPieces{})

	up, err := mender.CheckUpdate()
	assert.Error(t, err)
	assert.Nil(t, up)

	srv := cltest.NewClientTestServer()
	defer srv.Close()

	srv.Update.Has = true

	mender = newTestMender(nil,
		conf.MenderConfig{
			MenderConfigFromFile: conf.MenderConfigFromFile{
				Servers: []client.MenderServer{{ServerURL: srv.URL}},
			},
		},
		testMenderPieces{})
	mender.ArtifactInfoFile = artifactInfo
	mender.DeviceTypeFile = deviceType

	srv.Update.Current = &client.CurrentUpdate{
		Artifact:   "fake-id",
		DeviceType: "hammer",
	}

	// test server expects current update information, request should fail
	_, err = mender.CheckUpdate()
	assert.Error(t, err)
	assert.Nil(t, nil)

	// NOTE: manifest file data must match current update information expected by
	// the server
	ioutil.WriteFile(artifactInfo, []byte("artifact_name=fake-id\nDEVICE_TYPE=hammer"), 0600)
	ioutil.WriteFile(deviceType, []byte("device_type=hammer"), 0600)

	currID, sErr := mender.GetCurrentArtifactName()
	assert.NoError(t, sErr)
	assert.Equal(t, "fake-id", currID)
	// make artifact name same as current, will result in no updates being available
	srv.Update.Data.Artifact.ArtifactName = currID

	up, err = mender.CheckUpdate()
	assert.Equal(t, err, NewTransientError(os.ErrExist))
	assert.NotNil(t, up)

	// make artifact name different from current
	srv.Update.Data.Artifact.ArtifactName = currID + "-fake"
	srv.Update.Has = true
	up, err = mender.CheckUpdate()
	assert.NoError(t, err)
	if assert.NotNil(t, up) {
		assert.Equal(t, *up, srv.Update.Data)
	}

	// pretend that we got 204 No Content from the server, i.e empty response body
	srv.Update.Has = false
	up, err = mender.CheckUpdate()
	assert.NoError(t, err)
	assert.Nil(t, up)
}

func TestMenderGetUpdatePollInterval(t *testing.T) {
	mender := newTestMender(nil, conf.MenderConfig{
		MenderConfigFromFile: conf.MenderConfigFromFile{
			UpdatePollIntervalSeconds: 20,
		},
	}, testMenderPieces{})

	intvl := mender.GetUpdatePollInterval()
	assert.Equal(t, time.Duration(20)*time.Second, intvl)
}

func TestMenderGetInventoryPollInterval(t *testing.T) {
	mender := newTestMender(nil, conf.MenderConfig{
		MenderConfigFromFile: conf.MenderConfigFromFile{
			InventoryPollIntervalSeconds: 10,
		},
	}, testMenderPieces{})

	intvl := mender.GetInventoryPollInterval()
	assert.Equal(t, time.Duration(10)*time.Second, intvl)
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
		Data:      t.reqData,
		Token:     t.code,
		Signature: t.sigData,
	}, t.reqError
}

func (t *testAuthDataMessenger) RecvAuthResponse(data []byte, tenantToken, serverURL string) error {
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
	runner := stest.NewTestOSCalls("", -1)

	rspdata := []byte("foobar")

	atok := client.AuthToken("authorized")
	authMgr := &testAuthManager{
		authorized: false,
		authtoken:  atok,
	}

	srv := cltest.NewClientTestServer()
	defer srv.Close()

	mender := newTestMender(runner,
		conf.MenderConfig{},
		testMenderPieces{
			MenderPieces: MenderPieces{
				AuthMgr: authMgr,
			},
		})
	// Should return empty server list error
	err := mender.Authorize()
	assert.Error(t, err)

	mender.Config.Servers = make([]client.MenderServer, 1)
	mender.Config.Servers[0].ServerURL = srv.URL
	authMgr.authorized = true

	err = mender.Authorize()
	assert.NoError(t, err)
	// we should initialize with valid token
	assert.Equal(t, atok, mender.authToken)

	// 1. client already authorized
	err = mender.Authorize()
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

	ms := store.NewMemStore()
	mender := newTestMender(nil,
		conf.MenderConfig{
			MenderConfigFromFile: conf.MenderConfigFromFile{
				Servers: []client.MenderServer{{ServerURL: srv.URL}},
			},
		},
		testMenderPieces{
			MenderPieces: MenderPieces{
				Store: ms,
			},
		},
	)

	ms.WriteAll(datastore.AuthTokenName, []byte("tokendata"))

	err := mender.Authorize()
	assert.NoError(t, err)

	srv.Auth.Verify = true
	srv.Auth.Token = []byte("tokendata")

	// 1. successful report
	err = mender.ReportUpdateStatus(
		&datastore.UpdateInfo{
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
		&datastore.UpdateInfo{
			ID: "foobar",
		},
		client.StatusSuccess,
	)
	assert.NotNil(t, err)
	assert.False(t, err.IsFatal())

	// 3. pretend that deployment was aborted
	srv.Reset()
	srv.Auth.Authorize = true
	srv.Auth.Token = []byte("tokendata")
	srv.Auth.Verify = true
	srv.Status.Aborted = true
	err = mender.ReportUpdateStatus(
		&datastore.UpdateInfo{
			ID: "foobar",
		},
		client.StatusSuccess,
	)
	assert.NotNil(t, err)
	assert.True(t, err.IsFatal())
}

func TestMenderLogUpload(t *testing.T) {
	srv := cltest.NewClientTestServer()
	defer srv.Close()

	ms := store.NewMemStore()
	mender := newTestMender(nil,
		conf.MenderConfig{
			MenderConfigFromFile: conf.MenderConfigFromFile{
				Servers: []client.MenderServer{{ServerURL: srv.URL}},
			},
		},
		testMenderPieces{
			MenderPieces: MenderPieces{
				Store: ms,
			},
		},
	)

	ms.WriteAll(datastore.AuthTokenName, []byte("tokendata"))

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
		&datastore.UpdateInfo{
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
		&datastore.UpdateInfo{
			ID: "foobar",
		},
		logs,
	)
	assert.NotNil(t, err)
}

func TestAuthToken(t *testing.T) {
	ts := cltest.NewClientTestServer()
	defer ts.Close()

	ms := store.NewMemStore()
	mender := newTestMender(nil,
		conf.MenderConfig{
			MenderConfigFromFile: conf.MenderConfigFromFile{
				Servers: []client.MenderServer{{ServerURL: ts.URL}},
			},
		},
		testMenderPieces{
			MenderPieces: MenderPieces{
				Store: ms,
			},
		},
	)
	ms.WriteAll(datastore.AuthTokenName, []byte("tokendata"))
	token, err := ms.ReadAll(datastore.AuthTokenName)
	assert.NoError(t, err)
	assert.Equal(t, []byte("tokendata"), token)

	ts.Update.Unauthorized = true
	ts.Update.Current = &client.CurrentUpdate{
		Artifact:   "fake-id",
		DeviceType: "foo-bar",
	}

	td, _ := ioutil.TempDir("", "mender-install-update-")
	defer os.RemoveAll(td)

	artifactInfo := path.Join(td, "artifact_info")
	ioutil.WriteFile(artifactInfo, []byte("artifact_name=fake-id"), 0600)
	deviceType := path.Join(td, "device_type")
	ioutil.WriteFile(deviceType, []byte("device_type=foo-bar"), 0600)
	mender.ArtifactInfoFile = artifactInfo
	mender.DeviceTypeFile = deviceType

	_, updErr := mender.CheckUpdate()
	assert.EqualError(t, errors.Cause(updErr), client.ErrNotAuthorized.Error())

	token, err = ms.ReadAll(datastore.AuthTokenName)
	assert.Equal(t, os.ErrNotExist, err)
	assert.Empty(t, token)
}

func TestMenderInventoryRefresh(t *testing.T) {
	// create temp dir
	td, _ := ioutil.TempDir("", "mender-install-update-")
	defer os.RemoveAll(td)

	// prepare fake artifactInfo file, it is read when submitting inventory to
	// fill some default fields (device_type, artifact_name)
	artifactInfo := path.Join(td, "artifact_info")
	ioutil.WriteFile(artifactInfo, []byte("artifact_name=fake-id"), 0600)
	deviceType := path.Join(td, "device_type")
	ioutil.WriteFile(deviceType, []byte("device_type=foo-bar"), 0600)

	srv := cltest.NewClientTestServer()
	defer srv.Close()

	ms := store.NewMemStore()
	mender := newTestMender(nil,
		conf.MenderConfig{
			MenderConfigFromFile: conf.MenderConfigFromFile{
				Servers: []client.MenderServer{{ServerURL: srv.URL}},
			},
		},
		testMenderPieces{
			MenderPieces: MenderPieces{
				Store: ms,
			},
		},
	)
	mender.ArtifactInfoFile = artifactInfo
	mender.DeviceTypeFile = deviceType

	ms.WriteAll(datastore.AuthTokenName, []byte("tokendata"))

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

	oldDefaultPathDataDir := conf.DefaultPathDataDir
	// override datadir path for subsequent getDataDirPath() calls
	conf.DefaultPathDataDir = tdir
	defer func() {
		// restore old datadir path
		conf.DefaultPathDataDir = oldDefaultPathDataDir
	}()

	// 1a. no scripts hence no inventory data, submit should have been
	// called with default inventory attributes only
	srv.Auth.Verify = true
	srv.Auth.Token = []byte("tokendata")
	err = mender.InventoryRefresh()
	assert.Nil(t, err)

	assert.True(t, srv.Inventory.Called)
	exp := []client.InventoryAttribute{
		{Name: "device_type", Value: "foo-bar"},
		{Name: "artifact_name", Value: "fake-id"},
		{Name: "mender_client_version", Value: "unknown"},
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
		{Name: "device_type", Value: "foo-bar"},
		{Name: "artifact_name", Value: "fake-id"},
		{Name: "mender_client_version", Value: "unknown"},
		{Name: "foo", Value: "bar"},
	}
	for _, a := range exp {
		assert.Contains(t, srv.Inventory.Attrs, a)
	}

	// no artifact name should error
	ioutil.WriteFile(artifactInfo, []byte(""), 0600)
	err = mender.InventoryRefresh()
	assert.Error(t, err)
	assert.EqualError(t, errors.Cause(err), errNoArtifactName.Error())

	// 3. pretend client is no longer authorized
	srv.Auth.Token = []byte("footoken")
	err = mender.InventoryRefresh()
	assert.NotNil(t, err)
}

func MakeFakeUpdate(data string) (string, error) {
	f, err := ioutil.TempFile("", "test_update")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if len(data) > 0 {
		if _, err := f.WriteString(data); err != nil {
			return "", err
		}
	}
	return f.Name(), nil
}

type rc struct {
	*bytes.Buffer
}

func (r *rc) Close() error {
	return nil
}

const (
	PublicRSAKey = `-----BEGIN PUBLIC KEY-----
MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQDSTLzZ9hQq3yBB+dMDVbKem6ia
v1J6opg6DICKkQ4M/yhlw32BCGm2ArM3VwQRgq6Q1sNSq953n5c1EO3Xcy/qTAKc
XwaUNml5EhW79AdibBXZiZt8fMhCjUd/4ce3rLNjnbIn1o9L6pzV4CcVJ8+iNhne
5vbA+63vRCnrc8QuYwIDAQAB
-----END PUBLIC KEY-----`
	PrivateRSAKey = `-----BEGIN RSA PRIVATE KEY-----
MIICXAIBAAKBgQDSTLzZ9hQq3yBB+dMDVbKem6iav1J6opg6DICKkQ4M/yhlw32B
CGm2ArM3VwQRgq6Q1sNSq953n5c1EO3Xcy/qTAKcXwaUNml5EhW79AdibBXZiZt8
fMhCjUd/4ce3rLNjnbIn1o9L6pzV4CcVJ8+iNhne5vbA+63vRCnrc8QuYwIDAQAB
AoGAQKIRELQOsrZsxZowfj/ia9jPUvAmO0apnn2lK/E07k2lbtFMS1H4m1XtGr8F
oxQU7rLyyP/FmeJUqJyRXLwsJzma13OpxkQtZmRpL9jEwevnunHYJfceVapQOJ7/
6Oz0pPWEq39GCn+tTMtgSmkEaSH8Ki9t32g9KuQIKBB2hbECQQDsg7D5fHQB1BXG
HJm9JmYYX0Yk6Z2SWBr4mLO0C4hHBnV5qPCLyevInmaCV2cOjDZ5Sz6iF5RK5mw7
qzvFa8ePAkEA46Anom3cNXO5pjfDmn2CoqUvMeyrJUFL5aU6W1S6iFprZ/YwdHcC
kS5yTngwVOmcnT65Vnycygn+tZan2A0h7QJBAJNlowZovDdjgEpeCqXp51irD6Dz
gsLwa6agK+Y6Ba0V5mJyma7UoT//D62NYOmdElnXPepwvXdMUQmCtpZbjBsCQD5H
VHDJlCV/yzyiJz9+tZ5giaAkO9NOoUBsy6GvdfXWn2prXmiPI0GrrpSvp7Gj1Tjk
r3rtT0ysHWd7l+Kx/SUCQGlitd5RDfdHl+gKrCwhNnRG7FzRLv5YOQV81+kh7SkU
73TXPIqLESVrqWKDfLwfsfEpV248MSRou+y0O1mtFpo=
-----END RSA PRIVATE KEY-----`
)

func MakeRootfsImageArtifact(version int, signed bool) (io.ReadCloser, error) {
	upd, err := MakeFakeUpdate("test update")
	if err != nil {
		return nil, err
	}
	defer os.Remove(upd)

	art := bytes.NewBuffer(nil)
	var aw *awriter.Writer
	comp := artifact.NewCompressorGzip()
	if !signed {
		aw = awriter.NewWriter(art, comp)
	} else {
		s := artifact.NewSigner([]byte(PrivateRSAKey))
		aw = awriter.NewWriterSigned(art, comp, s)
	}
	var u handlers.Composer
	switch version {
	case 1:
		return nil, errors.New("Artifact version 1 is deprecated")
	case 2:
		u = handlers.NewRootfsV2(upd)
	case 3:
		u = handlers.NewRootfsV3(upd)
	}

	updates := &awriter.Updates{Updates: []handlers.Composer{u}}
	err = aw.WriteArtifact(&awriter.WriteArtifactArgs{
		Format:  "mender",
		Version: version,
		Devices: []string{"vexpress-qemu"},
		Name:    "mender-1.1",
		Updates: updates,
		Scripts: nil,
		Provides: &artifact.ArtifactProvides{
			ArtifactName: "TestName",
		},
		Depends: &artifact.ArtifactDepends{
			CompatibleDevices: []string{"vexpress-qemu"},
		},
		TypeInfoV3: &artifact.TypeInfoV3{},
	})
	if err != nil {
		return nil, err
	}
	return &rc{art}, nil
}

type mockReader struct {
	mock.Mock
}

func (m *mockReader) Read(p []byte) (int, error) {
	ret := m.Called()
	return ret.Get(0).(int), ret.Error(1)
}

func TestMenderStoreUpdate(t *testing.T) {
	// create temp dir
	td, _ := ioutil.TempDir("", "mender-install-update-")
	defer os.RemoveAll(td)

	// prepare fake artifactInfo file, with bogus
	deviceType := path.Join(td, "device_type")

	mender := newTestMender(nil, conf.MenderConfig{},
		testMenderPieces{
			MenderPieces: MenderPieces{
				DualRootfsDevice: &FakeDevice{ConsumeUpdate: true},
			},
		},
	)
	mender.DeviceTypeFile = deviceType

	// try some failure scenarios first

	// EOF
	_, err := mender.ReadArtifactHeaders(ioutil.NopCloser(&bytes.Buffer{}))
	assert.Error(t, err)

	// some error from reader
	mr := mockReader{}
	mr.On("Read").Return(0, errors.New("failed"))
	_, err = mender.ReadArtifactHeaders(ioutil.NopCloser(&mr))
	assert.Error(t, err)

	// make a fake update artifact
	upd, err := MakeRootfsImageArtifact(2, false)
	assert.NoError(t, err)
	assert.NotNil(t, upd)

	// setup some bogus device_type so that we don't match the update
	ioutil.WriteFile(deviceType, []byte("device_type=bogusdevicetype\n"), 0644)
	_, err = mender.ReadArtifactHeaders(upd)
	assert.Error(t, err)

	// try with a legit device_type
	upd, err = MakeRootfsImageArtifact(2, false)
	assert.NoError(t, err)
	assert.NotNil(t, upd)

	ioutil.WriteFile(deviceType, []byte("device_type=vexpress-qemu\n"), 0644)
	installer, err := mender.ReadArtifactHeaders(upd)
	require.NoError(t, err)
	err = installer.StorePayloads()
	assert.NoError(t, err)

	// now try with device throwing errors during install
	upd, err = MakeRootfsImageArtifact(2, false)
	assert.NoError(t, err)
	assert.NotNil(t, upd)

	mender = newTestMender(nil, conf.MenderConfig{},
		testMenderPieces{
			MenderPieces: MenderPieces{
				DualRootfsDevice: &FakeDevice{RetStoreUpdate: errors.New("failed")},
			},
		},
	)
	mender.DeviceTypeFile = deviceType
	installer, err = mender.ReadArtifactHeaders(upd)
	require.NoError(t, err)
	err = installer.StorePayloads()
	assert.Error(t, err)

}

func TestMenderFetchUpdate(t *testing.T) {
	srv := cltest.NewClientTestServer()
	defer srv.Close()

	srv.Update.Has = true

	ms := store.NewMemStore()
	mender := newTestMender(nil,
		conf.MenderConfig{
			MenderConfigFromFile: conf.MenderConfigFromFile{
				ServerURL: srv.URL,
			},
		},
		testMenderPieces{
			MenderPieces: MenderPieces{
				Store: ms,
			},
		})

	ms.WriteAll(datastore.AuthTokenName, []byte("tokendata"))
	merr := mender.Authorize()
	assert.NoError(t, merr)

	// populate download data with random bytes
	rdata := bytes.Buffer{}
	rcount := 8192
	_, err := io.CopyN(&rdata, rand.Reader, int64(rcount))
	assert.NoError(t, err)
	assert.Equal(t, rcount, rdata.Len())
	rbytes := rdata.Bytes()

	_, err = io.Copy(&srv.UpdateDownload.Data, &rdata)
	assert.NoError(t, err)
	assert.Equal(t, rcount, len(rbytes))

	img, sz, err := mender.FetchUpdate(srv.URL + "/api/devices/v1/download")
	assert.NoError(t, err)
	assert.NotNil(t, img)
	assert.EqualValues(t, len(rbytes), sz)

	dl := bytes.Buffer{}
	_, err = io.Copy(&dl, img)
	assert.NoError(t, err)
	assert.EqualValues(t, sz, dl.Len())

	assert.True(t, bytes.Equal(rbytes, dl.Bytes()))
}

// TestReauthorization triggers the reauthorization mechanic when
// issuing an API request and getting a 401 response code.
// In this test we use check update as our reference API-request for
// convenience, but the behaviour is similar across all API-requests
// (excluding auth_request). The test then changes the authorization
// token on the server, causing the first checkUpdate to retry after
// doing reauthorization, which causes the server to serve the request
// the other time around. Lastly we force the server to only give
// unauthorized responses, causing the request to fail and an error
// returned.
func TestReauthorization(t *testing.T) {

	// Create temporary artifact_info / device_type files
	artifactInfoFile, _ := os.Create("artifact_info")
	devInfoFile, _ := os.Create("device_type")
	defer os.Remove("artifact_info")
	defer os.Remove("device_type")

	// add artifact- / device name
	artifactInfoFile.WriteString("artifact_name=mender-image")
	devInfoFile.WriteString("device_type=dev")

	// Create and configure server
	srv := cltest.NewClientTestServer()
	defer srv.Close()
	srv.Auth.Token = []byte(`foo`)
	srv.Auth.Authorize = true
	srv.Auth.Verify = true
	srv.Update.Current = &client.CurrentUpdate{
		Artifact:   "mender-image",
		DeviceType: "dev",
	}

	// make and configure a mender
	mender := newTestMender(nil,
		conf.MenderConfig{
			MenderConfigFromFile: conf.MenderConfigFromFile{
				Servers: []client.MenderServer{{ServerURL: srv.URL}},
			},
		},
		testMenderPieces{})
	mender.ArtifactInfoFile = "artifact_info"
	mender.DeviceTypeFile = "device_type"

	// Get server token
	err := mender.Authorize()
	assert.NoError(t, err)

	// Successful reauth: server changed token
	srv.Auth.Token = []byte(`bar`)
	_, err = mender.CheckUpdate()
	assert.NoError(t, err)

	// Trigger reauth error: force response Unauthorized when querying update
	srv.Auth.Token = []byte(`foo`)
	srv.Update.Unauthorized = true
	_, err = mender.CheckUpdate()
	assert.Error(t, err)
}

// TestFailbackServers tests the optional failover feature for which
// a client can swap server if current server stops serving.
//
// Add multiple servers into conf.MenderConfig, and let the first one "fail".
// 1.
// Make the first server fail by having inconsistent artifact- / device name.
// This should trigger a 400 response (bad request) if we issue a check for
// pending updates, and hence swap to the second server.
// 2.
// Make the second server fail by refusing to authorize client, where as the
// first server serves this authorization request.
func TestFailoverServers(t *testing.T) {

	// Create temporary artifact_info / device_type files
	artifactInfoFile, _ := os.Create("artifact_info")
	devInfoFile, _ := os.Create("device_type")
	defer os.Remove("artifact_info")
	defer os.Remove("device_type")

	artifactInfoFile.WriteString("artifact_name=mender-image")
	devInfoFile.WriteString("device_type=dev")

	// Create and configure servers
	srv1 := cltest.NewClientTestServer()
	srv2 := cltest.NewClientTestServer()
	defer srv1.Close()
	defer srv2.Close()
	// Give srv2 knowledge about client artifact- and device name
	srv2.Update.Current = &client.CurrentUpdate{
		Artifact:   "mender-image",
		DeviceType: "dev",
	}
	srv2.Update.Has = true
	srv2.Update.Data = datastore.UpdateInfo{
		ID: "foo",
	}
	// Create mender- and conf.MenderConfig structs
	srvrs := make([]client.MenderServer, 2)
	srvrs[0].ServerURL = srv1.URL
	srvrs[1].ServerURL = srv2.URL
	srv2.Auth.Token = []byte(`jwt`)
	srv2.Auth.Authorize = true
	mender := newTestMender(nil,
		conf.MenderConfig{
			MenderConfigFromFile: conf.MenderConfigFromFile{
				ServerURL: srv1.URL,
				Servers:   srvrs,
			},
		},
		testMenderPieces{})
	mender.ArtifactInfoFile = "artifact_info"
	mender.DeviceTypeFile = "device_type"

	// Client is not authorized for server 1.
	err := mender.Authorize()
	assert.NoError(t, err)
	assert.True(t, srv1.Auth.Called)
	assert.True(t, srv2.Auth.Called)

	// Check for update: causes srv1 to return bad request (400) and trigger failover.
	rsp, err := mender.CheckUpdate()
	assert.NoError(t, err)
	// Both callbacks called, but only one returns 200
	assert.True(t, srv1.Update.Called)
	assert.True(t, srv2.Update.Called)
	assert.NotNil(t, rsp)
	assert.Equal(t, rsp.ID, srv2.Update.Data.ID)
}
