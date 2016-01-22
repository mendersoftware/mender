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

import "os"
import "testing"
import "time"

func init() {
	base_mount_device = "./dev/mmcblk0p"
}

func getModTime(t *testing.T, file string) time.Time {
	info, err := os.Stat(file)
	if err != nil {
		t.Fatalf("Stat() failed for '%s'", file)
	}
	// Sleep one second to ensure that the next call will return a different
	// value if file is written to.
	time.Sleep(time.Second)
	return info.ModTime()
}

func TestMockRootfs(t *testing.T) {
	const dummy string = "dummy_image.dat"

	if os.MkdirAll("dev", 0755) != nil {
		t.Fatal("Not able to create dev directory")
	}
	defer os.RemoveAll("dev")

	for _, file := range []string{
		dummy,
		base_mount_device + "1",
		base_mount_device + "2",
		base_mount_device + "3"} {

		defer os.Remove(file)

		fd, err := os.Create(file)
		if err != nil {
			t.Fatalf("Not able to open '%s' for writing: %s",
				file, err.Error())
		}
		defer fd.Close()

		buf := make([]byte, 4096)

		// Write dummy data
		_, err = fd.Write(buf)
		if err != nil {
			t.Fatalf("Cannot write to '%s': %s", file, err.Error())
		}
	}

	// ---------------------------------------------------------------------

	// Try to execute rootfs operation with the dummy file.
	{
		newRunner := &testRunnerMulti{}
		newRunner.cmdlines = StringPointerList(
			"mount ",
			"fw_printenv boot_part",
			"mount ",
			"fw_printenv boot_part",
			"fw_setenv upgrade_available 1",
			"fw_setenv boot_part 3",
			"fw_setenv bootcount 0")

		mount_output :=
			base_mount_device + "2 on / type ext4 (rw)\n" +
				"proc on /proc type proc (rw,noexec,nosuid,nodev)\n" +
				base_mount_device + "1 on /boot type ext4 (rw)\n"
		newRunner.outputs = []string{
			mount_output,
			"boot_part=2",
			mount_output,
			"boot_part=2",
			"",
			"",
			""}

		newRunner.ret_codes = []int{
			0,
			0,
			0,
			0,
			0,
			0,
			0}

		runner = newRunner
		prev := getModTime(t, base_mount_device+"3")
		if err := doMain([]string{"-rootfs", dummy}); err != nil {
			t.Fatalf("Updating image failed: %s", err.Error())
		}
		assertTrue(t, prev != getModTime(t, base_mount_device+"3"))
	}

	// ---------------------------------------------------------------------

	// Try to execute rootfs operation with the dummy file.
	{
		newRunner := &testRunnerMulti{}
		newRunner.cmdlines = StringPointerList(
			"mount ",
			"fw_printenv boot_part",
			"mount ",
			"fw_printenv boot_part",
			"fw_setenv upgrade_available 1",
			"fw_setenv boot_part 3",
			"fw_setenv bootcount 0")

		mount_output :=
			base_mount_device + "2 on / type ext4 (rw)\n" +
				"proc on /proc type proc (rw,noexec,nosuid,nodev)\n" +
				base_mount_device + "1 on /boot type ext4 (rw)\n"
		newRunner.outputs = []string{
			mount_output,
			"boot_part=2",
			mount_output,
			"boot_part=2",
			"",
			"",
			""}

		newRunner.ret_codes = []int{
			0,
			0,
			0,
			0,
			1,
			0,
			0}

		runner = newRunner
		if err := doMain([]string{"-rootfs", dummy}); err == nil {
			t.Fatal("Enabling the partition should have failed")
		} else {
			assertErrorSubstring(t, err, "Unable to activate partition")
		}
	}

	// ---------------------------------------------------------------------

	// Now try to shrink one partition, it should now fail.

	file := base_mount_device + "3"
	part_fd, err := os.Create(file)
	if err != nil {
		t.Fatalf("Could not open '%s': %s", file, err.Error())
	}
	err = part_fd.Truncate(2048)
	if err != nil {
		t.Fatalf("Could not open '%s': %s", file, err.Error())
	}

	{
		newRunner := &testRunnerMulti{}

		newRunner.cmdlines = StringPointerList(
			"mount ",
			"fw_printenv boot_part",
			"mount ",
			"fw_printenv boot_part",
			"fw_setenv upgrade_available 1",
			"fw_setenv boot_part 3",
			"fw_setenv bootcount 0")

		mount_output :=
			base_mount_device + "2 on / type ext4 (rw)\n" +
				"proc on /proc type proc (rw,noexec,nosuid,nodev)\n" +
				base_mount_device + "1 on /boot type ext4 (rw)\n"
		newRunner.outputs = []string{
			mount_output,
			"boot_part=2",
			mount_output,
			"boot_part=2",
			"",
			"",
			""}

		newRunner.ret_codes = []int{
			0,
			0,
			0,
			0,
			0,
			0,
			0}

		runner = newRunner
		prev := getModTime(t, base_mount_device+"3")
		if err := doMain([]string{"-rootfs", dummy}); err == nil {
			t.Fatal("Updating image should have failed " +
				"(partition too small)")
		}
		assertTrue(t, prev == getModTime(t, base_mount_device+"3"))
	}

	// ---------------------------------------------------------------------

	// Try to query active partition again, when U-Boot and mount don't
	// agree.

	{
		newRunner := &testRunnerMulti{}

		newRunner.cmdlines = StringPointerList(
			"mount ",
			"fw_printenv boot_part")

		mount_output :=
			base_mount_device + "2 on / type ext4 (rw)\n" +
				"proc on /proc type proc (rw,noexec,nosuid,nodev)\n" +
				base_mount_device + "1 on /boot type ext4 (rw)\n"
		newRunner.outputs = []string{
			mount_output,
			"boot_part=3"}

		newRunner.ret_codes = []int{
			0,
			0}

		runner = newRunner
		prev := getModTime(t, base_mount_device+"3")
		err := doMain([]string{"-rootfs", dummy})
		if err == nil {
			t.Fatal("Updating image should have failed " +
				"(mount and U-Boot don't agree on boot " +
				"partition)")
		}
		assertTrue(t, prev == getModTime(t, base_mount_device+"3"))
		assertErrorSubstring(t, err,
			"agree")
	}

	// ---------------------------------------------------------------------

	{
		mount_cmd := "mount "
		newRunner := &testRunner{&mount_cmd, "blah", 0}

		runner = newRunner
		err := doMain([]string{"-rootfs", dummy})
		if err == nil {
			t.Fatal("Updating image should have failed " +
				"(mount parsing failed)")
		}
		assertErrorSubstring(t, err,
			"Could not determine currently mounted root")
	}
}

func TestMockCommitRootfs(t *testing.T) {
	newRunner := &testRunnerMulti{}

	newRunner.cmdlines = StringPointerList(
		"fw_setenv upgrade_available 0",
		"fw_setenv bootcount 0")

	newRunner.outputs = []string{
		"",
		""}

	newRunner.ret_codes = []int{
		0,
		0}

	runner = newRunner
	if err := doMain([]string{"-commit"}); err != nil {
		t.Fatal("Could not commit rootfs")
	}
}

func TestPartitionsAPI(t *testing.T) {
	// Test various parts of the partitions API.

	{
		newRunner := &testRunnerMulti{}

		newRunner.cmdlines = StringPointerList(
			"mount ",
			"fw_printenv boot_part",
			"mount ",
			"fw_printenv boot_part",
			"mount ",
			"fw_printenv boot_part",
			"mount ",
			"fw_printenv boot_part")

		mount_output3 :=
			base_mount_device + "3 on / type ext4 (rw)\n" +
				"proc on /proc type proc (rw,noexec,nosuid,nodev)\n" +
				base_mount_device + "1 on /boot type ext4 (rw)\n"
		mount_output4 :=
			base_mount_device + "4 on / type ext4 (rw)\n" +
				"proc on /proc type proc (rw,noexec,nosuid,nodev)\n" +
				base_mount_device + "1 on /boot type ext4 (rw)\n"
		newRunner.outputs = []string{
			mount_output3,
			"boot_part=3",
			mount_output3,
			"boot_part=3",
			mount_output4,
			"boot_part=4",
			mount_output4,
			"boot_part=3"}

		newRunner.ret_codes = []int{
			0,
			0,
			0,
			0,
			0,
			0,
			0,
			0}

		runner = newRunner

		part, err := getActivePartition()
		assertTrue(t, err == nil)
		assertStringEqual(t, part, base_mount_device+"3")

		part, err = getInactivePartition()
		assertTrue(t, err == nil)
		assertStringEqual(t, part, base_mount_device+"2")

		part, err = getInactivePartition()
		assertTrue(t, err != nil)

		part, err = getActivePartition()
		assertTrue(t, err != nil)
	}
}
