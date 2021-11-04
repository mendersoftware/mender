// Copyright 2021 Northern.tech AS
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
	"github.com/mendersoftware/progressbar"
)

type ProgressWriter struct {
	bar      *progressbar.Bar
	finished bool
}

func NewProgressWriter(size int64) *ProgressWriter {
	return &ProgressWriter{
		bar: progressbar.New(size),
	}
}

func (p *ProgressWriter) Write(data []byte) (int, error) {
	if p.finished {
		return len(data), nil
	}
	n := len(data)
	if p.bar == nil {
		return n, nil
	}
	p.bar.Tick(int64(n))
	// Due to a small issue with logs intertwining with the progress bar,
	// due to the actual artifact being bigger than the payload, make the
	// progressbar a little forgiving at the end.
	if p.bar.Percentage == 99 {
		p.bar.Finish()
		p.finished = true
	}
	return n, nil
}

func (p *ProgressWriter) Tick(n uint64) {
	p.bar.Tick(int64(n))
}
