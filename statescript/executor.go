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

	"github.com/mendersoftware/log"
	"github.com/pkg/errors"
)

type Executor interface {
	ExecuteAll(state, action string) error
}

type Launcher struct {
	ArtScriptsPath          string
	RootfsScriptsPath       string
	SupportedScriptVersions []int
}

func (l Launcher) get(state, action string) ([]string, error) {

	sDir := l.ArtScriptsPath
	if state == "Idle" || state == "Sync" || state == "Download" {
		sDir = l.RootfsScriptsPath
	}

	// ReadDir reads the directory named by dirname and returns
	// a list of directory entries sorted by filename.
	// The list returned should be sorted which guarantees correct
	// order of scripts execution.
	files, err := ioutil.ReadDir(sDir)
	if err != nil && os.IsNotExist(err) {
		// no state scripts directory; just move on
		return nil, nil
	} else if err != nil {
		return nil, errors.Wrap(err, "statescript: can not read scripts directory")
	}

	scripts := make([]string, 0)
	execBits := os.FileMode(syscall.S_IXUSR | syscall.S_IXGRP | syscall.S_IXOTH)
	var version int

	for _, file := range files {
		if file.Name() == "version" {
			version, err = readVersion(file.Name())
			if err != nil {
				return nil, errors.Wrapf(err, "statescript: can not read version file")
			}
		}

		if strings.Contains(file.Name(), state) &&
			strings.Contains(file.Name(), action) {
			// check if script is executable
			if file.Mode()&execBits == 0 {
				return nil,
					errors.Errorf("statescript: script '%s' is not executable", file)
			}
			scripts = append(scripts, file.Name())
		}
	}

	for _, v := range l.SupportedScriptVersions {
		if v == version {
			return scripts, nil
		}
	}
	return nil, errors.Errorf("statescript: supproted versions does not match "+
		"(supported: %v; actual: %v)", l.SupportedScriptVersions, version)
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
			// In case of error scripts all should be executed.
			if action == "Error" {
				log.Errorf("statescript: error executing '%s': %d", s, ret)
			} else {
				return errors.Errorf("statescript: error executing '%s': %d", s, ret)
			}
		}
	}
	return nil
}
