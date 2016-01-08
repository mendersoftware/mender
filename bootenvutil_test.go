package main

import (
  "testing"
  "os/exec"
  "os"
  "fmt"
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

type TestRunnerSuccess struct{}
type TestRunnerError struct{}

func (r TestRunnerSuccess) Run(command string, args ...string) *exec.Cmd {
  sub_args := []string {"-test.run=TestHelperProcessSuccess", "--"}
  sub_args = append(sub_args, args...)

  cmd := exec.Command(os.Args[0], sub_args...)
  cmd.Env = []string{"NEED_MENDER_TEST_HELPER_PROCESS=1"}
  return cmd
}

func (r TestRunnerError) Run(command string, args ...string) *exec.Cmd {
  sub_args := []string {"-test.run=TestHelperProcessError", "--"}
  sub_args = append(sub_args, args...)

  cmd := exec.Command(os.Args[0], sub_args...)
  cmd.Env = []string{"NEED_MENDER_TEST_HELPER_PROCESS=1"}
  return cmd
}

func TestHelperProcessSuccess(*testing.T) {
  if os.Getenv("NEED_MENDER_TEST_HELPER_PROCESS") != "1" {
      return
  }
  defer os.Exit(0)
  fmt.Println(os.Args[3])
}

func TestHelperProcessError(*testing.T) {
  if os.Getenv("NEED_MENDER_TEST_HELPER_PROCESS") != "1" {
      return
  }
  defer os.Exit(1)
  fmt.Println(os.Args[3])
}

func TestSetEnvOK(t *testing.T) {
  runner = TestRunnerSuccess{}

  if SetBootEnv("") == false {
    t.FailNow()
  }
}

func TestSetEnvError(t *testing.T) {
  runner = TestRunnerError{}

  if SetBootEnv("") == true {
    t.FailNow()
  }

  if SetBootEnv("Cannot parse config file: No such file or directory\n") == true {
    t.FailNow()
  }

  runner = TestRunnerSuccess{}
  if SetBootEnv("Cannot parse config file: No such file or directory\n") == true {
    t.FailNow()
  }
}

func TestSetEnvNoConfigFile(t *testing.T) {
  runner = TestRunnerError{}

  if SetBootEnv("Cannot parse config file: No such file or directory") == true {
    t.FailNow()
  }
}

func TestPrintEnvOK(t *testing.T) {
  runner = TestRunnerSuccess{}
  variables, err:= GetBootEnv("arch=arm\n")

  if err != nil || variables["arch"] != "arm" {
    t.FailNow()
  }
}


func TestPrintMultipleEnvOK(t *testing.T) {
  runner = TestRunnerSuccess{}
  variables, err := GetBootEnv("var1=1\nvar2=2")

  if err != nil || variables["var1"] != "1" || variables["var2"] != "2" {
    t.FailNow()
  }
}

func TestPrintEnvWarning(t *testing.T) {
  runner = TestRunnerSuccess{}
  variables, err := GetBootEnv("Warning: Bad CRC, using default environment\nvar=1\n")
  if err == nil {
    t.FailNow()
  }

  if variables != nil {
    t.FailNow()
  }

  runner = TestRunnerError{}
  variables, err = GetBootEnv("Warning: Bad CRC, using default environment\nvar=1\n")
  if err == nil {
    t.FailNow()
  }

  if variables != nil {
    t.FailNow()
  }
}

func TestPrintEnvNonExisting(t *testing.T) {
  runner = TestRunnerSuccess{}
  variables, err := GetBootEnv("## Error: \"non_existing_var\" not defined\n")
  if err == nil {
    t.FailNow()
  }

  if variables != nil {
    t.FailNow()
  }

  runner = TestRunnerError{}
  variables, err = GetBootEnv("## Error: \"non_existing_var\" not defined\n")
  if err == nil {
    t.FailNow()
  }

  if variables != nil {
    t.FailNow()
  }
}
