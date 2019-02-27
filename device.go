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
	"bufio"
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/mendersoftware/mender/installer"

	"github.com/mendersoftware/log"
	"github.com/pkg/errors"
)

type deviceConfig struct {
	rootfsPartA string
	rootfsPartB string
}

type device struct {
	BootEnvReadWriter
	Commander
	*partitions
}

var (
	errorNoUpgradeMounted = errors.New("There is nothing to commit")
)

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

func NewDevice(env BootEnvReadWriter, sc StatCommander, config deviceConfig) *device {
	partitions := partitions{
		StatCommander:     sc,
		BootEnvReadWriter: env,
		rootfsPartA:       resolveLink(config.rootfsPartA),
		rootfsPartB:       resolveLink(config.rootfsPartB),
		active:            "",
		inactive:          "",
	}
	device := device{env, sc, &partitions}
	return &device
}

func (d *device) Reboot() error {
	log.Infof("Mender rebooting from active partition: %s", d.active)
	return d.Command("reboot").Run()
}

func (d *device) SwapPartitions() error {
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

func (d *device) InstallUpdate(image io.ReadCloser, size int64, initialOffset int64, ipc installer.InstallationProgressConsumer) error {

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

	var bdf BlockDeviceFile
	if typeUBI {
		bdf, err = newUBIDeviceFile(inactivePartition, size)
		if err != nil {
			log.Errorf("failed to open UBI block device file for device %s: %v",
				inactivePartition, err)
			return err
		}
	} else {
		bdf, err = newBasicDeviceFile(inactivePartition)
		if err != nil {
			log.Errorf("failed to open basic block device file for device %s: %v",
				inactivePartition, err)
			return err
		}
	}

	var pcb ProgressCallback

	if ipc != nil {
		pcb = ipc.UpdateInstallationProgress
	}

	b, err := NewBlockDeviceWriter(
		bdf,
		size,
		0, // automatically pick chunk size
		pcb,
	)
	if err != nil {
		log.Errorf("failed to create block device writer for device %s: %v",
			inactivePartition, err)
		bdf.Close()
		return err
	}

	if bsz, err := b.Size(); err != nil {
		log.Errorf("failed to read size of block device %s: %v",
			inactivePartition, err)
		b.Close()
		return err
	} else if bsz < uint64(size) {
		log.Errorf("update (%v bytes) is larger than the size of device %s (%v bytes)",
			size, inactivePartition, bsz)
		b.Close()
		return syscall.ENOSPC
	}

	_, err = b.Seek(initialOffset, io.SeekStart)
	if err != nil {
		log.Errorf("failed to seek to initial offset %d of device %s: %v",
			initialOffset, inactivePartition, err)
		b.Close()
		return err
	}

	// All of the image copying happens here
	w, err := b.ReadFrom(image)

	log.Infof("wrote %d of %d bytes (starting from offset %d) of update to device %v",
		w, size, initialOffset, inactivePartition)

	err = b.CheckFullImageWritten()
	if err != nil {
		b.Close()
		return err
	}

	if cerr := b.Close(); cerr != nil {
		log.Errorf("closing device %v failed: %v", inactivePartition, cerr)
		if cerr != nil {
			return cerr
		}
	}

	return nil
}

func (d *device) getInactivePartition() (string, string, error) {
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

var VerificationFailed = errors.New("verification of updated partition failed")

func (d *device) VerifyUpdatedPartition(size int64, expectedSHA256Checksum []byte) error {

	inactivePartition, err := d.GetInactive()
	if err != nil {
		return err
	}

	partition, err := os.OpenFile(inactivePartition, os.O_RDONLY, 0)
	if err != nil {
		return errors.Wrapf(err, "unable to open inactive partition (%s) for verification", inactivePartition)
	}
	defer partition.Close()

	h := sha256.New()
	bytesRead, err := io.CopyN(h, partition, size)

	if err != nil {
		return errors.Wrapf(err, "verification checksum failed after reading back %d bytes", bytesRead)
	}

	actualSum := h.Sum(nil)
	if !bytes.Equal(actualSum, expectedSHA256Checksum) {
		log.Errorf("Verification of image on partition %s (length %d) failed, expected checksum: %v, actual sum: %v",
			inactivePartition,
			size,
			expectedSHA256Checksum,
			actualSum,
		)
		return VerificationFailed
	}

	log.Infof("Verification of image on partition %s (length %d) succeeded", inactivePartition, size)
	return nil
}

func (d *device) EnableUpdatedPartition() error {

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

func (d *device) CommitUpdate() error {
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
	return errorNoUpgradeMounted
}

func (d *device) HasUpdate() (bool, error) {
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
