// Copyright 2022 Northern.tech AS
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
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/mendersoftware/mender-artifact/artifact"
	"github.com/mendersoftware/mender-artifact/handlers"
	"github.com/mendersoftware/mender/system"
)

const (
	verifyRebootError = "Reboot to the new update failed. " +
		"Expected \"upgrade_available\" flag to be true but it was false. " +
		"Either the switch to the new partition was unsuccessful, or the bootloader rolled back"
	verifyRollbackRebootError = "Reboot to the old update failed. " +
		"Expected \"upgrade_available\" flag to be false but it was true"
)

type DualRootfsDeviceConfig struct {
	RootfsPartA string
	RootfsPartB string
}

type dualRootfsDeviceImpl struct {
	BootEnvReadWriter
	system.Commander
	*partitions
	rebooter *system.SystemRebootCmd
}

// This interface is only here for tests.
type DualRootfsDevice interface {
	PayloadUpdatePerformer
	handlers.UpdateStorerProducer
	GetInactive() (string, error)
	GetActive() (string, error)
}

// checkMounted parses /proc/self/mounts to check
// if device partition @part is a mounted fileststem.
// return: The mount target if partition is mounted
//         else an empty string is returned
func checkMounted(part string) string {
	file, err := os.Open("/proc/self/mounts")
	if err != nil {
		return ""
	}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		words := strings.Fields(scanner.Text())
		if words[0] == part {
			// Found mounted device, return mountpoint
			return words[1]
		}
	}
	return ""
}

// Returns nil if config doesn't contain partition paths.
func NewDualRootfsDevice(
	env BootEnvReadWriter,
	sc system.StatCommander,
	config DualRootfsDeviceConfig,
) DualRootfsDevice {
	if config.RootfsPartA == "" || config.RootfsPartB == "" {
		return nil
	}

	partitions := partitions{
		StatCommander:     sc,
		BootEnvReadWriter: env,
		rootfsPartA:       maybeResolveLink(config.RootfsPartA),
		rootfsPartB:       maybeResolveLink(config.RootfsPartB),
		active:            "",
		inactive:          "",
	}
	dualRootfsDevice := dualRootfsDeviceImpl{
		BootEnvReadWriter: env,
		Commander:         sc,
		partitions:        &partitions,
		rebooter:          system.NewSystemRebootCmd(sc),
	}
	return &dualRootfsDevice
}

func (d *dualRootfsDeviceImpl) NeedsReboot() (RebootAction, error) {
	return RebootRequired, nil
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
		log.Info("No update available, so no rollback needed.")
		return nil
	}

	// If we are still on the active partition, do not switch partitions
	env, err := d.ReadEnv("mender_boot_part")
	if err != nil {
		return err
	}
	value, ok := env["mender_boot_part"]
	if !ok {
		// Oh my
		return errors.New(
			"The bootloader environment does not have the 'mender_boot_part' set." +
				" This is a critical error.",
		)
	}
	nextPartition, nextPartitionHex, err := d.getActivePartition()
	if err != nil {
		log.Error("Failed to get the active partition.")
		return err
	}
	// If 'mender_boot_part' does not equal the mounted rootfs, and
	// 'upgrade_available=1' we have not yet rebooted. This means we came
	// here from ArtifactInstall, and the rollback must switch back the
	// partition to the current active and running partition.
	if value != nextPartition {
		log.Infof("Rolling back to the active partition: (%s).", nextPartition)
	} else {
		nextPartition, nextPartitionHex, err = d.getInactivePartition()
		if err != nil {
			log.Error("Failed to get the inactive partition.")
			return err
		}
		log.Infof("Rolling back to the inactive partition (%s).", nextPartition)
	}

	err = d.WriteEnv(BootVars{
		"mender_boot_part":     nextPartition,
		"mender_boot_part_hex": nextPartitionHex,
		"upgrade_available":    "0",
	})
	if err != nil {
		return err
	}
	log.Debugf("Marking %s partition as a boot candidate successful.", nextPartition)
	return nil
}

func (d *dualRootfsDeviceImpl) Initialize(artifactHeaders,
	artifactAugmentedHeaders artifact.HeaderInfoer,
	payloadHeaders handlers.ArtifactUpdateHeaders) error {

	return nil
}

func (d *dualRootfsDeviceImpl) PrepareStoreUpdate() error {
	return nil
}

func (d *dualRootfsDeviceImpl) StoreUpdate(image io.Reader, info os.FileInfo) error {

	inactivePartition, err := d.GetInactive()
	if err != nil {
		return err
	}

	imageSize := info.Size()

	dev, err := blockdevice.Open(inactivePartition, imageSize)
	if err != nil {
		errmsg := "Failed to write the update to the inactive partition: %q"
		return errors.Wrapf(err, errmsg, inactivePartition)
	}

	n, err := io.Copy(dev, image)
	if err != nil {
		dev.Close()
		return err
	}

	if err = dev.Close(); err != nil {
		log.Errorf("Failed to close the block-device. Error: %v", err)
		return err
	}

	log.Infof("Wrote %d/%d bytes to the inactive partition", n, imageSize)

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
	return d.getPartitionImpl(inactivePartition)
}

func (d *dualRootfsDeviceImpl) getActivePartition() (string, string, error) {
	activePartition, err := d.GetActive()
	if err != nil {
		return "", "", errors.New("Error obtaining active partition: " + err.Error())
	}
	return d.getPartitionImpl(activePartition)
}

func (d *dualRootfsDeviceImpl) getPartitionImpl(partition string) (string, string, error) {

	partitionNumberDecStr := partition[len(strings.TrimRight(partition, "0123456789")):]
	partitionNumberDec, err := strconv.Atoi(partitionNumberDecStr)
	if err != nil {
		return "", "", errors.New("Invalid partition: " + partition)
	}

	partitionNumberHexStr := fmt.Sprintf("%X", partitionNumberDec)

	return partitionNumberDecStr, partitionNumberHexStr, nil
}

func (d *dualRootfsDeviceImpl) InstallUpdate() error {

	inactivePartition, inactivePartitionHex, err := d.getInactivePartition()
	if err != nil {
		return err
	}

	log.Info(
		"Enabling partition with new image installed to be a boot candidate: ",
		string(inactivePartition),
	)
	// For now we are only setting boot variables
	err = d.WriteEnv(
		BootVars{
			"upgrade_available":    "1",
			"mender_boot_part":     inactivePartition,
			"mender_boot_part_hex": inactivePartitionHex,
			"bootcount":            "0",
		})
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
		log.Info("Committing update")
		// For now set only appropriate boot flags
		return d.WriteEnv(BootVars{"upgrade_available": "0"})
	}
	return errors.New(verifyRebootError)
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
		return errors.New(verifyRebootError)
	} else {
		return nil
	}
}

func (d *dualRootfsDeviceImpl) VerifyRollbackReboot() error {
	hasUpdate, err := d.HasUpdate()
	if err != nil {
		return err
	} else if hasUpdate {
		return errors.New(verifyRollbackRebootError)
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

func (d *dualRootfsDeviceImpl) NewUpdateStorer(
	updateType *string,
	payloadNum int,
) (handlers.UpdateStorer, error) {
	// We don't maintain any particular state for each payload, just return
	// the same object.
	return d, nil
}
