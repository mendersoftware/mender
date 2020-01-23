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
	"compress/gzip"
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
	// Error messages //
	errMsgDataPartFmt = "Device-local data is stored on the rootfs " +
		"partition: %s. The recommended approach is to have  a " +
		"separate data-partition mounted on \"/data\" and add a " +
		"symbolic link (%s -> /data). https://docs.mender.io/devices/" +
		"general-system-requirements#partition-layout"
	errMsgThawFmt = "CRITICAL: Unable to unfreeze filesystem, try " +
		"running `fsfreeze -u %s` or press `SYSRQ+j`, immediately!"

	// Watchdog constants //
	// wdtExpired is set by the listening routine if the timer expires.
	wdtExpired int32 = -1
	// wdtReset is swapped in on read by the listening routine.
	wdtReset int32 = 0
	// wdtSet is set by the monitored process.
	wdtSet int32 = 1
	// wdtIntervalSec Sets the polling interval of the listening routine.
	// NOTE: this should be sufficient enough for the user to for example
	//       type their password to ssh etc.
	wdtIntervalSec = 30
)

var (
	errWatchDogExpired = fmt.Errorf("watchdog timer expired")
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

func checkSnapshotPreconditions(
	fsPath, dataPath string) (*system.MountInfo, error) {
	var err error

	dataDevID, err := system.GetDeviceIDFromPath(dataPath)
	if err != nil {
		return nil, err
	}
	rootDevID, err := system.GetDeviceIDFromPath(fsPath)
	if err != nil {
		return nil, err
	}
	if dataDevID == rootDevID {
		log.Errorf(errMsgDataPartFmt,
			dataPath, dataPath)
		return nil, errors.Errorf(
			"data store (%s) is located on rootfs partition",
			dataPath)
	}

	rootDev, err := system.GetMountInfoFromDeviceID(rootDevID)
	if err != nil {
		return nil, err
	} else if rootDev.FSType == "overlay" {
		log.Error("overlay filesystem detected, " +
			"filesystem type not supported")
		log.Error("please specify the path to the " +
			"block-device instead")
		return nil, fmt.Errorf(
			"snapshot: filesystem 'overlay' not supported")
	}

	return rootDev, nil
}

// CopySnapshot freezes the filesystem and copies a snapshot to out.
func (runOpts *runOptionsType) CopySnapshot(ctx *cli.Context, out io.Writer) error {

	var fd *os.File
	var err error
	var watchDog int32

	// Ensure we don't write logs to the filesystem
	log.SetOutput(os.Stderr)
	if ctx.Bool("quiet") {
		log.SetLevel(log.ErrorLevel)
	}

	rootDev, err := checkSnapshotPreconditions(
		ctx.String("fs-path"),
		ctx.GlobalString("data"),
	)
	if err == system.ErrDevNotMounted {
		// If not mounted, assume the path points to a device
	} else if err != nil {
		return err
	} else {
		var f *os.File
		sigChan := make(chan os.Signal)
		abortChan := make(chan struct{})
		if f, err = os.OpenFile(rootDev.MountPoint, 0, 0); err != nil {
			defer f.Close()
			// freezeHandler is a transparent signal handler that
			// ensures system.ThawFs is called upon a terminating
			// signal.
			signal.Notify(sigChan)
			go freezeHandler(sigChan, abortChan, f, &watchDog)
			defer stopFreezeHandler(sigChan, abortChan)
			if err == nil {
				err = system.FreezeFS(int(f.Fd()))
			}
		}
		if err != nil {
			log.Warnf("Failed to freeze filesystem on %s: %s",
				rootDev.MountSource, err.Error())
			log.Warn("The snapshot might become invalid.")
			abortChan <- struct{}{} // abort handler
			stopFreezeHandler(sigChan, abortChan)
			signal.Stop(sigChan)
		}
	}

	// Get filesystem size
	if rootDev == nil {
		fd, err = os.Open(ctx.String("fs-path"))
	} else {
		devPath := fmt.Sprintf("/dev/block/%d:%d",
			rootDev.DevID[0], rootDev.DevID[1])
		fd, err = os.Open(devPath)
	}
	if err != nil {
		return err
	}
	defer fd.Close()

	if ctx.GlobalString("compression") == "gzip" {
		out = gzip.NewWriter(out)
	}

	// Get file system size - need to do this the hard way (returns uint64)
	fsSize, err := system.GetBlockDeviceSize(fd)
	if err != nil {
		return errors.Wrap(err, "Unable to get partition size")
	}

	log.Infof("Initiating copy of uncompressed size %s",
		utils.StringifySize(fsSize, 3))
	if ctx.Bool("quiet") {
		err = CopyWithWatchdog(out, fd, &watchDog, nil)
	} else {
		pb := utils.NewProgressBar(os.Stderr, fsSize, utils.BYTES)
		if ctx.IsSet("file") {
			pb.SetPrefix(fmt.Sprintf("%s: ", ctx.String("file")))
		}
		err = CopyWithWatchdog(out, fd, &watchDog, pb)
	}

	if err != nil {
		return err
	}
	log.Info("Snapshot completed successfully!")

	return nil
}

// freezeHandler is a transparent signal handler and watchdog timer ensuring
// system.ThawFS is called on a terminating signal before relaying the signal
// to the system default handler. The only signal that is not relayed to the
// default handler is SIGUSR1 which can be used to trigger a FITHAW ioctl
// on the filesystem. The sigChan should be notified on ALL incomming signals to
// the process, signals that are ignored by default are also ignored by this
// handler. The abort chan, as the name implies, aborts the handler without
// executing FITHAW. This channel is also used to notify that the handler has
// returned.
func freezeHandler(
	sigChan chan os.Signal,
	abortChan chan struct{},
	fd *os.File,
	wdt *int32,
) {
	var sig os.Signal = nil
	var sigOpen bool = true
	var abortOpen bool = true
	for {
		select {
		case <-time.After(wdtIntervalSec * time.Second):
			if old := atomic.SwapInt32(wdt, wdtReset); old > 0 {
				continue
			}
			// Timer expired
			atomic.StoreInt32(wdt, wdtExpired)
			log.Error("Watchdog timer expired due to " +
				"blocked main process.")
			log.Info("Unfreezing filesystem")

		case _, abortOpen = <-abortChan:
			break

		case sig, sigOpen = <-sigChan:

		}
		if sig != nil {
			log.Debugf("Freeze handler received signal: %s",
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
		log.Debugf("Thawing filesystem at: %s", fd.Name())
		if err := system.ThawFS(int(fd.Fd())); err != nil {
			log.Errorf(errMsgThawFmt, fd.Name())
		}
		if sigOpen {
			signal.Stop(sigChan)
			if sig != nil && sig != unix.SIGUSR1 {
				// Invoke default signal handler
				unix.Kill(os.Getpid(), sig.(unix.Signal))
			}
		}
		break
	}
	if abortOpen {
		// Notify that the routine quit
		abortChan <- struct{}{}
	}
}

// stopFreezeHandler ensures freezeHandler stops with the appropriate
// preconditions
func stopFreezeHandler(sigChan chan os.Signal, abortChan chan struct{}) {
	// Check if handler has already returned
	var abortOpen bool = true
	var sigOpen bool = true
	select {
	case _, sigOpen = <-sigChan:

	default:
		signal.Stop(sigChan)
	}
	select {
	case _, abortOpen = <-abortChan:
		if sigOpen {
			close(sigChan)
		}
		if abortOpen {
			// The routine has already signaled abort
			close(abortChan)
		}

	default:
		if sigOpen {
			// Both channels are open
			sigChan <- unix.SIGUSR1
			select {
			// Wait no longer than a second (should not take long)
			case <-time.After(time.Second):

			case <-abortChan:
			}
			close(sigChan)
		}
		// Close abortChan regardless
		close(abortChan)
	}
}

// CopyWithWatchdog is an implementation of io.Copy that for each block it
// copies resets the watchdog timer.
func CopyWithWatchdog(
	dst io.Writer,
	src io.Reader,
	wdt *int32,
	pb *utils.ProgressBar,
) error {
	// Only tick every 10 writes
	const tickInterval uint64 = 32 * 1024 * 10
	var numTicks uint64

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
			return err
		} else if w < n {
			err = errors.Wrap(io.ErrShortWrite,
				"Error writing to stream")
			if err != nil {
				return err
			}
		}
		wd := atomic.SwapInt32(wdt, wdtSet)
		if wd == wdtExpired {
			return errWatchDogExpired
		}
		numTicks += uint64(n)
		if pb != nil && numTicks >= tickInterval {
			err = pb.Tick(numTicks)
			if err != nil {
				return err
			}
			numTicks = 0
		}
	}
	return nil
}
