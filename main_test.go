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
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/mendersoftware/log"
	"github.com/stretchr/testify/assert"
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

func TestLoggingOptions(t *testing.T) {
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

	expected := fmt.Sprintf("%s\n", VersionString())
	assert.Equal(t, expected, string(data),
		"unexpected version output '%s' expected '%s'", string(data), expected)
}
