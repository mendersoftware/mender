// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package gzip implements reading and writing of gzip format compressed files,
// as specified in RFC 1952.
package gzip

import (
	"bufio"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"time"

	"github.com/itchio/kompress/flate"
)

const (
	gzipID1     = 0x1f
	gzipID2     = 0x8b
	gzipDeflate = 8
	flagText    = 1 << 0
	flagHdrCrc  = 1 << 1
	flagExtra   = 1 << 2
	flagName    = 1 << 3
	flagComment = 1 << 4
)

var (
	// ErrChecksum is returned when reading GZIP data that has an invalid checksum.
	ErrChecksum = errors.New("gzip: invalid checksum")
	// ErrHeader is returned when reading GZIP data that has an invalid header.
	ErrHeader = errors.New("gzip: invalid header")
)

var le = binary.LittleEndian

// noEOF converts io.EOF to io.ErrUnexpectedEOF.
func noEOF(err error) error {
	if err == io.EOF {
		return io.ErrUnexpectedEOF
	}
	return err
}

// The gzip file stores a header giving metadata about the compressed file.
// That header is exposed as the fields of the Writer and Reader structs.
//
// Strings must be UTF-8 encoded and may only contain Unicode code points
// U+0001 through U+00FF, due to limitations of the GZIP file format.
type Header struct {
	Comment string    // comment
	Extra   []byte    // "extra data"
	ModTime time.Time // modification time
	Name    string    // file name
	OS      byte      // operating system type
}

// A Reader is an io.Reader that can be read to retrieve
// uncompressed data from a gzip-format compressed file.
//
// In general, a gzip file can be a concatenation of gzip files,
// each with its own header. Reads from the Reader
// return the concatenation of the uncompressed data of each.
// Only the first header is recorded in the Reader fields.
//
// Gzip files store a length and checksum of the uncompressed data.
// The Reader will return a ErrChecksum when Read
// reaches the end of the uncompressed data if it does not
// have the expected length or checksum. Clients should treat data
// returned by Read as tentative until they receive the io.EOF
// marking the end of the data.
type Reader struct {
	Header           // valid after NewReader or Reader.Reset
	headerSize   int // how many bytes do we need to skip to resume reading?
	r            flate.Reader
	decompressor flate.SaverReader
	digest       uint32 // CRC-32, IEEE polynomial (section 8)
	size         uint32 // Uncompressed size (section 2.3.1)
	buf          [512]byte
	err          error
	multistream  bool
}

// NewReader creates a new Reader reading the given reader.
// If r does not also implement io.ByteReader,
// the decompressor may read more data than necessary from r.
//
// It is the caller's responsibility to call Close on the Reader when done.
//
// The Reader.Header fields will be valid in the Reader returned.
func NewReader(r io.Reader) (*Reader, error) {
	z := new(Reader)
	if err := z.Reset(r); err != nil {
		return nil, err
	}
	return z, nil
}

type Saver interface {
	WantSave()
	Save() (*Checkpoint, error)
}

// A Checkpoint allows resuming decompression from a certain point
// in the compressed data stream
type Checkpoint struct {
	Roffset         int64
	FlateCheckpoint *flate.Checkpoint
	HeaderSize      int
	Header          Header
	Digest          uint32
	Size            uint32
}

type SaverReader interface {
	io.ReadCloser
	Saver
}

type saverReader struct {
	f *Reader
}

func NewSaverReader(r io.Reader) (SaverReader, error) {
	f, err := NewReader(r)
	if err != nil {
		return nil, err
	}

	return &saverReader{f}, nil
}

func (sr *saverReader) Read(b []byte) (int, error) {
	n, err := sr.f.Read(b)
	if err == flate.ReadyToSaveError {
		// don't save that error
		sr.f.err = nil
	}
	return n, err
}

func (sr *saverReader) Close() error {
	return sr.f.Close()
}

func (sr *saverReader) WantSave() {
	sr.f.decompressor.WantSave()
}

func (sr *saverReader) Save() (*Checkpoint, error) {
	f := sr.f

	flateCheckpoint, err := f.decompressor.Save()
	if err != nil {
		return nil, err
	}

	res := &Checkpoint{
		Roffset:         flateCheckpoint.Roffset + int64(f.headerSize),
		FlateCheckpoint: flateCheckpoint,
		Digest:          f.digest,
		Header:          f.Header,
		HeaderSize:      f.headerSize,
		Size:            f.size,
	}
	return res, nil
}

// Resume starts decompressing again from a given checkpoint
func (c *Checkpoint) Resume(r io.Reader) (SaverReader, error) {
	decompressor, err := c.FlateCheckpoint.Resume(r)
	if err != nil {
		return nil, err
	}

	f := new(Reader)
	f.r = makeReader(r)
	f.decompressor = decompressor
	f.multistream = true
	f.headerSize = c.HeaderSize
	f.Header = c.Header
	f.digest = c.Digest
	f.size = c.Size
	return &saverReader{f}, nil
}

// Reset discards the Reader z's state and makes it equivalent to the
// result of its original state from NewReader, but reading from r instead.
// This permits reusing a Reader rather than allocating a new one.
func (z *Reader) Reset(r io.Reader) error {
	*z = Reader{
		decompressor: z.decompressor,
		multistream:  true,
		r:            makeReader(r),
	}
	z.Header, z.err = z.readHeader()
	return z.err
}

func makeReader(r io.Reader) flate.Reader {
	if rr, ok := r.(flate.Reader); ok {
		return rr
	}
	return bufio.NewReader(r)
}

// Multistream controls whether the reader supports multistream files.
//
// If enabled (the default), the Reader expects the input to be a sequence
// of individually gzipped data streams, each with its own header and
// trailer, ending at EOF. The effect is that the concatenation of a sequence
// of gzipped files is treated as equivalent to the gzip of the concatenation
// of the sequence. This is standard behavior for gzip readers.
//
// Calling Multistream(false) disables this behavior; disabling the behavior
// can be useful when reading file formats that distinguish individual gzip
// data streams or mix gzip data streams with other data streams.
// In this mode, when the Reader reaches the end of the data stream,
// Read returns io.EOF. If the underlying reader implements io.ByteReader,
// it will be left positioned just after the gzip stream.
// To start the next stream, call z.Reset(r) followed by z.Multistream(false).
// If there is no next stream, z.Reset(r) will return io.EOF.
func (z *Reader) Multistream(ok bool) {
	z.multistream = ok
}

// readString reads a NUL-terminated string from z.r.
// It treats the bytes read as being encoded as ISO 8859-1 (Latin-1) and
// will output a string encoded using UTF-8.
// This method always updates z.digest with the data read.
func (z *Reader) readString(r flate.Reader) (string, error) {
	var err error
	needConv := false
	for i := 0; ; i++ {
		if i >= len(z.buf) {
			return "", ErrHeader
		}
		z.buf[i], err = r.ReadByte()
		if err != nil {
			return "", err
		}
		if z.buf[i] > 0x7f {
			needConv = true
		}
		if z.buf[i] == 0 {
			// Digest covers the NUL terminator.
			z.digest = crc32.Update(z.digest, crc32.IEEETable, z.buf[:i+1])

			// Strings are ISO 8859-1, Latin-1 (RFC 1952, section 2.3.1).
			if needConv {
				s := make([]rune, 0, i)
				for _, v := range z.buf[:i] {
					s = append(s, rune(v))
				}
				return string(s), nil
			}
			return string(z.buf[:i]), nil
		}
	}
}

// readHeader reads the GZIP header according to section 2.3.1.
// This method does not set z.err.
func (z *Reader) readHeader() (hdr Header, err error) {
	r := &countingReader{
		r: z.r,
	}

	if _, err = io.ReadFull(r, z.buf[:10]); err != nil {
		// RFC 1952, section 2.2, says the following:
		//	A gzip file consists of a series of "members" (compressed data sets).
		//
		// Other than this, the specification does not clarify whether a
		// "series" is defined as "one or more" or "zero or more". To err on the
		// side of caution, Go interprets this to mean "zero or more".
		// Thus, it is okay to return io.EOF here.
		return hdr, err
	}
	if z.buf[0] != gzipID1 || z.buf[1] != gzipID2 || z.buf[2] != gzipDeflate {
		return hdr, ErrHeader
	}
	flg := z.buf[3]
	if t := int64(le.Uint32(z.buf[4:8])); t > 0 {
		// Section 2.3.1, the zero value for MTIME means that the
		// modified time is not set.
		hdr.ModTime = time.Unix(t, 0)
	}
	// z.buf[8] is XFL and is currently ignored.
	hdr.OS = z.buf[9]
	z.digest = crc32.ChecksumIEEE(z.buf[:10])

	if flg&flagExtra != 0 {
		if _, err = io.ReadFull(r, z.buf[:2]); err != nil {
			return hdr, noEOF(err)
		}
		z.digest = crc32.Update(z.digest, crc32.IEEETable, z.buf[:2])
		data := make([]byte, le.Uint16(z.buf[:2]))
		if _, err = io.ReadFull(r, data); err != nil {
			return hdr, noEOF(err)
		}
		z.digest = crc32.Update(z.digest, crc32.IEEETable, data)
		hdr.Extra = data
	}

	var s string
	if flg&flagName != 0 {
		if s, err = z.readString(r); err != nil {
			return hdr, err
		}
		hdr.Name = s
	}

	if flg&flagComment != 0 {
		if s, err = z.readString(r); err != nil {
			return hdr, err
		}
		hdr.Comment = s
	}

	if flg&flagHdrCrc != 0 {
		if _, err = io.ReadFull(r, z.buf[:2]); err != nil {
			return hdr, noEOF(err)
		}
		digest := le.Uint16(z.buf[:2])
		if digest != uint16(z.digest) {
			return hdr, ErrHeader
		}
	}

	z.headerSize = r.Count()
	z.digest = 0
	z.decompressor = flate.NewSaverReader(z.r)
	return hdr, nil
}

// Read implements io.Reader, reading uncompressed bytes from its underlying Reader.
func (z *Reader) Read(p []byte) (n int, err error) {
	if z.err != nil {
		return 0, z.err
	}

	n, z.err = z.decompressor.Read(p)
	z.digest = crc32.Update(z.digest, crc32.IEEETable, p[:n])
	z.size += uint32(n)
	if z.err != io.EOF {
		// In the normal case we return here.
		return n, z.err
	}

	// Finished file; check checksum and size.
	if _, err := io.ReadFull(z.r, z.buf[:8]); err != nil {
		z.err = noEOF(err)
		return n, z.err
	}
	digest := le.Uint32(z.buf[:4])
	size := le.Uint32(z.buf[4:8])
	if digest != z.digest || size != z.size {
		z.err = ErrChecksum
		return n, z.err
	}
	z.digest, z.size = 0, 0

	// File is ok; check if there is another.
	if !z.multistream {
		return n, io.EOF
	}
	z.err = nil // Remove io.EOF

	if _, z.err = z.readHeader(); z.err != nil {
		return n, z.err
	}

	// Read from next file, if necessary.
	if n > 0 {
		return n, nil
	}
	return z.Read(p)
}

// Close closes the Reader. It does not close the underlying io.Reader.
// In order for the GZIP checksum to be verified, the reader must be
// fully consumed until the io.EOF.
func (z *Reader) Close() error { return z.decompressor.Close() }

type countingReader struct {
	r flate.Reader

	count int
}

var _ flate.Reader = (*countingReader)(nil)

func (cr *countingReader) Read(buf []byte) (int, error) {
	n, err := cr.r.Read(buf)
	cr.count += n
	return n, err
}

func (cr *countingReader) ReadByte() (byte, error) {
	ret, err := cr.r.ReadByte()
	if err != nil {
		return 0, err
	}

	cr.count++
	return ret, nil
}

func (cr *countingReader) Count() int {
	return cr.count
}
