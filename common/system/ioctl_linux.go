// Copyright 2021 Northern.tech AS
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

// +build !windows

package system

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

type stat unix.Stat_t

// SysLinux wraps the interface to unix-specific system calls or functions
// that make assertions about the system is running linux.
type SysLinux interface {
	Stat(string, *stat) error
	RawSyscall(uintptr, uintptr, uintptr, uintptr) (uintptr, uintptr, unix.Errno)
	IoctlSetInt(int, uint, int) error
	OpenMountInfo() (io.ReadCloser, error)
	DeviceFromID([2]uint32) (string, error)
	GetPipeSize(fd int) int
}

type linux struct{}

func (l *linux) Stat(name string, stat *stat) error {
	return unix.Stat(name, (*unix.Stat_t)(stat))
}

func (l *linux) RawSyscall(req, p1, p2, p3 uintptr) (uintptr, uintptr, unix.Errno) {
	return unix.RawSyscall(req, p1, p2, p3)
}

func (l *linux) IoctlSetInt(fd int, req uint, value int) error {
	return unix.IoctlSetInt(fd, req, value)
}

func (l *linux) OpenMountInfo() (io.ReadCloser, error) {
	return os.Open("/proc/self/mountinfo")
}

func (l *linux) DeviceFromID(devID [2]uint32) (string, error) {
	path := fmt.Sprintf("/dev/block/%d:%d", devID[0], devID[1])
	ret, err := filepath.EvalSymlinks(path)
	if err != nil {
		return ret, err
	}
	return filepath.Clean(ret), nil
}

func (l *linux) GetPipeSize(fd int) int {
	sz, err := unix.FcntlInt(uintptr(fd), unix.F_GETPIPE_SZ, 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return sz
}

// Sys is an interface for system-level operations such as system calls.
var sys SysLinux = &linux{}
