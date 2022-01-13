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

package utils

import (
	"os/exec"
	"path"
	"runtime"

	"github.com/pkg/errors"
)

var (
	ExternalBinaryPaths = []string{"/usr/sbin", "/sbin", "/usr/local/sbin"}
)

func GetBinaryPath(command string) (string, error) {
	// first check if command exists in PATH
	p, err := exec.LookPath(command)
	if err == nil {
		return p, nil
	}

	// maybe sbin isn't included in PATH, check there explicitly.
	for _, p = range ExternalBinaryPaths {
		p, err = exec.LookPath(path.Join(p, command))
		if err == nil {
			return p, nil
		}
	}

	// not found, but oh well...
	if runtime.GOOS == "darwin" && path.Base(command) == "parted" {
		return command, errors.Wrap(
			err,
			"Operations that use \"parted\" are unfortunately not available on Mac OS.",
		)
	}

	return command, err
}
