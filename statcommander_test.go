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
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strconv"
	"testing"
)

// The test runner, which simulates output and return code.
type testOSCalls struct {
	output  string
	retCode int
	*exec.Cmd
	file os.FileInfo
	err  error
}

func newTestOSCalls(output string, ret int) testOSCalls {
	tc := testOSCalls{}
	tc.output = output
	tc.retCode = ret

	return tc
}

func (sc testOSCalls) Stat(name string) (os.FileInfo, error) {
	return sc.file, sc.err
}

func (sc *testOSCalls) Command(command string, args ...string) *exec.Cmd {
	subArgs := []string{"-test.run=TestHelperProcessSuccess", "--"}

	//append helper process return code converted to string and return message
	subArgs = append(subArgs, strconv.Itoa(sc.retCode), sc.output)

	cmd := exec.Command(os.Args[0], subArgs...)
	cmd.Env = []string{"NEED_MENDER_TEST_HELPER_PROCESS=1"}

	return cmd
}

func TestHelperProcessSuccess(t *testing.T) {
	if os.Getenv("NEED_MENDER_TEST_HELPER_PROCESS") != "1" {
		return
	}

	// Drain input if there is any.
	_, _ = ioutil.ReadAll(os.Stdin)

	//set helper process return code
	i, err := strconv.Atoi(os.Args[3])
	if err != nil {
		defer os.Exit(1)
	} else {
		defer os.Exit(i)
	}

	//check if we have something to print
	if len(os.Args) == 5 && os.Args[4] != "" {
		fmt.Println(os.Args[4])
	}

	os.Exit(i)
}
