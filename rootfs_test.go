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

import "testing"
import "os"

func TestMockUpdates(t *testing.T) {
	const dummy string = "dummy_image.dat"

	if os.MkdirAll("dev", 0755) != nil {
		t.Fatal("Not able to create dev directory")
	}
	defer os.RemoveAll("dev")

	for _, file := range []string{
		dummy,
		"dev/sda1",
		"dev/sda2",
		"dev/sda3"} {

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
			"fw_setenv upgrade_available 1",
			"fw_setenv boot_part 3",
			"fw_setenv bootcount 0")
		newRunner.outputs = []string{"", "", ""}
		newRunner.ret_codes = []int{0, 0, 0}
		runner = newRunner
		if err := doRootfs(dummy); err != nil {
			t.Fatalf("Updating image failed: %s", err.Error())
		}
	}

	// ---------------------------------------------------------------------

	// Now try to shrink one partition, it should now fail.

	file := "dev/sda3"
	part_fd, err := os.Create(file)
	if err != nil {
		t.Fatalf("Could not open '%s': %s", file, err.Error())
	}
	err = part_fd.Truncate(2048)
	if err != nil {
		t.Fatalf("Could not open '%s': %s", file, err.Error())
	}

	{
		tmp := "ShouldNeverRun"
		newRunner := &testRunner{&tmp, "", 257}
		runner = newRunner
		if err := doRootfs(dummy); err == nil {
			t.Fatal("Updating image should have failed " +
				"(partition too small)")
		}
	}
}
