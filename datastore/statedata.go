// Copyright 2020 Northern.tech AS
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
package datastore

import (
	"encoding/json"
	"fmt"

	"github.com/pkg/errors"
)

////////////////////////////////////////////////////////////////////////////////
// All the structs related to state data which is saved in the database.
////////////////////////////////////////////////////////////////////////////////

// StateData is state information that can be used for restoring state from storage
type StateData struct {
	// version is providing information about the format of the data
	Version int
	// number representing the id of the last state to execute
	Name MenderState
	// update info and response data for the update that was in progress
	UpdateInfo UpdateInfo
}

// current version of the format of StateData;
// increase the version number once the format of StateData is changed
// StateDataVersion = 2 was introduced in Mender 2.0.0.
const StateDataVersion = 2

type MenderState int

const (
	// initial state
	MenderStateInit MenderState = iota
	// idle state; waiting for transition to the new state
	MenderStateIdle
	// client is bootstrapped, i.e. ready to go
	MenderStateAuthorize
	// wait before authorization attempt
	MenderStateAuthorizeWait
	// inventory update
	MenderStateInventoryUpdate
	// wait for new update or inventory sending
	MenderStateCheckWait
	// check update
	MenderStateUpdateCheck
	// update fetch
	MenderStateUpdateFetch
	// update store
	MenderStateUpdateStore
	// after update store (Download_Leave)
	MenderStateUpdateAfterStore
	// install update
	MenderStateUpdateInstall
	// wait before retrying fetch & install after first failing (timeout,
	// for example)
	MenderStateFetchStoreRetryWait
	// verify update
	MenderStateUpdateVerify
	// Retry sending status report before committing
	MenderStateUpdatePreCommitStatusReportRetry
	// commit needed
	MenderStateUpdateCommit
	// first commit is finished
	MenderStateUpdateAfterFirstCommit
	// all commits are finished
	MenderStateUpdateAfterCommit
	// status report
	MenderStateUpdateStatusReport
	// wait before retrying sending either report or deployment logs
	MenderStatusReportRetryState
	// error reporting status
	MenderStateReportStatusError
	// reboot
	MenderStateReboot
	// first state after booting device after rollback reboot
	MenderStateVerifyReboot
	// state which runs the ArtifactReboot_Leave scripts
	MenderStateAfterReboot
	// rollback
	MenderStateRollback
	// reboot after rollback
	MenderStateRollbackReboot
	// first state after booting device after rollback reboot
	MenderStateVerifyRollbackReboot
	// state which runs ArtifactRollbackReboot_Leave scripts
	MenderStateAfterRollbackReboot
	// error
	MenderStateError
	// update error
	MenderStateUpdateError
	// cleanup state
	MenderStateUpdateCleanup
	// exit state
	MenderStateDone
)

var (
	stateNames = map[MenderState]string{
		MenderStateInit:                             "init",
		MenderStateIdle:                             "idle",
		MenderStateAuthorize:                        "authorize",
		MenderStateAuthorizeWait:                    "authorize-wait",
		MenderStateInventoryUpdate:                  "inventory-update",
		MenderStateCheckWait:                        "check-wait",
		MenderStateUpdateCheck:                      "update-check",
		MenderStateUpdateFetch:                      "update-fetch",
		MenderStateUpdateStore:                      "update-store",
		MenderStateUpdateAfterStore:                 "update-after-store",
		MenderStateUpdateInstall:                    "update-install",
		MenderStateFetchStoreRetryWait:              "fetch-install-retry-wait",
		MenderStateUpdateVerify:                     "update-verify",
		MenderStateUpdateCommit:                     "update-commit",
		MenderStateUpdatePreCommitStatusReportRetry: "update-pre-commit-status-report-retry",
		MenderStateUpdateAfterFirstCommit:           "update-after-first-commit",
		MenderStateUpdateAfterCommit:                "update-after-commit",
		MenderStateUpdateStatusReport:               "update-status-report",
		MenderStatusReportRetryState:                "update-retry-report",
		MenderStateReportStatusError:                "status-report-error",
		MenderStateReboot:                           "reboot",
		MenderStateVerifyReboot:                     "verify-reboot",
		MenderStateAfterReboot:                      "after-reboot",
		MenderStateRollback:                         "rollback",
		MenderStateRollbackReboot:                   "rollback-reboot",
		MenderStateVerifyRollbackReboot:             "verify-rollback-reboot",
		MenderStateAfterRollbackReboot:              "after-rollback-reboot",
		MenderStateError:                            "error",
		MenderStateUpdateError:                      "update-error",
		MenderStateUpdateCleanup:                    "cleanup",
		MenderStateDone:                             "finished",
	}
)

func (m MenderState) MarshalJSON() ([]byte, error) {
	n, ok := stateNames[m]
	if !ok {
		return nil, fmt.Errorf("marshal error; unknown state %v", m)
	}
	return json.Marshal(n)
}

func (m MenderState) String() string {
	return stateNames[m]
}

func (m *MenderState) UnmarshalJSON(data []byte) error {
	var s string
	err := json.Unmarshal(data, &s)
	if err != nil {
		return err
	}
	for k, v := range stateNames {
		if v == s {
			*m = k
			return nil
		}
	}
	return fmt.Errorf("unmarshal error; unknown state %s", s)
}

type SupportsRollbackType string

const (
	RollbackSupportUnknown = ""
	RollbackNotSupported   = "rollback-not-supported"
	RollbackSupported      = "rollback-supported"
)

func (s *SupportsRollbackType) Set(value SupportsRollbackType) error {
	if *s == RollbackSupportUnknown {
		*s = value
	} else if *s != value {
		return errors.Errorf("Conflicting rollback support. Trying to set rollback support to "+
			"'%s' while already '%s'. All payloads' rollback support must be the same!",
			value, *s)
	}
	return nil
}

type RebootType string

const (
	RebootTypeNone      = ""
	RebootTypeCustom    = "reboot-type-custom"
	RebootTypeAutomatic = "reboot-type-automatic"
)

type RebootRequestedType []RebootType

func (r *RebootRequestedType) Get(n int) (RebootType, error) {
	if n >= len(*r) {
		return RebootTypeNone, errors.Errorf(
			"Reboot information missing for payload %04d", n)
	}
	switch (*r)[n] {
	case RebootTypeNone, RebootTypeCustom, RebootTypeAutomatic:
		return (*r)[n], nil
	default:
		return RebootTypeNone, errors.Errorf(
			"Corrupt RebootRequested entry: \"%s\"", (*r)[n])
	}
}

func (r *RebootRequestedType) Set(n int, t RebootType) error {
	if n == len(*r) {
		*r = append(*r, t)
	} else {
		return errors.New("RebootRequested not assigned in order")
	}
	return nil
}

type Artifact struct {
	Source struct {
		URI    string
		Expire string
	}
	// Compatible devices for dependency checking.
	CompatibleDevices []string `json:"device_types_compatible"`
	// What kind of payloads are embedded in the artifact
	// (e.g. rootfs-image).
	PayloadTypes []string
	// The following two properties implements ArtifactProvides header-info
	// field of artifact version >= 3. The Attributes are moved to the root
	// of the Artifact structure for backwards compatibility.
	ArtifactName  string `json:"artifact_name"`
	ArtifactGroup string `json:"artifact_group"`
	// Holds optional provides fields in the type-info header
	TypeInfoProvides map[string]string `json:"artifact_provides,omitempty"`

	// Holds options clears_artifact_provides fields from the type-info header.
	// Added in Mender client 2.5.
	ClearsArtifactProvides []string `json:"clears_artifact_provides,omitempty"`
}

// Info about the update in progress.
type UpdateInfo struct {
	Artifact Artifact
	ID       string

	// Whether the currently running payloads asked for reboots. It is
	// indexed the same as PayloadTypes above.
	RebootRequested RebootRequestedType

	// Whether the currently running update supports rollback. All payloads
	// must either support rollback or not, so this is one global flag for
	// all of them.
	SupportsRollback SupportsRollbackType

	// How many times this update's state has been stored. This is roughly,
	// but not exactly, equivalent to the number of state transitions, and
	// is used to break out of loops.
	StateDataStoreCount int

	// Whether the current update includes a DB schema update (this
	// structure, and the StateData structure). This is set if we load state
	// data and discover that it is a different version. See also the
	// StateDataKeyUncommitted key.
	HasDBSchemaUpdate bool
}

func (ur *UpdateInfo) CompatibleDevices() []string {
	return ur.Artifact.CompatibleDevices
}

func (ur *UpdateInfo) ArtifactName() string {
	return ur.Artifact.ArtifactName
}

func (ur *UpdateInfo) ArtifactGroup() string {
	return ur.Artifact.ArtifactGroup
}

func (ur *UpdateInfo) ArtifactTypeInfoProvides() map[string]string {
	return ur.Artifact.TypeInfoProvides
}

func (ur *UpdateInfo) ArtifactClearsProvides() []string {
	return ur.Artifact.ClearsArtifactProvides
}

func (ur *UpdateInfo) URI() string {
	return ur.Artifact.Source.URI
}
