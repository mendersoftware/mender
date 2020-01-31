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
	errMsgDataPartFmt = "Device-local data is stored on the partition " +
		"being snapshotted: %s. The recommended approach is to have " +
		"a separate data-partition mounted on \"/data\" and add a " +
		"symbolic link (%s -> /data). https://docs.mender.io/devices/" +
		"general-system-requirements#partition-layout"
	errMsgThawFmt = "Try running `fsfreeze -u %s` or press `SYSRQ+j`, " +
		"immediately!"

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
)

type snapshot struct {
	src io.ReadCloser
	dst io.WriteCloser
	// The FIFREEZE ioctl requires that the file descriptor points to a
	// directory
	freezeDir *os.File
	// Watchdog timer variable used to detect and release "freeze deadlocks"
	wdt *int32
	// freezerChan is the IPC variable for the freeze (signal) handler
	freezerChan chan interface{}
	// Optional progressbar.
	pb *utils.ProgressBar
}

// DumpSnapshot copies a snapshot of the root filesystem to stdout.
func (runOpts *runOptionsType) DumpSnapshot(ctx *cli.Context) error {

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
	var watchDog int32
	srcPath := ctx.String("source")
	ss := &snapshot{dst: dst, wdt: &watchDog}
	defer ss.cleanup()

	// Ensure we don't write logs to the filesystem
	log.SetOutput(os.Stderr)
	if ctx.Bool("quiet") {
		log.SetLevel(log.ErrorLevel)
	}

	srcID, err := ss.validateSrcDev(srcPath, ctx.GlobalString("data"))
	if err != nil {
		return err
	}

	srcPath, err = system.GetBlockDeviceFromID(srcID)
	if err != nil {
		return err
	}

	err = ss.init(srcPath, ctx.String("compression"), !ctx.Bool("quiet"))
	if err != nil {
		return err
	}

	srcDev, err := system.GetMountInfoFromDeviceID(srcID)
	if err == system.ErrDevNotMounted {
		// If not mounted we're good to go
	} else if err != nil {
		return errors.Wrap(err, "failed preconditions for snapshot")
	} else {
		ss.freezerChan = make(chan interface{})
		if ss.freezeDir, err = os.OpenFile(
			srcDev.MountPoint, 0, 0); err == nil {
			// freezeHandler is a transparent signal handler that
			// ensures system.ThawFs is called upon a terminating
			// signal.
			go freezeHandler(ss.freezerChan, ss.freezeDir, ss.wdt)
			log.Debugf("Freezing %s", srcDev.MountPoint)
			err = system.FreezeFS(int(ss.freezeDir.Fd()))
		}
		if err != nil {
			log.Warnf("Failed to freeze filesystem on %s: %s",
				srcDev.MountSource, err.Error())
			log.Warn("The snapshot might become invalid.")
			close(ss.freezerChan)
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
		var pSize uint64 = 1
		if f, ok := ss.dst.(*os.File); ok {
			pSize = uint64(system.GetPipeSize(int(f.Fd())))
		}

		// Initialize progress bar
		// Get file system size
		fsSize, err := system.GetBlockDeviceSize(fdSrc)
		if err != nil {
			return errors.Wrap(err,
				"unable to get partition size")
		}
		ss.pb = utils.NewProgressBar(os.Stderr, fsSize, utils.BYTES)
		ss.pb.SetTTY(int(os.Stderr.Fd()))
		ss.pb.SetPrefix(fmt.Sprintf("%s: ", fdSrc.Name()))

		wc := utils.NewByteCountWriteCloser(ss.dst)
		ss.dst = wc
		ss.pb.SetStartCondition(func(c uint64) bool {
			if wc.BytesWritten > pSize {
				log.Infof("Initiating copy of uncompressed "+
					"size %s", utils.StringifySize(
					fsSize, 3))
				return true
			}
			return false
		})
	}

	if err := ss.assignCompression(compression); err != nil {
		return err
	}

	if ss.pb != nil {
		// Start progressbar if set.
		ss.pb.Start(time.Millisecond * 150)
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

	// Release freezeHandler
	if ss.freezerChan != nil {
		select {
		case _, freezerStoped := <-ss.freezerChan:
			if freezerStoped {
				close(ss.freezerChan)
			}

		default:
			ss.freezerChan <- struct{}{}
			// Wait for freezeHandler to return
			select {
			case <-ss.freezerChan:

			case <-time.After(5 * time.Second):

			}
			close(ss.freezerChan)
		}
	}

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

	// Shut down progressbar
	if ss.pb != nil {
		err := ss.pb.Close()
		if err != nil {
			log.Errorf(
				"Error stopping progress bar thread: %s",
				err.Error())
		}
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
// system.ThawFS is called on a terminating signal before relaying the signal
// to the system default handler. The only signal that is not relayed to the
// default handler is SIGUSR1 which can be used to trigger a FITHAW ioctl
// on the filesystem. The sigChan should be notified on ALL incomming signals to
// the process, signals that are ignored by default are also ignored by this
// handler. The freezeChan is used for stopping the handler. If an empty
// struct is sent, the handler invokes ThawFS before returning, otherwise if the
// channel is closed the function returns immediately cleaning up the signal-
// handler.
func freezeHandler(
	freezeChan chan interface{},
	fd *os.File,
	wdt *int32,
) {
	var sig os.Signal = nil
	var abortOpen bool = true
	var err error
	sigChan := make(chan os.Signal)
	signal.Notify(sigChan)

	defer func() {
		signal.Stop(sigChan)
		if sig != nil {
			// Invoke default signal handler
			unix.Kill(os.Getpid(), sig.(unix.Signal))
		}
		close(sigChan)

		if abortOpen {
			// Notify that the routine quit
			freezeChan <- err
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
			log.Error("Freeze timer expired due to " +
				"blocked main process.")
			log.Info("Unfreezing filesystem")
			signal.Stop(sigChan)

		case _, abortOpen = <-freezeChan:
			if !abortOpen {
				return
			}

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
		log.Debugf("Thawing %s", fd.Name())
		if err = system.ThawFS(int(fd.Fd())); err != nil {
			log.Errorf("Unable to unfreeze filesystem: %s",
				err.Error())
			log.Errorf(errMsgThawFmt, fd.Name())
		}
		return
	}
}

// Do starts copying data from src to dst and conditionally updates the
// progressbar.
func (ss *snapshot) Do() error {

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
		wd := atomic.SwapInt32(ss.wdt, wdtSet)
		if wd == wdtExpired {
			return errWatchDogExpired
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
