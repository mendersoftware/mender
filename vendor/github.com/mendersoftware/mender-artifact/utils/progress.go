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
	"io"

	"github.com/mendersoftware/progressbar"
)

type ProgressReader struct {
	bar *progressbar.Bar
	io.Reader
}

func NewProgressReader() *ProgressReader {
	return &ProgressReader{}
}

func (p *ProgressReader) Wrap(r io.Reader, size int64) io.Reader {
	bar := progressbar.New(size)
	bar.Size = size
	return &ProgressReader{
		Reader: r,
		bar:    bar,
	}
}

func (p *ProgressReader) Read(b []byte) (int, error) {
	n, err := p.Reader.Read(b)
	p.bar.Tick(int64(n))
	return n, err
}

type ProgressWriter struct {
	bar    *progressbar.Bar
	Writer io.WriteCloser
	tot    int
}

func NewProgressWriter() *ProgressWriter {
	return &ProgressWriter{}
}

func (p *ProgressWriter) Wrap(w io.WriteCloser) io.Writer {
	p.Writer = w
	return p
}

func (p *ProgressWriter) Reset(size int64, filename string, payloadNumber int) {
	p.bar = progressbar.New(size)
	p.bar.Size = size
}

func (p *ProgressWriter) Finish() {
	if p.bar != nil {
		p.bar.Finish()
	}
}

func (p *ProgressWriter) Write(b []byte) (int, error) {
	n, err := p.Writer.Write(b)
	p.bar.Tick(int64(n))
	return n, err
}
