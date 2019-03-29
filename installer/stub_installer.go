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

package installer

import (
	"io"
	"os"

	"github.com/mendersoftware/log"
	"github.com/mendersoftware/mender-artifact/artifact"
	"github.com/mendersoftware/mender-artifact/handlers"
	"github.com/mendersoftware/mender/system"
	"github.com/pkg/errors"
)

// A stub installer that fails nearly every step. For use as a stub when we
// cannot find the module we're looking for.
type StubInstaller struct {
	payloadType     string
	systemRebootCmd *system.SystemRebootCmd
}

func NewStubInstaller(payloadType string) *StubInstaller {
	return &StubInstaller{
		payloadType:     payloadType,
		systemRebootCmd: system.NewSystemRebootCmd(system.OsCalls{}),
	}
}

const stubErrorFmt string = "Stub module: Cannot execute %s"

func (d *StubInstaller) Initialize(artifactHeaders,
	artifactAugmentedHeaders artifact.HeaderInfoer,
	payloadHeaders handlers.ArtifactUpdateHeaders) error {

	return errors.Errorf(stubErrorFmt, "Download")
}

func (d *StubInstaller) PrepareStoreUpdate() error {
	return errors.Errorf(stubErrorFmt, "Download")
}

func (d *StubInstaller) StoreUpdate(r io.Reader, info os.FileInfo) error {
	return errors.Errorf(stubErrorFmt, "Download")
}

func (d *StubInstaller) FinishStoreUpdate() error {
	return errors.Errorf(stubErrorFmt, "Download")
}

func (d *StubInstaller) InstallUpdate() error {
	return errors.Errorf(stubErrorFmt, "ArtifactInstall")
}

func (d *StubInstaller) NeedsReboot() (RebootAction, error) {
	// Return this so that we can make one last desperate attempt to reboot
	// to restore things. This should work for rootfs updates with
	// bootloader support, but probably not for most others.
	return RebootRequired, nil
}

func (d *StubInstaller) Reboot() error {
	return errors.Errorf(stubErrorFmt, "ArtifactReboot")
}

func (d *StubInstaller) CommitUpdate() error {
	return errors.Errorf(stubErrorFmt, "ArtifactCommit")
}

func (d *StubInstaller) SupportsRollback() (bool, error) {
	// Return this so that we can make one last desperate attempt to reboot
	// to restore things. This should work for rootfs updates with
	// bootloader support, but probably not for most others.
	log.Error("Pretending to support rollback so that host can reboot and try to restore state")
	return true, nil
}

func (d *StubInstaller) Rollback() error {
	log.Error("Unable to roll back with a stub module, but will try to reboot to restore state")
	return nil
}

func (d *StubInstaller) VerifyReboot() error {
	return errors.Errorf(stubErrorFmt, "ArtifactVerifyReboot")
}

func (d *StubInstaller) RollbackReboot() error {
	// Reboot at the rollback stage, to restore things. This should work for
	// rootfs updates with bootloader support, but probably not for most
	// others.
	return d.systemRebootCmd.Reboot()
}

func (d *StubInstaller) VerifyRollbackReboot() error {
	// If we get here, it means that our rebooting didn't work. We return
	// error to try again until we have looped so many times that the client
	// gives up.
	return errors.Errorf(stubErrorFmt, "ArtifactVerifyRollbackReboot. Client is still not recovered from missing update module")
}

func (d *StubInstaller) Failure() error {
	return errors.Errorf(stubErrorFmt, "ArtifactFailure")
}

func (d *StubInstaller) Cleanup() error {
	return errors.Errorf(stubErrorFmt, "Cleanup")
}

func (d *StubInstaller) GetType() string {
	return d.payloadType
}
