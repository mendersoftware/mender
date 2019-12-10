// Copyright 2019 Northern.tech AS
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
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/pkg/errors"
	"github.com/urfave/cli"
	"golang.org/x/sys/unix"

	"github.com/mendersoftware/log"
	"github.com/mendersoftware/mender/system"
	"github.com/mendersoftware/mender/utils"
)

func (runOpts *runOptionsType) CopySnapshot(ctx *cli.Context, out io.Writer) error {
	var fsSize uint64

	// Ensure we don't write logs to the filesystem
	log.SetOutput(os.Stderr)

	rootDev, err := system.GetFSDevFile("/")
	if err != nil {
		return err
	}
	dataDev, err := system.GetFSDevFile(runOpts.dataStore)
	if err != nil {
		return err
	}
	if rootDev == dataDev {
		return errors.Errorf(
			"State data store (%s) is located on rootfs partition",
			runOpts.dataStore)
	}

	thawChan := make(chan int)
	sigChan := make(chan os.Signal)
	signal.Notify(sigChan, unix.SIGINT, unix.SIGPIPE)
	go stopHandler(sigChan, thawChan)

	if err = system.FreezeFS("/"); err != nil {
		log.Error(err.Error())
		return err
	}

	f, err := os.Open(rootDev)
	if err != nil {
		thawChan <- 1
		return err
	}
	defer f.Close()

	// Get file system size - need to do this the hard way (returns uint64)
	fsSize, err = system.GetBlockDeviceSize(f)
	if err != nil {
		thawChan <- 1
		return errors.Wrap(err, "Unable to get partition size")
	}

	log.Infof("Initiating copy of size %s", utils.ShortSize(fsSize))
	pb := utils.NewProgressBar(os.Stderr, fsSize, utils.BYTES)
	if pb != nil {
		pb.SetPrefix(fmt.Sprintf("%s: ", ctx.String("file")))
		pb.Tick(0)

		err = CopyWithProgress(out, f, pb)
	} else {
		err = io.Copy(out, f)
	}

	thawChan <- 1
	if err != nil {
		log.Error(err.Error())
		return err
	}

	return nil
}

func stopHandler(sigChan chan os.Signal, thawChan chan int) {
	// Temporary signal handler / unfreeze routine
	var sig os.Signal
	select {
	case sig = <-sigChan:
		log.Infof("Received signal: %s",
			unix.SignalName(sig.(unix.Signal)))
	case <-thawChan:
	}
	if err := system.ThawFS("/"); err != nil {
		log.Error("CRITICAL: Unable to unfreeze filesystem, try " +
			"running `fsfreeze -u /` or `SYSRQ+j, immediately!")
	}
	signal.Stop(sigChan)
	if sig != nil {
		// Invoke os SIGINT handler (will close app)
		unix.Kill(os.Getpid(), unix.SIGINT)
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
