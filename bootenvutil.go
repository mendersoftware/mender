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
  EnvCmd string
}

type Runner interface {
  Run(string, ...string) *exec.Cmd
}

type RealRunner struct{}
var runner Runner

// the real runner for the actual program, actually execs the command
func (r RealRunner) Run(command string, args ...string) *exec.Cmd {
  return exec.Command(command, args...)
}

func (c *UbootEnvCommand) Command(params ...string) (map[string]string, error) {

  cmd := runner.Run(c.EnvCmd, params...)
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

func GetBootEnv(var_name ...string) (map[string]string, error) {
  get_env := UbootEnvCommand{"fw_printenv"}
  return get_env.Command(var_name...)
}

func SetBootEnv(var_name string) bool {

  set_env := UbootEnvCommand{"fw_setenv"}
  if _, err := set_env.Command(var_name); err != nil {
    fmt.Fprintln(os.Stderr, "Error setting U-Boot variable:", err)
    return false
  }
  return true
}
