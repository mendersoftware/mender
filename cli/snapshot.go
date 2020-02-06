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
	errMsgDataPartFmt = "Device-local data is stored on the partition " +
		"being snapshotted: %s. The recommended approach is to have " +
		"a separate data-partition mounted on \"/data\" and add a " +
		"symbolic link (%s -> /data). https://docs.mender.io/devices/" +
		"general-system-requirements#partition-layout"
	errMsgThawFmt = "Try running `fsfreeze -u %s` or press `SYSRQ+j`, " +
		"immediately!"

	// Watchdog constants //
	// Ping from main thread, keep the operation alive.
	wdtPing = 1
	// Exit request from the main thread. `response` can be read exactly
	// once, after this.
	wdtExit = 2
	// wdtIntervalSec Sets the polling interval of the listening routine.
	// NOTE: this should be sufficient enough for the user to for example
	//       type their password to ssh etc.
	wdtIntervalSec = 30

	// bufferSize for the Copy function
	bufferSize = 32 * 1024
)

var (
	errDumpTerminal = errors.New("Refusing to write to terminal")
)

type snapshot struct {
	src io.ReadCloser
	dst io.WriteCloser
	// The FIFREEZE ioctl requires that the file descriptor points to a
	// directory
	freezeDir *os.File
	// Watchdog used to detect and release "freeze deadlocks"
	wdt *watchDog
	// Optional progressbar.
	pb *utils.ProgressWriter
}

type watchDog struct {
	request  chan int
	response chan error
}

func newWatchDog() *watchDog {
	return &watchDog{
		request:  make(chan int),
		response: make(chan error),
	}
}

// DumpSnapshot copies a snapshot of the root filesystem to stdout.
func DumpSnapshot(ctx *cli.Context) error {
	log.SetOutput(os.Stderr)

	fd := int(os.Stdout.Fd())
	// Make sure stdout is redirected (not a tty device)
	if _, err := unix.IoctlGetTermios(fd, unix.TCGETS); err == nil {
		return errDumpTerminal
	}

	return CopySnapshot(ctx, os.Stdout)
}

// CopySnapshot freezes the filesystem and copies a snapshot to out.
func CopySnapshot(ctx *cli.Context, dst io.WriteCloser) error {
	var err error

	srcPath, _ := ctx.String("source")
	dataPath, _ := ctx.String("data")
	quiet, _ := ctx.Bool("quiet")
	compression, _ := ctx.String("compression")
	ss := &snapshot{dst: dst}
	defer ss.cleanup()

	// Ensure we don't write logs to the filesystem
	log.SetOutput(os.Stderr)
	if quiet {
		log.SetLevel(log.ErrorLevel)
	}

	srcID, err := ss.validateSrcDev(srcPath, dataPath)
	if err != nil {
		return err
	}

	srcPath, err = system.GetBlockDeviceFromID(srcID)
	if err != nil {
		return err
	}

	err = ss.init(srcPath, compression, !quiet)
	if err != nil {
		return err
	}

	srcDev, err := system.GetMountInfoFromDeviceID(srcID)
	if err == system.ErrDevNotMounted {
		// If not mounted we're good to go
	} else if err != nil {
		return errors.Wrap(err, "failed preconditions for snapshot")
	} else {
		if ss.freezeDir, err = os.OpenFile(
			srcDev.MountPoint, 0, 0); err == nil {
			// freezeHandler is a transparent signal handler that
			// ensures system.ThawFs is called upon a terminating
			// signal.
			ss.wdt = newWatchDog()
			fh := freezeHandler{
				fd:  ss.freezeDir,
				wdt: ss.wdt,
			}
			go fh.run()
			log.Debugf("Freezing %s", srcDev.MountPoint)
			err = system.FreezeFS(int(ss.freezeDir.Fd()))
		}
		if err != nil {
			log.Warnf("Failed to freeze filesystem on %s: %s",
				srcDev.MountSource, err.Error())
			log.Warn("The snapshot might become invalid.")
		}
	}

	err = ss.Do()
	if err != nil {
		return err
	}
	return nil
}

// init the input/output and optionally a progressbar for snapshot.
func (ss *snapshot) init(
	srcPath string,
	compression string,
	showProgress bool,
) error {
	var fdSrc *os.File
	var err error

	if fdSrc, err = os.Open(srcPath); err != nil {
		return err
	}
	ss.src = fdSrc

	if showProgress {
		// Initialize progress bar
		// Get file system size
		fsSize, err := system.GetBlockDeviceSize(fdSrc)
		if err != nil {
			return errors.Wrap(err,
				"unable to get partition size")
		}

		ss.pb = &utils.ProgressWriter{
			Out: os.Stderr,
			N:   int64(fsSize),
		}
	}

	if err := ss.assignCompression(compression); err != nil {
		return err
	}

	return err
}

// assignCompression parses the compression argument and wraps the output
// writer.
func (ss *snapshot) assignCompression(compression string) error {
	var err error

	switch compression {
	case "none":

	case "gzip":
		ss.dst = gzip.NewWriter(ss.dst)

	case "lzma":
		err = errors.New("lzma compression is not implemented for " +
			"snapshot command")

	default:
		err = errors.Errorf("Unknown compression '%s'", compression)

	}
	return err
}

// cleanup closes all open files and stops the freezeHandler if running.
func (ss *snapshot) cleanup() {
	// Close open file descriptors
	if ss.dst != nil {
		ss.dst.Close()
	}
	if ss.src != nil {
		ss.src.Close()
	}
	if ss.freezeDir != nil {
		ss.freezeDir.Close()
	}
}

// validateSrcDev checks that the device id associated with srcPath is valid and
// returns the device id [2]uint32{major, minor} number and an error upon
// failure.
func (ss *snapshot) validateSrcDev(
	srcPath string,
	dataPath string,
) ([2]uint32, error) {
	var err error
	var stat unix.Stat_t

	err = unix.Stat(srcPath, &stat)
	if err != nil {
		return [2]uint32{0, 0}, errors.Wrapf(err,
			"failed to validate source %s", srcPath)
	}

	if (stat.Mode & (unix.S_IFBLK | unix.S_IFDIR)) == 0 {
		return [2]uint32{0, 0}, errors.New(
			"source must point to a directory or block device")
	}

	rootDevID, err := system.GetDeviceIDFromPath(srcPath)
	if err != nil {
		return rootDevID, errors.Wrapf(err,
			"error getting device id from path '%s'", srcPath)
	}
	dataDevID, err := system.GetDeviceIDFromPath(dataPath)
	if err != nil {
		return rootDevID, errors.Wrapf(err,
			"error getting device id from path '%s'", dataPath)
	}

	if dataDevID == rootDevID {
		log.Errorf(errMsgDataPartFmt, dataPath, dataPath)
		return rootDevID, errors.Errorf(
			"data store (%s) is located on filesystem %s",
			dataPath, srcPath)
	}
	if rootDevID[0] == 0 {
		log.Errorf("Resolved an unnamed target device (device id: "+
			"%d:%d). Is the device running overlayfs? Try "+
			"passing the device file to the --source flag",
			rootDevID[0], rootDevID[1])
		return rootDevID, fmt.Errorf(
			"unnamed target device (device id: %d:%d)",
			rootDevID[0], rootDevID[1])
	}

	return rootDevID, err
}

// freezeHandler is a transparent signal handler and watchdog timer ensuring
// system.ThawFS is called on a terminating signal before relaying the signal to
// the system default handler. The sigChan should be notified on ALL incomming
// signals to the process, signals that are ignored by default are also ignored
// by this handler. The handler must be periodically pinged using the
// wdt.request and wdtPing or its timer will expire, which will trigger an error
// as the response, and calling ThawFS. wdtExit must be used to exit the
// handler.

type freezeHandler struct {
	fd          *os.File
	wdt         *watchDog
	thawed      bool
	timerActive bool
	sigChan     chan os.Signal
	retErr      error
	timer       *time.Timer
	exit        bool
}

func (fh *freezeHandler) run() {
	fh.timer = time.NewTimer(wdtIntervalSec * time.Second)
	fh.sigChan = make(chan os.Signal, 1)
	signal.Notify(fh.sigChan)
	fh.thawed = false
	fh.timerActive = true

	defer func() {
		signal.Stop(fh.sigChan)
		signal.Reset()
		close(fh.sigChan)
		if fh.timerActive && !fh.timer.Stop() {
			<-fh.timer.C
		}
	}()

	for !fh.exit {
		select {
		case request := <-fh.wdt.request:
			fh.handleRequest(request)

		case <-fh.timer.C:
			fh.handleExpiredTimer()

		case sig := <-fh.sigChan:
			fh.handleSignal(sig)
		}
	}
}

func (fh *freezeHandler) handleRequest(request int) {
	if request == wdtExit {
		if !fh.thawed {
			thawFs(fh.fd)
		}
		fh.exit = true

	} else if request == wdtPing {
		if fh.timerActive {
			if !fh.timer.Stop() {
				<-fh.timer.C
			}
			fh.timer.Reset(wdtIntervalSec * time.Second)
		}
	}
	fh.wdt.response <- fh.retErr
}

func (fh *freezeHandler) handleExpiredTimer() {
	fh.timerActive = false
	if fh.thawed {
		return
	}
	errStr := "Freeze timer expired due to " +
		"blocked main process."
	log.Error(errStr)
	log.Info("Unfreezing filesystem")
	thawFs(fh.fd)
	fh.retErr = errors.New(errStr)
	fh.thawed = true
}

func (fh *freezeHandler) handleSignal(sig os.Signal) {
	if fh.thawed {
		return
	}
	log.Debugf("Freeze handler received signal: %s",
		sig.String())
	if sig == unix.SIGURG ||
		sig == unix.SIGWINCH ||
		sig == unix.SIGCHLD {
		// Signals that are ignored by default
		// keep ignoring them.
		return
	}
	thawFs(fh.fd)
	fh.retErr = errors.Errorf("Freeze handler interrupted by signal: %s",
		sig.String())
	fh.thawed = true
}

func thawFs(fd *os.File) {
	log.Debugf("Thawing %s", fd.Name())
	if err := system.ThawFS(int(fd.Fd())); err != nil {
		log.Errorf("Unable to unfreeze filesystem: %s",
			err.Error())
		log.Errorf(errMsgThawFmt, fd.Name())
	}
}

// Do starts copying data from src to dst and conditionally updates the
// progressbar.
func (ss *snapshot) Do() (retErr error) {
	defer func() {
		if ss.wdt != nil {
			ss.wdt.request <- wdtExit
			err := <-ss.wdt.response
			if retErr == nil {
				retErr = err
			}
		}
	}()

	buf := make([]byte, bufferSize)
	for {
		n, err := copyChunk(buf, ss.src, ss.dst)
		if err == io.EOF {
			err = nil
			break
		} else if n < 0 {
			break
		} else if err != nil {
			return err
		}
		if ss.wdt != nil {
			ss.wdt.request <- wdtPing
			err = <-ss.wdt.response
		}
		if err != nil {
			return err
		}
		if ss.pb != nil {
			ss.pb.Tick(uint64(n))
		}
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
