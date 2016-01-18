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
	"testing"
)

//if no config file is present
//Cannot parse config file: No such file or directory

//no valid device in config file
//Cannot access MTD device /mnt/uboot.env: No such file or directory

//fw_printenv
//arch=arm
//baudrate=115200
//board=rpi

//fw_printenv arch
//arch=arm

//fw_printenv non_existing_var
//## Error: "non_existing_var" not defined

//fw_printenv
//Warning: Bad CRC, using default environment
//bootcmd=run distro_bootcmd
//bootdelay=2

//fw_setenv name value
//this prints nothing on success just returns 0

//fw_setenv name value
//Cannot access MTD device /mnt/uboot.env: No such file or directory
//Error: environment not initialized

//fw_setenv name
//this removes env variable; prints nothing on success just returns 0

func TestSetEnvOK(t *testing.T) {
	runner = testRunner{"", 0}

	if err := setBootEnv("bootcnt", "3"); err != nil {
		t.FailNow()
	}
}

func TestSetEnvError(t *testing.T) {
	runner = testRunner{"", 1}
	if err := setBootEnv("bootcnt", "3"); err == nil {
		t.FailNow()
	}

	runner = testRunner{"Cannot parse config file: No such file or directory\n", 1}
	if err := setBootEnv("bootcnt", "3"); err == nil {
		t.FailNow()
	}

	runner = testRunner{"Cannot parse config file: No such file or directory\n", 0}
	if err := setBootEnv("bootcnt", "3"); err == nil {
		t.FailNow()
	}
}

func TestPrintEnvOK(t *testing.T) {
	runner = testRunner{"arch=arm", 0}
	variables, err := getBootEnv("arch")

	if err != nil || variables["arch"] != "arm" {
		t.FailNow()
	}
}

func TestPrintEnvOKMultipleBlankLines(t *testing.T) {
	runner = testRunner{"arch=arm\n\n\n", 0}
	variables, err := getBootEnv("arch")

	if err != nil || variables["arch"] != "arm" {
		t.FailNow()
	}
}

func TestPrintMultipleEnvOK(t *testing.T) {
	runner = testRunner{"var1=1\nvar2=2", 0}
	variables, err := getBootEnv("var1", "var2")

	if err != nil || variables["var1"] != "1" || variables["var2"] != "2" {
		t.FailNow()
	}
}

func TestPrintEnvWarning(t *testing.T) {
	runner = testRunner{"Warning: Bad CRC, using default environment\nvar=1\n", 0}
	variables, err := getBootEnv("var")
	if err == nil || variables != nil {
		t.FailNow()
	}

	runner = testRunner{"Warning: Bad CRC, using default environment\nvar=1\n", 1}
	variables, err = getBootEnv("var")
	if err == nil || variables != nil {
		t.FailNow()
	}
}

func TestPrintEnvNonExisting(t *testing.T) {
	runner = testRunner{"## Error: \"non_existing_var\" not defined\n", 0}
	variables, err := getBootEnv("non_existing_var")
	if err == nil || variables != nil {
		t.FailNow()
	}

	runner = testRunner{"## Error: \"non_existing_var\" not defined\n", 1}
	variables, err = getBootEnv("non_existing_var")
	if err == nil || variables != nil {
		t.FailNow()
	}
}
