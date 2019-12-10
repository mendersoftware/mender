// Copyright 2017 Northern.tech AS
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
)

type ProgressWriter struct {
	Out  io.Writer // progress output
	N    int64     // size of the input
	c    int64     // current count
	over bool      // set to true of writes have gone over declared N bytes
}

func (p *ProgressWriter) Write(data []byte) (int, error) {
	n := len(data)

	p.reportGeneric(n)
	p.c += int64(n)
	return n, nil
}

func (p *ProgressWriter) Tick(n uint64) {
	p.reportGeneric(int(n))
	p.c += int64(n)
}

func (p *ProgressWriter) maybeWarn(then int64) {
	if p.N != 0 && then > p.N && !p.over {
		w := fmt.Sprintf("going over declared size, expected %v N, now %v\n",
			p.N, then)
		p.Out.Write([]byte(w))
		p.over = true
	}
}

func (p *ProgressWriter) reportGeneric(n int) {
	const perLine = 1 * 1024 * 1024 // 1MiB
	const dotsPerLine = 32
	const perDot = perLine / dotsPerLine

	now := p.c
	then := p.c + int64(n)
	nowDots := now / perDot
	thenDots := then / perDot

	p.maybeWarn(then)

	for ; nowDots < thenDots; nowDots++ {
		p.Out.Write([]byte("."))
		if nowDots != 0 && (nowDots+1)%dotsPerLine == 0 {
			// print percentage and size after each complete line
			var s string
			nowSize := (nowDots + 1) * perDot
			nowSizekB := nowSize / 1024
			if p.N == 0 || then > p.N {
				s = fmt.Sprintf(" %v KiB\n", nowSizekB)
			} else {
				s = fmt.Sprintf(" %3d%% %v KiB\n",
					100*nowSize/p.N, nowSizekB)
			}
			p.Out.Write([]byte(s))
		}
	}

	// fill up with spaces and display 100%, but only if we know the size
	if then == p.N {
		// a line was completed, so 100% progress was already displayed
		// at line end boundary
		if thenDots != 0 && thenDots%dotsPerLine == 0 {
			return
		}

		// round up expected line count
		lines := (then + perLine) / perLine
		if then%perLine == 0 {
			lines -= 1
		}

		left := (lines * dotsPerLine) - thenDots
		// if there was too little data no dots will be printed, make up
		// for this and print at least one to show progress
		if thenDots == 0 {
			p.Out.Write([]byte("."))
			left -= 1
		}
		// fill remaining space with spaces
		for i := int64(0); i < left; i++ {
			p.Out.Write([]byte(" "))
		}
		// print 100%, make sure we proper size suffix just in case
		// we are below 1kB with the whole size
		szSuffix := "KiB"
		size := p.N / 1024
		if p.N < 1024 {
			szSuffix = "B"
			size = p.N
		}
		s := fmt.Sprintf(" 100%% %v %s\n", size, szSuffix)
		p.Out.Write([]byte(s))
	}
}
