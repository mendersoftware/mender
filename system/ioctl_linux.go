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

// sysLinux wraps the interface to unix-specific system calls or functions
// that make assertions about the system is running linux.
type sysLinux interface {
	stat(string, *stat) error
	rawSyscall(uintptr, uintptr, uintptr, uintptr) (uintptr, uintptr, unix.Errno)
	ioctlSetInt(int, uint, int) error
	openMountInfo() (io.ReadCloser, error)
	deviceFromID([2]uint32) (string, error)
}

type linux struct{}

func (l *linux) stat(name string, stat *stat) error {
	return unix.Stat(name, (*unix.Stat_t)(stat))
}

func (l *linux) rawSyscall(req, p1, p2, p3 uintptr) (uintptr, uintptr, unix.Errno) {
	return unix.RawSyscall(req, p1, p2, p3)
}

func (l *linux) ioctlSetInt(fd int, req uint, value int) error {
	return unix.IoctlSetInt(fd, req, value)
}

func (l *linux) openMountInfo() (io.ReadCloser, error) {
	return os.Open("/proc/self/mountinfo")
}

func (l *linux) deviceFromID(devID [2]uint32) (string, error) {
	path := fmt.Sprintf("/dev/block/%d:%d", devID[0], devID[1])
	ret, err := filepath.EvalSymlinks(path)
	if err != nil {
		return ret, err
	}
	return filepath.Clean(ret), nil
}

var sys sysLinux = &linux{}
