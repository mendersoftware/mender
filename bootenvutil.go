package main

import (
  "bufio"
  "fmt"
  "os"
  "os/exec"
  "strings"
  "errors"
)

type UbootEnvCommand struct {
  Cmd string

}

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

func (c *UbootEnvCommand) Command(params ...string) (map[string]string, error) {

  cmd := exec.Command(c.Cmd, params...)
  cmdReader, err := cmd.StdoutPipe()

  if err != nil {
    fmt.Fprintln(os.Stderr, "Error creating StdoutPipe:", err)
		return nil, err
  }

  scanner := bufio.NewScanner(cmdReader)

  err = cmd.Start()
  if err != nil {
    fmt.Fprintln(os.Stderr, "There was an error getting or setting U-Boot env")
		return nil, err
  }

  var env_variables = make(map[string]string)

  for scanner.Scan() {
    fmt.Println("Have U-Boot variable:", scanner.Text())
    splited_line := strings.Split(scanner.Text(), "=")

    //we are having empty line (usually at the end of output)
    if scanner.Text() == "" {
      continue
    }

    //we have some malformed data or Warning/Error
    if len(splited_line) != 2 {
      fmt.Fprintln(os.Stderr, "U-Boot variable malformed or error occured")
      return nil, errors.New("Invalid U-Boot variable or error: " + scanner.Text())
    }

    env_variables[splited_line[0]] = splited_line[1]
  }

  err = cmd.Wait()
  if err != nil {
    fmt.Fprintln(os.Stderr, "U-Boot env command returned non zero status")
		return nil, err
  }

  if len(env_variables) > 0 {
    fmt.Println("List of U-Boot variables:", env_variables)
  }

  return env_variables, err
}

func GetBootEnv(var_name ...string) {
  var (
    env_variables map[string]string
    err error
  )

  get_env := UbootEnvCommand{"fw_printenv"}
  env_variables, err = get_env.Command(var_name...)
}

func SetBootEnv(var_name string) bool {

  set_env := UbootEnvCommand{"fw_setenv"}
  if _, err := set_env.Command(var_name); err != nil {
    fmt.Fprintln(os.Stderr, "Error setting U-Boot variable:", err)
    return false
  }
  return true
}
