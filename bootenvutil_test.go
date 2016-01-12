package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
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

type TestRunner struct {
	output   string
	ret_code int
}

func (r TestRunner) Run(command string, args ...string) *exec.Cmd {
	sub_args := []string{"-test.run=TestHelperProcessSuccess", "--"}

	//append helper process return code converted to string
	sub_args = append(sub_args, strconv.Itoa(r.ret_code))
	//append helper process return message
	sub_args = append(sub_args, r.output)

	cmd := exec.Command(os.Args[0], sub_args...)
	cmd.Env = []string{"NEED_MENDER_TEST_HELPER_PROCESS=1"}
	return cmd
}

func TestHelperProcessSuccess(*testing.T) {
	if os.Getenv("NEED_MENDER_TEST_HELPER_PROCESS") != "1" {
		return
	}

	//set helper process return code
	i, err := strconv.Atoi(os.Args[3])
	if err != nil {
		defer os.Exit(1)
	} else {
		defer os.Exit(i)
	}

	//check if we have something to print
	if len(os.Args) == 5 && os.Args[4] != "" {
		fmt.Println(os.Args[4])
	}
}

func TestSetEnvOK(t *testing.T) {
	runner = TestRunner{"", 0}

	if err := SetBootEnv("bootcnt", "3"); err != nil {
		t.FailNow()
	}
}

func TestSetEnvError(t *testing.T) {
	runner = TestRunner{"", 1}
	if err := SetBootEnv("bootcnt", "3"); err == nil {
		t.FailNow()
	}

	runner = TestRunner{"Cannot parse config file: No such file or directory\n", 1}
	if err := SetBootEnv("bootcnt", "3"); err == nil {
		t.FailNow()
	}

	runner = TestRunner{"Cannot parse config file: No such file or directory\n", 0}
	if err := SetBootEnv("bootcnt", "3"); err == nil {
		t.FailNow()
	}
}

func TestPrintEnvOK(t *testing.T) {
	runner = TestRunner{"arch=arm", 0}
	variables, err := GetBootEnv("arch")

	if err != nil || variables["arch"] != "arm" {
		t.FailNow()
	}
}

func TestPrintEnvOKMultipleBlankLines(t *testing.T) {
	runner = TestRunner{"arch=arm\n\n\n", 0}
	variables, err := GetBootEnv("arch")

	if err != nil || variables["arch"] != "arm" {
		t.FailNow()
	}
}

func TestPrintMultipleEnvOK(t *testing.T) {
	runner = TestRunner{"var1=1\nvar2=2", 0}
	variables, err := GetBootEnv("var1", "var2")

	if err != nil || variables["var1"] != "1" || variables["var2"] != "2" {
		t.FailNow()
	}
}

func TestPrintEnvWarning(t *testing.T) {
	runner = TestRunner{"Warning: Bad CRC, using default environment\nvar=1\n", 0}
	variables, err := GetBootEnv("var")
	if err == nil || variables != nil {
		t.FailNow()
	}

	runner = TestRunner{"Warning: Bad CRC, using default environment\nvar=1\n", 1}
	variables, err = GetBootEnv("var")
	if err == nil || variables != nil {
		t.FailNow()
	}
}

func TestPrintEnvNonExisting(t *testing.T) {
	runner = TestRunner{"## Error: \"non_existing_var\" not defined\n", 0}
	variables, err := GetBootEnv("non_existing_var")
	if err == nil || variables != nil {
		t.FailNow()
	}

	runner = TestRunner{"## Error: \"non_existing_var\" not defined\n", 1}
	variables, err = GetBootEnv("non_existing_var")
	if err == nil || variables != nil {
		t.FailNow()
	}
}
