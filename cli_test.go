// Copyright 2019 Northern.tech AS
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

	"github.com/mendersoftware/log"
	"github.com/mendersoftware/mender/client"
	"github.com/mendersoftware/mender/datastore"
	"github.com/mendersoftware/mender/installer"
	"github.com/mendersoftware/mender/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli"
)

func init() {
	defaultConfFile = "mender-default-test.conf"
}

//func TestMissingArgs(t *testing.T) {
//	err := setupCLI([]string{"mender", "-config", "mender.conf.production"})
//	assert.Error(t, err, "calling setupCLI() with no arguments should produce an error")
//	//assert.Contains(t, err.Error(), errMsgNoArgumentsGiven.Error())
//}

func TestAmbiguousArgumentsArgs(t *testing.T) {
	err := setupCLI([]string{"mender", "-daemon", "-commit"})
	assert.Error(t, err)
	assert.Equal(t, fmt.Sprintf(errMsgAmbiguousArgumentsGiven, "commit"),
		err.Error())
}

//func TestArgsParseCheckUpdate(t *testing.T) {
//	args := []string{"-check-update"}
//	runOpts, err := argsParse(args)
//	assert.NoError(t, err)
//	assert.Equal(t, true, *runOpts.updateCheck)
//}

func TestRunDaemon(t *testing.T) {
	// create directory for storing deployments logs
	tempDir, _ := ioutil.TempDir("", "logs")
	defer os.RemoveAll(tempDir)
	DeploymentLogger = NewDeploymentLogManager(tempDir)
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

	for name, test := range tests {
		mender := newDefaultTestMender()
		td := &menderDaemon{
			mender: mender,
			sctx: StateContext{
				store:      ds,
				wakeupChan: make(chan bool, 1),
			},
			store:        ds,
			forceToState: make(chan State, 1),
		}
		go func() {
			err := runDaemon(td)
			require.Nil(t, err, "Daemon returned with an error code")
		}()

		for td.mender.GetCurrentState() != authorizeWaitState {
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
	err := setupCLI([]string{"mender", "-log-level", "crap", "-commit"})
	assert.Error(t, err, "'crap' log level should have given error")
	// Should have a reference to log level.
	assert.Contains(t, err.Error(), "Level")

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
	setupCLI([]string{"mender", "-log-level", "panic"})
	log.Debugln("Should not show")
	setupCLI([]string{"mender", "-debug"})
	log.Debugln("Should show")
	setupCLI([]string{"mender", "-info"})
	log.Debugln("Should also not show")

	logdata := buf.String()
	assert.Contains(t, logdata, "Should show")
	assert.NotContains(t, logdata, "Should not show")
	assert.NotContains(t, logdata, "Should also not show")

	setupCLI([]string{"mender", "-log-modules", "cli_test,MyModule"})
	log.Errorln("Module filter should show cli_test")
	log.PushModule("MyModule")
	log.Errorln("Module filter should show MyModule")
	log.PushModule("MyOtherModule")
	log.Errorln("Module filter should not show MyOtherModule")
	log.PopModule()
	log.PopModule()

	assert.True(t, strings.Index(buf.String(),
		"Module filter should show cli_test") >= 0)
	assert.True(t, strings.Index(buf.String(),
		"Module filter should show MyModule") >= 0)
	assert.True(t, strings.Index(buf.String(),
		"Module filter should not show MyOtherModule") < 0)

	defer os.Remove("test.log")
	setupCLI([]string{"mender", "-log-file", "test.log"})
	log.Errorln("Should be in log file")
	fd, err := os.Open("test.log")
	assert.NoError(t, err)

	var bytebuf [4096]byte
	n, err := fd.Read(bytebuf[:])
	assert.True(t, err == nil)
	assert.True(t, strings.Index(string(bytebuf[0:n]),
		"Should be in log file") >= 0)

	err = setupCLI([]string{"mender", "-no-syslog"})
	// Just check that the flag can be specified.
	assert.True(t, err == nil)
	assert.True(t, strings.Index(buf.String(), "syslog") < 0)
}

func TestVersion(t *testing.T) {
	oldstdout := os.Stdout

	tfile, err := ioutil.TempFile("", "mendertest")
	assert.NoError(t, err)
	tname := tfile.Name()

	// pretend we're stdout now
	os.Stdout = tfile

	// running with stderr pointing to temp file
	err = setupCLI([]string{"mender", "-version"})

	// restore previous stderr
	os.Stdout = oldstdout
	assert.NoError(t, err, "calling main with -version should not produce an error")

	// rewind
	tfile.Seek(0, 0)
	data, _ := ioutil.ReadAll(tfile)
	tfile.Close()
	os.Remove(tname)

	expected := fmt.Sprintf("%s\truntime: %s\n", VersionString(), runtime.Version())
	assert.Equal(t, expected, string(data),
		"unexpected version output '%s' expected '%s'", string(data), expected)
}

func writeConfig(t *testing.T, path string, conf menderConfig) {
	cf, err := os.Create(path)
	assert.NoError(t, err)
	defer cf.Close()

	d, _ := json.Marshal(conf)

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
	defer os.RemoveAll(tdir)

	// setup a dirstore helper to easily access file contents in test dir
	ds := store.NewDirStore(tdir)
	assert.NotNil(t, ds)

	db := store.NewDBStore(tdir)
	defer db.Close()
	assert.NotNil(t, db)

	// setup test config
	cpath := path.Join(tdir, "mender.config")
	writeConfig(t, cpath, menderConfig{
		menderSysConfig: menderSysConfig{
			Servers: []client.MenderServer{{ServerURL: ts.URL}},
		},
	})

	// override identity helper script
	oldidh := identityDataHelper
	defer func(old string) {
		identityDataHelper = old
	}(oldidh)

	newidh := path.Join(tdir, "fakehelper")
	writeFakeIdentityHelper(t, newidh,
		`#!/bin/sh
echo mac=00:11:22:33:44:55
`)
	identityDataHelper = newidh

	// run bootstrap
	db.Remove(datastore.AuthTokenName)
	err = setupCLI([]string{"mender", "-data", tdir, "-config", cpath, "-debug", "-bootstrap"})
	assert.NoError(t, err)

	// should have generated a key
	keyold, err := ds.ReadAll(defaultKeyFile)
	assert.NoError(t, err)
	assert.NotEmpty(t, keyold)

	// and we should have a token
	d, err := db.ReadAll(datastore.AuthTokenName)
	assert.NoError(t, err)
	assert.Equal(t, []byte("foobar-token"), d)

	// force boostrap and run again, check if key was changed
	db.Remove(datastore.AuthTokenName)
	err = setupCLI([]string{"mender", "-data", tdir, "-config", cpath, "-debug", "-bootstrap", "-forcebootstrap"})
	assert.NoError(t, err)

	keynew, err := ds.ReadAll(defaultKeyFile)
	assert.NoError(t, err)
	assert.NotEmpty(t, keynew)
	assert.NotEqual(t, keyold, keynew)

	db.Remove(datastore.AuthTokenName)

	// return non 200 status code, we should get an error as authorization has
	// failed
	responder.httpStatus = http.StatusUnauthorized
	err = setupCLI([]string{"mender", "-data", tdir, "-config", cpath, "-debug", "-bootstrap", "-forcebootstrap"})
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

	config := &menderConfig{
		ArtifactInfoFile: tfile.Name(),
	}
	deviceManager := NewDeviceManager(nil, config, dbstore)

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
	tempDir, _ := ioutil.TempDir("", "logs")
	defer os.RemoveAll(tempDir)
	DeploymentLogger = NewDeploymentLogManager(tempDir)
	bootstrap := false
	dualRootfs := installer.NewDualRootfsDevice(nil, nil, installer.DualRootfsDeviceConfig{})
	d, err := initDaemon(&menderConfig{}, dualRootfs, &installer.UBootEnv{},
		&runOptionsType{dataStore: tempDir, bootstrapForce: bootstrap})
	require.Nil(t, err)
	assert.NotNil(t, d)
	//	// Test with failing init daemon
	ctx := cli.Context{
		App: &cli.App{},
		Command: cli.Command{
			Name: "daemon"},
	}
	runOpts := runOptionsType{
		logOptions: logOptionsType{logLevel: "info"},
	}
	assert.Error(t, runOpts.handleCLIOptions(&ctx))
}

// Tests that the client will boot with an error message in the case of an invalid server certificate.
func TestInvalidServerCertificateBoot(t *testing.T) {
	tdir, err := ioutil.TempDir("", "invalidcert-test")
	require.Nil(t, err)

	logBuf := bytes.NewBuffer(nil)
	defer func(oldLog *log.Logger) { log.Log = oldLog }(log.Log) // Restore standard logger
	log.Log = log.New()
	log.SetLevel(log.WarnLevel)
	log.SetOutput(logBuf)
	mconf := menderConfig{
		menderSysConfig: menderSysConfig{
			ServerCertificate: "/some/invalid/cert.crt",
		},
	}
	_, err = initDaemon(&mconf, nil, &installer.UBootEnv{},
		&runOptionsType{dataStore: tdir, bootstrapForce: false})

	assert.NoError(t, err, "initDaemon returned an unexpected error")

	assert.Contains(t, logBuf.String(), "IGNORING ERROR")
}
