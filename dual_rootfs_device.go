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
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/mendersoftware/log"
	"github.com/mendersoftware/mender-artifact/artifact"
	"github.com/mendersoftware/mender-artifact/handlers"
	"github.com/mendersoftware/mender/installer"
	"github.com/pkg/errors"
)

type dualRootfsDeviceConfig struct {
	rootfsPartA string
	rootfsPartB string
}

type dualRootfsDeviceImpl struct {
	BootEnvReadWriter
	Commander
	*partitions
	rebooter *systemRebooter
}

// This interface is only here for tests.
type dualRootfsDevice interface {
	installer.PayloadInstaller
	installer.PayloadInstallerProducer
	GetInactive() (string, error)
	GetActive() (string, error)
}

// Returns nil if config doesn't contain partition paths.
func NewDualRootfsDevice(env BootEnvReadWriter, sc StatCommander, config dualRootfsDeviceConfig) dualRootfsDevice {
	if config.rootfsPartA == "" || config.rootfsPartB == "" {
		return nil
	}

	partitions := partitions{
		StatCommander:     sc,
		BootEnvReadWriter: env,
		rootfsPartA:       config.rootfsPartA,
		rootfsPartB:       config.rootfsPartB,
		active:            "",
		inactive:          "",
	}
	dualRootfsDevice := dualRootfsDeviceImpl{
		BootEnvReadWriter: env,
		Commander:         sc,
		partitions:        &partitions,
		rebooter:          &systemRebooter{sc},
	}
	return &dualRootfsDevice
}

func (d *dualRootfsDeviceImpl) NeedsReboot() (installer.NeedsRebootType, error) {
	return installer.NeedsRebootYes, nil
}

func (d *dualRootfsDeviceImpl) SupportsRollback() (bool, error) {
	return true, nil
}

func (d *dualRootfsDeviceImpl) Reboot() error {
	log.Infof("Mender rebooting from active partition: %s", d.active)
	return d.rebooter.Reboot()
}

func (d *dualRootfsDeviceImpl) RollbackReboot() error {
	log.Infof("Mender rebooting from inactive partition: %s", d.active)
	return d.rebooter.Reboot()
}

func (d *dualRootfsDeviceImpl) Rollback() error {
	hasUpdate, err := d.HasUpdate()
	if err != nil {
		return errors.Wrap(err, "Could not determine whether device has an update")
	} else if !hasUpdate {
		// Nothing to do.
		return nil
	}

	// first get inactive partition
	inactivePartition, inactivePartitionHex, err := d.getInactivePartition()
	if err != nil {
		return err
	}
	log.Infof("setting partition for rollback: %s", inactivePartition)

	err = d.WriteEnv(BootVars{"mender_boot_part": inactivePartition, "mender_boot_part_hex": inactivePartitionHex, "upgrade_available": "0"})
	if err != nil {
		return err
	}
	log.Debug("Marking inactive partition as a boot candidate successful.")
	return nil
}

func (d *dualRootfsDeviceImpl) Initialize(artifactHeaders,
	artifactAugmentedHeaders artifact.HeaderInfoer,
	payloadHeaders handlers.ArtifactUpdateHeaders) error {

	return installer.MissingFeaturesCheck(artifactAugmentedHeaders, payloadHeaders)
}

func (d *dualRootfsDeviceImpl) PrepareStoreUpdate() error {
	return nil
}

func (d *dualRootfsDeviceImpl) StoreUpdate(image io.Reader, info os.FileInfo) error {

	size := info.Size()

	log.Debugf("Trying to install update of size: %d", size)
	if image == nil || size < 0 {
		return errors.New("Have invalid update. Aborting.")
	}

	inactivePartition, err := d.GetInactive()
	if err != nil {
		return err
	}

	typeUBI := isUbiBlockDevice(inactivePartition)
	if typeUBI {
		// UBI block devices are not prefixed with /dev due to the fact
		// that the kernel root= argument does not handle UBI block
		// devices which are prefixed with /dev
		//
		// Kernel root= only accepts:
		// - ubi0_0
		// - ubi:rootfsa
		inactivePartition = filepath.Join("/dev", inactivePartition)
	}

	b := &BlockDevice{Path: inactivePartition, typeUBI: typeUBI, ImageSize: size}

	if bsz, err := b.Size(); err != nil {
		log.Errorf("failed to read size of block device %s: %v",
			inactivePartition, err)
		return err
	} else if bsz < uint64(size) {
		log.Errorf("update (%v bytes) is larger than the size of device %s (%v bytes)",
			size, inactivePartition, bsz)
		return syscall.ENOSPC
	}

	ssz, err := b.SectorSize()
	if err != nil {
		log.Errorf("failed to read sector size of block device %s: %v",
			inactivePartition, err)
		return err
	}

	// allocate buffer based on sector size and provide it for staging
	// in io.CopyBuffer
	buf := make([]byte, ssz)

	w, err := io.CopyBuffer(b, image, buf)
	if err != nil {
		log.Errorf("failed to write image data to device %v: %v",
			inactivePartition, err)
	}

	log.Infof("wrote %v/%v bytes of update to device %v",
		w, size, inactivePartition)

	if cerr := b.Close(); cerr != nil {
		log.Errorf("closing device %v failed: %v", inactivePartition, cerr)
		if err != nil {
			return cerr
		}
	}

	return err
}

func (d *dualRootfsDeviceImpl) FinishStoreUpdate() error {
	return nil
}

func (d *dualRootfsDeviceImpl) getInactivePartition() (string, string, error) {
	inactivePartition, err := d.GetInactive()
	if err != nil {
		return "", "", errors.New("Error obtaining inactive partition: " + err.Error())
	}

	log.Debugf("Marking inactive partition (%s) as the new boot candidate.", inactivePartition)

	partitionNumberDecStr := inactivePartition[len(strings.TrimRight(inactivePartition, "0123456789")):]
	partitionNumberDec, err := strconv.Atoi(partitionNumberDecStr)
	if err != nil {
		return "", "", errors.New("Invalid inactive partition: " + inactivePartition)
	}

	partitionNumberHexStr := fmt.Sprintf("%X", partitionNumberDec)

	return partitionNumberDecStr, partitionNumberHexStr, nil
}

func (d *dualRootfsDeviceImpl) InstallUpdate() error {

	inactivePartition, inactivePartitionHex, err := d.getInactivePartition()
	if err != nil {
		return err
	}

	log.Info("Enabling partition with new image installed to be a boot candidate: ", string(inactivePartition))
	// For now we are only setting boot variables
	err = d.WriteEnv(BootVars{"upgrade_available": "1", "mender_boot_part": inactivePartition, "mender_boot_part_hex": inactivePartitionHex, "bootcount": "0"})
	if err != nil {
		return err
	}

	log.Debug("Marking inactive partition as a boot candidate successful.")

	return nil
}

func (d *dualRootfsDeviceImpl) CommitUpdate() error {
	// Check if the user has an upgrade to commit, if not, throw an error
	hasUpdate, err := d.HasUpdate()
	if err != nil {
		return err
	}
	if hasUpdate {
		log.Info("Commiting update")
		// For now set only appropriate boot flags
		return d.WriteEnv(BootVars{"upgrade_available": "0"})
	}
	return installer.ErrorNothingToCommit
}

func (d *dualRootfsDeviceImpl) HasUpdate() (bool, error) {
	env, err := d.ReadEnv("upgrade_available")
	if err != nil {
		return false, errors.Wrapf(err, "failed to read environment variable")
	}
	upgradeAvailable := env["upgrade_available"]

	if upgradeAvailable == "1" {
		return true, nil
	}
	return false, nil
}

func (d *dualRootfsDeviceImpl) VerifyReboot() error {
	hasUpdate, err := d.HasUpdate()
	if err != nil {
		return err
	} else if !hasUpdate {
		return errors.New("Reboot to new update failed. Expected \"upgrade_available\" flag to be true but it was false")
	} else {
		return nil
	}
}

func (d *dualRootfsDeviceImpl) VerifyRollbackReboot() error {
	hasUpdate, err := d.HasUpdate()
	if err != nil {
		return err
	} else if hasUpdate {
		return errors.New("Reboot to old update failed. Expected \"upgrade_available\" flag to be false but it was true")
	} else {
		return nil
	}
}

func (d *dualRootfsDeviceImpl) Failure() error {
	// Nothing to do for rootfs updates.
	return nil
}

func (d *dualRootfsDeviceImpl) Cleanup() error {
	// Nothing to do for rootfs updates.
	return nil
}

func (d *dualRootfsDeviceImpl) GetType() string {
	return "rootfs-image"
}

func (d *dualRootfsDeviceImpl) NewUpdateStorer(updateType string, payloadNum int) (handlers.UpdateStorer, error) {
	// We don't maintain any particular state for each payload, just return
	// the same object.
	return d, nil
}

type missingDualRootfsDevice struct {
}

func (m *missingDualRootfsDevice) NewUpdateStorer(updateType string, payloadNum int) (handlers.UpdateStorer, error) {
	return nil, errors.New("No dual rootfs configuration present")
}
