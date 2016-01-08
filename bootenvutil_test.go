package main

import (
  "testing"
)

func TestSetEnvOK(t *testing.T) {
  set_env := UbootEnvCommand{"true"}

  _, err := set_env.Command("var_name")
  if err != nil {
    t.FailNow()
  }
}

func TestSetEnvError(t *testing.T) {
  set_env := UbootEnvCommand{"false"}

  _, err := set_env.Command("var_name")
  if err == nil {
    t.FailNow()
  }
}

func TestSetEnvNoConfigFile(t *testing.T) {
  set_env := UbootEnvCommand{"echo"}

  _, err := set_env.Command("Cannot parse config file: No such file or directory")
  if err == nil {
    t.FailNow()
  }
}

func TestPrintEnvOK(t *testing.T) {
  set_env := UbootEnvCommand{"echo"}

  variables, err := set_env.Command("var=1\n")
  if err != nil || variables["var"] != "1" {
    t.FailNow()
  }
}


func TestPrintMultipleEnvOK(t *testing.T) {
  set_env := UbootEnvCommand{"echo"}

  variables, err := set_env.Command("var1=1\nvar2=2")
  if err != nil || variables["var1"] != "1" || variables["var2"] != "2" {
    t.FailNow()
  }
}

func TestPrintEnvWarning(t *testing.T) {
  set_env := UbootEnvCommand{"echo"}

  variables, err := set_env.Command("Warning: Bad CRC, using default environment\nvar=1\n")
  if err == nil {
    t.FailNow()
  }

  if variables != nil {
    t.FailNow()
  }
}

func TestPrintEnvNonExisting(t *testing.T) {
  set_env := UbootEnvCommand{"echo"}

  variables, err := set_env.Command("## Error: \"non_existing_var\" not defined\n")
  if err == nil {
    t.FailNow()
  }

  if variables != nil {
    t.FailNow()
  }
}

//TODO: implement
/*
func TestSetEnvReal(t *testing.T) {
  TestSetEnvOK(t)
  TestPrintEnvOK(t)
}

func TestSetEnvClearReal(t *testing.T) {
  TestSetEnvOK(t)
  TestPrintEnvNonExisting(t)
}
*/
