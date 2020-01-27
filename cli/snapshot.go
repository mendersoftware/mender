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

	"github.com/alfrunes/cli"
	"github.com/pkg/errors"
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

	// bufferSize for the Copy function
	bufferSize = 32 * 1024
)

var (
	errWatchDogExpired = fmt.Errorf("watchdog timer expired")

	// pbCondition is used to determine when the progress bar can start
	// printing. This value is nil if the snapshot is not dumped to a pipe.
	pbCondition utils.StartCondition
)

// DumpSnapshot copies a snapshot of the root filesystem to stdout.
func DumpSnapshot(ctx *cli.Context) error {
	log.SetOutput(os.Stderr)
	fsPath, _ := ctx.String("source")

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
	if err := unix.Stat(fsPath, &rootStat); err != nil {
		return errors.Wrap(err, "Unable to stat root filesystem")
	}
	if stdoutStat.Dev == rootStat.Dev {
		return errors.New(
			"Dumping the filesystem to itself is not permitted")
	}

	return CopySnapshot(ctx, os.Stdout)
}

func checkSnapshotPreconditions(
	fsPath string,
	dataPath string,
) (*system.MountInfo, error) {
	var err error

	rootDevID, err := system.GetDeviceIDFromPath(fsPath)
	if err != nil {
		return nil, errors.Wrapf(err,
			"error getting device id from path '%s'", fsPath)
	}
	dataDevID, err := system.GetDeviceIDFromPath(dataPath)
	if err != nil {
		return nil, errors.Wrapf(err,
			"error getting device id from path '%s'", dataPath)
	}
	if dataDevID == rootDevID {
		log.Errorf(errMsgDataPartFmt, dataPath, dataPath)
		return nil, errors.Errorf(
			"data store (%s) is located on filesystem %s",
			dataPath, fsPath)
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

func prepareOutStream(out io.WriteCloser, compression string) (io.WriteCloser, error) {

	var pSize uint64

	if f, ok := out.(*os.File); ok {
		pSize = uint64(system.GetPipeSize(int(f.Fd())))
	}

	switch compression {
	case "none":
		pbCondition = func(c uint64) bool {
			if c > pSize {
				return true
			}
			return false
		}
		return out, nil
	case "gzip":
		wc := utils.NewByteCountWriteCloser(out)
		pbCondition = func(c uint64) bool {
			if wc.BytesWritten > pSize {
				return true
			}
			return false
		}
		return gzip.NewWriter(wc), nil
	case "lzma":
		return nil, errors.New("lzma compression is not implemented for snapshot command")
	default:
		return nil, errors.Errorf("Unknown compression '%s'", compression)
	}
}

// CopySnapshot freezes the filesystem and copies a snapshot to out.
func CopySnapshot(ctx *cli.Context, out io.WriteCloser) error {
	var fdRoot *os.File
	var err error
	var watchDog int32
	fsPath, _ := ctx.String("source")
	dataStore, _ := ctx.String("data")
	compression, _ := ctx.String("compression")

	// Ensure we don't write logs to the filesystem
	log.SetOutput(os.Stderr)
	if quiet, _ := ctx.Bool("quiet"); quiet {
		log.SetLevel(log.ErrorLevel)
	}

	rootDev, err := checkSnapshotPreconditions(fsPath, dataStore)
	if err == system.ErrDevNotMounted {
		// If not mounted, find device path the hard way

	} else if err != nil {
		return errors.Wrap(err, "failed preconditions for snapshot")
	} else {
		var f *os.File
		sigChan := make(chan os.Signal)
		abortChan := make(chan struct{})
		if f, err = os.OpenFile(rootDev.MountPoint, 0, 0); err == nil {
			defer f.Close()
			// freezeHandler is a transparent signal handler that
			// ensures system.ThawFs is called upon a terminating
			// signal.
			signal.Notify(sigChan)
			go freezeHandler(sigChan, abortChan, f, &watchDog)
			defer stopFreezeHandler(sigChan, abortChan)
			log.Debugf("Freezing %s", rootDev.MountPoint)
			err = system.FreezeFS(int(f.Fd()))
		}
		if err != nil {
			log.Warnf("Failed to freeze filesystem on %s: %s",
				rootDev.MountSource, err.Error())
			log.Warn("The snapshot might become invalid.")
			close(abortChan)
			stopFreezeHandler(sigChan, abortChan)
		}
	}

	devID, err := system.GetDeviceIDFromPath(fsPath)
	if err != nil {
		return errors.Wrapf(err,
			"failed to retrieve device id belonging to %s",
			fsPath,
		)
	}
	fsPath, err = system.GetBlockDeviceFromID(devID)
	if err != nil {
		return errors.Wrapf(err, "failed to expand device path")
	}
	if fdRoot, err = os.Open(fsPath); err != nil {
		return err
	}
	defer fdRoot.Close()

	out, err = prepareOutStream(out, compression)
	if err != nil {
		return err
	}
	defer out.Close()

	// Get file system size
	fsSize, err := system.GetBlockDeviceSize(fdRoot)
	if err != nil {
		return errors.Wrap(err, "Unable to get partition size")
	}

	log.Infof("Initiating copy of uncompressed size %s",
		utils.StringifySize(fsSize, 3))
	if quiet, _ := ctx.Bool("quiet"); quiet {
		err = CopyWithWatchdog(out, fdRoot, &watchDog, nil)
	} else {
		pb := utils.NewProgressBar(os.Stderr, fsSize, utils.BYTES)
		fd := int(os.Stderr.Fd())
		pb.SetTTY(fd)
		pb.SetPrefix(fmt.Sprintf("%s: ", fsPath))
		pb.Start(time.Millisecond*150, pbCondition)
		defer func() {
			err := pb.Close()
			if err != nil {
				log.Errorf(
					"Error stopping progress bar thread: %s",
					err.Error())
			}
		}()
		err = CopyWithWatchdog(out, fdRoot, &watchDog, pb)
	}

	if err != nil {
		return err
	}
	log.Info("Snapshot completed successfully!")

	return nil
}

// freezeHandler is a transparent signal handler and watchdog timer ensuring
// system.ThawFS is called on a terminating signal before relaying the signal
// to the system default handler. Closing sigChan can be used to trigger a
// FITHAW ioctl on the filesystem. The sigChan should be notified on ALL
// incoming signals to the process, signals that are ignored by default are
// also ignored by this handler. The abort chan, as the name implies, aborts
// the handler without executing FITHAW. This channel is also used to notify
// that the handler has returned.
func freezeHandler(
	sigChan chan os.Signal,
	abortChan chan struct{},
	fd *os.File,
	wdt *int32,
) {
	var sig os.Signal = nil
	var sigOpen bool = true
	var abortOpen bool = true
	defer func() {
		if abortOpen {
			// Notify that the routine quit
			abortChan <- struct{}{}
		}
	}()
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
			return

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
		log.Debugf("Thawing %s", fd.Name())
		if err := system.ThawFS(int(fd.Fd())); err != nil {
			log.Errorf(errMsgThawFmt, fd.Name())
		}

		if sigOpen {
			signal.Stop(sigChan)
			if sig != nil {
				// Invoke default signal handler
				unix.Kill(os.Getpid(), sig.(unix.Signal))
			}
		}
		break
	}
}

// stopFreezeHandler ensures freezeHandler stops with the appropriate
// preconditions
func stopFreezeHandler(sigChan chan os.Signal, abortChan chan struct{}) {
	// Check if handler has already returned
	var abortOpen bool = true
	var sigOpen bool = true

	select {
	// Check if sigChan has been closed
	case _, sigOpen = <-sigChan:
	default:
	}
	if sigOpen {
		// Close sigChan to trigger thaw
		signal.Stop(sigChan)
		close(sigChan)
	}

	select {
	// Check if abort channel has been closed
	case _, abortOpen = <-abortChan:
		if abortOpen {
			// handler has signal that it returned
			close(abortChan)
			abortOpen = false
		}
	default:
	}

	if abortOpen {
		select {
		// Wait no longer than a second (should not take long)
		case <-time.After(time.Second):

		case <-abortChan:
		}
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

	buf := make([]byte, bufferSize)
	for {
		n, err := copyChunk(buf, src, dst)
		if err == io.EOF {
			err = nil
			break
		} else if n < 0 {
			break
		}
		wd := atomic.SwapInt32(wdt, wdtSet)
		if wd == wdtExpired {
			return errWatchDogExpired
		}
		pb.Tick(uint64(n))
	}
	return nil
}

func copyChunk(buf []byte, src io.Reader, dst io.Writer) (int, error) {
	n, err := src.Read(buf)
	if err != nil {
		return n, err
	} else if n < 0 {
		return n, fmt.Errorf("source returned negative number of bytes")
	}

	w, err := dst.Write(buf[:n])
	if err != nil {
		return w, err
	} else if w < n {
		err = errors.Wrap(io.ErrShortWrite,
			"Error writing to stream")
		return w, err
	}
	return w, nil
}
