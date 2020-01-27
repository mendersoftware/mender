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
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"golang.org/x/sys/unix"
)

var Msys *SysMock = &SysMock{}

func init() {
	sys = Msys
}

func TestGetMountInfoFromDeviceID(t *testing.T) {
	const validMountInfo = "36 35 98:0 /mnt1 /mnt2 rw,noatime master:1 " +
		"- ext3 /dev/root rw,errors=continue"

	// Provoke this scanfError as part of the testCase expectations
	var tmp int
	_, scanfIntErr := fmt.Sscanf("abv", "%d", &tmp)

	testCases := []struct {
		Name string

		DevID          [2]uint32
		MountInfoError error
		MountInfo      string
		ReturnErrorMsg string
	}{
		{
			Name:           "Success",
			DevID:          [2]uint32{98, 0},
			MountInfoError: nil,
			MountInfo:      validMountInfo,
			ReturnErrorMsg: "",
		},
		{
			Name:           "Fail - open returns error",
			DevID:          [2]uint32{1, 2},
			MountInfoError: fmt.Errorf("io error"),
			ReturnErrorMsg: "failed to get mountpoint: io error",
		},
		{
			Name:           "Fail - malformed mount file",
			DevID:          [2]uint32{3, 4},
			MountInfoError: nil,
			MountInfo:      "o k 3:4 m e n d e r e r\n",
			ReturnErrorMsg: fmt.Sprintf(
				"malformed mountinfo format: %s",
				scanfIntErr.Error()),
		},
		{
			Name:           "Fail - device not mounted",
			DevID:          [2]uint32{2, 3},
			MountInfoError: nil,
			MountInfo:      validMountInfo,
			ReturnErrorMsg: ErrDevNotMounted.Error(),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {

			if tc.MountInfoError != nil {
				Msys.On("OpenMountInfo").Return(
					nil, tc.MountInfoError).Once()
			} else {
				mountInfo := ioutil.NopCloser(
					bytes.NewReader([]byte(tc.MountInfo)))
				Msys.On("OpenMountInfo").Return(
					mountInfo, nil).Once()
			}

			ret, err := GetMountInfoFromDeviceID(tc.DevID)
			if tc.ReturnErrorMsg != "" {
				assert.EqualError(
					t, err, tc.ReturnErrorMsg)
				assert.Nil(t, ret)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, ret)
			}
		})
	}
}

func TestSetUbiUpdateVolume(t *testing.T) {
	testFile, err := ioutil.TempFile("", "test")
	if err != nil {
		t.Fatal("Failed to initialize test tempfile")
		return
	}
	defer func() {
		err := os.Remove(testFile.Name())
		if err != nil {
			t.Logf("ERROR: Failed to remove tempfile '%v': %v",
				testFile.Name(), err)
		}
	}()
	imageSize := uint64(123)
	Msys.On("RawSyscall", uintptr(unix.SYS_IOCTL), testFile.Fd(),
		uintptr(unix.UBI_IOCVOLUP),
		uintptr(imageSize)).Return(1, 2, unix.Errno(0))

	err = SetUbiUpdateVolume(testFile, imageSize)
	assert.NoError(t, err)

	// See note at RawSyscall regarding Errno
	Msys.eno = unix.ENOTTY
	err = SetUbiUpdateVolume(testFile, imageSize)
	assert.EqualError(t, err, unix.ENOTTY.Error())
}

func TestFreezeFs(t *testing.T) {
	tmpFile, err := ioutil.TempFile("", "test")
	if err != nil {
		t.Fatal("Failed to initialize required test-file")
		return
	}
	defer func() {
		tmpFile.Close()
		err := os.Remove(tmpFile.Name())
		if err != nil {
			t.Logf("ERROR: failed to clean up after test: %v", err)
		}
	}()

	Msys.On("IoctlSetInt", int(tmpFile.Fd()), IOCTL_FIFREEZE_MAGIC, 0).
		Return(nil).Once()
	err = FreezeFS(int(tmpFile.Fd()))
	assert.NoError(t, err)

	mockErr := fmt.Errorf("mock mock... who's there?")
	Msys.On("IoctlSetInt", int(tmpFile.Fd()), IOCTL_FIFREEZE_MAGIC, 0).
		Return(mockErr).Once()
	err = FreezeFS(int(tmpFile.Fd()))
	assert.Error(t, err)
	assert.EqualError(t, err,
		"error freezing fs from writing: "+mockErr.Error())
}

// Epilogue with a mock for the sys interface
type SysMock struct {
	mock.Mock
	eno unix.Errno
}

func (m *SysMock) Stat(name string, fStat *stat) error {
	ret := m.Called(name, fStat)

	var r0 error
	if rf, ok := ret.Get(0).(func(string, *stat) error); ok {
		r0 = rf(name, fStat)
	} else {
		r0 = ret.Error(0)
	}

	return r0

}

func (m *SysMock) RawSyscall(req, p1, p2, p3 uintptr) (uintptr, uintptr, unix.Errno) {
	ret := m.Called(req, p1, p2, p3)

	var r0 uintptr
	if rf, ok := ret.Get(0).(func(
		uintptr, uintptr, uintptr, uintptr) uintptr); ok {
		r0 = rf(req, p1, p2, p3)
	} else {
		if ret.Get(0) != nil {
			ret0 := ret.Get(0)
			switch ret0.(type) {
			case uintptr:
				r0 = ret0.(uintptr)
			case int:
				r0 = uintptr(ret0.(int))
			}
		}
	}
	var r1 uintptr
	if rf, ok := ret.Get(1).(func(
		uintptr, uintptr, uintptr, uintptr) uintptr); ok {
		r1 = rf(req, p1, p2, p3)
	} else {
		if ret.Get(1) != nil {
			ret1 := ret.Get(1)
			switch ret1.(type) {
			case uintptr:
				r1 = ret1.(uintptr)
			case int:
				r1 = uintptr(ret1.(int))
			}
		}
	}
	var r2 unix.Errno
	// NOTE: for some reason the way mock uses the reflect package
	//       the unix.Errno type always becomes 0, so we'll have to work
	//       around this shortcoming
	r2 = Msys.eno
	// if rf, ok := ret.Get(2).(func(
	// 	uintptr, uintptr, uintptr, uintptr) unix.Errno); ok {
	// 	r2 = rf(req, p1, p2, p2)
	// } else {
	// 	if ret.Get(2) != nil {
	// 		r2 = ret.Get(2).(unix.Errno)
	// 	}
	// }
	return r0, r1, r2
}

func (m *SysMock) IoctlSetInt(fd int, req uint, value int) error {
	ret := m.Called(fd, req, value)

	var r0 error
	if rf, ok := ret.Get(0).(func(int, uint, int) error); ok {
		r0 = rf(fd, req, value)
	} else {
		r0 = ret.Error(0)
	}
	return r0
}

func (m *SysMock) OpenMountInfo() (io.ReadCloser, error) {
	ret := m.Called()

	var r0 io.ReadCloser
	if rf, ok := ret.Get(0).(func() io.ReadCloser); ok {
		r0 = rf()
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(io.ReadCloser)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func() error); ok {
		r1 = rf()
	} else {
		r1 = ret.Error(1)
	}
	return r0, r1
}

func (m *SysMock) DeviceFromID(devID [2]uint32) (string, error) {
	ret := m.Called()

	var r0 string
	if rf, ok := ret.Get(0).(func([2]uint32) string); ok {
		r0 = rf(devID)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(string)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func([2]uint32) error); ok {
		r1 = rf(devID)
	} else {
		r1 = ret.Error(1)
	}
	return r0, r1
}

func (m *SysMock) GetPipeSize(fd int) int {
	return 1
}
