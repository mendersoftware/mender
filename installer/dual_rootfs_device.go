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
	"bufio"
	"bytes"
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
	"github.com/mendersoftware/mender/system"
	"github.com/pkg/errors"
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
func NewDualRootfsDevice(env BootEnvReadWriter, sc system.StatCommander, config DualRootfsDeviceConfig) DualRootfsDevice {
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

	return MissingFeaturesCheck(artifactAugmentedHeaders, payloadHeaders)
}

func (d *dualRootfsDeviceImpl) PrepareStoreUpdate() error {
	return nil
}

// chunkedCopy copies data from in to out in chunks of exactly chunkSize
// bytes.
// Data is held in memory until chunkSize bytes are available to be written.
func chunkedCopy(out io.Writer, in io.Reader, chunkSize int64) (totalWritten int64, err error) {
	buf := bytes.NewBuffer(make([]byte, 0, chunkSize))

	for {
		buf.Reset()
		// read chunkSize bytes into buf
		bytesRead, readErr := io.CopyN(buf, in, chunkSize)

		if bytesRead > 0 {
			// write all of buf to out
			bytesWritten, writeErr := buf.WriteTo(out)
			totalWritten += bytesWritten

			if writeErr != nil {
				return totalWritten, writeErr
			}
			if bytesWritten != bytesRead {
				return totalWritten, fmt.Errorf(
					"Unexpected short write: attempted %d bytes but only wrote %d",
					bytesRead,
					bytesWritten,
				)
			}
		}

		if readErr != nil {
			// read error io.EOF isn't exposed since it's just a marker of input data end
			if readErr == io.EOF {
				readErr = nil
			}
			return totalWritten, readErr
		}
	}
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

	// Make sure the file system is not mounted (MEN-2084)
	if mnt_pt := checkMounted(inactivePartition); mnt_pt != "" {
		log.Warnf("Inactive partition %q is mounted at %q. "+
			"This might be caused by some \"auto mount\" service "+
			"(e.g udisks2) that mounts all block devices. It is "+
			"recommended to blacklist the partitions used by "+
			"Mender to avoid any issues.", inactivePartition, mnt_pt)
		log.Warnf("Performing umount on %q.", mnt_pt)
		err = syscall.Unmount(inactivePartition, 0)
		if err != nil {
			log.Errorf("Error unmounting partition %s",
				inactivePartition)
			return err
		}
	}

	typeUBI := system.IsUbiBlockDevice(inactivePartition)
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

	b := &BlockDevice{
		Path:               inactivePartition,
		typeUBI:            typeUBI,
		ImageSize:          size,
		FlushIntervalBytes: 4 * 1024 * 1024,
	}

	if bsz, err := b.Size(); err != nil {
		log.Errorf("failed to read size of block device %s: %v",
			inactivePartition, err)
		return err
	} else if bsz < uint64(size) {
		log.Errorf("update (%v bytes) is larger than the size of device %s (%v bytes)",
			size, inactivePartition, bsz)
		return syscall.ENOSPC
	}

	native_ssz, err := b.SectorSize()
	if err != nil {
		log.Errorf("failed to read sector size of block device %s: %v",
			inactivePartition, err)
		return err
	}

	// The size of an individual sector tends to be quite small.  Rather than
	// doing a zillion small writes, do medium-size-ish writes that are still
	// sector aligned.  (Doing too many small writes can put pressure on the
	// DMA subsystem (unless writes are able to be coalesced) by requiring large numbers of scatter-gather descriptors to be allocated.)
	chunk_size := native_ssz

	// Pick a multiple of the sector size that's around 1 MiB.
	for chunk_size < 1*1024*1024 {
		chunk_size = chunk_size * 2 // avoid doing logarithms...
	}

	log.Infof("native sector size of block device %s is %v, we will write in chunks of %v",
		inactivePartition,
		native_ssz,
		chunk_size,
	)

	w, err := chunkedCopy(b, image, int64(chunk_size))
	if err != nil {
		log.Errorf("failed to write image data to device %v: %v",
			inactivePartition, err)
	}

	log.Infof("wrote %v/%v bytes of update to device %v",
		w, size, inactivePartition)

	if cerr := b.Close(); cerr != nil {
		log.Errorf("closing device %v failed: %v", inactivePartition, cerr)
		if cerr != nil {
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
	return ErrorNothingToCommit
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
