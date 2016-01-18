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
	"os"
	"os/exec"
	"strconv"
	"testing"
)

// The test runner, which simulates output and return code.
type testRunner struct {
	output   string
	ret_code int
}

// A slightly more advanced version, with a list of outputs and return codes.
// Each iteration it pops off one and uses it, until the last one, which is
// repeated forever.
type testRunnerMulti struct {
	outputs   []string
	ret_codes []int
}

func (r testRunner) run(command string, args ...string) *exec.Cmd {
	sub_args := []string{"-test.run=TestHelperProcessSuccess", "--"}

	//append helper process return code converted to string
	sub_args = append(sub_args, strconv.Itoa(r.ret_code))
	//append helper process return message
	sub_args = append(sub_args, r.output)

	cmd := exec.Command(os.Args[0], sub_args...)
	cmd.Env = []string{"NEED_MENDER_TEST_HELPER_PROCESS=1"}
	return cmd
}

func (r testRunnerMulti) run(command string, args ...string) *exec.Cmd {
	output := r.outputs[0]
	ret_code := r.ret_codes[0]
	if len(r.outputs) > 1 && len(r.ret_codes) > 1 {
		r.outputs = r.outputs[1:]
		r.ret_codes = r.ret_codes[1:]
	}

	var runner testRunner
	runner.output = output
	runner.ret_code = ret_code
	return runner.run(command, args...)
}

func TestHelperProcessSuccess(t *testing.T) {
	if os.Getenv("NEED_MENDER_TEST_HELPER_PROCESS") != "1" {
		return
	}

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
