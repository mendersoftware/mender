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

package artifact

import (
	"path/filepath"
	"regexp"

	"github.com/pkg/errors"
)

type Scripts struct {
	names map[string]string
}

var availableScriptType = map[string]bool{
	// Idle, Sync and Download scripts are part of rootfs and can not
	// be a part of the artifact itself; Leaving below for refference...
	//"Idle":                   true,
	//"Sync":                   true,
	//"Download":               true,
	"ArtifactInstall":        true,
	"ArtifactReboot":         true,
	"ArtifactCommit":         true,
	"ArtifactRollback":       true,
	"ArtifactRollbackReboot": true,
	"ArtifactFailure":        true,
}

func (s *Scripts) Add(path string) error {
	if s.names == nil {
		s.names = make(map[string]string)
	}

	name := filepath.Base(path)

	// all scripts must be formated like `ArtifactInstall_Enter_05_wifi-driver`
	re := regexp.MustCompile(`([A-Za-z]+)_(Enter|Leave|Error)_[0-9][0-9](_\S+)?`)

	// `matches` should contain a slice of string of match of regex;
	// the first element should be the whole matched name of the script and
	// the second one shold be the name of the state
	matches := re.FindStringSubmatch(name)
	if matches == nil || len(matches) < 3 {
		return errors.Errorf(
			"Invalid script name: %q. Scripts must have a name on the form:"+
				" <STATE_NAME>_<ACTION>_<ORDERING_NUMBER>_<OPTIONAL_DESCRIPTION>."+
				" For example: 'Download_Enter_05_wifi-driver' is a valid script name.",
			name,
		)
	}
	if _, found := availableScriptType[matches[1]]; !found {
		return errors.Errorf("Unsupported script state: %s", matches[1])
	}

	if _, exists := s.names[name]; exists {
		return errors.Errorf("Script already exists: %s", name)
	}

	s.names[name] = path
	return nil
}

func (s *Scripts) Get() []string {
	scr := make([]string, 0, len(s.names))
	for _, script := range s.names {
		scr = append(scr, script)
	}
	return scr
}
