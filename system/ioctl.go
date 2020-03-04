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
	"bufio"
	"fmt"
	"os"
	"strings"
	"unsafe"

	log "github.com/sirupsen/logrus"
	"github.com/pkg/errors"
	"github.com/ungerik/go-sysfs"
	"golang.org/x/sys/unix"
)

const (
	// ioctl magics from <linux/fs.h>
	IOCTL_FIFREEZE_MAGIC uint = 0xC0045877 // _IOWR('X', 119, int)
	IOCTL_FITHAW_MAGIC   uint = 0xC0045878 // _IOWR('X', 120, int)
)

var (
	ErrDevNotMounted = fmt.Errorf("device not mounted")
	NotABlockDevice  = fmt.Errorf("not a block device")
)

// MountInfo maps a single line in /proc/<pid|self>/mountinfo
// See the linux kernel documentation: linux/Documentation/filesystems/proc.txt
// A line takes the form:
// 36 35 98:0 /mnt1 /mnt2 rw,noatime master:1 - ext3 /dev/root rw,errors=continue
// (1)(2)(3)   (4)   (5)      (6)      (7)   (8) (9)   (10)         (11)
type MountInfo struct {
	// MountID: is the unique identifier of the mount
	MountID uint32 // (1)
	// ParentID: is the MountID of the parent (or self if on top)
	ParentID uint32 // (2)
	// DevID: is the st_dev uint32{Major, Minor} number of the device
	DevID [2]uint32 // (3)
	// Root: root of the mount within the filesystem
	Root string // (4)
	// MountPoint: mount point relative to the process's root
	MountPoint string // (5)
	// MountSource: filesystem specific information or "none"
	MountSource string // (10)
	// FSType: name of the filesystem of the form "type[.subtype]"
	FSType string // (9)
	// MountOptions: per mount options
	MountOptions []string // (6)
	// TagFields: optional list of fields of the form "tag[:value]"
	TagFields []string // (7)
	// SuperOptions: per super block options
	SuperOptions []string // (11)
}

// GetMountInfoFromDeviceID parses /proc/self/mountinfo and, on success, returns
// a populated MountInfo for the device given the devID
// ([2]uint32{major, minor}). If the device is not mounted ErrDevNotMounted is
// returned, otherwise the function returns an internal error with a descriptive
// error message.
// NOTE: You can get the mount info of an arbitrary path by first calling
//       "GetDeviceIDFromPath".
// Pro tip: use together with GetDeviceIDFromPath to get
func GetMountInfoFromDeviceID(devID [2]uint32) (*MountInfo, error) {
	var major, minor uint32

	fdes, err := sys.OpenMountInfo()
	if err != nil {
		return nil, fmt.Errorf(
			"failed to get mountpoint: %s", err.Error())
	}
	defer fdes.Close()
	// scan /proc/mounts and find mountpoint (2)
	procScanner := bufio.NewScanner(fdes)

	// Match entry by device ID
	for procScanner.Scan() {
		fields := strings.Fields(procScanner.Text())
		if len(fields) < 10 {
			log.Debugf("Failed to parse mount entry: '%s' "+
				"(invalid format)",
				procScanner.Text())
			continue
		}
		_, err := fmt.Sscanf(fields[2], "%d:%d", &major, &minor)
		if err != nil {
			log.Debugf("Failed to parse device id field: '%s'",
				procScanner.Text())
			continue
		}

		if major == devID[0] && minor == devID[1] {
			var mntID, parentID uint32
			var root, mntPt, mntOpts string
			var tagFields []string
			entry := procScanner.Text()
			_, err := fmt.Sscanf(entry, "%d %d %d:%d %s %s %s",
				&mntID, &parentID, &devID[0], &devID[1], &root,
				&mntPt, &mntOpts)
			if err != nil {
				return nil, fmt.Errorf(
					"malformed mountinfo format: %s", err.Error())
			}
			if len(fields) > 11 {
				tagFields = fields[6 : len(fields)-4]
			}
			return &MountInfo{
				MountID:      mntID,
				ParentID:     parentID,
				DevID:        devID,
				Root:         root,
				MountPoint:   mntPt,
				TagFields:    tagFields,
				FSType:       fields[len(fields)-3],
				MountSource:  fields[len(fields)-2],
				MountOptions: strings.Split(mntOpts, ","),
				SuperOptions: strings.Split(
					fields[len(fields)-1], ","),
			}, nil
		}
	}
	return nil, ErrDevNotMounted
}

// GetBlockDeviceFromID returns the expanded path to the device with the
// given device ID, devID, on the form [2]uint32{major, minor}
func GetBlockDeviceFromID(devID [2]uint32) (string, error) {
	return sys.DeviceFromID(devID)
}

// GetDeviceIDFromPath retrieves the device id for the block device pointed to by
// the inode at path.
func GetDeviceIDFromPath(path string) ([2]uint32, error) {
	var stat stat

	if err := sys.Stat(path, &stat); err != nil {
		return [2]uint32{^uint32(0), ^uint32(0)}, errors.Wrapf(err,
			"stat %s", path)
	}

	devType := stat.Mode & unix.S_IFMT

	switch devType {
	// If path refers to a special file (e.g. device file under /dev), then
	// st_dev refers to the device number of the mounted devfs. The device
	// number for a special file is under the st_rdev property, ref stat(2).
	case unix.S_IFBLK,
		unix.S_IFCHR,
		unix.S_IFIFO,
		unix.S_IFSOCK:
		return [2]uint32{
			unix.Major(stat.Rdev), unix.Minor(stat.Rdev)}, nil

	// If path refers to a regular file, then st_dev gives the device number
	// of the underlying block device.
	case unix.S_IFDIR,
		unix.S_IFREG,
		unix.S_IFLNK:
		return [2]uint32{
			unix.Major(stat.Dev), unix.Minor(stat.Dev)}, nil
	}
	return [2]uint32{^uint32(0), ^uint32(0)},
		fmt.Errorf("invalid stat(2) st_mode %04X", devType)
}

// GetPipeSize returns the buffer-size of a pipe or 1 if the file descriptor
// is not a pipe.
func GetPipeSize(fd int) int {
	return sys.GetPipeSize(fd)
}

func IsUbiBlockDevice(deviceName string) bool {
	return sysfs.Class.Object("ubi").SubObject(deviceName).Exists()
}

func SetUbiUpdateVolume(file *os.File, imageSize uint64) error {
	_, _, errno := sys.RawSyscall(
		uintptr(unix.SYS_IOCTL), file.Fd(),
		uintptr(unix.UBI_IOCVOLUP),
		uintptr(unsafe.Pointer(&imageSize)))
	if errno != 0 {
		return errno
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

func GetBlockDeviceSectorSize(file *os.File) (int, error) {
	var sectorSize int
	var err error

	_, _, errno := sys.RawSyscall(
		uintptr(unix.SYS_IOCTL),
		file.Fd(),
		uintptr(unix.BLKSSZGET),
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
	_, _, errno := sys.RawSyscall(
		uintptr(unix.SYS_IOCTL), uintptr(file.Fd()),
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

// FreezeFS freezes the filesystem for which the inode that fd points to belongs
// to, maintaining read-consistency. All write operations to the filesystem will
// be blocked until ThawFS is called.
func FreezeFS(fd int) error {
	err := sys.IoctlSetInt(fd, IOCTL_FIFREEZE_MAGIC, 0)
	if err != nil {
		return errors.Wrap(err, "error freezing fs from writing")
	}
	return nil
}

// ThawFS unfreezes the filesystem after FreezeFS is called.
// The error returned by this function is system critical, if we can't unfreeze
// the filesystem, we need to ask the user to run `fsfreeze -u /` if this fails
// then the user has no option but to "pull the plug" (or sys request unfreeze?)
func ThawFS(fd int) error {
	err := sys.IoctlSetInt(fd, IOCTL_FITHAW_MAGIC, 0)
	if err != nil {
		return errors.Wrap(err, "Error un-freezing fs for writing")
	}
	return nil
}
