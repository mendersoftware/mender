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
package progressbar

// TODO -- Add terminal width respect

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
	// "golang.org/x/sys/unix" - Do not add for now (split into minimal and not (?))
)

type Renderer interface {
	Render(int) // Write the progressbar
}

type Bar struct {
	Size         int64
	currentCount int64
	Renderer
	Percentage int
}

func New(size int64) *Bar {
	if isatty.IsTerminal(os.Stderr.Fd()) {
		return &Bar{
			Renderer: &TTYRenderer{
				Out:            os.Stderr,
				ProgressMarker: ".",
				terminalWidth:  80,
			},
			Size: size,
		}
	} else {
		return &Bar{
			Renderer: &NoTTYRenderer{
				Out:            os.Stderr,
				ProgressMarker: ".",
				terminalWidth:  80,
			},
			Size: size,
		}
	}
}

func (b *Bar) Tick(n int64) {
	b.currentCount += n
	if b.Size > 0 {
		percentage := int((float64(b.currentCount) / float64(b.Size)) * 100)
		b.Percentage = percentage
		b.Renderer.Render(percentage)
	}
}

func (b *Bar) Reset() {
	b.currentCount = 0
	b.Renderer.Render(0)
}

func (b *Bar) Finish() {
	b.Renderer.Render(100)
}

type TTYRenderer struct {
	Out            io.Writer // Output device
	ProgressMarker string
	terminalWidth  int
	lastPercentage int
}

func (p *TTYRenderer) Render(percentage int) {
	if percentage <= p.lastPercentage {
		return
	}
	suffix := fmt.Sprintf(" - %3d %%", percentage)
	widthAvailable := p.terminalWidth - len(suffix)
	number_of_dots := int((float64(widthAvailable) * float64(percentage)) / 100)
	number_of_fillers := widthAvailable - number_of_dots
	if percentage > 100 {
		number_of_dots = widthAvailable
		number_of_fillers = 0
	}
	if percentage < 0 {
		return
	}
	if number_of_dots < 0 {
		return
	}
	if number_of_fillers < 0 {
		return
	}
	p.lastPercentage = percentage
	fmt.Fprintf(p.Out, "\r%s%s%s",
		strings.Repeat(p.ProgressMarker, number_of_dots),
		strings.Repeat(" ", number_of_fillers),
		suffix)
	if percentage == 100 {
		fmt.Fprintln(p.Out)
	}
}

type NoTTYRenderer struct {
	Out            io.Writer // Output device
	ProgressMarker string
	lastPercent    int
	terminalWidth  int
}

func (p *NoTTYRenderer) Render(percentage int) {
	if percentage > p.lastPercent {
		if percentage > 100 {
			return
		}
		number_of_dots := int((float64(p.terminalWidth) * float64(percentage-p.lastPercent)) / 100)
		str := strings.Repeat(p.ProgressMarker, number_of_dots)
		if number_of_dots > 0 {
			p.lastPercent = percentage
		}
		fmt.Fprintf(p.Out, str)
	}
}
