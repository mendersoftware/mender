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
// +build !mock

package main

import "os/exec"
import "strings"
import "errors"
import "fmt"

const base_mount_device string = "/dev/mmcblk0p"

type partitionsInterface interface {
	getMountedRoot() (string, error)
	getActivePartition() (string, error)
	getInactivePartition() (string, error)
	getActivePartitionNumber() (string, error)
	getInactivePartitionNumber() (string, error)
}

type partitionsType struct{}

// Used for normal partitions.XXX() calls. In testing this is switched with
// mock functions.
var partitions partitionsInterface = new(partitionsType)

func (self *partitionsType) getMountedRoot() (string, error) {
	output, err := exec.Command("mount").Output()
	if err != nil {
		return "", err
	}

	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Split(line, " ")
		if fields[2] == "/" {
			return fields[0], nil
		}
	}

	return "", errors.New("Could not determine currently mounted root device")
}

func (self *partitionsType) getActivePartition() (string, error) {
	ret, err := self.getActivePartitionNumber()
	return base_mount_device + ret, err
}

func (self *partitionsType) getActivePartitionNumber() (string, error) {
	mounted_root, err := self.getMountedRoot()
	if err != nil || mounted_root[0:len(mounted_root)-1] != base_mount_device {
		return "", err
	}
	mounted_num := mounted_root[len(mounted_root)-1 : len(mounted_root)]

	uboot_env_list, err := GetBootEnv("boot_part")
	if err != nil {
		return "", err
	}
	uboot_num := uboot_env_list["boot_part"]

	if mounted_num != uboot_num {
		return "", fmt.Errorf("'mount' and U-Boot don't agree on "+
			"which partition is active: ['%s', '%s']", mounted_num, uboot_num)
	}

	return uboot_num, nil
}

func (self *partitionsType) getInactivePartition() (string, error) {
	ret, err := self.getInactivePartitionNumber()
	return base_mount_device + ret, err
}

func (self *partitionsType) getInactivePartitionNumber() (string, error) {
	active, err := self.getActivePartitionNumber()
	if err != nil {
		return "", err
	}

	switch active {
	case "2":
		return "3", nil
	case "3":
		return "2", nil
	default:
		return "", errors.New("Unexpected active partition returned: " + active)
	}
}

func (self *partitionsType) enableUpdatedPartition() error {
	act, err := self.getInactivePartitionNumber()
	if err != nil {
		return err
	}

	err = SetBootEnv("upgrade_available", "1")
	if err != nil {
		return err
	}
	err = SetBootEnv("boot_part", act)
	if err != nil {
		return err
	}
	// TODO REMOVE?
	err = SetBootEnv("bootcount", "0")
	if err != nil {
		return err
	}

	return nil
}
