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
package utils

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/pkg/errors"
	"golang.org/x/sys/unix"
)

type Units int

const (
	BYTES Units = iota
	TICKS
	NONE
)

const (
	defaultTerminalWidth = 80
)

type StartCondition func(count uint64) bool

type ProgressBar struct {
	// N is the progress bar limit
	N uint64
	// C is the current tick count
	C uint64
	// Prefix is the prefix text of the progress bar
	Prefix string

	out   io.Writer
	ttyFd int
	pchar string
	units Units

	ticker *time.Ticker
	cond   StartCondition
	// IPC channels for the printer go-routine. Need two channels to avoid
	// race condition in the close procedure.
	errChan  chan error
	stopChan chan struct{}
}

func NewProgressBar(out io.Writer, N uint64, units Units) *ProgressBar {
	return &ProgressBar{
		out:   out,
		N:     N,
		C:     0,
		pchar: "#",
		units: units,

		errChan:  make(chan error),
		stopChan: make(chan struct{}),
	}
}

func (pb *ProgressBar) SetProgressChar(char rune) {
	pb.pchar = string(char)
}

func (pb *ProgressBar) SetPrefix(prefix string) {
	pb.Prefix = prefix
}

// SetTTY notifies the progress bar that out is a tty device and adjusts the
// width to that of the terminal. If fd is not a terminal an error is returned
// and the width remains the default.
func (pb *ProgressBar) SetTTY(fd int) error {
	_, err := unix.IoctlGetWinsize(
		fd, unix.TIOCGWINSZ)
	if err == nil {
		pb.ttyFd = fd
	}
	return err
}

func (pb *ProgressBar) SetStartCondition(cond StartCondition) {
	pb.cond = cond
}

// Tick advances the progressbar by n ticks.
func (pb *ProgressBar) Tick(n uint64) error {
	var err error
	var open bool
	pb.C += n
	if pb.C > pb.N {
		err = fmt.Errorf("Progressbar limit exceeded (%d > %d)",
			pb.C, pb.N)
	}

	select {
	case err, open = <-pb.errChan:
		if !open {
			err = fmt.Errorf("tick on closed progressbar")
		}
	default:
	}

	return err
}

// Start initializes the progress bar printer. The printInterval parameter
// determines the interval for which the progress bar should update the output.
func (pb *ProgressBar) Start(printInterval time.Duration) {
	pb.ticker = time.NewTicker(printInterval)
	go pb.printer(pb.ticker)
}

// Close cleans up the stops the printer routine and cleans up internal
// structures
func (pb *ProgressBar) Close() error {
	var err error
	var open bool
	pb.ticker.Stop()
	select {
	case err, open = <-pb.errChan:
		if open {
			close(pb.errChan)
		}
	default:
		pb.stopChan <- struct{}{}
		select {
		case err, open = <-pb.errChan:
			if open {
				close(pb.errChan)
			}

		case <-time.After(5 * time.Second):
		}
	}
	// Close stopChan
	select {
	case _, open = <-pb.stopChan:
		// in case close is called earlier this should set open to false
	default:
	}
	if open {
		close(pb.stopChan)
	}
	return err
}

func (pb *ProgressBar) waitForPrecondition(ticker *time.Ticker) (bool, error) {
	var err error
	var open bool

	if pb.cond != nil {
		for err == nil {
			select {
			case <-ticker.C:
				if pb.cond(pb.C) {
					return true, nil
				}

			case err, open = <-pb.errChan:
				if !open {
					return false, fmt.Errorf(
						"progressbar already closed")
				}
			case <-pb.stopChan:
				return false, nil
			}
		}
	}
	return true, nil
}

func (pb *ProgressBar) printer(ticker *time.Ticker) {
	var width int
	var suffix string
	var err error
	var winSz *unix.Winsize
	var keepTicking bool = true

	defer func() {
		pb.errChan <- err
	}()
	keepTicking, err = pb.waitForPrecondition(ticker)
	for keepTicking {
		select {
		case <-ticker.C:

		case _, open := <-pb.stopChan:
			if !open {
				break
			}
			keepTicking = false
		}
		if pb.ttyFd != 0 {
			// Adjust to terminal width
			winSz, err = unix.IoctlGetWinsize(
				pb.ttyFd, unix.TIOCGWINSZ)
			if err != nil {
				err = errors.Wrap(err,
					"error getting tty window size")
				return
			}
			width = int(winSz.Col)
		} else {
			width = defaultTerminalWidth
		}

		percentF := 100 * (float64(pb.C) / float64(pb.N))
		// percentInt is the rounded percentage
		percentInt := int(percentF)
		if percentF-float64(percentInt) >= 0.5 {
			percentInt++
		}
		switch pb.units {
		case BYTES:
			suffix = StringifySize(pb.C, 4)
			suffix = fmt.Sprintf("%3d%% | %s", percentInt, suffix)
		case TICKS:
			suffix = fmt.Sprintf("%3d%% | #%d", percentInt, pb.C)
		default:
			// None
			suffix = ""
		}

		progWidth := width - len(suffix) - len(pb.Prefix) - 2
		if progWidth <= 0 {
			// Ignore nasty line wrapping and force default
			progWidth = defaultTerminalWidth
		}

		numPChars := int(float64(progWidth*percentInt) / 100.0)
		pChars := strings.Repeat(pb.pchar, numPChars)
		_, err = pb.out.Write([]byte(fmt.Sprintf(
			"\r%s[%-*s]%s", pb.Prefix, progWidth, pChars, suffix)))
		if err != nil {
			return
		}
	}
}

// TODO: Create a separate utility source file with funcs like this
func StringifySize(bytes uint64, width int) string {
	var suffixes = [...]string{"B  ", "KiB", "MiB", "GiB", "TiB"}
	var suffix string
	bytesF := float64(bytes)
	for _, unit := range suffixes {
		suffix = unit
		if bytesF/1024.0 < 1.0 {
			break
		}
		bytesF /= 1024.0
	}

	// Fix the character width
	var decimalWidth int
	var fracWidth int
	size := bytesF
	for decimalWidth = 0; size >= 1.0; decimalWidth++ {
		size /= 10.0
	}

	// Don't miss the dot (-1)
	fracWidth = width - decimalWidth - 1
	if fracWidth < 0 {
		fracWidth = 0
	}
	if fracWidth == 0 {
		// Dot is missing!
		decimalWidth++
	}
	return fmt.Sprintf("%*.*f%s", decimalWidth,
		fracWidth, bytesF, suffix)
}
