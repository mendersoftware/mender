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
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"syscall"
	"testing"
	"time"

	"github.com/mendersoftware/mender-artifact/artifact"
	"github.com/mendersoftware/mender-artifact/awriter"
	"github.com/mendersoftware/mender-artifact/handlers"
	"github.com/mendersoftware/mender/app/updatecontrolmap"
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

const defaultKeyPassphrase = ""

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

func newTestMenderAndAuthManager(_ *stest.TestOSCalls, config conf.MenderConfig,
	pieces testMenderPieces) (*Mender, AuthManager) {
	// fill out missing pieces

	if pieces.Store == nil {
		pieces.Store = store.NewMemStore()
	}

	if pieces.DualRootfsDevice == nil {
		pieces.DualRootfsDevice = &FakeDevice{}
	}

	if pieces.AuthManager == nil {

		ks := store.NewKeystore(pieces.Store, conf.DefaultKeyFile, "", false, defaultKeyPassphrase)

		cmdr := stest.NewTestOSCalls("mac=foobar", 0)
		pieces.AuthManager = NewAuthManager(AuthManagerConfig{
			AuthDataStore: pieces.Store,
			KeyStore:      ks,
			IdentitySource: &dev.IdentityDataRunner{
				Cmdr: cmdr,
			},
			Config: &config,
		})
	}

	mender, _ := NewMender(&config, pieces.MenderPieces)
	mender.StateScriptPath = ""

	return mender, pieces.AuthManager
}

func newTestMender(dummy *stest.TestOSCalls, config conf.MenderConfig,
	pieces testMenderPieces) *Mender {
	mender, _ := newTestMenderAndAuthManager(dummy, config, pieces)
	return mender
}

func newDefaultTestMender() *Mender {
	return newTestMender(nil, conf.MenderConfig{
		MenderConfigFromFile: conf.MenderConfigFromFile{
			Servers: []client.MenderServer{{}},
		},
	}, testMenderPieces{})
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
				Servers:                               []client.MenderServer{{ServerURL: srv.URL}},
				UpdateControlMapExpirationTimeSeconds: 3,
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

	//
	// UpdateControlMap update response tests
	//

	// Matching deployment ID and map ID
	srv.Update.Has = true
	pool := NewControlMap(mender.Store, 10)
	mender.controlMapPool = pool
	srv.Update.ControlMap = &updatecontrolmap.UpdateControlMap{
		ID:       TEST_UUID,
		Priority: 1,
	}
	srv.Update.Data.ID = TEST_UUID
	up, err = mender.CheckUpdate()
	assert.NoError(t, err)
	assert.NotNil(t, up)
	active, _ := pool.Get(TEST_UUID)
	assert.Equal(t, 1, len(active))

	// Mismatched deployment ID and map ID
	srv.Update.Has = true
	pool = NewControlMap(mender.Store, 10)
	mender.controlMapPool = pool
	srv.Update.ControlMap = &updatecontrolmap.UpdateControlMap{
		ID:       TEST_UUID2,
		Priority: 1,
	}
	up, err = mender.CheckUpdate()
	assert.Error(t, err)
	active, _ = pool.Get(TEST_UUID2)
	assert.Equal(t, 0, len(active))

	// No control map in the update deletes the existing map from the pool
	srv.Update.Has = true
	pool = NewControlMap(mender.Store, 10)
	pool.Insert(&updatecontrolmap.UpdateControlMap{ID: TEST_UUID3, Priority: 1})
	srv.Update.Data.ID = TEST_UUID3
	active, _ = pool.Get(TEST_UUID3)
	require.Equal(t, 1, len(active))
	mender.controlMapPool = pool
	srv.Update.ControlMap = nil
	up, err = mender.CheckUpdate()
	assert.NoError(t, err)
	active, _ = pool.Get(TEST_UUID3)
	assert.Equal(t, 0, len(active))
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

func (m *testAuthManager) GetSendMessageChan() chan<- AuthManagerRequest {
	return nil
}

func (m *testAuthManager) GetRecvMessageChan() <-chan AuthManagerResponse {
	return nil
}

func (m *testAuthManager) Start() error {
	return nil
}

func (m *testAuthManager) ForceBootstrap() {
}

func (m *testAuthManager) Bootstrap() menderError {
	return nil
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

func TestMenderReportStatus(t *testing.T) {
	srv := cltest.NewClientTestServer()
	defer srv.Close()

	config := conf.MenderConfig{
		MenderConfigFromFile: conf.MenderConfigFromFile{
			Servers: []client.MenderServer{{ServerURL: srv.URL}},
		},
	}

	ms := store.NewMemStore()

	ks := store.NewKeystore(ms, conf.DefaultKeyFile, "", false, defaultKeyPassphrase)
	cmdr := stest.NewTestOSCalls("mac=foobar", 0)
	authManager := NewAuthManager(AuthManagerConfig{
		AuthDataStore: ms,
		KeyStore:      ks,
		IdentitySource: &dev.IdentityDataRunner{
			Cmdr: cmdr,
		},
		Config: &config,
	})

	mender := newTestMender(nil,
		config,
		testMenderPieces{
			MenderPieces: MenderPieces{
				Store:       ms,
				AuthManager: authManager,
			},
		},
	)
	ms.WriteAll(datastore.AuthTokenName, []byte("tokendata"))

	srv.Auth.Verify = true
	srv.Auth.Token = []byte("tokendata")

	// 1. successful report
	err := mender.ReportUpdateStatus(
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

	srv.Auth.Verify = true
	srv.Auth.Token = []byte("tokendata")

	// 1. log upload successful
	logs := []byte(`{ "messages":
[{ "time": "12:12:12", "level": "error", "msg": "log foo" },
{ "time": "12:12:13", "level": "debug", "msg": "log bar" }]
}`)

	err := mender.UploadLog(
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

type testObject struct {
	failed   bool
	errorStr []string
}

func (t *testObject) Errorf(fmtStr string, args ...interface{}) {
	t.failed = true
	t.errorStr = append(t.errorStr, fmt.Sprintf(fmtStr, args...))
}

func TestMutualTLSClientConnection(t *testing.T) {

	correctServerCert, err := tls.LoadX509KeyPair("../client/test/server.crt", "../client/test/server.key")
	require.NoError(t, err)

	correctClientCertPool := x509.NewCertPool()
	pb, err := ioutil.ReadFile("../client/testdata/client.crt")
	require.NoError(t, err)
	correctClientCertPool.AppendCertsFromPEM(pb)

	tc := tls.Config{
		Certificates: []tls.Certificate{correctServerCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    correctClientCertPool,
	}

	tests := map[string]struct {
		conf       conf.MenderConfigFromFile
		assertFunc func(t assert.TestingT, err error, srvLog []byte, msgAndArgs ...interface{})
	}{
		"Error: Wrong client certificate": {
			conf: conf.MenderConfigFromFile{
				ServerCertificate: "../client/test/server.crt",
				HttpsClient: client.HttpsClient{
					Certificate: "../client/testdata/server.crt", // Wrong
					Key:         "../client/testdata/client-cert.key",
				},
			},
			assertFunc: func(t assert.TestingT, err error, srvLog []byte, msgAndArgs ...interface{}) {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "bad certificate")
			},
		},
		"Error: Wrong server certificate": {
			conf: conf.MenderConfigFromFile{
				ServerCertificate: "../client/testdata/client.crt", // Wrong
				HttpsClient: client.HttpsClient{
					Certificate: "../client/testdata/client.crt",
					Key:         "../client/testdata/client-cert.key",
				},
			},
			assertFunc: func(t assert.TestingT, err error, srvLog []byte, msgAndArgs ...interface{}) {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "depth zero self-signed")
			},
		},
		"Error: No client private key": {
			conf: conf.MenderConfigFromFile{
				ServerCertificate: "../client/test/server.crt",
				HttpsClient: client.HttpsClient{
					Certificate: "../client/testdata/client.crt",
					// Key: "../client/testdata/client-cert.key", // Missing
				},
			},
			assertFunc: func(t assert.TestingT, err error, srvLog []byte, msgAndArgs ...interface{}) {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "bad certificate")
			},
		},
		"Error: No client certificate": {
			conf: conf.MenderConfigFromFile{
				ServerCertificate: "../client/test/server.crt",
				HttpsClient: client.HttpsClient{
					// Certificate: "../client/testdata/client.crt", // Missing
					Key: "../client/testdata/client-cert.key",
				},
			},
			assertFunc: func(t assert.TestingT, err error, srvLog []byte, msgAndArgs ...interface{}) {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "bad certificate")
			},
		},
		"Success: Correct configuration": {
			conf: conf.MenderConfigFromFile{
				ServerCertificate: "../client/test/server.crt",
				HttpsClient: client.HttpsClient{
					Certificate: "../client/testdata/client.crt",
					Key:         "../client/testdata/client-cert.key",
				},
			},
			assertFunc: func(t assert.TestingT, err error, srvLog []byte, msgAndArgs ...interface{}) {
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
					   ]}`, string(srvLog))
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			srv := cltest.NewClientTestServer(
				cltest.Options{
					TLSConfig: &tc,
				})
			defer srv.Close()

			test.conf.ServerURL = srv.URL
			test.conf.Servers = []client.MenderServer{{srv.URL}}

			ms := store.NewMemStore()
			mender := newTestMender(nil,
				conf.MenderConfig{
					MenderConfigFromFile: test.conf,
				},
				testMenderPieces{
					MenderPieces: MenderPieces{
						Store: ms,
					},
				},
			)

			ms.WriteAll(datastore.AuthTokenName, []byte("tokendata"))

			srv.Auth.Verify = true
			srv.Auth.Token = []byte("tokendata")

			logs := []byte(`{ "messages":
[{ "time": "12:12:12", "level": "error", "msg": "log foo" },
{ "time": "12:12:13", "level": "debug", "msg": "log bar" }]
}`)

			const maxWait = 10 * time.Second
			const testInterval = 500 * time.Millisecond

			var t2 *testObject
			assert.Eventually(t, func() bool {
				t2 = &testObject{}
				err = mender.UploadLog(
					&datastore.UpdateInfo{
						ID: "foobar",
					},
					logs,
				)
				t2.failed = false
				test.assertFunc(t2, err, srv.Log.Logs)
				return !t2.failed
			}, maxWait, testInterval)
			if t2.failed {
				for _, str := range t2.errorStr {
					t.Log(str)
				}
			}
		})
	}
}

func TestSpinEventLoop(t *testing.T) {
	update := &datastore.UpdateInfo{
		ID: "foo",
	}

	tests := map[string]struct {
		Action           string
		state            string
		OnActionExecuted string
		to               State
		expected         State
	}{
		"AuthorizeWait - Unaffected": {
			to: &authorizeWaitState{
				baseState: baseState{
					id: datastore.MenderStateAuthorizeWait,
					t:  ToIdle,
				},
			},
			expected: &authorizeWaitState{},
		},
		"ArtifactInstall - pause -> continue": {
			state:            "ArtifactInstall_Enter",
			Action:           "pause",
			OnActionExecuted: "continue",
			to:               NewUpdateInstallState(update),
			expected:         &updateInstallState{},
		},
		"ArtifactInstall - pause -> fail": {
			state:            "ArtifactInstall_Enter",
			Action:           "pause",
			OnActionExecuted: "fail",
			to:               NewUpdateInstallState(update),
			expected:         &updateErrorState{},
		},
		"ArtifactInstall - fail": {
			state:    "ArtifactInstall_Enter",
			Action:   "fail",
			to:       NewUpdateInstallState(update),
			expected: &updateErrorState{},
		},
		"ArtifactCommit_Enter - pause -> continue": {
			state:            "ArtifactCommit_Enter",
			Action:           "pause",
			OnActionExecuted: "continue",
			to:               NewUpdateCommitState(update),
			expected:         &updateCommitState{},
		},
		"ArtifactCommit_Enter - pause -> fail": {
			state:            "ArtifactCommit_Enter",
			Action:           "pause",
			OnActionExecuted: "fail",
			to:               NewUpdateCommitState(update),
			expected:         &updateErrorState{},
		},
		"ArtifactCommit_Enter - fail": {
			state:    "ArtifactCommit_Enter",
			Action:   "fail",
			to:       NewUpdateCommitState(update),
			expected: &updateErrorState{},
		},
		"ArtifactReboot_Enter - pause -> continue": {
			state:            "ArtifactReboot_Enter",
			Action:           "pause",
			OnActionExecuted: "continue",
			to:               NewUpdateRebootState(update),
			expected:         &updateRebootState{},
		},
		"ArtifactReboot_Enter - pause -> fail": {
			state:            "ArtifactReboot_Enter",
			Action:           "pause",
			OnActionExecuted: "fail",
			to:               NewUpdateRebootState(update),
			expected:         &updateErrorState{},
		},
		"ArtifactReboot_Enter - fail": {
			state:    "ArtifactReboot_Enter",
			Action:   "fail",
			to:       NewUpdateRebootState(update),
			expected: &updateErrorState{},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			pool := NewControlMap(
				store.NewMemStore(),
				conf.DefaultUpdateControlMapBootExpirationTimeSeconds)
			pool.Insert(&updatecontrolmap.UpdateControlMap{
				ID:       "foo",
				Priority: 1,
				States: map[string]updatecontrolmap.UpdateControlMapState{
					test.state: updatecontrolmap.UpdateControlMapState{
						Action:           test.Action,
						OnActionExecuted: test.OnActionExecuted,
					},
				},
			})
			ms := store.NewMemStore()
			ctx := &StateContext{
				Store: ms,
			}
			controller := newDefaultTestMender()
			reporter := func(status string) error {
				return nil
			}
			s := spinEventLoop(pool, test.to, ctx, controller, reporter)
			assert.IsType(t, test.expected, s)
		})
	}
}

func TestUploadPauseStatus(t *testing.T) {

	update := &datastore.UpdateInfo{
		ID: "foo",
	}

	ac, err := client.NewApiClient(
		client.Config{ServerCert: "", NoVerify: true},
	)
	require.Nil(t, err)

	responder := &struct {
		httpStatus int
		recdata    chan []byte
	}{
		http.StatusNoContent, // 204
		make(chan []byte),
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(responder.httpStatus)
		recdata, _ := ioutil.ReadAll(r.Body)
		responder.recdata <- recdata
	}))
	defer ts.Close()

	tests := map[string]struct {
		to       State
		report   *client.StatusReportWrapper
		expected string
		state    string
	}{
		"Install": {
			to:    NewUpdateInstallState(update),
			state: "ArtifactInstall_Enter",
			report: &client.StatusReportWrapper{
				API: ac,
				URL: ts.URL,
				Report: client.StatusReport{
					DeploymentID: "foo",
					Status:       "installing",
					SubState:     "DoesNotMatterHere",
				},
			},
			expected: string(`{"status":"installing","substate":"pause_before_installing"}`),
		},
		"Commit": {
			to:    NewUpdateCommitState(update),
			state: "ArtifactCommit_Enter",
			report: &client.StatusReportWrapper{
				API: ac,
				URL: ts.URL,
				Report: client.StatusReport{
					DeploymentID: "foo",
					Status:       "committing",
					SubState:     "DoesNotMatterHere",
				},
			},
			expected: string(`{"status":"committing","substate":"pause_before_committing"}`),
		},
		"Reboot": {
			to:    NewUpdateRebootState(update),
			state: "ArtifactReboot_Enter",
			report: &client.StatusReportWrapper{
				API: ac,
				URL: ts.URL,
				Report: client.StatusReport{
					DeploymentID: "foo",
					Status:       "rebooting",
					SubState:     "DoesNotMatterHere",
				},
			},
			expected: string(`{"status":"rebooting","substate":"pause_before_rebooting"}`),
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			pool := NewControlMap(
				store.NewMemStore(),
				conf.DefaultUpdateControlMapBootExpirationTimeSeconds)
			pool.Insert(&updatecontrolmap.UpdateControlMap{
				ID:       "foo",
				Priority: 1,
				States: map[string]updatecontrolmap.UpdateControlMapState{
					test.state: updatecontrolmap.UpdateControlMapState{
						Action:           "pause",
						OnActionExecuted: "pause",
					},
				},
			})
			ms := store.NewMemStore()
			ctx := &StateContext{
				Store: ms,
			}
			controller := newDefaultTestMender()
			updateStatusClient := client.NewStatus()
			reporter := func(status string) error {
				return updateStatusClient.Report(test.report.API, test.report.URL, client.StatusReport{
					DeploymentID: test.report.Report.DeploymentID,
					Status:       test.report.Report.Status,
					SubState:     status,
				})
			}
			go spinEventLoop(pool, test.to, ctx, controller, reporter)
			assert.Eventually(t, func() bool {
				select {
				case recdata := <-responder.recdata:
					return assert.JSONEq(t,
						test.expected,
						string(recdata))
				default:
					return assert.True(t, false, "Received no response")
				}
			},
				1*time.Second, 100*time.Millisecond)
		})
	}

}
