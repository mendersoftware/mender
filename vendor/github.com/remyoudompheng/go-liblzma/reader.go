// Copyright 2011-2019 Rémy Oudompheng. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xz

/*
#cgo LDFLAGS: -llzma
#include <lzma.h>
#include <stdlib.h>

int go_lzma_code(
    lzma_stream* handle,
    void* next_in,
    void* next_out,
    lzma_action action
) {
    handle->next_in = next_in;
    handle->next_out = next_out;
    return lzma_code(handle, action);
}
*/
import "C"

import (
	"io"
	"math"
	"unsafe"
)

type Decompressor struct {
	handle *C.lzma_stream
	rd     io.Reader
	buffer []byte // buffer allocated when the Decompressor was created to hold data read from rd
	offset int    // offset of the next byte in the buffer to read
	length int    // number of actual bytes in the buffer from the reader
}

var _ io.ReadCloser = &Decompressor{}

func NewReader(r io.Reader) (*Decompressor, error) {
	dec := new(Decompressor)
	dec.rd = r
	dec.buffer = make([]byte, DefaultBufsize)
	dec.offset = DefaultBufsize
	dec.handle = allocLzmaStream(dec.handle)
	// Initialize decoder
	ret := C.lzma_auto_decoder(dec.handle, C.uint64_t(math.MaxUint64),
		C.uint32_t(concatenated))
	if Errno(ret) != Ok {
		return nil, Errno(ret)
	}

	return dec, nil
}

func (r *Decompressor) Read(out []byte) (out_count int, er error) {
	if r.offset >= r.length {
		n, err := r.rd.Read(r.buffer)

		if err != nil && err != io.EOF {
			return 0, err
		}
		r.offset, r.length = 0, n
		r.handle.avail_in = C.size_t(n)
	}
	r.handle.avail_out = C.size_t(len(out))
	action := Run
	if r.handle.avail_in == 0 {
		action = Finish
	}
	ret := C.go_lzma_code(
		r.handle,
		unsafe.Pointer(&r.buffer[r.offset]),
		unsafe.Pointer(&out[0]),
		C.lzma_action(action),
	)
	r.offset = r.length - int(r.handle.avail_in)
	switch Errno(ret) {
	case Ok:
		er = nil
	case StreamEnd:
		er = io.EOF
	default:
		er = Errno(ret)
	}

	return len(out) - int(r.handle.avail_out), er
}

// Frees any resources allocated by liblzma. It does not close the
// underlying reader.
func (r *Decompressor) Close() error {
	if r != nil {
		C.lzma_end(r.handle)
		C.free(unsafe.Pointer(r.handle))
		r.handle = nil
	}
	return nil
}
