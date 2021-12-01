// Copyright 2021 Northern.tech AS
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
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/mendersoftware/mender/system"
)

const (
	menderUBootSeparatorProbe = "mender_uboot_separator"

	uBootEnvStandardSeparator = "="
	uBootEnvLegacySeparator   = " "
)

type UBootEnv struct {
	system.Commander
	setCommand []string
	getCommand []string
}

type BootVars map[string]string

type BootEnvReadWriter interface {
	ReadEnv(...string) (BootVars, error)
	WriteEnv(BootVars) error
}

func NewEnvironment(cmd system.Commander, setCmd, getCmd string) *UBootEnv {
	env := UBootEnv{
		Commander: cmd,
	}
	if setCmd != "" {
		env.setCommand = []string{setCmd}
	} else {
		// These are grub-mender-grubenv's and U-Boot's tools,
		// respectively. On older versions of grub-mender-grubenv, the
		// fw_setenv name was still used.
		env.setCommand = []string{"grub-mender-grubenv-set", "fw_setenv"}
	}
	if getCmd != "" {
		env.getCommand = []string{getCmd}
	} else {
		// See above comment.
		env.getCommand = []string{"grub-mender-grubenv-print", "fw_printenv"}
	}
	return &env
}

// If "mender_check_saveenv_canary=1" exists in the environment, check that
// "mender_saveenv_canary=1" also does. This is a way to verify that U-Boot has
// successfully written to the environment and that the user space tools can
// read it. Only the former variable will preexist in the default environment
// and then U-Boot will write the latter during the boot process.
func (e *UBootEnv) checkEnvCanary() error {
	vars, err := e.getEnvironmentVariable([]string{"mender_check_saveenv_canary"})
	if err != nil {
		// Checking should not be done.
		return nil
	}

	value, ok := vars["mender_check_saveenv_canary"]
	if !ok || value != "1" {
		// Checking should not be done.
		return nil
	}

	errMsg := "Failed mender_saveenv_canary check. There is an error in the U-Boot setup." +
		" Likely causes are: 1) Mismatch between the U-Boot boot loader environment location" +
		" and the location specified in /etc/fw_env.config. 2) 'mender_setup' is not run by" +
		" the U-Boot boot script"
	vars, err = e.getEnvironmentVariable([]string{"mender_saveenv_canary"})
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
	vars, err := e.getEnvironmentVariable(names)
	if err != nil && os.Getuid() != 0 {
		return nil, errors.Wrap(err, "requires root privileges")
	}
	return vars, err
}

// probeSeparator tries writing 'mender_uboot_separator' to the U-Boot
// environment using whitespace as a separator. If this write is successful it
// means that we are on a legacy U-Boot implementation, which supports the
// 'key<whitespace>value' syntax. If not, then use the new '=' separator.
func (e *UBootEnv) probeSeparator() (string, error) {
	log.Debug("Probing the Bootloader environment for which separator to use")
	// Try writing using the old separator syntax
	err := e.writeEnvImpl(BootVars{
		menderUBootSeparatorProbe: "1",
	}, uBootEnvLegacySeparator)
	if err != nil {
		return "", err
	}
	v, err := e.ReadEnv(menderUBootSeparatorProbe)
	if err != nil {
		return "", err
	}
	if v[menderUBootSeparatorProbe] == "1" {
		// Clean variable from the environment
		err = e.writeEnvImpl(BootVars{
			menderUBootSeparatorProbe: "",
		}, uBootEnvLegacySeparator)
		if err != nil {
			return "", err
		}
		return uBootEnvLegacySeparator, nil
	}
	return uBootEnvStandardSeparator, nil
}

// WriteEnv attempts to write the given 'BootVars' to the bootloader
// environment.
func (e *UBootEnv) WriteEnv(vars BootVars) error {
	if err := e.checkEnvCanary(); err != nil {
		return err
	}
	// Probe for the separator used by U-Boot. Newer versions support '=',
	// and libubootenv only supports '=' as the separator, older versions
	// only support ' '
	separator, err := e.probeSeparator()
	if err != nil {
		log.Errorf(
			"Failed to probe the U-Boot environment for which separator to use. Got error: %s",
			err.Error(),
		)
		return err
	}
	log.Debugf("Using (%s) as the bootloader environment separator", separator)
	return e.writeEnvImpl(vars, separator)
}

func (e *UBootEnv) writeEnvImpl(vars BootVars, separator string) error {

	log.Debugf("Writing %v to the U-Boot environment, using separator: %s",
		vars, separator)

	var setEnvCmd *system.Cmd
	var pipe io.WriteCloser
	var err error
	found := false
	for _, executable := range e.setCommand {
		// Make environment update atomic by using "-s" option.
		setEnvCmd = e.Command(executable, "-s", "-")
		pipe, err = setEnvCmd.StdinPipe()
		if err != nil {
			log.Errorf("Could not set up pipe to %q command: %s", executable, err.Error())
			return err
		}
		err = setEnvCmd.Start()
		if errors.Is(err, exec.ErrNotFound) {
			log.Debugf("Tried command %q, but it was not found", executable)
			continue
		} else if err != nil {
			log.Errorf("Could not execute %q: %s", executable, err.Error())
			pipe.Close()
			if os.Geteuid() != 0 {
				return errors.Wrap(err, "requires root privileges")
			}
			return err
		}

		found = true
		break
	}

	if !found {
		errMsg := fmt.Sprintf("Command to write boot environment not found. Tried %s",
			strings.Join(e.setCommand, ", "))
		log.Errorln(errMsg)
		pipe.Close()
		return errors.Wrap(exec.ErrNotFound, errMsg)
	}

	for k, v := range vars {
		_, err = fmt.Fprintf(pipe, "%s%s%s\n", k, separator, v)
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

func (e *UBootEnv) getEnvironmentVariable(args []string) (BootVars, error) {
	var cmd *system.Cmd
	var cmdReader io.Reader
	var err error
	found := false
	for _, executable := range e.getCommand {
		cmd = e.Command(executable, args...)

		cmdReader, err = cmd.StdoutPipe()

		if err != nil {
			log.Errorln("Error creating StdoutPipe: ", err)
			return nil, err
		}

		err = cmd.Start()
		if errors.Is(err, exec.ErrNotFound) {
			log.Debugf("Tried command %q, but it was not found", executable)
			continue
		} else if err != nil {
			return nil, err
		}

		found = true
		break
	}

	if !found {
		return nil, errors.Wrapf(exec.ErrNotFound,
			"Command to read boot environment not found. Tried %s",
			strings.Join(e.getCommand, ", "))
	}

	scanner := bufio.NewScanner(cmdReader)

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
