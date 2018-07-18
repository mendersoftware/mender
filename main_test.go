// Copyright 2018 Northern.tech AS
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
	"testing"
	"time"

	"github.com/mendersoftware/log"
	"github.com/mendersoftware/mender/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	defaultConfFile = "mender-default-test.conf"
}

func TestMissingArgs(t *testing.T) {
	err := doMain([]string{"-config", "mender.conf.example"})
	assert.Error(t, err, "calling doMain() with no arguments should produce an error")
	assert.Contains(t, err.Error(), errMsgNoArgumentsGiven.Error())
}

func TestAmbiguousArgumentsArgs(t *testing.T) {
	err := doMain([]string{"-daemon", "-commit"})
	assert.Error(t, err)
	assert.Equal(t, errMsgAmbiguousArgumentsGiven, err)
}

func TestArgsParseRootfsForce(t *testing.T) {
	args := []string{"-rootfs", "newImage", "-f"}
	runOpts, err := argsParse(args)
	assert.NoError(t, err)
	assert.Equal(t, "newImage", *runOpts.imageFile)
	assert.Equal(t, true, *runOpts.runStateScripts)
}

func TestArgsParseCheckUpdate(t *testing.T) {
	args := []string{"-check-update"}
	runOpts, err := argsParse(args)
	assert.NoError(t, err)
	assert.Equal(t, true, *runOpts.updateCheck)
}

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
	mender := newDefaultTestMender()
	td := &menderDaemon{
		mender:      mender,
		sctx:        StateContext{store: ds},
		store:       ds,
		updateCheck: make(chan bool, 1),
	}
	go func() {
		time.Sleep(time.Second * 2)
		cmd := exec.Command("pkill", "--signal", "SIGUSR1", "mender")
		require.Nil(t, cmd.Run())
		t.Log("signal sent")
	}()
	go func() {
		err := runDaemon(td)
		if err != nil {
			buf.WriteString("FUCK!")
		}
	}()
	// Give the client some time to settle in the authorizeWaitState.
	time.Sleep(time.Second * 3)
	td.StopDaemon()
	assert.Contains(t, buf.String(), "forced wake-up from sleep", "daemon was not forced from sleep")
}

func TestLoggingOptionsFoo(t *testing.T) {
	err := doMain([]string{"-commit", "-log-level", "crap"})
	assert.Error(t, err, "'crap' log level should have given error")
	// Should have a reference to log level.
	assert.Contains(t, err.Error(), "Level")

	err = doMain([]string{"-info", "-log-level", "debug"})
	assert.Error(t, err, "Incompatible log levels should have given error")
	assert.Contains(t, err.Error(), errMsgIncompatibleLogOptions.Error())

	var buf bytes.Buffer
	oldOutput := log.Log.Out
	log.SetOutput(&buf)
	defer log.SetOutput(oldOutput)

	// Ignore errors for now, we just want to know if the logging level was
	// applied.
	log.SetLevel(log.DebugLevel)
	doMain([]string{"-log-level", "panic"})
	log.Debugln("Should not show")
	doMain([]string{"-debug"})
	log.Debugln("Should show")
	doMain([]string{"-info"})
	log.Debugln("Should also not show")

	logdata := buf.String()
	assert.Contains(t, logdata, "Should show")
	assert.NotContains(t, logdata, "Should not show")
	assert.NotContains(t, logdata, "Should also not show")

	doMain([]string{"-log-modules", "main_test,MyModule"})
	log.Errorln("Module filter should show main_test")
	log.PushModule("MyModule")
	log.Errorln("Module filter should show MyModule")
	log.PushModule("MyOtherModule")
	log.Errorln("Module filter should not show MyOtherModule")
	log.PopModule()
	log.PopModule()

	assert.True(t, strings.Index(buf.String(),
		"Module filter should show main_test") >= 0)
	assert.True(t, strings.Index(buf.String(),
		"Module filter should show MyModule") >= 0)
	assert.True(t, strings.Index(buf.String(),
		"Module filter should not show MyOtherModule") < 0)

	defer os.Remove("test.log")
	doMain([]string{"-log-file", "test.log"})
	log.Errorln("Should be in log file")
	fd, err := os.Open("test.log")
	assert.NoError(t, err)

	var bytebuf [4096]byte
	n, err := fd.Read(bytebuf[:])
	assert.True(t, err == nil)
	assert.True(t, strings.Index(string(bytebuf[0:n]),
		"Should be in log file") >= 0)

	err = doMain([]string{"-no-syslog"})
	// Just check that the flag can be specified.
	assert.True(t, err != nil)
	assert.True(t, strings.Index(err.Error(), "syslog") < 0)
}

func TestBinarySize(t *testing.T) {
	// Test that the binary does not unexpectedly increase a lot in size,
	// this is intended to protect against introducing very large
	// dependencies. It is perfectly okay to increase this number as the
	// program grows, but the binary size should be verified before doing
	// so.
	//
	// When increasing, use current binary size on amd64 + 1M.
	const maxSize int64 = 9525080
	programName := "mender"
	built := false

	statbuf, err := os.Stat(programName)
	if os.IsNotExist(err) {
		// Try building first
		programName = "/tmp/mender"
		cmd := exec.Command("go", "build", "-o", programName)
		err = cmd.Run()
		if err != nil {
			t.Fatalf("Could not build '%s': %s",
				programName, err.Error())
		}
		built = true
		statbuf, err = os.Stat(programName)
	}

	if err != nil {
		t.Fatalf("Could not stat '%s': %s. Please build before "+
			"testing.", programName, err.Error())
	}

	if statbuf.Size() > maxSize {
		t.Fatalf("'%s' has grown unexpectedly big (%d bytes). "+
			"Check that file size is ok?", programName,
			statbuf.Size())
	}

	if built {
		os.Remove(programName)
	}
}

func TestVersion(t *testing.T) {
	oldstdout := os.Stdout

	tfile, err := ioutil.TempFile("", "mendertest")
	assert.NoError(t, err)
	tname := tfile.Name()

	// pretend we're stdout now
	os.Stdout = tfile

	// running with stderr pointing to temp file
	err = doMain([]string{"-version"})

	// restore previous stderr
	os.Stdout = oldstdout
	assert.NoError(t, err, "calling main with -version should not produce an error")

	// rewind
	tfile.Seek(0, 0)
	data, _ := ioutil.ReadAll(tfile)
	tfile.Close()
	os.Remove(tname)

	expected := fmt.Sprintf("%s\nruntime: %s\n", VersionString(), runtime.Version())
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
		ServerURL: ts.URL,
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
	db.Remove(authTokenName)
	err = doMain([]string{"-data", tdir, "-config", cpath, "-debug", "-bootstrap"})
	assert.NoError(t, err)

	// should have generated a key
	keyold, err := ds.ReadAll(defaultKeyFile)
	assert.NoError(t, err)
	assert.NotEmpty(t, keyold)

	// and we should have a token
	d, err := db.ReadAll(authTokenName)
	assert.NoError(t, err)
	assert.Equal(t, []byte("foobar-token"), d)

	// force boostrap and run again, check if key was changed
	db.Remove(authTokenName)
	err = doMain([]string{"-data", tdir, "-config", cpath, "-debug", "-bootstrap", "-forcebootstrap"})
	assert.NoError(t, err)

	keynew, err := ds.ReadAll(defaultKeyFile)
	assert.NoError(t, err)
	assert.NotEmpty(t, keynew)
	assert.NotEqual(t, keyold, keynew)

	db.Remove(authTokenName)

	// return non 200 status code, we should get an error as authorization has
	// failed
	responder.httpStatus = http.StatusUnauthorized
	err = doMain([]string{"-data", tdir, "-config", cpath, "-debug", "-bootstrap", "-forcebootstrap"})
	assert.Error(t, err)

	_, err = db.ReadAll(authTokenName)
	assert.Error(t, err)
	assert.True(t, os.IsNotExist(err))

}

func TestPrintArtifactName(t *testing.T) {

	tfile, err := ioutil.TempFile("", "artinfo")
	assert.Nil(t, err)
	// no error
	_, err = io.WriteString(tfile, "artifact_name=foobar")
	assert.Nil(t, err)
	assert.Nil(t, PrintArtifactName(tfile.Name()))

	// empty artifact_name should fail
	err = ioutil.WriteFile(tfile.Name(), []byte("artifact_name="), 0644)
	//overwrite file contents
	assert.Nil(t, err)

	assert.EqualError(t, PrintArtifactName(tfile.Name()), "the artifact_name is empty. Please set a valid name for the artifact")

	// two artifact_names is also an error
	err = ioutil.WriteFile(tfile.Name(), []byte(fmt.Sprint("artifact_name=a\ninfo=i\nartifact_name=b\n")), 0644)
	assert.Nil(t, err)

	expected := "there seems to be two instances of artifact_name in the artifact info file (located at /etc/mender/artifact_info). Please remove the duplicate(s)"
	assert.EqualError(t, PrintArtifactName(tfile.Name()), expected)

	// wrong formatting of the artifact_info file
	err = ioutil.WriteFile(tfile.Name(), []byte("artifact_name=foo=bar"), 0644)
	assert.Nil(t, err)

	assert.EqualError(t, PrintArtifactName(tfile.Name()), "Wrong formatting of the artifact_info file")

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
	d, err := initDaemon(&menderConfig{}, &device{}, &uBootEnv{}, &runOptionsType{dataStore: &tempDir, bootstrapForce: &bootstrap})
	require.Nil(t, err)
	assert.NotNil(t, d)
	// Test with failing init daemon
	runOpts, err := argsParse([]string{"-daemon"})
	require.Nil(t, err)
	assert.Error(t, handleCLIOptions(runOpts, &uBootEnv{}, &device{}, &menderConfig{}))
}
