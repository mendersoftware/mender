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
//go:build !nodbus && cgo
// +build !nodbus,cgo

package dbus

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// We need to start our own DBus server to avoid the need for a session DBus
// server to already be running.
func TestMain(m *testing.M) {
	libgioTestSetup()

	tmpdir, err := ioutil.TempDir("", "mender-test-dbus-daemon")
	if err != err {
		panic(fmt.Sprintf("Could not create temporary directory: %s", err.Error()))
	}
	defer os.RemoveAll(tmpdir)

	busAddr := fmt.Sprintf("unix:path=%s/bus", tmpdir)

	cmd := exec.Command("dbus-daemon", "--session", "--address="+busAddr)
	err = cmd.Start()
	if err != nil {
		panic(fmt.Sprintf("Could not start test DBus server: %s", err.Error()))
	}
	defer func() {
		cmd.Process.Signal(syscall.SIGTERM)
		err := cmd.Wait()
		if err != nil {
			fmt.Printf("DBus test server returned error: %s\n", err.Error())
		}
	}()

	const maxAttempts = 10
	var attempt int
	for attempt = 0; attempt < 10; attempt++ {
		// Give a chance to get up and running.
		testCmd := exec.Command("dbus-send", "--print-reply", "--address="+busAddr,
			"--dest=io.mender.AuthenticationManager", "/io/mender/AuthenticationManager",
			"io.mender.Authentication1.GetJwtToken")
		output, _ := testCmd.CombinedOutput()
		if err == nil && strings.Index(string(output), "org.freedesktop.DBus.Error.NoReply") >= 0 {
			// Server is up, but does not respond to endpoint
			// yet. This is natural, since Mender has not been
			// started yet.
			break
		}
		time.Sleep(time.Second)
	}
	if attempt >= maxAttempts {
		panic("Unable to connect to dbus-daemon after starting it")
	}

	oldEnv := os.Getenv("DBUS_SESSION_BUS_ADDRESS")
	os.Setenv("DBUS_SESSION_BUS_ADDRESS", busAddr)
	defer os.Setenv("DBUS_SESSION_BUS_ADDRESS", oldEnv)

	m.Run()
}

func TestGetDBusAPI(t *testing.T) {
	api, err := GetDBusAPI()
	assert.NoError(t, err)
	assert.NotNil(t, api)
}
