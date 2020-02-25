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
package main

import (
	"io/ioutil"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMainExitCodes(t *testing.T) {
	// Cache args.
	args := os.Args
	// Successfull main call (0)
	os.Args = []string{"mender", "--version"}
	exitCode := doMain()
	assert.Equal(t, exitCode, 0)
	// Ambiguous arguments (1)
	os.Args = []string{"mender", "commit", "install"}
	exitCode = doMain()
	assert.Equal(t, exitCode, 1)
	// Nothing to commit (2)
	storeDir, err := ioutil.TempDir("", "temp_store")
	assert.NoError(t, err)
	os.Args = []string{"mender", "-d", storeDir, "commit"}
	exitCode = doMain()
	assert.Equal(t, exitCode, 2)
	os.Args = args
}

func TestBinarySize(t *testing.T) {
	// Test that the binary does not unexpectedly increase a lot in size,
	// this is intended to protect against introducing very large
	// dependencies. It is perfectly okay to increase this number as the
	// program grows, but the binary size should be verified before doing
	// so.
	//
	// When increasing, use current binary size on amd64 + 1M.
	const maxSize int64 = 9103464

	cmd := exec.Command("go", "version")
	version, err := cmd.CombinedOutput()
	require.NoError(t, err)
	t.Logf("Go version: %s", string(version))

	programName := "/tmp/mender"
	cmd = exec.Command("go", "build", "-o", programName)
	err = cmd.Run()
	if err != nil {
		t.Fatalf("Could not build '%s': %s",
			programName, err.Error())
	}
	defer os.Remove(programName)

	cmd = exec.Command("strip", programName)
	err = cmd.Run()
	require.NoError(t, err)

	statbuf, err := os.Stat(programName)

	if err != nil {
		t.Fatalf("Could not stat '%s': %s. Please build before "+
			"testing.", programName, err.Error())
	}

	if statbuf.Size() > maxSize {
		t.Fatalf("'%s' has grown unexpectedly big (%d bytes). "+
			"Check that file size is ok?", programName,
			statbuf.Size())
	}
}
