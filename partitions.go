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

import "errors"
import "fmt"
import "io"
import "os"
import "path"
import "strings"
import "syscall"

// Will change in testing.
var baseMountDevice string = "/dev/mmcblk0p"

type statterType interface {
	Stat(file string) (os.FileInfo, error)
}

type realStatter struct {
}

func (self *realStatter) Stat(file string) (os.FileInfo, error) {
	return os.Stat(file)
}

var statter statterType = &realStatter{}

func getMountedRoot() (string, error) {
	output, err := runner.run("mount").Output()
	candidate := ""
	if err == nil {
		for _, line := range strings.Split(string(output), "\n") {
			fields := strings.Split(line, " ")
			if len(fields) >= 3 && fields[2] == "/" {
				candidate = fields[0]
			}
		}
	}

	rootStat, err := statter.Stat("/")
	if err != nil {
		// Seriously??
		// Something is *very* wrong.
		return "", err
	}
	root := rootStat.Sys().(*syscall.Stat_t)

	if candidate != "" {
		if isMountedRoot(candidate, root) {
			return candidate, nil
		}
		// If not just fall through to next part.
	}

	devDir := path.Dir(baseMountDevice)
	devFd, err := os.Open(devDir)
	if err != nil {
		return "", err
	}
	defer devFd.Close()

	for {
		names, err := devFd.Readdirnames(10)
		if err == io.EOF {
			break
		} else if err != nil {
			return "", err
		}
		for i := 0; i < len(names); i += 1 {
			joinedPath := path.Join(devDir, names[i])
			if isMountedRoot(joinedPath, root) {
				return joinedPath, nil
			}
		}
	}

	return "", errors.New("Could not determine currently mounted root " +
		"device: No device matches device ID of root filesystem")
}

func isMountedRoot(dev string, root *syscall.Stat_t) bool {
	// First check if the filename is even remotely correct.
	if len(dev) < len(baseMountDevice) ||
		dev[:len(baseMountDevice)] != baseMountDevice {
		return false
	}

	// Check if this is a device file and its device ID matches that of the
	// root directory.
	stat, err := statter.Stat(dev)
	if err != nil ||
		(stat.Mode()&os.ModeDevice) == 0 ||
		stat.Sys().(*syscall.Stat_t).Rdev != root.Dev {
		return false
	}

	return true
}

func getActivePartition() (string, error) {
	ret, err := getActivePartitionNumber()
	return baseMountDevice + ret, err
}

func getActivePartitionNumber() (string, error) {
	mounted_root, err := getMountedRoot()
	if err != nil {
		return "", err
	}
	if mounted_root[0:len(mounted_root)-1] != baseMountDevice {
		return "", fmt.Errorf("Unexpected active partition: %s",
			mounted_root)
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

	switch uboot_num {
	case "2", "3":
		return uboot_num, nil
	default:
		return "", fmt.Errorf("Unexpected partition number: %s",
			uboot_num)
	}
}

func getInactivePartition() (string, error) {
	ret, err := getInactivePartitionNumber()
	return baseMountDevice + ret, err
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
