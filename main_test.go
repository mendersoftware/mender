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

import "bytes"
import "os/exec"
import "github.com/mendersoftware/mender/internal/log"
import mt "github.com/mendersoftware/mender/internal/mendertesting"
import "os"
import "strings"
import "testing"

func TestMissingArgs(t *testing.T) {
	if err := doMain([]string{}); err == nil {
		t.Fatal("Calling doMain() with no arguments does not " +
			"produce error")
	} else {
		mt.AssertErrorSubstring(t, err, errMsgNoArgumentsGiven.Error())
	}
}

func TestInvalidArgs(t *testing.T) {
	if err := doMain([]string{"-crap"}); err == nil {
		t.Fatal("Calling doMain() with wrong arguments does not " +
			"produce error")
	}
}

func TestLoggingOptions(t *testing.T) {
	if err := doMain([]string{"-commit", "-log-level", "crap"}); err == nil {
		t.Fatal("'crap' log level should have given error")
	} else {
		// Should have a reference to log level.
		mt.AssertErrorSubstring(t, err, "Level")
	}

	if err := doMain([]string{"-info", "-log-level", "debug"}); err == nil {
		t.Fatal("Incompatible log levels should have given error")
	} else {
		mt.AssertErrorSubstring(t, err,
			errMsgIncompatibleLogOptions.Error())
	}

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

	mt.AssertTrue(t, strings.Index(buf.String(), "Should show") >= 0)
	mt.AssertTrue(t, strings.Index(buf.String(), "Should not show") < 0)
	mt.AssertTrue(t, strings.Index(buf.String(), "Should also not show") < 0)

	doMain([]string{"-log-modules", "main_test,MyModule"})
	log.Errorln("Module filter should show main_test")
	log.PushModule("MyModule")
	log.Errorln("Module filter should show MyModule")
	log.PushModule("MyOtherModule")
	log.Errorln("Module filter should not show MyOtherModule")
	log.PopModule()
	log.PopModule()

	mt.AssertTrue(t, strings.Index(buf.String(),
		"Module filter should show main_test") >= 0)
	mt.AssertTrue(t, strings.Index(buf.String(),
		"Module filter should show MyModule") >= 0)
	mt.AssertTrue(t, strings.Index(buf.String(),
		"Module filter should not show MyOtherModule") < 0)

	doMain([]string{"-log-file", "test.log"})
	log.Errorln("Should be in log file")
	fd, err := os.Open("test.log")
	mt.AssertTrue(t, err == nil)

	var bytebuf [4096]byte
	n, err := fd.Read(bytebuf[:])
	mt.AssertTrue(t, err == nil)
	mt.AssertTrue(t, strings.Index(string(bytebuf[0:n]),
		"Should be in log file") >= 0)

	err = doMain([]string{"-no-syslog"})
	// Just check that the flag can be specified.
	mt.AssertTrue(t, err != nil)
	mt.AssertTrue(t, strings.Index(err.Error(), "syslog") < 0)
}

func TestBinarySize(t *testing.T) {
	// Test that the binary does not unexpectedly increase a lot in size,
	// this is intended to protect against introducing very large
	// dependencies. It is perfectly okay to increase this number as the
	// program grows, but the binary size should be verified before doing
	// so.
	//
	// When increasing, use current binary size on amd64 + 1M.
	const maxSize int64 = 5500000
	var programName string = "mender"
	var built bool = false

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
