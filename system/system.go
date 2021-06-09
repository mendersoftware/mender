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

package system

import (
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/pkg/errors"
)

type SystemRebootCmd struct {
	command Commander
}

func NewSystemRebootCmd(command Commander) *SystemRebootCmd {
	return &SystemRebootCmd{
		command: command,
	}
}

func (s *SystemRebootCmd) Reboot() error {
	err := s.command.Command("reboot").Run()
	if err != nil {
		return err
	}

	// Wait up to ten minutes for reboot to kill the client, otherwise the
	// client may mistake a successful return code as "reboot is complete,
	// continue". *Any* return from this function is an error.
	time.Sleep(10 * time.Minute)
	return errors.New("System did not reboot, even though 'reboot' call succeeded.")
}

type Commander interface {
	Command(name string, arg ...string) *Cmd
}

type StatCommander interface {
	Stat(string) (os.FileInfo, error)
	Commander
}

type Cmd struct {
	*exec.Cmd
}

func (c *Cmd) Output() ([]byte, error) {
	c.Stdout = nil
	return c.Cmd.Output()
}

func (c *Cmd) CombinedOutput() ([]byte, error) {
	c.Stdout = nil
	c.Stderr = nil
	return c.Cmd.CombinedOutput()
}

func (c *Cmd) StderrPipe() (io.ReadCloser, error) {
	c.Stderr = nil
	return c.Cmd.StderrPipe()
}

func (c *Cmd) StdoutPipe() (io.ReadCloser, error) {
	c.Stdout = nil
	return c.Cmd.StdoutPipe()
}

func Command(name string, arg ...string) *Cmd {
	var cmd Cmd
	cmd.Cmd = exec.Command(name, arg...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return &cmd
}

// we need real OS implementation
type OsCalls struct {
}

func (OsCalls) Command(name string, arg ...string) *Cmd {
	return Command(name, arg...)
}

func (OsCalls) Stat(name string) (os.FileInfo, error) {
	return os.Stat(name)
}
