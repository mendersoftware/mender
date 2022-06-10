// Copyright 2022 Northern.tech AS
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
	"bytes"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
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

	// *Any* return from this function is an error.

	if err != nil {
		// MEN-5340: If there's an error, it may be because systemd is
		// in the process of shutting down our service, and happens to
		// kill `reboot` first. Give it a few seconds to follow up with
		// killing the client.
		time.Sleep(10 * time.Second)
		return err
	}

	// Wait up to ten minutes for reboot to kill the client, otherwise the
	// client may mistake a successful return code as "reboot is complete,
	// continue".
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

func (c *Cmd) Run() error {
	err := c.Cmd.Run()
	if logger, ok := c.Stdout.(Flusher); ok {
		logger.Flush()
	}
	if logger, ok := c.Stderr.(Flusher); ok {
		logger.Flush()
	}
	return err
}

func (c *Cmd) Wait() error {
	err := c.Cmd.Wait()
	if logger, ok := c.Stdout.(Flusher); ok {
		logger.Flush()
	}
	if logger, ok := c.Stderr.(Flusher); ok {
		logger.Flush()
	}
	return err
}

func (c *Cmd) Output() ([]byte, error) {
	c.Stdout = nil
	b, err := c.Cmd.Output()
	if logger, ok := c.Stderr.(Flusher); ok {
		logger.Flush()
	}
	return b, err
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

// Command wraps the golang/exec Cmd struct, and captures the stderr/stdout
// output by default, and logs properly.
func Command(name string, arg ...string) *Cmd {
	var cmd Cmd
	cmd.Cmd = exec.Command(name, arg...)
	stdoutLogger := NewCmdLoggerStdout(name)
	cmd.Stdout = stdoutLogger
	stderrLogger := NewCmdLoggerStderr(name)
	cmd.Stderr = stderrLogger
	return &cmd
}

type cmdLogger struct {
	commandName string
	stream      string
	buf         *bytes.Buffer
}

func NewCmdLoggerStdout(cmd string) *cmdLogger {
	return &cmdLogger{
		commandName: cmd,
		stream:      "stdout",
		buf:         bytes.NewBuffer(nil),
	}
}

func NewCmdLoggerStderr(cmd string) *cmdLogger {
	return &cmdLogger{
		commandName: cmd,
		stream:      "stderr",
		buf:         bytes.NewBuffer(nil),
	}
}

func (c *cmdLogger) Write(b []byte) (int, error) {
	n, err := c.buf.Write(b)
	c.writeLines()
	return n, err
}

func (c *cmdLogger) writeLines() {
	if bytes.Contains(c.buf.Bytes(), []byte{'\n'}) {
		line, lerr := c.buf.ReadString('\n')
		if lerr != nil {
			panic("This should never happen unless we have a race condition!")
		}
		log.Infof(
			"Output (%s) from command %q: %s",
			c.stream,
			c.commandName,
			line[:len(line)-1], // Do not write the newline delimeter
		)
	}
}

type Flusher interface {
	Flush()
}

func (c *cmdLogger) Flush() {
	c.writeLines()
	// Empty the remaining bytes
	if len(c.buf.Bytes()) > 0 {
		log.Infof(
			"Output (%s) from command %q: %s",
			c.stream,
			c.commandName,
			c.buf.String(),
		)
	}
	c.buf.Reset()
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
