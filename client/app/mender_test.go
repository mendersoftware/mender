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
	"context"
	"crypto/rand"
	cryptotls "crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/mendersoftware/mender-artifact/artifact"
	"github.com/mendersoftware/mender-artifact/awriter"
	"github.com/mendersoftware/mender-artifact/handlers"
	"github.com/mendersoftware/mender/authmanager"
	authconf "github.com/mendersoftware/mender/authmanager/conf"
	"github.com/mendersoftware/mender/authmanager/device"
	"github.com/mendersoftware/mender/client/api"
	cltest "github.com/mendersoftware/mender/client/api/test"
	"github.com/mendersoftware/mender/client/app/updatecontrolmap"
	"github.com/mendersoftware/mender/client/conf"
	"github.com/mendersoftware/mender/client/datastore"
	commonconf "github.com/mendersoftware/mender/common/conf"
	dbustest "github.com/mendersoftware/mender/common/dbus/test"
	"github.com/mendersoftware/mender/common/store"
	stest "github.com/mendersoftware/mender/common/system/testing"
	"github.com/mendersoftware/mender/common/tls"

	log "github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
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

type testMender struct {
	*Mender

	dbusServer  *dbustest.DBusTestServer
	authManager authmanager.AuthManager
}

func newTestMender(_ *stest.TestOSCalls, config *conf.MenderConfig,
	pieces testMenderPieces, authConfig *authconf.AuthConfig) *testMender {

	// fill out missing pieces

	if pieces.Store == nil {
		pieces.Store = store.NewMemStore()
	}

	if pieces.DualRootfsDevice == nil {
		pieces.DualRootfsDevice = &FakeDevice{}
	}

	mender, err := NewMender(config, pieces.MenderPieces)
	if err != nil {
		panic("Got error from NewAuthManager: " + err.Error())
	}
	mender.StateScriptPath = ""

	m := &testMender{
		Mender: mender,
	}

	// Start common components needed for most tests.

	m.dbusServer = dbustest.NewDBusTestServer()
	dbusAPI := m.dbusServer.GetDBusAPI()

	if authConfig == nil {
		authConfig = authconf.NewAuthConfig()
	}

	m.authManager, err = authmanager.NewAuthManager(authmanager.AuthManagerConfig{
		AuthConfig:    authConfig,
		AuthDataStore: store.NewMemStore(),
		KeyDirStore:   store.NewDirStore("."),
		DBusAPI:       dbusAPI,
		IdentitySource: &device.IdentityDataRunner{
			Cmdr: stest.NewTestOSCalls("mac=foobar", 0),
		},
	})
	if err != nil {
		panic("Got error from NewAuthManager: " + err.Error())
	}
	m.authManager.Start()

	m.Mender.api.DBusAPI = dbusAPI

	return m
}

func newDefaultTestMender() *testMender {
	return newTestMender(nil, conf.NewMenderConfig(), testMenderPieces{}, authconf.NewAuthConfig())
}

func (m *testMender) Close() {
	m.authManager.Stop()
	m.dbusServer.Close()
}

func TestCheckUpdateSimple(t *testing.T) {
	// create temp dir
	td, _ := ioutil.TempDir("", "mender-install-update-")
	defer os.RemoveAll(td)

	// prepare fake artifactInfo file
	artifactInfo := path.Join(td, "artifact_info")
	// prepare fake device type file
	deviceType := path.Join(td, "device_type")

	mender := newTestMender(nil,
		conf.NewMenderConfig(),
		testMenderPieces{},
		&authconf.AuthConfig{
			Servers: []authconf.MenderServer{{ServerURL: "bogusurl"}},
		},
	)

	up, err := mender.CheckUpdate()
	assert.Error(t, err)
	assert.Nil(t, up)

	mender.Close()

	srv := cltest.NewClientTestServer()
	defer srv.Close()

	srv.Auth.Authorize = true
	srv.Auth.Token = []byte("token")
	srv.Update.Has = true

	config := conf.NewMenderConfig()
	config.UpdateControlMapExpirationTimeSeconds = 3
	mender = newTestMender(nil,
		config,
		testMenderPieces{},
		&authconf.AuthConfig{
			Servers: []authconf.MenderServer{{ServerURL: srv.Server.URL}},
		},
	)
	defer mender.Close()
	mender.ArtifactInfoFile = artifactInfo
	mender.DeviceTypeFile = deviceType

	// Mock DBus interface io.mender.Proxy
	ctx, cancel := context.WithCancel(context.Background())
	go dbustest.RegisterAndServeIoMenderProxy(mender.dbusServer, ctx, srv.Server.URL)
	defer cancel()

	srv.Update.Current = &api.CurrentUpdate{
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
	assert.EqualError(t, errors.Cause(err), api.ErrNoDeploymentAvailable.Error())
	assert.Nil(t, up)

	//
	// UpdateControlMap update response tests
	//

	// Wrong content in map
	srv.Update.Has = true
	pool := NewControlMap(mender.Store, 10, 5)
	mender.controlMapPool = pool
	srv.Update.ControlMap = &updatecontrolmap.UpdateControlMap{
		ID:       TEST_UUID,
		Priority: 1,
		States: map[string]updatecontrolmap.UpdateControlMapState{
			"bogus": updatecontrolmap.UpdateControlMapState{},
		},
	}
	up, err = mender.CheckUpdate()
	assert.Error(t, err)
	active, _ := pool.Get(TEST_UUID)
	assert.Equal(t, 0, len(active))
	assert.NotNil(t, up)

	// Matching deployment ID and map ID
	srv.Update.Has = true
	pool = NewControlMap(mender.Store, 10, 5)
	mender.controlMapPool = pool
	srv.Update.ControlMap = &updatecontrolmap.UpdateControlMap{
		ID:       TEST_UUID,
		Priority: 1,
	}
	srv.Update.Data.ID = TEST_UUID
	up, err = mender.CheckUpdate()
	assert.NoError(t, err)
	assert.NotNil(t, up)
	active, _ = pool.Get(TEST_UUID)
	assert.Equal(t, 1, len(active))

	// Mismatched deployment ID and map ID
	srv.Update.Has = true
	pool = NewControlMap(mender.Store, 10, 5)
	mender.controlMapPool = pool
	srv.Update.ControlMap = &updatecontrolmap.UpdateControlMap{
		ID:       TEST_UUID2,
		Priority: 1,
	}
	up, err = mender.CheckUpdate()
	assert.Error(t, err)
	active, _ = pool.Get(TEST_UUID2)
	assert.Equal(t, 0, len(active))
	// Update Info should still be present even if the map is wrong, so that
	// we can report status.
	assert.NotNil(t, up)

	// No control map in the update deletes the existing map from the pool
	srv.Update.Has = true
	pool = NewControlMap(mender.Store, 10, 5)
	pool.Insert(&updatecontrolmap.UpdateControlMap{
		ID: TEST_UUID3, Priority: 1,
	})
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
	mender := newTestMender(nil, &conf.MenderConfig{
		MenderConfigFromFile: conf.MenderConfigFromFile{
			UpdatePollIntervalSeconds: 20,
		},
	}, testMenderPieces{}, nil)

	intvl := mender.GetUpdatePollInterval()
	assert.Equal(t, time.Duration(20)*time.Second, intvl)
}

func TestMenderGetInventoryPollInterval(t *testing.T) {
	mender := newTestMender(nil, &conf.MenderConfig{
		MenderConfigFromFile: conf.MenderConfigFromFile{
			InventoryPollIntervalSeconds: 10,
		},
	}, testMenderPieces{}, nil)

	intvl := mender.GetInventoryPollInterval()
	assert.Equal(t, time.Duration(10)*time.Second, intvl)
}

func TestMenderReportStatus(t *testing.T) {
	srv := cltest.NewClientTestServer()
	defer srv.Close()

	authConfig := authconf.AuthConfig{
		Servers: []authconf.MenderServer{{ServerURL: srv.Server.URL}},
	}
	menderConfig := conf.NewMenderConfig()

	ms := store.NewMemStore()

	mender := newTestMender(nil,
		menderConfig,
		testMenderPieces{
			MenderPieces: MenderPieces{
				Store: ms,
			},
		},
		&authConfig,
	)
	defer mender.Close()

	srv.Auth.Verify = true
	srv.Auth.Authorize = true
	srv.Auth.Token = []byte("tokendata")

	// Mock DBus interface io.mender.Proxy
	ctx, cancel := context.WithCancel(context.Background())
	go dbustest.RegisterAndServeIoMenderProxy(mender.dbusServer, ctx, srv.Server.URL)
	defer cancel()

	// 1. successful report
	merr := mender.ReportUpdateStatus(
		&datastore.UpdateInfo{
			ID: "foobar",
		},
		api.StatusSuccess,
	)
	assert.Nil(t, merr)
	assert.Equal(t, api.StatusSuccess, srv.Status.Status)

	// 2. pretend authorization fails, server expects a different token
	srv.Reset()
	srv.Auth.Token = []byte("footoken")
	srv.Auth.Verify = true
	srv.Auth.Authorize = false
	merr = mender.ReportUpdateStatus(
		&datastore.UpdateInfo{
			ID: "foobar",
		},
		api.StatusSuccess,
	)
	assert.NotNil(t, merr)
	assert.False(t, merr.IsFatal())

	// 3. pretend that deployment was aborted
	srv.Reset()
	srv.Auth.Authorize = true
	srv.Auth.Token = []byte("tokendata")
	srv.Auth.Verify = true
	srv.Status.Aborted = true
	merr = mender.ReportUpdateStatus(
		&datastore.UpdateInfo{
			ID: "foobar",
		},
		api.StatusSuccess,
	)
	assert.NotNil(t, merr)
	assert.True(t, merr.IsFatal())
}

func TestMenderLogUpload(t *testing.T) {
	srv := cltest.NewClientTestServer()
	defer srv.Close()

	authConfig := authconf.AuthConfig{
		Servers: []authconf.MenderServer{{ServerURL: srv.Server.URL}},
	}

	ms := store.NewMemStore()
	mender := newTestMender(nil,
		conf.NewMenderConfig(),
		testMenderPieces{
			MenderPieces: MenderPieces{
				Store: ms,
			},
		},
		&authConfig,
	)
	defer mender.Close()

	srv.Auth.Authorize = true
	srv.Auth.Verify = true
	srv.Auth.Token = []byte("tokendata")

	// Mock DBus interface io.mender.Proxy
	ctx, cancel := context.WithCancel(context.Background())
	go dbustest.RegisterAndServeIoMenderProxy(mender.dbusServer, ctx, srv.Server.URL)
	defer cancel()

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
	srv.Auth.Authorize = false
	err = mender.UploadLog(
		&datastore.UpdateInfo{
			ID: "foobar",
		},
		logs,
	)
	assert.NotNil(t, err)
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
	srv.Auth.Authorize = true
	srv.Auth.Token = []byte("token")

	authConfig := authconf.AuthConfig{
		Servers: []authconf.MenderServer{{ServerURL: srv.Server.URL}},
	}

	ms := store.NewMemStore()
	mender := newTestMender(nil,
		conf.NewMenderConfig(),
		testMenderPieces{
			MenderPieces: MenderPieces{
				Store: ms,
			},
		},
		&authConfig,
	)
	defer mender.Close()
	mender.ArtifactInfoFile = artifactInfo
	mender.DeviceTypeFile = deviceType

	// Mock DBus interface io.mender.Proxy
	ctx, cancel := context.WithCancel(context.Background())
	go dbustest.RegisterAndServeIoMenderProxy(mender.dbusServer, ctx, srv.Server.URL)
	defer cancel()

	// prepare fake inventory scripts
	// 1. setup a temporary path $TMPDIR/mendertest<random>/inventory
	tdir, err := ioutil.TempDir("", "mendertest")
	assert.NoError(t, err)
	invpath := path.Join(tdir, "inventory")
	err = os.MkdirAll(invpath, os.FileMode(syscall.S_IRWXU))
	assert.NoError(t, err)
	defer os.RemoveAll(tdir)

	oldDefaultPathDataDir := commonconf.DefaultPathDataDir
	// override datadir path for subsequent getDataDirPath() calls
	commonconf.DefaultPathDataDir = tdir
	defer func() {
		// restore old datadir path
		commonconf.DefaultPathDataDir = oldDefaultPathDataDir
	}()

	// 1a. no scripts hence no inventory data, submit should have been
	// called with default inventory attributes only
	srv.Auth.Verify = true
	srv.Auth.Token = []byte("tokendata")
	err = mender.InventoryRefresh()
	assert.Nil(t, err)

	assert.True(t, srv.Inventory.Called)
	exp := []api.InventoryAttribute{
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
	exp = []api.InventoryAttribute{
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

	mender := newTestMender(nil, conf.NewMenderConfig(),
		testMenderPieces{
			MenderPieces: MenderPieces{
				DualRootfsDevice: &FakeDevice{ConsumeUpdate: true},
			},
		},
		nil,
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

	mender = newTestMender(nil, conf.NewMenderConfig(),
		testMenderPieces{
			MenderPieces: MenderPieces{
				DualRootfsDevice: &FakeDevice{RetStoreUpdate: errors.New("failed")},
			},
		},
		nil,
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

	authConfig := authconf.AuthConfig{
		ServerURL: srv.Server.URL,
	}
	ms := store.NewMemStore()
	mender := newTestMender(nil,
		conf.NewMenderConfig(),
		testMenderPieces{
			MenderPieces: MenderPieces{
				Store: ms,
			},
		},
		&authConfig,
	)
	defer mender.Close()

	srv.Auth.Authorize = true
	srv.Auth.Token = []byte("token")

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

	img, sz, err := mender.FetchUpdate(srv.Server.URL + "/api/devices/v1/download")
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
	srv.Update.Current = &api.CurrentUpdate{
		Artifact:   "mender-image",
		DeviceType: "dev",
	}

	// make and configure a mender
	authConfig := authconf.AuthConfig{
		Servers: []authconf.MenderServer{{ServerURL: srv.Server.URL}},
	}
	mender := newTestMender(nil,
		conf.NewMenderConfig(),
		testMenderPieces{},
		&authConfig,
	)
	defer mender.Close()
	mender.ArtifactInfoFile = "artifact_info"
	mender.DeviceTypeFile = "device_type"

	// Mock DBus interface io.mender.Proxy
	ctx, cancel := context.WithCancel(context.Background())
	go dbustest.RegisterAndServeIoMenderProxy(mender.dbusServer, ctx, srv.Server.URL)
	defer cancel()

	// Successful reauth: server changed token
	srv.Auth.Token = []byte(`bar`)
	_, err := mender.CheckUpdate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), api.ErrNoDeploymentAvailable.Error())

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
	srv2.Update.Current = &api.CurrentUpdate{
		Artifact:   "mender-image",
		DeviceType: "dev",
	}
	srv2.Update.Has = true
	srv2.Update.Data = datastore.UpdateInfo{
		ID: "foo",
	}
	// Create mender- and conf.MenderConfig structs
	srvrs := make([]authconf.MenderServer, 2)
	srvrs[0].ServerURL = srv1.Server.URL
	srvrs[1].ServerURL = srv2.Server.URL
	srv2.Auth.Token = []byte(`jwt`)
	srv2.Auth.Authorize = true
	srv2.Auth.Verify = true
	authConfig := authconf.AuthConfig{
		ServerURL: srv1.Server.URL,
		Servers:   srvrs,
	}
	mender := newTestMender(nil,
		conf.NewMenderConfig(),
		testMenderPieces{},
		&authConfig,
	)
	defer mender.Close()
	mender.ArtifactInfoFile = "artifact_info"
	mender.DeviceTypeFile = "device_type"

	// Mock DBus interface io.mender.Proxy
	ctx, cancel := context.WithCancel(context.Background())
	go dbustest.RegisterAndServeIoMenderProxy(mender.dbusServer, ctx, srv2.Server.URL)
	defer cancel()

	// Client is not authorized for server 1.
	_, err := mender.CheckUpdate()
	assert.NoError(t, err)
	assert.True(t, srv1.Auth.Called)
	assert.True(t, srv2.Auth.Called)

	// Check for update: causes srv1 to return bad request (400) and trigger failover.
	rsp, err := mender.CheckUpdate()
	assert.NoError(t, err)
	// When checking update, only the known good server is called.
	assert.False(t, srv1.Update.Called)
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

func logContains(entries []*log.Entry, expected string) bool {
	for _, entry := range entries {
		if strings.Index(entry.Message, expected) >= 0 {
			return true
		}
	}
	return false
}

func TestMutualTLSClientConnection(t *testing.T) {

	correctServerCert, err := cryptotls.LoadX509KeyPair("../api/test/server.crt", "../api/test/server.key")
	require.NoError(t, err)

	correctClientCertPool := x509.NewCertPool()
	pb, err := ioutil.ReadFile("../../common/tls/testdata/client.crt")
	require.NoError(t, err)
	correctClientCertPool.AppendCertsFromPEM(pb)

	tc := cryptotls.Config{
		Certificates: []cryptotls.Certificate{correctServerCert},
		ClientAuth:   cryptotls.RequireAndVerifyClientCert,
		ClientCAs:    correctClientCertPool,
	}

	tests := map[string]struct {
		conf       authconf.AuthConfig
		assertFunc func(t assert.TestingT, err error, srvLog []byte, log []*log.Entry, msgAndArgs ...interface{})
	}{
		"Error: Wrong client certificate": {
			conf: authconf.AuthConfig{
				Config: commonconf.Config{
					ServerCertificate: "../api/test/server.crt",
					HttpsClient: tls.HttpsClient{
						Certificate: "../../common/tls/testdata/server.crt", // Wrong
						Key:         "../../common/tls/testdata/client-cert.key",
					},
				},
			},
			assertFunc: func(t assert.TestingT, err error, srvLog []byte, log []*log.Entry, msgAndArgs ...interface{}) {
				assert.Error(t, err)
				assert.True(t, logContains(log, "bad certificate"))
			},
		},
		"Error: Wrong server certificate": {
			conf: authconf.AuthConfig{
				Config: commonconf.Config{
					ServerCertificate: "../../common/tls/testdata/client.crt", // Wrong
					HttpsClient: tls.HttpsClient{
						Certificate: "../../common/tls/testdata/client.crt",
						Key:         "../../common/tls/testdata/client-cert.key",
					},
				},
			},
			assertFunc: func(t assert.TestingT, err error, srvLog []byte, log []*log.Entry, msgAndArgs ...interface{}) {
				assert.Error(t, err)
				assert.True(t, logContains(log, "depth zero self-signed"))
			},
		},
		"Error: No client private key": {
			conf: authconf.AuthConfig{
				Config: commonconf.Config{
					ServerCertificate: "../api/test/server.crt",
					HttpsClient: tls.HttpsClient{
						Certificate: "../../common/tls/testdata/client.crt",
						// Key: "../../common/tls/testdata/client-cert.key", // Missing
					},
				},
			},
			assertFunc: func(t assert.TestingT, err error, srvLog []byte, log []*log.Entry, msgAndArgs ...interface{}) {
				assert.Error(t, err)
				assert.True(t, logContains(log, "bad certificate"))
			},
		},
		"Error: No client certificate": {
			conf: authconf.AuthConfig{
				Config: commonconf.Config{
					ServerCertificate: "../api/test/server.crt",
					HttpsClient: tls.HttpsClient{
						// Certificate: "../../common/tls/testdata/client.crt", // Missing
						Key: "../../common/tls/testdata/client-cert.key",
					},
				},
			},
			assertFunc: func(t assert.TestingT, err error, srvLog []byte, log []*log.Entry, msgAndArgs ...interface{}) {
				assert.Error(t, err)
				assert.True(t, logContains(log, "bad certificate"))
			},
		},
		"Success: Correct configuration": {
			conf: authconf.AuthConfig{
				Config: commonconf.Config{
					ServerCertificate: "../api/test/server.crt",
					HttpsClient: tls.HttpsClient{
						Certificate: "../../common/tls/testdata/client.crt",
						Key:         "../../common/tls/testdata/client-cert.key",
					},
				},
			},
			assertFunc: func(t assert.TestingT, err error, srvLog []byte, log []*log.Entry, msgAndArgs ...interface{}) {
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
			hook := logtest.NewGlobal()
			defer hook.Reset()

			srv := cltest.NewClientTestServer(&tc)
			defer srv.Close()
			srv.Auth.Token = []byte("jwt")
			srv.Auth.Authorize = true

			test.conf.ServerURL = srv.Server.URL
			test.conf.Servers = []authconf.MenderServer{{srv.Server.URL}}

			ms := store.NewMemStore()
			menderConf := conf.NewMenderConfig()
			menderConf.Config = test.conf.Config
			mender := newTestMender(nil,
				menderConf,
				testMenderPieces{
					MenderPieces: MenderPieces{
						Store: ms,
					},
				},
				&test.conf,
			)
			defer mender.Close()

			srv.Auth.Verify = true
			srv.Auth.Token = []byte("tokendata")

			// Mock DBus interface io.mender.Proxy
			ctx, cancel := context.WithCancel(context.Background())
			go dbustest.RegisterAndServeIoMenderProxy(mender.dbusServer, ctx, srv.Server.URL)
			defer cancel()

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
				test.assertFunc(t2, err, srv.Log.Logs, hook.AllEntries())
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
