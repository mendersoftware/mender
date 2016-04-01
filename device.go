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
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/mendersoftware/log"
)

type UInstaller interface {
	InstallUpdate(io.ReadCloser, int64) error
	EnableUpdatedPartition() error
}

type UInstallCommitRebooter interface {
	UInstaller
	CommitUpdate() error
	Reboot() error
}

type device struct {
	BootEnvReadWriter
	Commander
	*partitions
}

func NewDevice(env BootEnvReadWriter, sc StatCommander, baseMount string) *device {
	partitions := partitions{sc, env, baseMount, "", "", getBlockDeviceSize}
	device := device{env, sc, &partitions}
	return &device
}

func (d *device) Reboot() error {
	return d.Command("reboot").Run()
}

func (d *device) InstallUpdate(image io.ReadCloser, size int64) error {

	if image == nil || size < 0 {
		return errors.New("Have invalid update. Aborting.")
	}

	incativePartition, err := d.GetInactive()
	if err != nil {
		return err
	}
	//TODO: fixme
	partitionSize, err := d.getPartitionSize(incativePartition)
	if err != nil {
		return err
	}

	if size <= partitionSize {
		if err := writeToPartition(image, size, incativePartition); err != nil {
			return err
		}
		return nil
	}
	return errors.New("Can not install image to partition. " +
		"Size of inactive partition is smaller than image size")
}

func (d *device) EnableUpdatedPartition() error {
	incativePartition, err := d.GetInactive()
	if err != nil {
		return err
	}
	partitionNumber := incativePartition[len(incativePartition)-1:]
	if _, err := strconv.Atoi(partitionNumber); err != nil {
		return errors.New("Invalid inactive partition: " + incativePartition)
	}

	log.Info("Enabling partition with new image installed to be a boot candidate: ", string(partitionNumber))
	// For now we are only setting boot variables
	err = d.WriteEnv(BootVars{"upgrade_available": "1", "boot_part": partitionNumber, "bootcount": "0"})
	if err != nil {
		return err
	}
	return nil
}

func (d *device) CommitUpdate() error {
	log.Info("Commiting update")
	// For now set only appropriate boot flags
	return d.WriteEnv(BootVars{"upgrade_available": "0"})
}

// Returns a byte stream of the fiven file, and also returns the size of the
// file.
func FetchUpdateFromFile(file string) (io.ReadCloser, int64, error) {
	fd, err := os.Open(file)
	if err != nil {
		return nil, 0, fmt.Errorf("Not able to open image file: %s: %s\n",
			file, err.Error())
	}

	imageInfo, err := fd.Stat()
	if err != nil {
		return nil, 0, fmt.Errorf("Unable to stat() file: %s: %s\n", file, err.Error())
	}

	return fd, imageInfo.Size(), nil
}

func writeToPartition(image io.Reader, imageSize int64, partition string) error {
	// Write image file into partition.
	partFd, err := os.OpenFile(partition, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("Not able to open partition: %s: %s\n",
			partition, err.Error())
	}
	defer partFd.Close()

	buf := make([]byte, 4096)
	for {
		read, readErr := image.Read(buf)

		if readErr != nil && readErr != io.EOF {
			return fmt.Errorf("Error while reading image file: %s", readErr.Error())
		}

		if read > 0 {
			_, writeErr := partFd.Write(buf[:read])
			if writeErr != nil {
				return fmt.Errorf("Error while writing to partition: %s: %s",
					partition, writeErr.Error())
			}
		}

		if readErr == io.EOF {
			break
		}
	}
	partFd.Sync()
	return nil
}
