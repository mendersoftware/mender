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
package utils

import (
	"fmt"
	"os"
	"strings"

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
	defaultTerminalWidth = 32
)

type ProgressBar struct {
	out        *os.File
	N          uint64
	c          uint64
	pchar      string
	prefix     string
	units      Units
	exceeded   bool
	isTerminal bool
}

func NewProgressBar(out *os.File, N uint64, units Units) *ProgressBar {
	isTerminal := true
	_, err := unix.IoctlGetTermios(int(out.Fd()), unix.TCGETS)
	if err != nil {
		// If out is not a terminal there is no point in creating
		// a progress bar.
		return nil
	}
	return &ProgressBar{
		out:        out,
		N:          N,
		c:          0,
		pchar:      "#",
		units:      units,
		exceeded:   false,
		isTerminal: isTerminal,
	}
}

func (pb *ProgressBar) SetProgressChar(char rune) {
	pb.pchar = string(char)
}

func (pb *ProgressBar) SetPrefix(prefix string) {
	pb.prefix = prefix
}

func (pb *ProgressBar) Tick(n uint64) error {

	var width int
	var suffix string
	pb.c += n
	if pb.c > pb.N {
		log.Warnf("Progressbar exceeded maximum (%d > %d)", pb.c, pb.N)
	}
	percent := float64(pb.c) / float64(pb.N)

	if pb.isTerminal {
		// Adjust to terminal width
		winSz, err := unix.IoctlGetWinsize(int(pb.out.Fd()), unix.TIOCGWINSZ)
		if err != nil {
			return err
		}
		width = int(winSz.Col)
		if _, err := pb.out.WriteString("\r"); err != nil {
			return err
		}
	} else {
		width = defaultTerminalWidth
	}

	switch pb.units {
	case BYTES:
		suffix = ShortSize(pb.c)
		suffix = fmt.Sprintf("%3d%% | %s", int(100*percent), suffix)
	case TICKS:
		suffix = fmt.Sprintf("%3d%% | #%d", int(100*percent), pb.c)
	default:
		// None
		suffix = ""
	}

	progWidth := width - len(suffix) - len(pb.prefix) - 2
	if progWidth <= 0 {
		// Ignore nasty line wrapping and force default
		progWidth = defaultTerminalWidth
	}

	numPChars := int(float64(progWidth) * percent)
	pChars := strings.Repeat(pb.pchar, numPChars)
	spaces := strings.Repeat(" ", progWidth-numPChars)
	_, err := pb.out.WriteString(fmt.Sprintf(
		"%s[%s%s]%s", pb.prefix, pChars, spaces, suffix))
	return err
}

// TODO: Create a separate utility source file with funcs like this
func ShortSize(bytes uint64) string {
	var suffixes = [...]string{"B", "KiB", "MiB", "GiB", "TiB"}
	var suffix string
	tmp := bytes
	for _, unit := range suffixes {
		suffix = unit
		if tmp/1024 <= 0 {
			break
		}
		tmp /= 1024
	}
	return fmt.Sprintf("%d%s", tmp, suffix)
}
