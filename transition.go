// Copyright 2018 Northern.tech AS
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

	"github.com/mendersoftware/mender/client"
	"github.com/mendersoftware/mender/statescript"
	"github.com/mendersoftware/mender/store"
	"github.com/pkg/errors"
)

type Transition int

func (t Transition) IsToError() bool {
	return t == ToError ||
		t == ToArtifactFailure ||
		t == ToArtifactRollback ||
		t == ToArtifactRollbackReboot_Enter ||
		t == ToArtifactRollbackReboot_Leave
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
	// should have Enter and Error actions
	ToArtifactReboot_Enter
	// should have Leave action only
	ToArtifactReboot_Leave
	ToArtifactCommit
	ToArtifactRollback
	// should have Enter and Error actions
	ToArtifactRollbackReboot_Enter
	// should have Leave action only
	ToArtifactRollbackReboot_Leave
	ToArtifactFailure
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
		ToArtifactFailure:              "ArtifactFailure",
	}
)

func (t Transition) String() string {
	return transitionNames[t]
}

// For some states we should ignore errors as recovery is not possible
// and we might end up with device bricked.
func ignoreErrors(t Transition, action string) bool {
	return t == ToIdle ||
		t == ToArtifactRollback ||
		t == ToArtifactRollbackReboot_Enter ||
		t == ToArtifactRollbackReboot_Leave ||
		t == ToArtifactFailure ||
		// for now just ignore ArtifactCommit.Leave errors
		(t == ToArtifactCommit && action == "Leave")
}

// Transition implements statescript.Launcher interface
func (t Transition) Enter(exec statescript.Executor, report *client.StatusReportWrapper, store store.Store) error {
	if t == ToNone {
		return nil
	}

	name := getName(t, "Enter")
	if name == "" {
		return nil
	}
	if t == ToArtifactReboot_Enter {
		// Store reboot-state here, so that a powerloss in reboot-enter
		// will not cause the client to reboot back into the old partition.
		sd, err := LoadStateData(store)
		if err != nil {
			return errors.Wrap(err, "ArtifactRollbackReboot_Enter: ")
		}
		sd.Name = MenderStateReboot
		err = StoreStateData(store, sd)
		if err != nil {
			return errors.Wrap(err, "ArtifactRollbackReboot_Enter: failed to store state-data. ")
		}
	}

	if err := exec.ExecuteAll(name, "Enter", ignoreErrors(t, "Enter"), report); err != nil {
		return errors.Wrapf(err, "error running enter state script(s) for %v state", t)
	}
	return nil
}

func (t Transition) Leave(exec statescript.Executor, report *client.StatusReportWrapper, store store.Store) error {
	if t == ToNone {
		return nil
	}

	name := getName(t, "Leave")
	if name == "" {
		return nil
	}

	if err := exec.ExecuteAll(name, "Leave", ignoreErrors(t, "Leave"), report); err != nil {
		return errors.Wrapf(err, "error running leave state script(s) for %v state", t)
	}
	return nil
}

func (t Transition) Error(exec statescript.Executor, report *client.StatusReportWrapper) error {
	if t == ToNone {
		return nil
	}

	name := getName(t, "Error")
	if name == "" {
		return nil
	}

	if err := exec.ExecuteAll(name, "Error", true, report); err != nil {
		return errors.Wrapf(err, "error running error state script(s) for %v state", t)
	}
	return nil
}

func getName(t Transition, action string) string {
	// For ToArtifactReboot and ToArtifactRollbackReboot transitions we are having
	// two internal states each: ToArtifactReboot_Enter, ToArtifactReboot_Leave
	// and ToArtifactRollbackReboot_Enter, ToArtifactRollbackReboot_Leave
	// respectively.
	// The reason is to be able to enter correct transition after device is
	// rebooted and being able to call correct state scripts.
	// If we are entering _Leave state ONLY Leave or Error action will be
	// called skipping Enter action.
	name := t.String()
	spl := strings.Split(name, "_")
	if len(spl) == 2 {
		name = spl[0]
		if action != "Error" && spl[1] != action {
			return ""
		}
	}
	return name
}
