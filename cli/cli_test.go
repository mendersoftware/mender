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
package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/alfrunes/cli"
	"github.com/mendersoftware/log"
	"github.com/mendersoftware/mender/app"
	"github.com/mendersoftware/mender/client"
	"github.com/mendersoftware/mender/conf"
	"github.com/mendersoftware/mender/datastore"
	dev "github.com/mendersoftware/mender/device"
	"github.com/mendersoftware/mender/installer"
	"github.com/mendersoftware/mender/store"
	stest "github.com/mendersoftware/mender/system/testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	conf.DefaultConfFile = "mender-default-test.conf"
}

func TestAmbiguousArgumentsArgs(t *testing.T) {
	err := SetupCLI([]string{"-daemon", "-commit"})
	assert.Error(t, err)
	assert.Equal(t, fmt.Sprintf(errMsgAmbiguousArgumentsGivenF, []string{"commit"}),
		err.Error())
}

func TestCheckUpdate(t *testing.T) {
	args := []string{"-check-update"}
	err := SetupCLI(args)
	// Should produce an error since daemon is not running
	assert.Error(t, err)
}

func TestRunDaemon(t *testing.T) {
	// create directory for storing deployments logs
	tempDir, _ := ioutil.TempDir("", "logs")
	defer os.RemoveAll(tempDir)
	app.DeploymentLogger = app.NewDeploymentLogManager(tempDir)
	var buf bytes.Buffer
	oldOutput := log.Log.Out
	log.SetOutput(&buf)
	log.SetLevel(log.DebugLevel)
	defer log.SetOutput(oldOutput)
	ds := store.NewMemStore()

	tests := map[string]struct {
		signal syscall.Signal
	}{
		"check-update": {
			signal: syscall.SIGUSR1,
		},
		"inventory-update": {
			signal: syscall.SIGUSR2,
		},
	}
	config := conf.MenderConfig{
		MenderConfigFromFile: conf.MenderConfigFromFile{
			Servers: []client.MenderServer{{}},
		},
	}
	pieces := app.MenderPieces{
		Store: store.NewMemStore(),
		DualRootfsDevice: installer.NewDualRootfsDevice(
			nil, nil, installer.DualRootfsDeviceConfig{}),
	}

	pieces.AuthMgr = app.NewAuthManager(app.AuthManagerConfig{
		AuthDataStore: pieces.Store,
		KeyStore:      store.NewKeystore(pieces.Store, conf.DefaultKeyFile),
		IdentitySource: &dev.IdentityDataRunner{
			Cmdr: stest.NewTestOSCalls("mac=foobar", 0),
		},
	})

	for name, test := range tests {
		mender, err := app.NewMender(&config, pieces)
		assert.NoError(t, err)
		td := &app.MenderDaemon{
			Mender: mender,
			Sctx: app.StateContext{
				Store:      ds,
				WakeupChan: make(chan bool, 1),
			},
			Store:        ds,
			ForceToState: make(chan app.State, 1),
		}
		go func() {
			err := runDaemon(td)
			require.Nil(t, err, "Daemon returned with an error code")
		}()

		for td.Mender.GetCurrentState() != app.States.AuthorizeWait {
			time.Sleep(time.Millisecond * 200)
		}

		proc, err := os.FindProcess(os.Getpid())
		require.Nil(t, err)
		require.Nil(t, proc.Signal(test.signal))

		// Give the client some time to handle the signal.
		time.Sleep(time.Second * 1)
		td.StopDaemon()
		assert.Contains(t, buf.String(), "forced wake-up", name+" signal did not force daemon from sleep")
		buf.Reset()

	}
}

func TestLoggingOptions(t *testing.T) {
	err := SetupCLI([]string{"-log-level", "crap", "-commit"})
	assert.Error(t, err, "'crap' log level should have given error")
	// Should have a reference to log level.
	assert.Contains(t, err.Error(), "level")

	//err = setupCLI([]string{"mender", "-info", "-log-level", "debug"})
	//assert.Error(t, err, "Incompatible log levels should have given error")
	//assert.Contains(t, err.Error(), errMsgIncompatibleLogOptions.Error())

	var buf bytes.Buffer
	oldOutput := log.Log.Out
	log.SetOutput(&buf)
	defer log.SetOutput(oldOutput)

	// Ignore errors for now, we just want to know if the logging level was
	// applied.
	log.SetLevel(log.DebugLevel)
	SetupCLI([]string{"-log-level", "panic"})
	log.Debugln("Should not show")
	SetupCLI([]string{"-debug"})
	log.Debugln("Should show")
	SetupCLI([]string{"-info"})
	log.Debugln("Should also not show")

	logdata := buf.String()
	assert.Contains(t, logdata, "Should show")
	assert.NotContains(t, logdata, "Should not show")
	assert.NotContains(t, logdata, "Should also not show")

	SetupCLI([]string{"mender", "-log-modules", "cli_test,MyModule"})
	log.Errorln("Module filter should show cli_test")
	log.PushModule("MyModule")
	log.Errorln("Module filter should show MyModule")
	log.PushModule("MyOtherModule")
	log.Errorln("Module filter should not show MyOtherModule")
	log.PopModule()
	log.PopModule()

	assert.True(t, strings.Contains(buf.String(),
		"Module filter should show cli_test"))
	assert.True(t, strings.Contains(buf.String(),
		"Module filter should show MyModule"))
	assert.False(t, strings.Contains(buf.String(),
		"Module filter should not show MyOtherModule"))

	defer os.Remove("test.log")
	SetupCLI([]string{"-log-file", "test.log"})
	log.Errorln("Should be in log file")
	fd, err := os.Open("test.log")
	assert.NoError(t, err)

	var bytebuf [4096]byte
	n, err := fd.Read(bytebuf[:])
	assert.True(t, err == nil)
	assert.True(t, strings.Contains(string(bytebuf[0:n]),
		"Should be in log file"))

	err = SetupCLI([]string{"-no-syslog"})
	// Just check that the flag can be specified.
	assert.True(t, err == nil)
	assert.False(t, strings.Contains(buf.String(), "syslog"))
}

func TestVersion(t *testing.T) {
	oldstdout := os.Stdout

	tfile, err := ioutil.TempFile("", "mendertest")
	assert.NoError(t, err)
	tname := tfile.Name()

	// pretend we're stdout now
	os.Stdout = tfile

	// running with stderr pointing to temp file
	err = SetupCLI([]string{"-version"})

	// restore previous stderr
	os.Stdout = oldstdout
	assert.NoError(t, err, "calling main with -version should not produce an error")

	// rewind
	tfile.Seek(0, 0)
	data, _ := ioutil.ReadAll(tfile)
	tfile.Close()
	os.Remove(tname)

	expected := fmt.Sprintf("%s\truntime: %s\n",
		conf.VersionString(), runtime.Version())
	assert.Equal(t, expected, string(data),
		"unexpected version output '%s' expected '%s'", string(data), expected)
}

func writeConfig(t *testing.T, path string, config conf.MenderConfig) {
	cf, err := os.Create(path)
	assert.NoError(t, err)
	defer cf.Close()

	d, _ := json.Marshal(config)

	_, err = cf.Write(d)
	assert.NoError(t, err)
}

func writeFakeIdentityHelper(t *testing.T, path string, script string) {
	f, err := os.Create(path)
	assert.NoError(t, err)
	defer f.Close()

	_, err = f.WriteString(script)
	assert.NoError(t, err)

	err = os.Chmod(path, 0755)
	assert.NoError(t, err)
}

// go through bootstrap procedure
func TestMainBootstrap(t *testing.T) {

	var err error

	// fake server first
	responder := &struct {
		httpStatus int
		data       string
		headers    http.Header
	}{
		http.StatusOK,
		"foobar-token",
		http.Header{},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		responder.headers = r.Header
		w.WriteHeader(responder.httpStatus)
		w.Header().Set("Content-Type", "application/json")

		fmt.Fprint(w, responder.data)
	}))
	defer ts.Close()

	// directory for keeping test data
	tdir, err := ioutil.TempDir("", "mendertest")
	assert.NoError(t, err)
	defer os.RemoveAll(tdir)

	// setup a dirstore helper to easily access file contents in test dir
	ds := store.NewDirStore(tdir)
	assert.NotNil(t, ds)

	db := store.NewDBStore(tdir)
	defer db.Close()
	assert.NotNil(t, db)

	// setup test config
	cpath := path.Join(tdir, "mender.config")
	writeConfig(t, cpath, conf.MenderConfig{
		MenderConfigFromFile: conf.MenderConfigFromFile{
			Servers: []client.MenderServer{{ServerURL: ts.URL}},
		},
	})

	// override identity helper script
	oldidh := dev.IdentityDataHelper
	defer func(old string) {
		dev.IdentityDataHelper = old
	}(oldidh)

	newidh := path.Join(tdir, "fakehelper")
	writeFakeIdentityHelper(t, newidh,
		`#!/bin/sh
echo mac=00:11:22:33:44:55
`)
	dev.IdentityDataHelper = newidh

	// run bootstrap
	db.Remove(datastore.AuthTokenName)
	err = SetupCLI([]string{"-data", tdir, "-config", cpath,
		"-debug", "-bootstrap"})
	assert.NoError(t, err)

	// should have generated a key
	keyold, err := ds.ReadAll(conf.DefaultKeyFile)
	assert.NoError(t, err)
	assert.NotEmpty(t, keyold)

	// and we should have a token
	d, err := db.ReadAll(datastore.AuthTokenName)
	assert.NoError(t, err)
	assert.Equal(t, []byte("foobar-token"), d)

	// force bootstrap and run again, check if key was changed
	db.Remove(datastore.AuthTokenName)
	err = SetupCLI([]string{"-data", tdir, "-config", cpath,
		"-debug", "-bootstrap", "-forcebootstrap"})
	assert.NoError(t, err)

	keynew, err := ds.ReadAll(conf.DefaultKeyFile)
	assert.NoError(t, err)
	assert.NotEmpty(t, keynew)
	assert.NotEqual(t, keyold, keynew)

	db.Remove(datastore.AuthTokenName)

	// return non 200 status code, we should get an error as authorization has
	// failed
	responder.httpStatus = http.StatusUnauthorized
	err = SetupCLI([]string{"-data", tdir, "-config", cpath,
		"-debug", "-bootstrap", "-forcebootstrap"})
	assert.Error(t, err)

	_, err = db.ReadAll(datastore.AuthTokenName)
	assert.Error(t, err)
	assert.True(t, os.IsNotExist(err))

}

func TestPrintArtifactName(t *testing.T) {

	tmpdir, err := ioutil.TempDir("", "TestPrintArtifactName")
	require.NoError(t, err)
	defer os.RemoveAll(tmpdir)

	require.NoError(t, os.MkdirAll(path.Join(tmpdir, "etc"), 0755))
	require.NoError(t, os.MkdirAll(path.Join(tmpdir, "data"), 0755))

	tfile, err := os.OpenFile(path.Join(tmpdir, "etc", "artifact_info"),
		os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	require.NoError(t, err)

	dbstore := store.NewDBStore(path.Join(tmpdir, "data"))

	config := &conf.MenderConfig{
		ArtifactInfoFile: tfile.Name(),
	}
	deviceManager := dev.NewDeviceManager(nil, config, dbstore)

	// no error
	_, err = io.WriteString(tfile, "artifact_name=foobar")
	require.NoError(t, err)
	assert.Nil(t, PrintArtifactName(deviceManager))
	name, err := deviceManager.GetCurrentArtifactName()
	require.NoError(t, err)
	assert.Equal(t, "foobar", name)

	// DB should override file.
	dbstore.WriteAll(datastore.ArtifactNameKey, []byte("db-name"))
	assert.Nil(t, PrintArtifactName(deviceManager))
	name, err = deviceManager.GetCurrentArtifactName()
	require.NoError(t, err)
	assert.Equal(t, "db-name", name)

	// Erasing it should restore old.
	dbstore.Remove(datastore.ArtifactNameKey)
	assert.Nil(t, PrintArtifactName(deviceManager))
	name, err = deviceManager.GetCurrentArtifactName()
	require.NoError(t, err)
	assert.Equal(t, "foobar", name)

	// empty artifact_name should fail
	err = ioutil.WriteFile(tfile.Name(), []byte("artifact_name="), 0644)
	//overwrite file contents
	require.NoError(t, err)

	assert.EqualError(t, PrintArtifactName(deviceManager), "The Artifact name is empty. Please set a valid name for the Artifact!")

	// two artifact_names is also an error
	err = ioutil.WriteFile(tfile.Name(), []byte(fmt.Sprint("artifact_name=a\ninfo=i\nartifact_name=b\n")), 0644)
	require.NoError(t, err)

	expected := "More than one instance of artifact_name found in manifest file"
	err = PrintArtifactName(deviceManager)
	require.Error(t, err)
	assert.Contains(t, err.Error(), expected)
}

func TestGetMenderDaemonPID(t *testing.T) {
	tests := map[string]struct {
		cmd      *exec.Cmd
		expected string
	}{
		"error": {
			exec.Command("abc"),
			"getMenderDaemonPID: Failed to run systemctl",
		},
		"error: no output": {
			exec.Command("printf", ""),
			"could not find the PID of the mender daemon",
		},
		"return PID": {
			exec.Command("echo", "MainPID=123"),
			"123",
		},
	}
	for name, test := range tests {
		pid, err := getMenderDaemonPID(test.cmd)
		if err != nil && test.expected != "" {
			assert.Contains(t, err.Error(), test.expected, name)
		}
		if pid != "" {
			assert.Equal(t, test.expected, pid, name)
		}
	}
	cmdKill := exec.Command("abc")
	cmdPID := exec.Command("echo", "123")
	assert.Error(t, updateCheck(cmdKill, cmdPID))
}

// Minimal init
func TestInitDaemon(t *testing.T) {
	// create directory for storing deployments logs
	ctx := NewTestContext(t)
	tempDir, _ := ioutil.TempDir("", "logs")
	defer os.RemoveAll(tempDir)
	app.DeploymentLogger = app.NewDeploymentLogManager(tempDir)
	dualRootfs := installer.NewDualRootfsDevice(nil, nil, installer.DualRootfsDeviceConfig{})
	ctx.Set("data", tempDir)
	d, err := initDaemon(ctx, &conf.MenderConfig{}, dualRootfs)
	require.Nil(t, err)
	assert.NotNil(t, d)
	// Test with failing init daemon
	ctx.Command = &cli.Command{Name: "daemon", InheritParentFlags: true}
	ctx.Set("log-level", "info")
	assert.Error(t, handleCLIOptions(ctx))
}

// Tests that the client will boot with an error message in the case of an invalid server certificate.
func TestInvalidServerCertificateBoot(t *testing.T) {
	tdir, err := ioutil.TempDir("", "invalidcert-test")
	require.Nil(t, err)

	ctx := NewTestContext(t)

	logBuf := bytes.NewBuffer(nil)
	defer func(oldLog *log.Logger) { log.Log = oldLog }(log.Log) // Restore standard logger
	log.Log = log.New()
	log.SetLevel(log.WarnLevel)
	log.SetOutput(logBuf)
	mconf := conf.MenderConfig{
		MenderConfigFromFile: conf.MenderConfigFromFile{
			ServerCertificate: "/some/invalid/cert.crt",
		},
	}
	ctx.Set("data", tdir)
	_, err = initDaemon(ctx, &mconf, nil)

	assert.NoError(t, err, "initDaemon returned an unexpected error")

	assert.Contains(t, logBuf.String(), "IGNORING ERROR")
}

func NewTestContext(t *testing.T) *cli.Context {
	app := &cli.App{
		Name: t.Name(),
		Flags: []*cli.Flag{
			{
				Name:    "config",
				Default: conf.DefaultConfFile},
			{
				Name:    "fallback-config",
				Default: conf.DefaultFallbackConfFile},
			{
				Name:    "data",
				Default: conf.DefaultDataStore},
			{
				Name: "log-file"},
			{
				Name:    "log-level",
				Default: "info",
				Choices: []string{"debug", "info", "warn",
					"error", "fatal", "panic"}},
			{
				Name: "log-modules"},
			{
				Name: "trusted-certs"},
			{
				Name: "forcebootstrap", Type: cli.Bool},
			{
				Name: "no-syslog", Type: cli.Bool},
			{
				Name: "skipverify", Type: cli.Bool},
			{
				Name: "version", Type: cli.Bool},
		},
	}

	ctx, err := cli.NewContext(app, nil, nil)
	assert.NoError(t, err)
	return ctx
}
