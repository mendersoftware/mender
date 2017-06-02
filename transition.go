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
	"strings"

	"github.com/mendersoftware/mender/statescript"
	"github.com/pkg/errors"
)

type Transition int

func (t Transition) IsError() bool {
	return t == ToError || t == ToArtifactError
}

// For some states we should ignore errors as recovery is not possible
// and we might end up with device bricked.
func (t Transition) IgnoreError() bool {
	return t == ToIdle ||
		t == ToArtifactRollback ||
		t == ToArtifactRollbackReboot_Enter
}

const (
	// no transition is happening
	ToNone Transition = iota
	// initial transition
	ToIdle
	ToSync
	ToError
	ToDownload
	ToArtifactInstall
	// should hsve Enter and Error actions
	ToArtifactReboot_Enter
	// should have Leave action only
	ToArtifactReboot_Leave
	ToArtifactCommit
	ToArtifactRollback
	// should hsve Enter and Error actions
	ToArtifactRollbackReboot_Enter
	// should have Leave action only
	ToArtifactRollbackReboot_Leave
	ToArtifactError
)

var (
	transitionNames = map[Transition]string{
		ToNone:                         "none",
		ToIdle:                         "Idle",
		ToSync:                         "Sync",
		ToError:                        "Error",
		ToDownload:                     "Download",
		ToArtifactInstall:              "ArtifactInstall",
		ToArtifactReboot_Enter:         "ArtifactReboot_Enter",
		ToArtifactReboot_Leave:         "ArtifactReboot_Leave",
		ToArtifactCommit:               "ArtifactCommit",
		ToArtifactRollback:             "ArtifactRollback",
		ToArtifactRollbackReboot_Enter: "ArtifactRollbackReboot_Enter",
		ToArtifactRollbackReboot_Leave: "ArtifactRollbackReboot_Leave",
		ToArtifactError:                "ArtifactError",
	}
)

func (t Transition) String() string {
	return transitionNames[t]
}

// Transition implements statescript.Launcher interface
func (t Transition) Enter(exec statescript.Executor) error {
	if t == ToNone {
		return nil
	}

	name := t.String()

	spl := strings.Split(name, "_")
	if len(spl) == 2 {
		name = spl[0]
		if spl[1] != "Enter" {
			return nil
		}
	}

	if err := exec.ExecuteAll(name, "Enter"); err != nil {
		return errors.Wrapf(err, "error running enter state script(s) for %v state", t)
	}
	return nil
}

func (t Transition) Leave(exec statescript.Executor) error {
	if t == ToNone {
		return nil
	}

	name := t.String()

	spl := strings.Split(name, "_")
	if len(spl) == 2 {
		name = spl[0]
		if spl[1] != "Leave" {
			return nil
		}
	}

	if err := exec.ExecuteAll(name, "Leave"); err != nil {
		return errors.Wrapf(err, "error running leave state script(s) for %v state", t)
	}
	return nil
}

func (t Transition) Error(exec statescript.Executor) error {
	if t == ToNone {
		return nil
	}

	name := t.String()

	spl := strings.Split(name, "_")
	if len(spl) == 2 {
		name = spl[0]
		if spl[1] != "Enter" {
			return nil
		}
	}

	if err := exec.ExecuteAll(name, "Error"); err != nil {
		return errors.Wrapf(err, "error running error state script(s) for %v state", t)
	}
	return nil
}
