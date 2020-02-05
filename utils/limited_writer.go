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
	"syscall"

	"github.com/mendersoftware/log"
)

type LimitedWriteCloser struct {
	W io.WriteCloser // underlying resource
	N uint64         // number of bytes remaining
}

func (lw *LimitedWriteCloser) Write(p []byte) (int, error) {
	if lw.W == nil {
		return 0, syscall.EBADF
	}
	var selferr error
	toWrite := p

	if uint64(len(p)) > lw.N {
		// https://godoc.org/io#Writer Write writes len(p) bytes from p to the
		// underlying data stream. It returns the number of bytes written from p (0
		// <= n <= len(p)) and any error encountered that caused the write to stop
		// early.
		toWrite = p[:lw.N]
		selferr = syscall.ENOSPC
	}

	w, err := lw.W.Write(toWrite)
	if w != 0 {
		lw.N -= uint64(w)
	}
	if err != nil {
		selferr = err
	}
	return w, selferr
}

func (lw *LimitedWriteCloser) Close() error {
	log.Infof("%d bytes remaining to be written", lw.N)
	return lw.W.Close()
}

// ByteCountWriteCloser - as the name implies, just counts how many bytes the
// Writer/WriteCloser interface has written.
type ByteCountWriteCloser struct {
	BytesWritten uint64
	wc           io.WriteCloser
}

func NewByteCountWriteCloser(wc io.WriteCloser) *ByteCountWriteCloser {
	return &ByteCountWriteCloser{wc: wc}
}

// Write - Writer/WriteCloser interface function
func (bcwc *ByteCountWriteCloser) Write(p []byte) (int, error) {
	n, err := bcwc.wc.Write(p)
	bcwc.BytesWritten += uint64(n)
	return n, err
}

// Close - WriteCloser interface function
func (bcwc *ByteCountWriteCloser) Close() error {
	return bcwc.wc.Close()
}
