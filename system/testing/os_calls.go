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
package testing

import (
	"os"
	"os/exec"
	"path"
	"runtime"
	"strconv"
)

// The test runner, which simulates output and return code.
type TestOSCalls struct {
	Output  string
	RetCode int
	*exec.Cmd
	File os.FileInfo
	Err  error
}

func NewTestOSCalls(output string, ret int) *TestOSCalls {
	tc := TestOSCalls{}
	tc.Output = output
	tc.RetCode = ret

	return &tc
}

func (sc *TestOSCalls) Stat(name string) (os.FileInfo, error) {
	return sc.File, sc.Err
}

func (sc *TestOSCalls) Command(command string, args ...string) *exec.Cmd {
	_, file, _, _ := runtime.Caller(0)

	// Append helper process return code converted to string and return message
	subArgs := []string{strconv.Itoa(sc.RetCode), sc.Output}

	// Find script to call
	script := path.Join(path.Dir(file), "os_calls_helper.sh")

	cmd := exec.Command(script, subArgs...)

	return cmd
}
