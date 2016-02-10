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
	"bufio"
	"errors"
	"github.com/mendersoftware/mender/internal/log"
	"strings"
)

type uBootEnvCommand struct {
	EnvCmd string
}

type uBootVars map[string]string

func (c *uBootEnvCommand) command(params ...string) (uBootVars, error) {

	cmd := runner.run(c.EnvCmd, params...)
	cmdReader, err := cmd.StdoutPipe()

	if err != nil {
		log.Errorln("Error creating StdoutPipe:", err)
		return nil, err
	}

	scanner := bufio.NewScanner(cmdReader)

	err = cmd.Start()
	if err != nil {
		log.Errorln("There was an error getting or setting U-Boot env")
		return nil, err
	}

	var env_variables = make(uBootVars)

	for scanner.Scan() {
		log.Debugln("Have U-Boot variable:", scanner.Text())
		splited_line := strings.Split(scanner.Text(), "=")

		//we are having empty line (usually at the end of output)
		if scanner.Text() == "" {
			continue
		}

		//we have some malformed data or Warning/Error
		if len(splited_line) != 2 {
			log.Errorln("U-Boot variable malformed or error occured")
			return nil, errors.New("Invalid U-Boot variable or error: " + scanner.Text())
		}

		env_variables[splited_line[0]] = splited_line[1]
	}

	err = cmd.Wait()
	if err != nil {
		log.Errorln("U-Boot env command returned non zero status")
		return nil, err
	}

	if len(env_variables) > 0 {
		log.Debugln("List of U-Boot variables:", env_variables)
	}

	return env_variables, err
}

func getBootEnv(var_name ...string) (uBootVars, error) {
	get_env := uBootEnvCommand{"fw_printenv"}
	return get_env.command(var_name...)
}

func setBootEnv(var_name string, value string) error {

	set_env := uBootEnvCommand{"fw_setenv"}

	if _, err := set_env.command(var_name, value); err != nil {
		log.Errorln("Error setting U-Boot variable:", err)
		return err
	}
	return nil
}
