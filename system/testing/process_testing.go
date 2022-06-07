// Copyright 2022 Northern.tech AS
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
package testing

import (
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This won't run automatically because of the filename of this file, but it is
// exported and tests from other packages are meant to call it directly.
func TestZombieProcessLeaking(t *testing.T) {
	// Test that we don't leave behind any zombies after a test run.

	// Perform a sanity check first. Make a zombie process so that we can
	// verify that our detection works.
	cmd := exec.Command("true")
	require.NoError(t, cmd.Start())
	require.Eventually(t, func() bool {
		_, _, result := zombieProcessesPresent()
		return result
	},
		1*time.Second,
		100*time.Millisecond,
		"Zombie detection doesn't work. Perhaps ps works differently on this platform?",
	)
	require.NoError(t, cmd.Wait())

	// This is not primarily to enable parallalism, but to move the test to
	// the end of the run, since Go executes all parallel tests at the
	// end. This doesn't guarantee that it's the last one though, since we
	// can execute at the same time as the others.
	t.Parallel()

	var output string
	var err error
	// Use Eventually(), since we aren't guaranteed to be the last test.
	assert.Eventuallyf(t, func() bool {
		var result bool
		output, err, result = zombieProcessesPresent()
		return !result
	},
		60*time.Second,
		1*time.Second,
		"Zombie processes are still present! Missing Wait()?",
	)
	if t.Failed() {
		t.Logf("ps output: %s, error: %v", output, err)
	}
}

func zombieProcessesPresent() (string, error, bool) {
	cmdStr := fmt.Sprintf("ps -ef | grep %d | grep '<defunct>' | grep -v grep", os.Getpid())
	cmd := exec.Command("/bin/sh", "-c", cmdStr)
	output, err := cmd.Output()
	if err == nil {
		return string(output), err, true
	} else {
		return string(output), err, false
	}
}
