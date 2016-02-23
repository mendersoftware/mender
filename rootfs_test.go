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

import "bytes"
import "fmt"
import "io"
import "net/http"
import mt "github.com/mendersoftware/mendertesting"
import "net"
import "os"
import "testing"

const dummy string = "dummy_image.dat"

// Unlikely-to-be-used port number.
const testPortString string = "8081"

func init() {
	base_mount_device = "./dev/mmcblk0p"
}

// Compares the contents of two files. However if:
// `n = min(size(file1), size(file2))`, then only `n` bytes will be compared,
// so the biggest file will not have all content compared.
func checkFileOverlapEqual(t *testing.T, file1, file2 string) bool {
	var buf1 [4096]byte
	var buf2 [4096]byte

	fd1, err := os.Open(file1)
	if err != nil {
		t.Logf("Could not open %s: %s", file1, err.Error())
		return false
	}
	defer fd1.Close()

	fd2, err := os.Open(file2)
	if err != nil {
		t.Logf("Could not open %s: %s", file2, err.Error())
		return false
	}
	defer fd2.Close()

	for {
		n1, err := fd1.Read(buf1[:])
		if n1 == 0 && err == io.EOF {
			break
		}

		n2, err := fd2.Read(buf2[:])
		if n2 == 0 && err == io.EOF {
			break
		}

		n := n2
		if n1 < n2 {
			n = n1
		}
		if bytes.Compare(buf1[:n], buf2[:n]) != 0 {
			return false
		}
	}

	return true
}

func prepareMockDevices(t *testing.T) {
	if os.MkdirAll("dev", 0755) != nil {
		t.Fatal("Not able to create dev directory")
	}

	for _, file := range []string{
		dummy,
		base_mount_device + "1",
		base_mount_device + "2",
		base_mount_device + "3"} {

		fd, err := os.Create(file)
		if err != nil {
			t.Fatalf("Not able to open '%s' for writing: %s",
				file, err.Error())
		}
		defer fd.Close()

		buf := make([]byte, 4096)
		copy(buf, []byte(file+" content"))

		// Write dummy data
		_, err = fd.Write(buf)
		if err != nil {
			t.Fatalf("Cannot write to '%s': %s", file, err.Error())
		}
	}
}

func cleanupMockDevices() {
	os.RemoveAll("dev")
}

// Test various ways to upgrade using a file. See each block for comments about
// each section.
func TestMockRootfs(t *testing.T) {
	prepareMockDevices(t)
	defer cleanupMockDevices()

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
		if err := doMain([]string{"-rootfs", dummy}); err != nil {
			t.Fatalf("Updating image failed: %s", err.Error())
		}
		mt.AssertTrue(t, checkFileOverlapEqual(t, base_mount_device+"3", dummy))
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
			mt.AssertErrorSubstring(t, err, "Unable to activate partition")
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
		if err := doMain([]string{"-rootfs", dummy}); err == nil {
			t.Fatal("Updating image should have failed " +
				"(partition too small)")
		}
		mt.AssertTrue(t, !checkFileOverlapEqual(t, base_mount_device+"3", dummy))
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
		err := doMain([]string{"-rootfs", dummy})
		if err == nil {
			t.Fatal("Updating image should have failed " +
				"(mount and U-Boot don't agree on boot " +
				"partition)")
		}
		mt.AssertTrue(t, !checkFileOverlapEqual(t, base_mount_device+"3", dummy))
		mt.AssertErrorSubstring(t, err,
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
		mt.AssertErrorSubstring(t, err,
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
		mt.AssertTrue(t, err == nil)
		mt.AssertStringEqual(t, part, base_mount_device+"3")

		part, err = getInactivePartition()
		mt.AssertTrue(t, err == nil)
		mt.AssertStringEqual(t, part, base_mount_device+"2")

		part, err = getInactivePartition()
		mt.AssertTrue(t, err != nil)

		part, err = getActivePartition()
		mt.AssertTrue(t, err != nil)
	}
}

// Test network updates, very similar to TestMockRootfs, but using network as
// the transport for the image.
func TestNetworkRootfs(t *testing.T) {
	prepareMockDevices(t)
	defer cleanupMockDevices()

	var server http.Server

	server.Handler = http.FileServer(http.Dir("."))
	addr := ":" + testPortString
	listen, err := net.Listen("tcp", addr)
	mt.AssertNoError(t, err)

	defer listen.Close()
	go server.Serve(listen)

	// Do this test twice, once with a valid update, and once with a too
	// short/broken update.
	for _, mode := range []int{0, os.O_TRUNC}[:] {
		executeNetworkTest(t, mode)
	}
}

func executeNetworkTest(t *testing.T, mode int) {
	imageFd, err := os.OpenFile(dummy, os.O_WRONLY|mode, 0777)
	mt.AssertNoError(t, err)

	const imageString string = "CORRECT UPDATE"
	n, err := imageFd.Write([]byte(imageString))
	mt.AssertNoError(t, err)
	mt.AssertTrue(t, n == len(imageString))
	imageFd.Close()

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
	httpString := fmt.Sprintf("http://localhost:%s/%s", testPortString,
		dummy)
	err = doMain([]string{"-rootfs", httpString})
	if err != nil {
		if mode == os.O_TRUNC {
			// This update should fail.
			mt.AssertErrorSubstring(t, err, "Less than")
			return
		}
		t.Fatalf("Updating image failed: %s", err.Error())
	} else {
		if mode == os.O_TRUNC {
			t.Fatal("Update should have failed")
		}
	}
	mt.AssertTrue(t, checkFileOverlapEqual(t, base_mount_device+"3", dummy))

	fd, err := os.Open(base_mount_device + "3")
	mt.AssertNoError(t, err)
	buf := new([len(imageString)]byte)
	n, err = fd.Read(buf[:])
	mt.AssertNoError(t, err)
	mt.AssertTrue(t, n == len(imageString))
	mt.AssertStringEqual(t, string(buf[:]), imageString)

	fd.Close()
}
