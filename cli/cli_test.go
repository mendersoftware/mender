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
	"os/signal"
	"path"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mendersoftware/mender/app"
	"github.com/mendersoftware/mender/client"
	"github.com/mendersoftware/mender/conf"
	"github.com/mendersoftware/mender/datastore"
	dev "github.com/mendersoftware/mender/device"
	"github.com/mendersoftware/mender/installer"
	"github.com/mendersoftware/mender/store"
	"github.com/mendersoftware/mender/system"
	stest "github.com/mendersoftware/mender/system/testing"
	log "github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const defaultKeyPassphrase = ""

func init() {
	conf.DefaultConfFile = "mender-default-test.conf"
}

func TestAmbiguousArgumentsArgs(t *testing.T) {
	err := SetupCLI([]string{"mender", "daemon", "commit"})
	assert.Error(t, err)
	assert.Equal(t, fmt.Sprintf(errMsgAmbiguousArgumentsGivenF, "commit"),
		err.Error())
}

func TestCheckUpdate(t *testing.T) {
	args := []string{"mender", "check-update"}
	err := SetupCLI(args)
	// Should produce an error since daemon is not running
	assert.Error(t, err)
}

func TestSendInventory(t *testing.T) {
	args := []string{"mender", "send-inventory"}
	err := SetupCLI(args)
	// Should produce an error since daemon is not running
	assert.Error(t, err)
}

func testLogContainsMessage(entries []*log.Entry, msg string) bool {
	for _, entry := range entries {
		if strings.Contains(entry.Message, msg) {
			return true
		}
	}
	return false
}

func TestRunDaemon(t *testing.T) {
	// create directory for storing deployments logs
	tempDir, _ := ioutil.TempDir("", "logs")
	app.DeploymentLogger = app.NewDeploymentLogManager(tempDir)
	defer func() {
		app.DeploymentLogger = nil
		os.RemoveAll(tempDir)
	}()
	var hook = logtest.NewGlobal() // Install a global test hook
	defer hook.Reset()
	log.SetLevel(log.DebugLevel)
	ds := store.NewMemStore()

	tests := map[string]struct {
		signal syscall.Signal
	}{
		"check-update": {
			signal: syscall.SIGUSR1,
		},
		"send-inventory": {
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

	pieces.AuthManager = app.NewAuthManager(app.AuthManagerConfig{
		AuthDataStore: pieces.Store,
		KeyStore:      store.NewKeystore(pieces.Store, conf.DefaultKeyFile, "", false, defaultKeyPassphrase),
		IdentitySource: &dev.IdentityDataRunner{
			Cmdr: stest.NewTestOSCalls("mac=foobar", 0),
		},
		Config: &config,
	})

	for name, test := range tests {
		mender, err := app.NewMender(&config, pieces)
		assert.NoError(t, err)
		td := &app.MenderDaemon{
			Mender:      mender,
			AuthManager: pieces.AuthManager,
			Sctx: app.StateContext{
				Store:      ds,
				WakeupChan: make(chan bool, 1),
			},
			Store:        ds,
			ForceToState: make(chan app.State, 1),
		}
		go func() {
			SignalHandlerChan = make(chan os.Signal, 2)
			signal.Notify(SignalHandlerChan, syscall.SIGUSR1, syscall.SIGUSR2, syscall.SIGTERM)
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
		assert.True(t, testLogContainsMessage(hook.AllEntries(), "Forced wake-up"), name+" signal did not force daemon from sleep")

	}
}

func TestLoggingOptions(t *testing.T) {
	err := SetupCLI([]string{"mender", "--log-level", "crap", "commit"})
	assert.Error(t, err, "'crap' log level should have given error")
	// Should have a reference to log level.
	assert.Contains(t, err.Error(), "Level")

	var hook = logtest.NewGlobal() // Install a global test hook
	defer hook.Reset()

	// Ignore errors for now, we just want to know if the logging level was
	// applied.
	log.SetLevel(log.DebugLevel)
	SetupCLI([]string{"mender", "--log-level", "panic"})
	log.Debugln("Should not show")
	assert.False(t, testLogContainsMessage(hook.AllEntries(), "Should not show"))
	SetupCLI([]string{"mender", "--log-level", "debug"})
	log.Debugln("Should show")
	assert.True(t, testLogContainsMessage(hook.AllEntries(), "Should show"))
	SetupCLI([]string{"mender", "--log-level", "info"})
	log.Debugln("Should also not show")
	assert.False(t, testLogContainsMessage(hook.AllEntries(), "Should also not show"))

	defer os.Remove("test.log")
	SetupCLI([]string{"mender", "--log-file", "test.log"})
	log.Errorln("Should be in log file")
	fd, err := os.Open("test.log")
	assert.NoError(t, err)

	var bytebuf [4096]byte
	n, err := fd.Read(bytebuf[:])
	assert.True(t, err == nil)
	assert.True(t, strings.Contains(string(bytebuf[0:n]),
		"Should be in log file"))

	err = SetupCLI([]string{"mender", "--no-syslog"})
	// Just check that the flag can be specified.
	assert.True(t, err == nil)
}

func TestVersion(t *testing.T) {
	oldstdout := os.Stdout

	tfile, err := ioutil.TempFile("", "mendertest")
	assert.NoError(t, err)
	tname := tfile.Name()

	// pretend we're stdout now
	os.Stdout = tfile

	// running with stdout pointing to temp file
	err = SetupCLI([]string{"mender", "--version"})

	// restore previous stdout
	os.Stdout = oldstdout
	assert.NoError(t, err, "calling main with --version should not produce an error")

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
			DBus: conf.DBusConfig{
				Enabled: true,
			},
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
	err = SetupCLI([]string{"mender", "--data", tdir, "--config", cpath,
		"--log-level", "debug", "bootstrap"})
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
	err = SetupCLI([]string{"mender", "--data", tdir, "--config", cpath,
		"--log-level", "debug", "bootstrap", "--forcebootstrap"})
	assert.NoError(t, err)

	keynew, err := ds.ReadAll(conf.DefaultKeyFile)
	assert.NoError(t, err)
	assert.NotEmpty(t, keynew)
	assert.NotEqual(t, keyold, keynew)

	db.Remove(datastore.AuthTokenName)

	// return non 200 status code, we should get an error as authorization has
	// failed
	responder.httpStatus = http.StatusUnauthorized
	err = SetupCLI([]string{"mender", "--data", tdir, "--config", cpath,
		"-debug", "bootstrap", "--forcebootstrap"})
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

	out = bytes.NewBuffer(nil)
	assert.Nil(t, PrintArtifactName(deviceManager))

	name, err := deviceManager.GetCurrentArtifactName()
	require.NoError(t, err)
	assert.Equal(t, "foobar", name)

	output := out.(*bytes.Buffer).String()
	assert.Equal(t, name+"\n", output)

	// DB should override file.
	dbstore.WriteAll(datastore.ArtifactNameKey, []byte("db-name"))

	out = bytes.NewBuffer(nil)
	assert.Nil(t, PrintArtifactName(deviceManager))

	name, err = deviceManager.GetCurrentArtifactName()
	require.NoError(t, err)
	assert.Equal(t, "db-name", name)

	output = out.(*bytes.Buffer).String()
	assert.Equal(t, name+"\n", output)

	// Erasing it should restore old.
	dbstore.Remove(datastore.ArtifactNameKey)

	out = bytes.NewBuffer(nil)
	assert.Nil(t, PrintArtifactName(deviceManager))

	name, err = deviceManager.GetCurrentArtifactName()
	require.NoError(t, err)
	assert.Equal(t, "foobar", name)

	output = out.(*bytes.Buffer).String()
	assert.Equal(t, name+"\n", output)

	// empty artifact_name should fail
	err = ioutil.WriteFile(tfile.Name(), []byte("artifact_name="), 0644)
	require.NoError(t, err)
	assert.EqualError(t, PrintArtifactName(deviceManager), errArtifactNameEmpty.Error())

	// two artifact_names is also an error
	err = ioutil.WriteFile(tfile.Name(), []byte(fmt.Sprint("artifact_name=a\ninfo=i\nartifact_name=b\n")), 0644)
	require.NoError(t, err)

	expected := "More than one instance of artifact_name found in manifest file"
	err = PrintArtifactName(deviceManager)
	require.Error(t, err)
	assert.Contains(t, err.Error(), expected)
}

func TestPrintProvides(t *testing.T) {
	bak := out
	defer func() { out = bak }()

	tmpdir, err := ioutil.TempDir("", "PrintProvides")
	require.NoError(t, err)
	defer os.RemoveAll(tmpdir)

	require.NoError(t, os.MkdirAll(path.Join(tmpdir, "data"), 0755))
	dbstore := store.NewDBStore(path.Join(tmpdir, "data"))

	config := &conf.MenderConfig{}
	deviceManager := dev.NewDeviceManager(nil, config, dbstore)

	// artifact name
	dbstore.WriteAll(datastore.ArtifactNameKey, []byte("name"))

	out = bytes.NewBuffer(nil)
	assert.Nil(t, PrintProvides(deviceManager))
	output := out.(*bytes.Buffer).String()
	assert.Equal(t, "artifact_name=name\n", output)

	// artifact name and group
	dbstore.WriteAll(datastore.ArtifactNameKey, []byte("name"))
	dbstore.WriteAll(datastore.ArtifactGroupKey, []byte("group"))

	out = bytes.NewBuffer(nil)
	assert.Nil(t, PrintProvides(deviceManager))
	output = out.(*bytes.Buffer).String()
	assert.Equal(t, "artifact_group=group\nartifact_name=name\n", output)

	// artifact name and group, provides
	dbstore.WriteAll(datastore.ArtifactNameKey, []byte("name"))
	dbstore.WriteAll(datastore.ArtifactGroupKey, []byte("group"))

	typeInfoProvides := map[string]interface{}{"testKey": "testValue"}
	typeInfoProvidesBuf, err := json.Marshal(typeInfoProvides)
	assert.NoError(t, err)
	dbstore.WriteAll(datastore.ArtifactTypeInfoProvidesKey, typeInfoProvidesBuf)

	out = bytes.NewBuffer(nil)
	assert.Nil(t, PrintProvides(deviceManager))
	output = out.(*bytes.Buffer).String()
	assert.Equal(t, "artifact_group=group\nartifact_name=name\ntestKey=testValue\n", output)
}

func TestGetMenderDaemonPID(t *testing.T) {
	tests := map[string]struct {
		cmd      *system.Cmd
		expected string
	}{
		"error": {
			system.Command("abc"),
			"getMenderDaemonPID: Failed to run systemctl",
		},
		"error: no output": {
			system.Command("printf", ""),
			"could not find the PID of the mender daemon",
		},
		"return PID": {
			system.Command("echo", "MainPID=123"),
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
	cmdKill := system.Command("abc")
	cmdPID := system.Command("echo", "123")
	assert.Error(t, sendSignalToProcess(cmdKill, cmdPID))
}

// Tests that the client will boot with an error message in the case of an invalid server certificate.
func TestInvalidServerCertificateBoot(t *testing.T) {
	tdir, err := ioutil.TempDir("", "invalidcert-test")
	require.Nil(t, err)

	var hook = logtest.NewGlobal()
	defer hook.Reset()
	log.SetLevel(log.WarnLevel)
	mconf := conf.MenderConfig{
		MenderConfigFromFile: conf.MenderConfigFromFile{
			ServerCertificate: "/some/invalid/cert.crt",
		},
	}
	_, err = initDaemon(&mconf, &runOptionsType{dataStore: tdir, bootstrapForce: false})

	assert.NoError(t, err, "initDaemon returned an unexpected error")

	assert.True(t, testLogContainsMessage(hook.AllEntries(), "IGNORING ERROR"))
}

func TestIgnoreServerConfigVerification(t *testing.T) {
	// Config with invalid Server fields
	config := conf.NewMenderConfig()
	config.ServerURL = ""
	config.Servers = nil

	// Save to tempdir for the test to load
	tdir, err := ioutil.TempDir("", "mendertest")
	assert.NoError(t, err)
	defer os.RemoveAll(tdir)
	cpath := path.Join(tdir, "invalid-servers.conf")
	writeConfig(t, cpath, *config)
	conf.DefaultConfFile = cpath

	// Capture warnings
	var hook = logtest.NewGlobal()
	defer hook.Reset()
	log.SetLevel(log.WarnLevel)

	// Run mender setup command (non-interactive)
	menderSetupNonInteractive := []string{
		"mender",
		"--no-syslog",
		"setup",
		"--device-type",
		"fancy-stuff",
		"--demo=false",
		"--hosted-mender",
		"--tenant-token",
		"Paste your Hosted Mender token here",
		"--update-poll",
		"1800",
		"--inventory-poll",
		"28800",
		"--retry-poll",
		"30",
	}
	conf.DefaultDataStore = tdir
	err = SetupCLI(menderSetupNonInteractive)

	// Shall succeed with no warnings
	assert.NoError(t, err)
	assert.Empty(t, hook.AllEntries())
}

func TestCliHelpText(t *testing.T) {
	oldstdout := os.Stdout
	defer func() {
		os.Stdout = oldstdout
	}()

	// tmpfile pretending to be stdout
	menderAppCliHelp, err := ioutil.TempFile("", "mendertest")
	assert.NoError(t, err)
	defer os.Remove(menderAppCliHelp.Name())
	os.Stdout = menderAppCliHelp

	// get App level help
	err = SetupCLI([]string{"mender", "-help"})
	assert.NoError(t, err, "calling main with -help should not produce an error")

	// rewind and check
	menderAppCliHelp.Seek(0, 0)
	data, _ := ioutil.ReadAll(menderAppCliHelp)
	menderAppCliHelp.Close()

	// top level App help shall contain:
	assert.Contains(t, string(data), "NAME")
	assert.Contains(t, string(data), "\nUSAGE")
	assert.Contains(t, string(data), "\nVERSION")
	assert.Contains(t, string(data), "\nDESCRIPTION")
	assert.Contains(t, string(data), "\nCOMMANDS")
	assert.NotContains(t, string(data), "\nOPTIONS")
	assert.Contains(t, string(data), "\nGLOBAL OPTIONS")

	// tmpfile pretending to be stdout
	menderCmdCliHelp, err := ioutil.TempFile("", "mendertest")
	assert.NoError(t, err)
	defer os.Remove(menderCmdCliHelp.Name())
	os.Stdout = menderCmdCliHelp

	// get command level help
	err = SetupCLI([]string{"mender", "setup", "-help"})
	assert.NoError(t, err, "calling main with -help should not produce an error")

	// rewind and check
	menderCmdCliHelp.Seek(0, 0)
	data, _ = ioutil.ReadAll(menderCmdCliHelp)
	menderCmdCliHelp.Close()

	// command help shall contain:
	assert.Contains(t, string(data), "NAME")
	assert.Contains(t, string(data), "\nUSAGE")
	assert.NotContains(t, string(data), "\nVERSION")
	assert.NotContains(t, string(data), "\nDESCRIPTION")
	assert.NotContains(t, string(data), "\nCOMMANDS")
	assert.Contains(t, string(data), "\nOPTIONS")
	assert.NotContains(t, string(data), "\nGLOBAL OPTIONS")

	// tmpfile pretending to be stdout
	menderCmdSubcommandsCliHelp, err := ioutil.TempFile("", "mendertest")
	assert.NoError(t, err)
	defer os.Remove(menderCmdSubcommandsCliHelp.Name())
	os.Stdout = menderCmdSubcommandsCliHelp

	// get "command with subcommands" level help
	err = SetupCLI([]string{"mender", "snapshot", "-help"})
	assert.NoError(t, err, "calling main with -help should not produce an error")

	// rewind and check
	menderCmdSubcommandsCliHelp.Seek(0, 0)
	data, _ = ioutil.ReadAll(menderCmdSubcommandsCliHelp)
	menderCmdSubcommandsCliHelp.Close()

	// command with subcommands help shall contain:
	assert.Contains(t, string(data), "NAME")
	assert.Contains(t, string(data), "\nUSAGE")
	assert.NotContains(t, string(data), "\nVERSION")
	assert.Contains(t, string(data), "\nDESCRIPTION")
	assert.Contains(t, string(data), "\nCOMMANDS")
	assert.Contains(t, string(data), "\nOPTIONS")
	assert.NotContains(t, string(data), "\nGLOBAL OPTIONS")
}

func TestDeprecatedArgs(t *testing.T) {
	err := SetupCLI([]string{"mender", "-install"})
	assert.Error(t, err)
	assert.Equal(t, "deprecated command \"-install\", use \"install\" instead", err.Error())

	err = SetupCLI([]string{"mender", "-commit"})
	assert.Error(t, err)
	assert.Equal(t, "deprecated command \"-commit\", use \"commit\" instead", err.Error())

	err = SetupCLI([]string{"mender", "-rollback"})
	assert.Error(t, err)
	assert.Equal(t, "deprecated command \"-rollback\", use \"rollback\" instead", err.Error())

	err = SetupCLI([]string{"mender", "-daemon"})
	assert.Error(t, err)
	assert.Equal(t, "deprecated command \"-daemon\", use \"daemon\" instead", err.Error())

	err = SetupCLI([]string{"mender", "-bootstrap"})
	assert.Error(t, err)
	assert.Equal(t, "deprecated command \"-bootstrap\", use \"bootstrap\" instead", err.Error())

	err = SetupCLI([]string{"mender", "-check-update"})
	assert.Error(t, err)
	assert.Equal(t, "deprecated command \"-check-update\", use \"check-update\" instead", err.Error())

	err = SetupCLI([]string{"mender", "-send-inventory"})
	assert.Error(t, err)
	assert.Equal(t, "deprecated command \"-send-inventory\", use \"send-inventory\" instead", err.Error())

	err = SetupCLI([]string{"mender", "-show-artifact"})
	assert.Error(t, err)
	assert.Equal(t, "deprecated command \"-show-artifact\", use \"show-artifact\" instead", err.Error())

	err = SetupCLI([]string{"mender", "-info"})
	assert.Error(t, err)
	assert.Equal(t, "deprecated flag \"-info\", use \"--log-level info\" instead", err.Error())

	err = SetupCLI([]string{"mender", "-debug"})
	assert.Error(t, err)
	assert.Equal(t, "deprecated flag \"-debug\", use \"--log-level debug\" instead", err.Error())
}
