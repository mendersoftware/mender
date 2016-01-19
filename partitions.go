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

import "strings"
import "errors"
import "fmt"

// Will change in testing.
var base_mount_device string = "/dev/mmcblk0p"

func getMountedRoot() (string, error) {
	output, err := runner.run("mount").Output()
	if err != nil {
		return "", err
	}

	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Split(line, " ")
		if len(fields) >= 3 && fields[2] == "/" {
			return fields[0], nil
		}
	}

	return "", errors.New("Could not determine currently mounted root " +
		"device")
}

func getActivePartition() (string, error) {
	ret, err := getActivePartitionNumber()
	return base_mount_device + ret, err
}

func getActivePartitionNumber() (string, error) {
	mounted_root, err := getMountedRoot()
	if err != nil ||
		mounted_root[0:len(mounted_root)-1] != base_mount_device {
		return "", err
	}
	mounted_num := mounted_root[len(mounted_root)-1 : len(mounted_root)]

	uboot_env_list, err := getBootEnv("boot_part")
	if err != nil {
		return "", err
	}
	uboot_num := uboot_env_list["boot_part"]

	if mounted_num != uboot_num {
		return "", fmt.Errorf("'mount' and U-Boot don't agree on "+
			"which partition is active: ['%s', '%s']",
			mounted_num, uboot_num)
	}

	return uboot_num, nil
}

func getInactivePartition() (string, error) {
	ret, err := getInactivePartitionNumber()
	return base_mount_device + ret, err
}

func getInactivePartitionNumber() (string, error) {
	active, err := getActivePartitionNumber()
	if err != nil {
		return "", err
	}

	switch active {
	case "2":
		return "3", nil
	case "3":
		return "2", nil
	default:
		return "", errors.New("Unexpected active partition returned: " +
			active)
	}
}
