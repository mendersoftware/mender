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
package cli

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"os/signal"
	"time"

	"github.com/pkg/errors"
	"github.com/urfave/cli"
	"golang.org/x/sys/unix"

	"github.com/mendersoftware/log"
	"github.com/mendersoftware/mender/system"
	"github.com/mendersoftware/mender/utils"
)

const (
	errMsgDataPartF = "Device-local data is stored on the rootfs " +
		"partition: %s. The recommended approach is to have  a " +
		"separate data-partition mouted on \"/data\" and add a " +
		"symbolic link (%s -> /data). https://docs.mender.io/devices/" +
		"general-system-requirements#partition-layout"
)

func (runOpts *runOptionsType) DumpSSH(ctx *cli.Context) error {
	const sshInitMagic = `Initializing snapshot...`
	numArgs := ctx.NArg()
	args := make([]string, numArgs)
	for i := 0; i < numArgs; i++ {
		args[i] = ctx.Args().Get(i)
	}
	args = append(args, "/bin/sh", "-c",
		fmt.Sprintf(`'echo "`+sshInitMagic+`"; cat > %s'`,
			ctx.String("file")))
	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		return err
	}
	defer stdinR.Close()
	cmd := exec.Command("ssh", args...)
	cmd.Stderr = os.Stderr
	cmd.Stdin = stdinR
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	if err = cmd.Start(); err != nil {
		return err
	}

	errChan := make(chan error)
	go func() {
		var err error
		var line []byte
		stdoutReader := bufio.NewReader(stdout)
		for {
			line, _, err = stdoutReader.ReadLine()
			if err != nil {
				break
			} else if bytes.Contains(line, []byte(sshInitMagic)) {
				break
			}
			_, err = os.Stdout.Write(line)
			if err != nil {
				break
			}
		}
		if err == io.EOF {
			errChan <- errors.New(
				"SSH process quit on prompting for input.")
		} else {
			errChan <- err
		}
	}()
	select {
	case <-time.After(time.Minute):
		err = errors.New("Timed out waiting for user input to ssh.")
	case err = <-errChan:
		// SSH listener routine returned
	}
	if err != nil {
		cmd.Process.Kill()
		return err
	}

	if err = runOpts.CopySnapshot(ctx, stdinW); err != nil {
		return err
	}
	err = cmd.Wait()
	return err
}

func (runOpts *runOptionsType) CopySnapshot(ctx *cli.Context, out io.Writer) error {
	var fsSize uint64

	// Ensure we don't write logs to the filesystem
	if ctx.Bool("quiet") {
		log.SetOutput(ioutil.Discard)
	} else {
		log.SetOutput(os.Stderr)
	}

	rootDev, err := system.GetFSDevFile("/")
	if err != nil {
		return err
	}
	dataDev, err := system.GetFSDevFile(runOpts.dataStore)
	if err != nil {
		return err
	}
	if rootDev == dataDev {
		log.Errorf(errMsgDataPartF,
			runOpts.dataStore, runOpts.dataStore)
		return errors.Errorf(
			"Data store (%s) is located on rootfs partition",
			runOpts.dataStore)
	}

	// stopHandler is a transparent signal handler that ensures that
	// system.ThawFs is called upon a terminating signal before calling
	// relaying the signal to the system.
	sigChan := make(chan os.Signal)
	doneChan := make(chan struct{})
	signal.Notify(sigChan)
	go stopHandler(sigChan, doneChan)
	defer func() {
		close(sigChan) // Terminate signal handler.
		<-doneChan     // Ensure that the signal handler returns first.
	}()

	if err = system.FreezeFS("/"); err != nil {
		log.Error(err.Error())
		return err
	}

	f, err := os.Open(rootDev)
	if err != nil {
		return err
	}
	defer f.Close()

	// Get file system size - need to do this the hard way (returns uint64)
	fsSize, err = system.GetBlockDeviceSize(f)
	if err != nil {
		return errors.Wrap(err, "Unable to get partition size")
	}

	log.Infof("Initiating copy of size %s", utils.ShortSize(fsSize))
	if ctx.Bool("quiet") {
		_, err = io.Copy(out, f)
	} else {
		pb := utils.NewProgressBar(os.Stderr, fsSize, utils.BYTES)
		if ctx.IsSet("file") {
			pb.SetPrefix(fmt.Sprintf("%s: ", ctx.String("file")))
		}
		pb.Tick(0)
		err = CopyWithProgress(out, f, pb)
	}

	if err != nil {
		log.Error(err.Error())
		return err
	}

	return nil
}

// Transparent signal handler ensuring system.ThawFS is called on a terminating
// signal before relaying the signal to the system default handler. The sigChan
// should be notified on ALL incomming signals to the process, signals that are
// ignored by default are also ignored by this handler. The doneChan is closed
// when this routine returns.
func stopHandler(sigChan chan os.Signal, doneChan chan struct{}) {
	defer close(doneChan)
	for {
		sig, isSig := <-sigChan
		if isSig {
			log.Debugf("Received signal: %s",
				sig.String())
			if sig == unix.SIGURG ||
				sig == unix.SIGWINCH ||
				sig == unix.SIGCHLD {
				// Signals that are ignored by default
				// keep ignoring them.
				continue
			}
		}
		// Terminating condition met (signal or closed channel
		// -> Unfreeze rootfs
		if err := system.ThawFS("/"); err != nil {
			log.Error("CRITICAL: Unable to unfreeze filesystem, try " +
				"running `fsfreeze -u /` or press `SYSRQ+j`, " +
				"immediately!")
		}
		signal.Stop(sigChan)
		if isSig {
			// Invoke default signal handler
			unix.Kill(os.Getpid(), sig.(unix.Signal))
		}
		return
	}
}

func CopyWithProgress(dst io.Writer, src io.Reader, pb *utils.ProgressBar) error {
	buf := make([]byte, 32*1024)
	for {
		n, err := src.Read(buf)
		if err != nil {
			if err == io.EOF {
				err = nil
				break
			}
			return err
		} else if n < 0 {
			break
		}
		w, err := dst.Write(buf[:n])
		if err != nil {
			log.Error(err.Error())
			return err
		} else if w < n {
			err = errors.Wrap(io.ErrShortWrite,
				"Error writing to stream")
		}
		err = pb.Tick(uint64(n))
		if err != nil {
			return err
		}
	}
	return nil
}
