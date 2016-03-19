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
	"os/exec"
	"strings"
	"syscall"
	"testing"
)

func Test_GetInactive_HaveActivePartitionSet_ReturnsInactive(t *testing.T) {
	partitionsSetup := []struct {
		active        string
		inactive      string
		expected      string
		expectedError error
	}{
		{"/dev/mmc2", "", "/dev/mmc3", nil},
		{"/dev/mmc3", "", "/dev/mmc2", nil},
		{"/dev/mmc", "", "", InvalidActivePartition},
		{"/dev/mmc4", "", "", InvalidActivePartition},
	}

	for _, testData := range partitionsSetup {
		fakePartitions := partitions{new(osCalls), new(uBootEnv), "", testData.active, testData.inactive, nil}
		inactive, err := fakePartitions.GetInactive()
		if err != testData.expectedError || strings.Compare(testData.expected, inactive) != 0 {
			t.Fatal(err)
		}
	}

}

type fakeStatCommander struct {
	file     os.FileInfo
	cmd      *exec.Cmd
	mountOut string
	err      error
}

func (sc fakeStatCommander) Command(name string, arg ...string) *exec.Cmd {
	return sc.cmd
}

func (sc fakeStatCommander) Stat(name string) (os.FileInfo, error) {
	return sc.file, sc.err
}

func Test_GetMountRoot(t *testing.T) {
	testRootCandidates := []struct {
		mountOut string
		expected string
	}{
		{"/dev/mmcblk0p2 on / type ext4 (rw,errors=remount-ro)", "/dev/mmcblk0p2"},
		{"invalid output", ""},
	}

	for _, test := range testRootCandidates {
		candidate := getRootCandidateFromMount([]byte(test.mountOut))
		if candidate != test.expected {
			t.Fatal("Invalid mount candidate received: ", candidate)
		}
	}
}

func Test_getRootDevice_HaveDevice_ReturnsDevice(t *testing.T) {
	testSC := fakeStatCommander{}
	testSC.err = errors.New("")

	if getRootDevice(testSC) != nil {
		t.Fail()
	}

	testSC.err = nil
	file, _ := os.Create("tempFile")
	testSC.file, _ = file.Stat()

	defer os.Remove("tempFile")

	if getRootDevice(testSC) == nil {
		t.Fail()
	}
}

func Test_matchRootWithMout_HaveValidMount(t *testing.T) {
	testSC := fakeStatCommander{}

	falseChecker := func(StatCommander, string, *syscall.Stat_t) bool { return false }
	trueChecker := func(StatCommander, string, *syscall.Stat_t) bool { return true }

	testData := []struct {
		rootChecker      func(StatCommander, string, *syscall.Stat_t) bool
		mounted          []string
		mountPoint       string
		expectedRootPart string
	}{
		{trueChecker, []string{"1", "2"}, "/dev/", "/dev/1"},
		{trueChecker, []string{"2", "1"}, "/dev/", "/dev/2"},
		{falseChecker, []string{"2", "1"}, "/dev/", ""},
	}

	for _, test := range testData {
		rootPart := getRootFromMountedDevices(testSC, test.rootChecker, test.mountPoint, test.mounted, nil)
		if rootPart != test.expectedRootPart {
			t.Fatalf("Received invalid root partition: [%s] expected: [%s]", rootPart, test.expectedRootPart)
		}
	}
}

// Be ready for the hard stuff...
// Hope this can be simplified somehow
func Test_getActivePartition_noActiveInactiveSet(t *testing.T) {
	// this will fake all exec.Commmand calls
	testOS := newTestOSCalls("", 0)

	testOS.err = nil
	file, _ := os.Create("tempFile")
	testOS.file, _ = file.Stat()

	defer os.Remove("tempFile")

	//this will fake all calls to get or set environment variables
	envCaller := newTestOSCalls("", 0)
	fakeEnv := uBootEnv{&envCaller}

	fakePartitions := partitions{&testOS, &fakeEnv, "", "", "", nil}

	trueChecker := func(StatCommander, string, *syscall.Stat_t) bool { return true }
	falseChecker := func(StatCommander, string, *syscall.Stat_t) bool { return false }

	testData := []struct {
		fakeExec       string
		fakeEnv        string
		fakeEnvRet     int
		rootChecker    func(StatCommander, string, *syscall.Stat_t) bool
		mountOutput    []string
		mountCallError error
		expectedError  error
		expectedActive string
	}{
		// have mount candidate to return
		{"/dev/mmcblk0p2 on / type ext4 (rw,errors=remount-ro)", "boot_part=1", 0, trueChecker, nil, nil, nil, "/dev/mmcblk0p2"},
		{"/dev/mmcblk0p2 on / type ext4 (rw,errors=remount-ro)", "boot_part=1", 0, falseChecker, nil, nil, RootPartitionDoesNotMatchMount, ""},
		// no mount candidate
		{"", "boot_part=1", 0, falseChecker, nil, nil, RootPartitionDoesNotMatchMount, ""},
		{"", "boot_part=1", 0, trueChecker, nil, nil, RootPartitionDoesNotMatchMount, ""},
		{"", "boot_part=1", 0, trueChecker, []string{"/dev/mmc1", "/dev/mmc2"}, nil, nil, "/dev/mmc1"},
		{"", "boot_part=1", 0, falseChecker, []string{"/dev/mmc1", "/dev/mmc2"}, nil, RootPartitionDoesNotMatchMount, ""},
		{"", "boot_part=2", 0, trueChecker, []string{"/dev/mmc1", "/dev/mmc2"}, nil, ErrorNoMatchBootPartRootPart, ""},
		{"", "boot_part=2", 1, trueChecker, []string{"/dev/mmc1", "/dev/mmc2"}, nil, ErrorNoMatchBootPartRootPart, ""},
	}

	for _, test := range testData {
		mountedDevicesGetter := func(string) ([]string, error) { return test.mountOutput, test.mountCallError }
		testOS.output = test.fakeExec
		envCaller.output = test.fakeEnv
		envCaller.retCode = test.fakeEnvRet
		active, err := fakePartitions.getAndSetActivePartition(test.rootChecker, mountedDevicesGetter)
		if err != test.expectedError || active != test.expectedActive {
			t.Fatal(err, active)
		}
	}
}

// env BootEnvReadWriter, stat StatCommander, baseMount string
func Test_getSizeOfPartition_haveVariousBDReturnCodes(t *testing.T) {

	fakePartitions := partitions{}
	fakePartitions.inactive = "/dev/1"
	testFile, _ := os.Create("tempFile")
	testSC := fakeStatCommander{}
	testSC.err = nil
	testSC.file, _ = testFile.Stat()
	fakePartitions.StatCommander = testSC

	defer os.Remove("tempFile")

	testData := []struct {
		bdSize        uint64
		bdError       error
		partitionFile string
		shouldFail    bool
	}{
		// make sure we can not read size of non-existing partition
		{0, nil, "/non/existing/partition", true},
		{0, nil, "tempFile", false},
		{0, NotABlockDevice, "tempFile", false},
		{0, errors.New(""), "tempFile", true},
	}

	for _, test := range testData {
		fakeBDGetSize := func(file *os.File) (uint64, error) { return test.bdSize, test.bdError }
		fakePartitions.blockDevSizeGetFunc = fakeBDGetSize

		_, err := fakePartitions.getPartitionSize(test.partitionFile)
		if (test.shouldFail && err == nil) || (!test.shouldFail && err != nil) {
			t.FailNow()
		}
	}
}
