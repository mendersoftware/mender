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
package system

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"github.com/pkg/errors"
	"github.com/ungerik/go-sysfs"
	"golang.org/x/sys/unix"
)

const (
	// ioctl magics from <linux/fs.h>
	IOCTL_FIFREEZE_MAGIC = 0xC0045877 // _IOWR('X', 119, int)
	IOCTL_FITHAW_MAGIC   = 0xC0045878 // _IOWR('X', 120, int)
)

// This is a bit weird, Syscall() says it accepts uintptr in the request field,
// but this in fact not true. By inspecting the calls with strace, it's clear
// that the pointer value is being passed as an int to ioctl(), which is just
// wrong. So write the ioctl request value (int) directly into the pointer value
// instead.
type ioctlRequestValue uintptr

var NotABlockDevice = errors.New("Not a block device.")

func IsUbiBlockDevice(deviceName string) bool {
	return sysfs.Class.Object("ubi").SubObject(deviceName).Exists()
}

func SetUbiUpdateVolume(file *os.File, imageSize int64) error {
	err := ioctlWrite(file.Fd(), UBI_IOCVOLUP, imageSize)
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

	reservedEraseBlocks := sysfs.Class.Object("ubi").SubObject(dev).Attribute("reserved_ebs")
	ebSize := sysfs.Class.Object("ubi").SubObject(dev).Attribute("usable_eb_size")

	if !reservedEraseBlocks.Exists() || !ebSize.Exists() {
		return 0, NotABlockDevice
	}

	sectorSize, err := ebSize.ReadUint64()
	if err != nil {
		return 0, NotABlockDevice
	}

	reservedSectors, err := reservedEraseBlocks.ReadUint64()
	if err != nil {
		return 0, NotABlockDevice
	}

	return reservedSectors * sectorSize, nil
}

// Freezes the filesystem the fsRootPath belongs to, maintaing read-consistency.
// All write operations to the filesystem will be blocked until ThawFS is called.
func FreezeFS(fsRootPath string) error {
	fd, err := unix.Open(fsRootPath, unix.O_DIRECTORY, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	err = unix.IoctlSetInt(fd, IOCTL_FIFREEZE_MAGIC, 0)
	if err != nil {
		return errors.Wrap(err, "Error freezing fs from writing")
	}

	return nil
}

// Unfreezes the filesystem after FreezeFS is called.
// The error returned by this function is system critical, if we can't unfreeze
// the filesystem, we need to ask the user to run `fsfreeze -u /` if this fails
// then the user has no option but to "pull the plug" (or sys request unfreeze?)
func ThawFS(fsRootPath string) error {
	fd, err := unix.Open(fsRootPath, unix.O_DIRECTORY, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	err = unix.IoctlSetInt(fd, IOCTL_FITHAW_MAGIC, 0)
	if err != nil {
		return errors.Wrap(err, "Error un-freezing fs for writing")
	}
	return nil
}

// Gets the device file for the partition associated with the fsRootPath.
func GetFSDevFile(fsRootPath string) (string, error) {
	var statfs unix.Statfs_t
	var stat unix.Stat_t

	if err := unix.Statfs(fsRootPath, &statfs); err != nil {
		return "", err
	}

	if err := unix.Stat(fsRootPath, &stat); err != nil {
		return "", err
	}

	fsDevMajor := unix.Major(stat.Dev)
	fsDevMinor := unix.Minor(stat.Dev)

	devPath, err := filepath.EvalSymlinks(
		fmt.Sprintf("/dev/block/%d:%d", fsDevMajor, fsDevMinor))
	if err != nil {
		return "", errors.Wrap(err, "Error resolving device file path")
	}

	return devPath, nil
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

func GetBlockDeviceSectorSize(file *os.File) (int, error) {
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

func GetBlockDeviceSize(file *os.File) (uint64, error) {
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
