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
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync/atomic"
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
		"separate data-partition mounted on \"/data\" and add a " +
		"symbolic link (%s -> /data). https://docs.mender.io/devices/" +
		"general-system-requirements#partition-layout"

	// Watchdog constants //

	// WDTExpired is set by the listening routine if the timer expires.
	WDTExpired int32 = -1
	// WDTReset is swapped in on read by the listening routine.
	WDTReset int32 = 0
	// WDTSet is set by the monitored process.
	WDTSet int32 = 1
	// WDTIntervalSec Sets the polling interval of the listening routine.
	WDTIntervalSec = 5
)

// DumpSnapshot copies a snapshot of the root filesystem to stdout.
func (runOpts *runOptionsType) DumpSnapshot(ctx *cli.Context) error {

	log.SetOutput(os.Stderr)

	fd := int(os.Stdout.Fd())
	// Make sure stdout is redirected (not a tty device)
	if _, err := unix.IoctlGetTermios(fd, unix.TCGETS); err == nil {
		return errDumpTerminal
	}

	// Make sure we don't dump to rootfs
	// which would cause the system to freeze
	var stdoutStat unix.Stat_t
	var rootStat unix.Stat_t
	if err := unix.Fstat(fd, &stdoutStat); err != nil {
		return errors.Wrap(err, "Unable to stat output file")
	}
	if err := unix.Stat(ctx.String("fs-path"), &rootStat); err != nil {
		return errors.Wrap(err, "Unable to stat root filesystem")
	}
	if stdoutStat.Dev == rootStat.Dev {
		return errors.New(
			"Dumping the filesystem to itself is not permitted")
	}

	return runOpts.CopySnapshot(ctx, os.Stdout)
}

// CopySnapshot freezes the filesystem and copies a snapshot to out.
func (runOpts *runOptionsType) CopySnapshot(ctx *cli.Context, out io.Writer) error {
	var fsSize uint64

	// Ensure we don't write logs to the filesystem
	if ctx.Bool("quiet") {
		log.SetLevel(log.ErrorLevel)
	}
	log.SetOutput(os.Stderr)

	rootDev, err := system.GetFSBlockDev(ctx.String("fs-path"))
	if err != nil {
		return err
	}
	dataDev, err := system.GetFSBlockDev(runOpts.dataStore)
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

	// freezeHandler is a transparent signal handler that ensures that
	// system.ThawFs is called upon a terminating signal before calling
	// relaying the signal to the system.
	var watchDog int32
	sigChan := make(chan os.Signal)
	doneChan := make(chan struct{})
	signal.Notify(sigChan)
	go freezeHandler(sigChan, doneChan, ctx.String("fs-path"), &watchDog)
	defer func() {
		doneChan <- struct{}{} // Terminate signal handler.
		<-doneChan             // Ensure that the signal handler returns first.
	}()
	if err = system.FreezeFS(ctx.String("fs-path")); err != nil {
		log.Warnf("Failed to freeze filesystem on %s: %s",
			rootDev, err.Error())
		log.Warn("The snapshot might become invalid.")
		close(doneChan) // abort handler
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
		err = CopyWithWatchdog(out, f, &watchDog, nil)
	} else {
		pb := utils.NewProgressBar(os.Stderr, fsSize, utils.BYTES)
		if ctx.IsSet("file") {
			pb.SetPrefix(fmt.Sprintf("%s: ", ctx.String("file")))
		}
		pb.Tick(0)
		err = CopyWithWatchdog(out, f, &watchDog, pb)
	}

	if err != nil {
		log.Error(err.Error())
		return err
	}
	log.Info("Snapshot completed successfully!")

	return nil
}

// Transparent signal handler and watchdog timer ensuring system.ThawFS is
// called on a terminating signal before relaying the signal to the system
// default handler. The sigChan should be notified on ALL incomming signals to
// the process, signals that are ignored by default are also ignored by this
// handler. The doneChan is closed when this routine returns, if the channel
// receives any data, the handler releases the filesystem, and if the channel
// is closed the routine is aborts immediately.
func freezeHandler(sigChan chan os.Signal, doneChan chan struct{}, fsPath string, wdt *int32) {
	var sig os.Signal = nil
	var doneOpen bool = true
	var sigOpen bool = true
	for {
		select {
		case <-time.After(WDTIntervalSec * time.Second):
			if old := atomic.SwapInt32(wdt, WDTReset); old > 0 {
				continue
			}
			// Timer expired
			atomic.StoreInt32(wdt, WDTExpired)
			log.Error("Watchdog timer expired due to " +
				"blocked main process.")
			log.Info("Unfreezing filesystem")

		case _, doneOpen = <-doneChan:
			if !doneOpen {
				// Abort if closed
				break
			}
			// ... else thaw filesystem before returning.

		case sig, sigOpen = <-sigChan:
		}
		if sig != nil {
			log.Debugf("Received signal: %s",
				sig.String())
			if sig == unix.SIGURG ||
				sig == unix.SIGWINCH ||
				sig == unix.SIGCHLD {
				// Signals that are ignored by default
				// keep ignoring them.
				sig = nil
				continue
			}
		}
		// Terminating condition met (signal or closed channel
		// -> Unfreeze rootfs
		if err := system.ThawFS(fsPath); err != nil {
			log.Errorf("CRITICAL: Unable to unfreeze filesystem, try "+
				"running `fsfreeze -u %s` or press `SYSRQ+j`, "+
				"immediately!", fsPath)
		}
		signal.Stop(sigChan)
		if sig != nil {
			// Invoke default signal handler
			unix.Kill(os.Getpid(), sig.(unix.Signal))
		}
		break
	}
	if doneOpen {
		close(doneChan)
	}
	if sigOpen {
		close(sigChan)
	}
}

func CopyWithWatchdog(dst io.Writer, src io.Reader, wdt *int32, pb *utils.ProgressBar) error {
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
			if err != nil {
				return err
			}
		}
		wd := atomic.SwapInt32(wdt, WDTSet)
		if wd == WDTExpired {
			return errors.New("Watchdog timer expired")
		}

		if pb != nil {
			err = pb.Tick(uint64(n))
		}
		if err != nil {
			return err
		}
	}
	return nil
}
