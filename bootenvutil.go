package main

import (
  "fmt"
  "os"
  "os/exec"
)

type EnvVar struct {
  Name    string
  Value   string
}

type UbootEnvCommand struct {
  Cmd string

}

func (c *UbootEnvCommand) Command(params ...string) *exec.Cmd {
  return exec.Command(c.Cmd, params...)
}

func GetBootEnv(name ...string) {
  var (
		cmdOut []byte
		err    error
	)

  get_env := UbootEnvCommand{"echo"}

  if cmdOut, err = get_env.Command(name...).Output(); err != nil {
    fmt.Fprintln(os.Stderr, "There was an error getting U-Boot env: ", err)
		os.Exit(1)
  }

  data := string(cmdOut)
  fmt.Println("The first six chars of the SHA at HEAD in this repo are", data)

}
