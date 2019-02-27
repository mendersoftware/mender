package counter

import "io"

// A Reader keeps track of how many bytes have been read from a reader
type Reader struct {
	count  int64
	reader io.Reader

	onRead CountCallback
}

// NewReader returns a new counting reader. If the specified reader is
// nil, it will still count all bytes written, then discard them
func NewReader(reader io.Reader) *Reader {
	return &Reader{reader: reader}
}

// NewReaderCallback returns a new counting reader with a callback
// to be called on every read.
func NewReaderCallback(onRead CountCallback, reader io.Reader) *Reader {
	return &Reader{
		reader: reader,
		onRead: onRead,
	}
}

// Count returns the number of bytes read through that counting reader
func (r *Reader) Count() int64 {
	return r.count
}

// Read is our io.Reader implementation
func (r *Reader) Read(buffer []byte) (n int, err error) {
	if r.reader == nil {
		n = len(buffer)
	} else {
		n, err = r.reader.Read(buffer)
	}

	r.count += int64(n)
	if r.onRead != nil {
		r.onRead(r.count)
	}
	return
}

// Close closes the counting reader, but DOES NOT close the underlying reader
func (r *Reader) Close() error {
	return nil
}
