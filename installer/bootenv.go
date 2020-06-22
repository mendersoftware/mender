// Copyright 2020 Northern.tech AS
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
package installer

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/mendersoftware/mender/system"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

type UBootEnv struct {
	system.Commander
}

type BootVars map[string]string

type BootEnvReadWriter interface {
	ReadEnv(...string) (BootVars, error)
	WriteEnv(BootVars) error
}

func NewEnvironment(cmd system.Commander) *UBootEnv {
	env := UBootEnv{cmd}
	return &env
}

// If "mender_check_saveenv_canary=1" exists in the environment, check that
// "mender_saveenv_canary=1" also does. This is a way to verify that U-Boot has
// successfully written to the environment and that the user space tools can
// read it. Only the former variable will preexist in the default environment
// and then U-Boot will write the latter during the boot process.
func (e *UBootEnv) checkEnvCanary() error {
	getEnvCmd := e.Command("fw_printenv", "mender_check_saveenv_canary")
	vars, err := getEnvironmentVariable(getEnvCmd)
	if err != nil {
		// Checking should not be done.
		return nil
	}

	value, ok := vars["mender_check_saveenv_canary"]
	if !ok || value != "1" {
		// Checking should not be done.
		return nil
	}

	errMsg := "Failed mender_saveenv_canary check. There is an error in the U-Boot setup. Likely causes are: 1) Mismatch between the U-Boot boot loader environment location and the location specified in /etc/fw_env.config. 2) 'mender_setup' is not run by the U-Boot boot script"
	getEnvCmd = e.Command("fw_printenv", "mender_saveenv_canary")
	vars, err = getEnvironmentVariable(getEnvCmd)
	if err != nil {
		return errors.Wrapf(err, errMsg)
	}
	value, ok = vars["mender_saveenv_canary"]
	if !ok || value != "1" {
		err = errors.New("mender_saveenv_canary variable could not be parsed")
		return errors.Wrapf(err, errMsg)
	}

	// Canary OK!
	return nil
}

func (e *UBootEnv) ReadEnv(names ...string) (BootVars, error) {
	if err := e.checkEnvCanary(); err != nil {
		if os.Geteuid() != 0 {
			return nil, errors.Wrap(err, "requires root privileges")
		}
		return nil, err
	}
	getEnvCmd := e.Command("fw_printenv", names...)
	vars, err := getEnvironmentVariable(getEnvCmd)
	if err != nil && os.Getuid() != 0 {
		return nil, errors.Wrap(err, "requires root privileges")
	}
	return vars, err
}

func (e *UBootEnv) WriteEnv(vars BootVars) error {
	if err := e.checkEnvCanary(); err != nil {
		return err
	}

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
		if os.Geteuid() != 0 {
			return errors.Wrap(err, "requires root privileges")
		}
		return err
	}
	for k, v := range vars {
		_, err = fmt.Fprintf(pipe, "%s=%s\n", k, v)
		if err != nil {
			log.Error("Error while setting U-Boot variable: ", err)
			pipe.Close()
			return err
		}
	}
	pipe.Close()
	err = setEnvCmd.Wait()
	if err != nil {
		if os.Geteuid() != 0 {
			return errors.Wrap(err, "requires root privileges")
		}
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
			log.Error("U-Boot variable malformed or error occurred")
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
