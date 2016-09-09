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
	"fmt"
	"io"
	"os"
	"strconv"

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
		StatCommander:       sc,
		BootEnvReadWriter:   env,
		rootfsPartA:         config.rootfsPartA,
		rootfsPartB:         config.rootfsPartB,
		active:              "",
		inactive:            "",
		blockDevSizeGetFunc: getBlockDeviceSize,
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

	log.Debugf("Trying to install update: [%v] of size: %d", image, size)
	if image == nil || size < 0 {
		return errors.New("Have invalid update. Aborting.")
	}

	inactivePartition, err := d.GetInactive()
	if err != nil {
		return err
	}

	log.Debugf("Installing update to inactive partition: %s", inactivePartition)

	partitionSize, err := d.getPartitionSize(inactivePartition)
	if err != nil {
		return err
	}

	if size <= partitionSize {
		if err := writeToPartition(image, size, inactivePartition); err != nil {
			return err
		}
		return nil
	}
	return errors.Errorf("inactive partition %s too small, partition: %v image %v",
		inactivePartition, partitionSize, size)
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

func writeToPartition(image io.Reader, imageSize int64, partition string) error {
	// Write image file into partition.
	log.Debugf("Writing image [%v] to partition: %s.", image, partition)
	partFd, err := os.OpenFile(partition, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("Not able to open partition: %s: %s\n",
			partition, err.Error())
	}
	defer partFd.Close()

	if _, err = io.Copy(partFd, image); err != nil {
		return err
	}

	partFd.Sync()
	return nil
}
