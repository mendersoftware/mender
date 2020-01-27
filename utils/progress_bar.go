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

	"github.com/mendersoftware/log"
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
	err    chan error
}

func NewProgressBar(out io.Writer, N uint64, units Units) *ProgressBar {
	return &ProgressBar{
		out:   out,
		N:     N,
		C:     0,
		pchar: "#",
		units: units,

		err: make(chan error),
	}
}

func (pb *ProgressBar) SetProgressChar(char rune) {
	pb.pchar = string(char)
}

func (pb *ProgressBar) SetPrefix(prefix string) {
	pb.Prefix = prefix
}

// SetTTY notifies the progress bar that out is a tty device and adjusts the
// width to that of the terminal. If fd is not a terminal nothing happens but
// unnecessary overhead is added to the tick loop.
func (pb *ProgressBar) SetTTY(fd int) {
	pb.ttyFd = fd
}

func (pb *ProgressBar) Tick(n uint64) uint64 {
	pb.C += n
	if pb.C > pb.N {
		log.Warnf("Progressbar exceeded maximum (%d > %d)", pb.C, pb.N)
	}
	return pb.C
}

// Start initializes the progress bar printer. The printInterval parameter
// determines the interval for which the progress bar should update the output.
func (pb *ProgressBar) Start(printInterval time.Duration, cond StartCondition) {
	pb.ticker = time.NewTicker(printInterval)
	go pb.printer(pb.ticker, cond)
}

func (pb *ProgressBar) Close() error {
	var err error
	var open bool
	pb.ticker.Stop()
	select {
	case err, open = <-pb.err:
		if open {
			close(pb.err)
		}
	default:
		pb.err <- nil
		err = <-pb.err
		close(pb.err)
	}
	return err
}

func (pb *ProgressBar) waitForPrecondition(
	ticker *time.Ticker,
	cond StartCondition,
) bool {

	var keepTicking = true
	if cond != nil {
		for keepTicking {
			select {
			case <-ticker.C:
				if cond(pb.C) {
					return true
				}

			case <-pb.err:
				keepTicking = false
			}
		}
	}
	return keepTicking
}

func (pb *ProgressBar) printer(ticker *time.Ticker, cond StartCondition) {
	var width int
	var suffix string
	var err error
	var winSz *unix.Winsize

	keepTicking := pb.waitForPrecondition(ticker, cond)
	for keepTicking {
		select {
		case <-ticker.C:

		case _, open := <-pb.err:
			if !open {
				break
			}
			defer func() { pb.err <- err }()
			keepTicking = false
		}
		if pb.ttyFd != 0 {
			// Adjust to terminal width
			winSz, err = unix.IoctlGetWinsize(
				pb.ttyFd, unix.TIOCGWINSZ)
			if err != nil {
				log.Debug("Stderr is not a tty, falling back " +
					"to default progress bar width")
				pb.ttyFd = 0
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
			pb.err <- err
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
