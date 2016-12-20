// Copyright 2016 Mender Software AS
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
)

type LimitedWriter struct {
	W io.Writer // underlying writer
	N uint64    // number of bytes remaining
}

func (lw *LimitedWriter) Write(p []byte) (int, error) {
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
