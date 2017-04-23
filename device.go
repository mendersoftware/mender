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
	"io"
	"path/filepath"
	"strconv"
	"syscall"

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

func NewDevice(env BootEnvReadWriter, sc StatCommander, config deviceConfig) *device {
	partitions := partitions{
		StatCommander:     sc,
		BootEnvReadWriter: env,
		rootfsPartA:       config.rootfsPartA,
		rootfsPartB:       config.rootfsPartB,
		active:            "",
		inactive:          "",
	}
	device := device{env, sc, &partitions}
	return &device
}

func (d *device) Reboot() error {
	return d.Command("reboot").Run()
}

func (d *device) Rollback() error {
	// first get inactive partition
	inactivePartition, err := d.getInactivePartition()
	if err != nil {
		return err
	}
	log.Infof("setting partition for rollback: %s", inactivePartition)

	err = d.WriteEnv(BootVars{"mender_boot_part": inactivePartition, "upgrade_available": "0"})
	if err != nil {
		return err
	}
	log.Debug("Marking inactive partition as a boot candidate successful.")
	return nil
}

func (d *device) InstallUpdate(image io.ReadCloser, size int64) error {

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

func (d *device) getInactivePartition() (string, error) {
	inactivePartition, err := d.GetInactive()
	if err != nil {
		return "", errors.New("Error obtaining inactive partition: " + err.Error())
	}

	log.Debugf("Marking inactive partition (%s) as the new boot candidate.", inactivePartition)

	partitionNumber := inactivePartition[len(inactivePartition)-1:]
	if _, err := strconv.Atoi(partitionNumber); err != nil {
		return "", errors.New("Invalid inactive partition: " + inactivePartition)
	}

	return partitionNumber, nil
}

func (d *device) EnableUpdatedPartition() error {

	inactivePartition, err := d.getInactivePartition()
	if err != nil {
		return err
	}

	log.Info("Enabling partition with new image installed to be a boot candidate: ", string(inactivePartition))
	// For now we are only setting boot variables
	err = d.WriteEnv(BootVars{"upgrade_available": "1", "mender_boot_part": inactivePartition, "bootcount": "0"})
	if err != nil {
		return err
	}

	log.Debug("Marking inactive partition as a boot candidate successful.")

	return nil
}

func (d *device) CommitUpdate() error {
	log.Info("Commiting update")
	// For now set only appropriate boot flags
	return d.WriteEnv(BootVars{"upgrade_available": "0"})
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
