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
	"path/filepath"
	"strings"
	"syscall"

	"github.com/mendersoftware/log"
	"github.com/pkg/errors"
)

type Executor interface {
	ExecuteAll(state, action string, ignoreError bool) error
}

type Launcher struct {
	ArtScriptsPath          string
	RootfsScriptsPath       string
	SupportedScriptVersions []int
}

//TODO: we can optimize for reading directories once and then creating
// a map with all the scripts that needs to be executed.

func (l Launcher) get(state, action string) ([]os.FileInfo, string, error) {

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
		return nil, "", nil
	} else if err != nil {
		return nil, "", errors.Wrap(err, "statescript: can not read scripts directory")
	}

	scripts := make([]os.FileInfo, 0)
	var version int

	for _, file := range files {
		if file.Name() == "version" {
			version, err = readVersion(filepath.Join(sDir, file.Name()))
			if err != nil {
				return nil, "", errors.Wrapf(err, "statescript: can not read version file")
			}
		}

		if strings.Contains(file.Name(), state) &&
			strings.Contains(file.Name(), action) {
			scripts = append(scripts, file)
		}
	}

	for _, v := range l.SupportedScriptVersions {
		if v == version {
			return scripts, sDir, nil
		}
	}
	return nil, "", errors.Errorf("statescript: supproted versions does not match "+
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

func (l Launcher) ExecuteAll(state, action string, ignoreError bool) error {
	scr, dir, err := l.get(state, action)
	if err != nil {
		if ignoreError {
			log.Errorf("statescript: ignoring error while executing script: %v", err)
			return nil
		}
		return err
	}

	execBits := os.FileMode(syscall.S_IXUSR | syscall.S_IXGRP | syscall.S_IXOTH)

	for _, s := range scr {
		// check if script is executable
		if s.Mode()&execBits == 0 {
			if ignoreError {
				log.Errorf("statescript: ignoring script '%s' being not executable",
					filepath.Join(dir, s.Name()))
				continue
			} else {
				return errors.Errorf("statescript: script '%s' is not executable",
					filepath.Join(dir, s.Name()))
			}
		}

		if ret := execute(filepath.Join(dir, s.Name())); ret != 0 {
			// In case of error scripts all should be executed.
			if ignoreError {
				log.Errorf("statescript: ignoring error executing '%s': %d", s.Name(), ret)
			} else {
				return errors.Errorf("statescript: error executing '%s': %d",
					s.Name(), ret)
			}
		}
	}
	return nil
}
