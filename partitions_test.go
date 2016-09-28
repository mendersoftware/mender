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
	"github.com/stretchr/testify/assert"
	"os"
	"os/exec"
	"sort"
	"strings"
	"syscall"
	"testing"
)

func Test_GetInactive_HaveActivePartitionSet_ReturnsInactive(t *testing.T) {
	partitionsSetup := []struct {
		active        string
		inactive      string
		rootfsPartA   string
		rootfsPartB   string
		expected      string
		expectedError error
	}{
		{"/dev/mmc2", "", "/dev/mmc2", "/dev/mmc3", "/dev/mmc3", nil},
		{"/dev/mmc3", "", "/dev/mmc2", "/dev/mmc3", "/dev/mmc2", nil},
		{"/dev/mmc", "", "/dev/mmc2", "/dev/mmc3", "", ErrorPartitionNoMatchActive},
		{"/dev/mmc4", "", "/dev/mmc2", "/dev/mmc3", "", ErrorPartitionNoMatchActive},
		{"/dev/mmc2", "", "/dev/mmc2", "/dev/mmc2", "", ErrorPartitionNumberSame},
		{"/dev/mmc2", "", "/dev/mmc2", "", "", ErrorPartitionNumberNotSet},
		{"/dev/mmc2", "", "", "/dev/mmc2", "", ErrorPartitionNumberNotSet},
	}

	for _, testData := range partitionsSetup {
		fakePartitions := partitions{
			StatCommander:     new(osCalls),
			BootEnvReadWriter: new(uBootEnv),
			rootfsPartA:       testData.rootfsPartA,
			rootfsPartB:       testData.rootfsPartB,
			active:            testData.active,
			inactive:          testData.inactive,
		}
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
		expectedRootPart string
		success          bool
	}{
		{trueChecker, []string{"/dev/1", "/dev/2"}, "/dev/1", true},
		{trueChecker, []string{"/dev/2", "/dev/1"}, "/dev/2", true},
		{falseChecker, []string{"/dev/2", "/dev/1"}, "", false},
	}

	for _, test := range testData {
		rootPart, err := getRootFromMountedDevices(testSC, test.rootChecker, test.mounted, nil)
		assert.True(t, (test.success && err == nil) || (!test.success && err != nil))
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

	fakePartitions := partitions{
		StatCommander:     &testOS,
		BootEnvReadWriter: &fakeEnv,
		rootfsPartA:       "/dev/mmcblk0p2",
		rootfsPartB:       "/dev/mmcblk0p3",
		active:            "",
		inactive:          "",
	}

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
		{"/dev/mmcblk0p2 on / type ext4 (rw,errors=remount-ro)", "mender_boot_part=1", 0, trueChecker, nil, nil, nil, "/dev/mmcblk0p2"},
		{"/dev/mmcblk0p2 on / type ext4 (rw,errors=remount-ro)", "mender_boot_part=1", 0, falseChecker, nil, nil, RootPartitionDoesNotMatchMount, ""},
		// no mount candidate
		{"", "mender_boot_part=1", 0, falseChecker, nil, nil, RootPartitionDoesNotMatchMount, ""},
		{"", "mender_boot_part=1", 0, trueChecker, nil, nil, RootPartitionDoesNotMatchMount, ""},
		{"", "mender_boot_part=1", 0, trueChecker, []string{"/dev/mmc1", "/dev/mmc2"}, nil, nil, "/dev/mmc1"},
		{"", "mender_boot_part=1", 0, falseChecker, []string{"/dev/mmc1", "/dev/mmc2"}, nil, RootPartitionDoesNotMatchMount, ""},
		{"", "mender_boot_part=2", 0, trueChecker, []string{"/dev/mmc1", "/dev/mmc2"}, nil, ErrorNoMatchBootPartRootPart, ""},
		{"", "mender_boot_part=2", 1, trueChecker, []string{"/dev/mmc1", "/dev/mmc2"}, nil, ErrorNoMatchBootPartRootPart, ""},
	}

	for _, test := range testData {
		mountedDevicesGetter := func(string) ([]string, error) { return test.mountOutput, test.mountCallError }
		testOS.output = test.fakeExec
		envCaller.output = test.fakeEnv
		envCaller.retCode = test.fakeEnvRet
		active, err := fakePartitions.getAndCacheActivePartition(test.rootChecker, mountedDevicesGetter)
		if err != test.expectedError || active != test.expectedActive {
			t.Fatal(err, active)
		}
	}
}

func Test_getAllMountedDevices(t *testing.T) {
	_, err := getAllMountedDevices("dev-tmp")
	assert.Error(t, err)

	assert.NoError(t, os.MkdirAll("dev-tmp", 0755))
	defer os.RemoveAll("dev-tmp")

	expected := []string{
		"dev-tmp/mmc1",
		"dev-tmp/mmc2",
		"dev-tmp/mmc3",
	}

	for _, entry := range expected {
		file, err := os.Create(entry)
		assert.NoError(t, err)
		file.Close()
	}

	names, err := getAllMountedDevices("dev-tmp")
	assert.NoError(t, err)
	var actual sort.StringSlice = names
	sort.Sort(actual)
	assert.Equal(t, actual, sort.StringSlice(expected))
}
