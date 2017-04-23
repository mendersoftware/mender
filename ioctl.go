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
	"os"
	"syscall"
	"unsafe"
)

// This is a bit weird, Syscall() says it accepts uintptr in the request field,
// but this in fact not true. By inspecting the calls with strace, it's clear
// that the pointer value is being passed as an int to ioctl(), which is just
// wrong. So write the ioctl request value (int) directly into the pointer value
// instead.
type ioctlRequestValue uintptr

var NotABlockDevice = errors.New("Not a block device.")

// Returns value in first return. Second returns error condition.
// If the device is not a block device NotABlockDevice error and
// value 0 will be returned.
func ioctl(fd uintptr, request ioctlRequestValue) (uint64, error) {
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

func getBlockDeviceSize(file *os.File) (uint64, error) {
	blkSize, err := ioctl(file.Fd(), BLKGETSIZE64)
	if err != nil {
		return 0, err
	}

	return blkSize, nil
}
