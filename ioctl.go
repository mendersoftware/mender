// Copyright 2018 Northern.tech AS
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
	"os"
	"strings"
	"syscall"
	"unsafe"

	"github.com/ungerik/go-sysfs"
	"golang.org/x/sys/unix"
)

// This is a bit weird, Syscall() says it accepts uintptr in the request field,
// but this in fact not true. By inspecting the calls with strace, it's clear
// that the pointer value is being passed as an int to ioctl(), which is just
// wrong. So write the ioctl request value (int) directly into the pointer value
// instead.
type ioctlRequestValue uintptr

var NotABlockDevice = errors.New("Not a block device.")

func isUbiBlockDevice(deviceName string) bool {
	return sysfs.Class.Object("ubi").SubObject(deviceName).Exists()
}

func setUbiUpdateVolume(file *os.File, imageSize int64) error {
	err := ioctlWrite(file.Fd(), unix.UBI_IOCVOLUP, imageSize)
	if err != nil {
		return err
	}

	return nil
}

func getUbiDeviceSectorSize(file *os.File) (int, error) {
	dev := strings.TrimPrefix(file.Name(), "/dev/")

	ebSize := sysfs.Class.Object("ubi").SubObject(dev).Attribute("usable_eb_size")

	if !ebSize.Exists() {
		return 0, NotABlockDevice
	}

	sectorSize, err := ebSize.ReadUint64()
	if err != nil {
		return 0, NotABlockDevice
	}

	return int(sectorSize), nil
}

func getUbiDeviceSize(file *os.File) (uint64, error) {
	dev := strings.TrimPrefix(file.Name(), "/dev/")

	dataBytes := sysfs.Class.Object("ubi").SubObject(dev).Attribute("data_bytes")

	if !dataBytes.Exists() {
		return 0, NotABlockDevice
	}

	devSize, err := dataBytes.ReadUint64()
	if err != nil {
		return 0, NotABlockDevice
	}

	return devSize, nil
}

// Returns value in first return. Second returns error condition.
// If the device is not a block device NotABlockDevice error and
// value 0 will be returned.
func ioctlRead(fd uintptr, request ioctlRequestValue) (uint64, error) {
	var response uint64
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd,
		uintptr(unsafe.Pointer(request)),
		uintptr(unsafe.Pointer(&response)))

	if errno == syscall.ENOTTY {
		// This means the descriptor is not a block device.
		// ENOTTY... weird, I know.
		return 0, NotABlockDevice
	} else if errno != 0 {
		return 0, errno
	}

	return response, nil
}

func ioctlWrite(fd uintptr, request ioctlRequestValue, data int64) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd,
		uintptr(unsafe.Pointer(request)),
		uintptr(unsafe.Pointer(&data)))

	if errno == syscall.ENOTTY {
		// This means the descriptor is not a block device.
		// ENOTTY... weird, I know.
		return NotABlockDevice
	} else if errno != 0 {
		return errno
	}

	return nil
}

func getBlockDeviceSectorSize(file *os.File) (int, error) {
	var sectorSize int

	blockSectorSize, err := ioctlRead(file.Fd(), unix.BLKSSZGET)
	if err != nil && err != NotABlockDevice {
		return 0, err
	}

	if err == NotABlockDevice {
		// Check if it is an UBI block device
		sectorSize, err = getUbiDeviceSectorSize(file)
		if err != nil {
			return 0, err
		}
	} else {
		sectorSize = int(blockSectorSize)
	}

	return sectorSize, nil
}

func getBlockDeviceSize(file *os.File) (uint64, error) {
	var devSize uint64

	blkSize, err := ioctlRead(file.Fd(), unix.BLKGETSIZE64)
	if err != nil && err != NotABlockDevice {
		return 0, err
	}

	if err == NotABlockDevice {
		// Check if it is an UBI block device
		devSize, err = getUbiDeviceSize(file)
		if err != nil {
			return 0, err
		}
	} else {
		devSize = blkSize
	}

	return devSize, nil
}
