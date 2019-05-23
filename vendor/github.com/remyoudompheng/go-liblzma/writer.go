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
);
*/
import "C"
import (
	"bytes"
	"io"
	"unsafe"
)

type Compressor struct {
	handle *C.lzma_stream
	writer io.Writer
	buffer []byte
}

var _ io.WriteCloser = &Compressor{}

func allocLzmaStream(t *C.lzma_stream) *C.lzma_stream {
	return (*C.lzma_stream)(C.calloc(1, (C.size_t)(unsafe.Sizeof(*t))))
}

func NewWriter(w io.Writer, preset Preset) (*Compressor, error) {
	enc := new(Compressor)
	// The zero lzma_stream is the same thing as LZMA_STREAM_INIT.
	enc.writer = w
	enc.buffer = make([]byte, DefaultBufsize)
	enc.handle = allocLzmaStream(enc.handle)
	// Initialize encoder
	ret := C.lzma_easy_encoder(enc.handle, C.uint32_t(preset), C.lzma_check(CheckCRC64))
	if Errno(ret) != Ok {
		return nil, Errno(ret)
	}

	return enc, nil
}

// Initializes a XZ encoder with additional settings.
func NewWriterCustom(w io.Writer, preset Preset, check Checksum, bufsize int) (*Compressor, error) {
	enc := new(Compressor)
	// The zero lzma_stream is the same thing as LZMA_STREAM_INIT.
	enc.writer = w
	enc.buffer = make([]byte, bufsize)
	enc.handle = allocLzmaStream(enc.handle)

	// Initialize encoder
	ret := C.lzma_easy_encoder(enc.handle, C.uint32_t(preset), C.lzma_check(check))
	if Errno(ret) != Ok {
		return nil, Errno(ret)
	}

	return enc, nil
}

func (enc *Compressor) Write(in []byte) (n int, er error) {
	for n < len(in) {
		enc.handle.avail_in = C.size_t(len(in) - n)
		enc.handle.avail_out = C.size_t(len(enc.buffer))
		ret := C.go_lzma_code(
			enc.handle,
			unsafe.Pointer(&in[n]),
			unsafe.Pointer(&enc.buffer[0]),
			C.lzma_action(Run),
		)
		switch Errno(ret) {
		case Ok:
			break
		default:
			er = Errno(ret)
		}

		n = len(in) - int(enc.handle.avail_in)
		// Write back result.
		produced := len(enc.buffer) - int(enc.handle.avail_out)
		_, er = enc.writer.Write(enc.buffer[:produced])
		if er != nil {
			// Short write.
			return
		}
	}
	return
}

func (enc *Compressor) Flush() error {
	enc.handle.avail_in = 0

	for {
		enc.handle.avail_out = C.size_t(len(enc.buffer))
		// If Flush is invoked after Write produced an error, avail_in and next_in will point to
		// the bytes previously provided to Write, which may no longer be valid.
		enc.handle.avail_in = 0
		ret := C.go_lzma_code(
			enc.handle,
			nil,
			unsafe.Pointer(&enc.buffer[0]),
			C.lzma_action(Finish),
		)

		// Write back result.
		produced := len(enc.buffer) - int(enc.handle.avail_out)
		to_write := bytes.NewBuffer(enc.buffer[:produced])
		_, er := io.Copy(enc.writer, to_write)
		if er != nil {
			// Short write.
			return er
		}

		if Errno(ret) == StreamEnd {
			return nil
		}
	}
}

// Frees any resources allocated by liblzma. It does not close the
// underlying reader.
func (enc *Compressor) Close() error {
	if enc != nil {
		er := enc.Flush()
		C.lzma_end(enc.handle)
		C.free(unsafe.Pointer(enc.handle))
		enc.handle = nil
		if er != nil {
			return er
		}
	}
	return nil
}
