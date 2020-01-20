// Copyright 2020 Northern.tech AS
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

var NotABlockDevice = errors.New("Not a block device.")

func IsUbiBlockDevice(deviceName string) bool {
	return sysfs.Class.Object("ubi").SubObject(deviceName).Exists()
}

func SetUbiUpdateVolume(file *os.File, imageSize int64) error {
	_, _, errno := unix.RawSyscall(unix.SYS_IOCTL,
		file.Fd(),
		UBI_IOCVOLUP,
		uintptr(unsafe.Pointer(&imageSize)))
	if errno != 0 {
		return errors.New(errno.Error())
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

// Gets the device file for the partition associated with path.
func GetFSBlockDev(path string) (string, error) {
	var stat unix.Stat_t

	if err := unix.Stat(path, &stat); err != nil {
		return "", err
	}

	devType := stat.Mode & unix.S_IFMT
	if devType == unix.S_IFBLK || devType == unix.S_IFCHR {
		return path, nil
	}

	devNum := stat.Dev
	// If path refers to a device file under /dev, then st_dev refers
	// to the device number of the mounted devfs. The device number for a
	// special file is under the st_rdev property, ref stat(2).
	if stat.Rdev != 0 {
		devNum = stat.Rdev
	}

	fsDevMajor := unix.Major(devNum)
	fsDevMinor := unix.Minor(devNum)

	devPath, err := filepath.EvalSymlinks(
		fmt.Sprintf("/dev/block/%d:%d", fsDevMajor, fsDevMinor))
	if err != nil {
		return "", errors.Wrap(err,
			"Error resolving device file path")
	}

	return devPath, nil
}

func GetBlockDeviceSectorSize(file *os.File) (int, error) {
	var sectorSize int
	var err error

	_, _, errno := unix.RawSyscall(unix.SYS_IOCTL,
		file.Fd(),
		unix.BLKSSZGET,
		uintptr(unsafe.Pointer(&sectorSize)))

	if errno != 0 {
		// ENOTTY: Inappropriate I/O control operation - in this context
		// it means that the file descriptor is not a block-device
		if errno == unix.ENOTTY {
			// Check if it is an UBI block device
			sectorSize, err = getUbiDeviceSectorSize(file)
			if err != nil {
				return 0, err
			}
		} else {
			return 0, errors.New(errno.Error())
		}
	}
	return sectorSize, nil
}

func GetBlockDeviceSize(file *os.File) (uint64, error) {
	var devSize uint64
	var err error
	_, _, errno := unix.RawSyscall(unix.SYS_IOCTL,
		uintptr(file.Fd()),
		uintptr(unsafe.Pointer(uintptr(unix.BLKGETSIZE64))),
		uintptr(unsafe.Pointer(&devSize)))

	if errno != 0 {
		// ENOTTY: Inappropriate I/O control operation - in this context
		// it means that the file descriptor is not a block-device
		if errno == unix.ENOTTY {
			// Check if it is an UBI block device
			devSize, err = getUbiDeviceSize(file)
			if err != nil {
				return 0, err
			}
		} else {
			return 0, errors.New(errno.Error())
		}
	}
	return devSize, nil
}
