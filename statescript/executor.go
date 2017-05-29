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

package statescript

import (
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/pkg/errors"
)

type Executor interface {
	ExecuteAll(state, action string) error
}

type Launcher struct {
	artScriptsPath    string
	rootfsScriptsPath string
}

func (l Launcher) get(state, action string) ([]string, error) {

	sDir := l.artScriptsPath
	if state == "Idle" || state == "Sync" {
		sDir = l.rootfsScriptsPath
	}

	files, err := ioutil.ReadDir(sDir)
	// TODO: should we error or rather run no scripts
	if err != nil {
		return nil, errors.Wrap(err, "statescript: can not read scripts directory")
	}

	// in most cases we will have one script for a given state
	scripts := make([]string, 1)

	execBits := os.FileMode(syscall.S_IXUSR | syscall.S_IXGRP | syscall.S_IXOTH)

	for _, file := range files {
		// check if script is executable
		if file.Mode()&execBits == 0 {
			return nil,
				errors.Errorf("statescript: script '%s' is not executable", file)
		}
		if strings.Contains(file.Name(), state) &&
			strings.Contains(file.Name(), action) {
			scripts = append(scripts, file.Name())
		}
	}
	return scripts, nil
}

func execute(name string) int {
	defaultFailedCode := 1

	cmd := exec.Command(name)
	err := cmd.Run()

	if err != nil {
		// try to get the exit code
		if exitError, ok := err.(*exec.ExitError); ok {
			ws := exitError.Sys().(syscall.WaitStatus)
			return ws.ExitStatus()
		} else {
			return defaultFailedCode
		}
	}
	return 0
}

func (l Launcher) ExecuteAll(state, action string) error {
	scr, err := l.get(state, action)
	if err != nil {
		return err
	}

	for _, s := range scr {
		if ret := execute(s); ret != 0 {
			return errors.Errorf("statescript: error executing '%s': %d", s, ret)
		}
	}
	return nil
}
