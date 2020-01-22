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
	errMsgDataPartF = "Device-local data is stored on the partition " +
		"being snapshotted: %s. The recommended approach is to have " +
		"a separate data-partition mounted on \"/data\" and add a " +
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
	// NOTE: this should be sufficient enough for the user to for example
	//       type their password to ssh etc.
	WDTIntervalSec = 30
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

// CopySnapshot freezes the filesystem and copies a snapshot to out.
func (runOpts *runOptionsType) CopySnapshot(ctx *cli.Context, out io.Writer) error {
	var err error
	var fsSize uint64

	fsPath := ctx.String("fs-path")

	// Ensure we don't write logs to the filesystem
	if ctx.Bool("quiet") {
		log.SetLevel(log.ErrorLevel)
	}
	log.SetOutput(os.Stderr)

	switch ctx.String("compression") {
	case "none":
		// Keep existing `out`
	case "gzip":
		out = gzip.NewWriter(out)
	case "lzma":
		return errors.New("lzma compression is not implemented for snapshot command")
	default:
		return errors.Errorf("Unknown compression '%s'", ctx.String("compression"))
	}

	dataDevID, err := system.GetDeviceIDFromPath(runOpts.dataStore)
	if err != nil {
		return err
	}
	rootDevID, err := system.GetDeviceIDFromPath(fsPath)
	if err != nil {
		return err
	}
	if dataDevID == rootDevID {
		log.Errorf(errMsgDataPartF,
			runOpts.dataStore, runOpts.dataStore)
		return errors.Errorf(
			"Data store (%s) is located on filesystem %s",
			runOpts.dataStore, fsPath)
	}

	var watchDog int32
	rootDev, err := system.GetMountInfoFromDeviceID(rootDevID)
	if err == system.ErrDevNotMounted {
		// If not mounted, assume the path points to a device
		log.Debugf("Device %s is not mounted, not freezing it", fsPath)
	} else if err != nil {
		return err
	} else {
		sigChan := make(chan os.Signal)
		abortChan := make(chan struct{})
		signal.Notify(sigChan)
		// freezeHandler is a transparent signal handler that ensures
		// system.ThawFs is called upon a terminating signal.
		// fsPath must be a directory in order to use the FIFREEZE ioctl,
		// moreover, we don't have to freeze an unmounted device.
		go freezeHandler(sigChan, abortChan,
			rootDev.MountPoint, &watchDog)
		defer func() {
			// Check if handler has already returned
			var hasAborted bool = false
			select {
			case _, hasAborted = <-abortChan:

			default:
			}
			if !hasAborted {
				// Terminate signal handler.
				sigChan <- unix.SIGUSR1
				// Ensure that the signal handler returns first.
				<-abortChan
			}
			close(sigChan)
			close(abortChan)
		}()

		log.Debugf("Freezing %s", rootDev.MountPoint)
		if err = system.FreezeFS(rootDev.MountPoint); err != nil {
			log.Warnf("Failed to freeze filesystem on %s: %s",
				rootDev.MountPoint, err.Error())
			log.Warn("The snapshot might become invalid.")
			abortChan <- struct{}{} // abort handler
			signal.Stop(sigChan)
		}
	}

	var fd *os.File
	if rootDev == nil {
		fd, err = os.Open(fsPath)
	} else {
		devPath := fmt.Sprintf("/dev/block/%d:%d",
			rootDev.DevID[0], rootDev.DevID[1])
		fd, err = os.Open(devPath)
	}
	if err != nil {
		return err
	}
	defer fd.Close()

	// Get file system size - need to do this the hard way (returns uint64)
	fsSize, err = system.GetBlockDeviceSize(fd)
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
func freezeHandler(sigChan chan os.Signal, abortChan chan struct{}, fsPath string, wdt *int32) {
	var sig os.Signal = nil
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

		case <-abortChan:
			break

		case sig = <-sigChan:
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
		log.Debugf("Thawing %s", fsPath)
		if err := system.ThawFS(fsPath); err != nil {
			log.Errorf("CRITICAL: Unable to unfreeze filesystem, try "+
				"running `fsfreeze -u %s` or press `SYSRQ+j`, "+
				"immediately!", fsPath)
		}
		signal.Stop(sigChan)
		if sig != nil && sig != unix.SIGUSR1 {
			// Invoke default signal handler
			unix.Kill(os.Getpid(), sig.(unix.Signal))
		}
		break
	}
	// Notify that the routine quit
	abortChan <- struct{}{}
}

func CopyWithWatchdog(dst io.Writer, src io.Reader, wdt *int32, pb *utils.ProgressBar) error {
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
		wd := atomic.SwapInt32(wdt, WDTSet)
		if wd == WDTExpired {
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
