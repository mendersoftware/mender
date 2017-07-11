// Copyright 2017 Northern.tech AS
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
	"bufio"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/mendersoftware/log"
)

type uBootEnv struct {
	Commander
}

func NewEnvironment(cmd Commander) *uBootEnv {
	env := uBootEnv{cmd}
	return &env
}

func (e *uBootEnv) ReadEnv(names ...string) (BootVars, error) {
	getEnvCmd := e.Command("fw_printenv", names...)
	return getEnvironmentVariable(getEnvCmd)
}

func (e *uBootEnv) WriteEnv(vars BootVars) error {
	// Make environment update atomic by using fw_setenv "-s" option.
	setEnvCmd := e.Command("fw_setenv", "-s", "-")
	pipe, err := setEnvCmd.StdinPipe()
	if err != nil {
		log.Errorln("Could not set up pipe to fw_setenv command: ", err)
		return err
	}
	err = setEnvCmd.Start()
	if err != nil {
		log.Errorln("Could not execute fw_setenv: ", err)
		pipe.Close()
		return err
	}
	for k, v := range vars {
		_, err = fmt.Fprintf(pipe, "%s %s\n", k, v)
		if err != nil {
			log.Error("Error while setting U-Boot variable: ", err)
			pipe.Close()
			return err
		}
	}
	pipe.Close()
	err = setEnvCmd.Wait()
	if err != nil {
		log.Errorln("fw_setenv returned failure: ", err)
		return err
	}
	return nil
}

func getEnvironmentVariable(cmd *exec.Cmd) (BootVars, error) {
	cmdReader, err := cmd.StdoutPipe()

	if err != nil {
		log.Errorln("Error creating StdoutPipe: ", err)
		return nil, err
	}

	scanner := bufio.NewScanner(cmdReader)

	err = cmd.Start()
	if err != nil {
		return nil, err
	}

	var env_variables = make(BootVars)

	for scanner.Scan() {
		log.Debug("Have U-Boot variable: ", scanner.Text())
		splited_line := strings.Split(scanner.Text(), "=")

		//we are having empty line (usually at the end of output)
		if scanner.Text() == "" {
			continue
		}

		//we have some malformed data or Warning/Error
		if len(splited_line) != 2 {
			log.Error("U-Boot variable malformed or error occured")
			return nil, errors.New("Invalid U-Boot variable or error: " + scanner.Text())
		}

		env_variables[splited_line[0]] = splited_line[1]
	}

	err = cmd.Wait()
	if err != nil {
		return nil, err
	}

	if len(env_variables) > 0 {
		log.Debug("List of U-Boot variables:", env_variables)
	}

	return env_variables, err
}
