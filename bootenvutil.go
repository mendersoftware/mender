package main

import (
	"bufio"
	"errors"
	"log"
	"os"
	"os/exec"
	"strings"
)

type UbootEnvCommand struct {
	EnvCmd string
}

type UbootVars map[string]string

type Runner interface {
	Run(string, ...string) *exec.Cmd
}

type RealRunner struct{}

var (
	runner Runner
	Log    *log.Logger
)

func init() {
	runner = RealRunner{}
	Log = log.New(os.Stdout, "MESSAGE:", log.Ldate|log.Ltime|log.Lshortfile)
}

// the real runner for the actual program, actually execs the command
func (r RealRunner) Run(command string, args ...string) *exec.Cmd {
	return exec.Command(command, args...)
}

func (c *UbootEnvCommand) Command(params ...string) (UbootVars, error) {

	cmd := runner.Run(c.EnvCmd, params...)
	cmdReader, err := cmd.StdoutPipe()

	if err != nil {
		Log.Println("Error creating StdoutPipe:", err)
		return nil, err
	}

	scanner := bufio.NewScanner(cmdReader)

	err = cmd.Start()
	if err != nil {
		Log.Println("There was an error getting or setting U-Boot env")
		return nil, err
	}

	var env_variables = make(UbootVars)

	for scanner.Scan() {
		Log.Println("Have U-Boot variable:", scanner.Text())
		splited_line := strings.Split(scanner.Text(), "=")

		//we are having empty line (usually at the end of output)
		if scanner.Text() == "" {
			continue
		}

		//we have some malformed data or Warning/Error
		if len(splited_line) != 2 {
			Log.Println("U-Boot variable malformed or error occured")
			return nil, errors.New("Invalid U-Boot variable or error: " + scanner.Text())
		}

		env_variables[splited_line[0]] = splited_line[1]
	}

	err = cmd.Wait()
	if err != nil {
		Log.Println("U-Boot env command returned non zero status")
		return nil, err
	}

	if len(env_variables) > 0 {
		Log.Println("List of U-Boot variables:", env_variables)
	}

	return env_variables, err
}

func GetBootEnv(var_name ...string) (UbootVars, error) {
	get_env := UbootEnvCommand{"fw_printenv"}
	return get_env.Command(var_name...)
}

func SetBootEnv(var_name string, value string) error {

	set_env := UbootEnvCommand{"fw_setenv"}

	if _, err := set_env.Command(var_name, value); err != nil {
		Log.Println("Error setting U-Boot variable:", err)
		return err
	}
	return nil
}
