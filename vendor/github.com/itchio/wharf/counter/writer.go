package counter

import "io"

// A Writer keeps track of how many bytes have been written to a writer
type Writer struct {
	count  int64
	writer io.Writer

	onWrite CountCallback
}

// NewWriter returns a new counting writer. If the specified writer is
// nil, it will still count all bytes written, then discard them
func NewWriter(writer io.Writer) *Writer {
	return &Writer{writer: writer}
}

// NewWriterCallback returns a new counting writer with a callback
// to be called on every write.
func NewWriterCallback(onWrite CountCallback, writer io.Writer) *Writer {
	return &Writer{
		writer:  writer,
		onWrite: onWrite,
	}
}

// Count returns the number of bytes written through this counting writer
func (w *Writer) Count() int64 {
	return w.count
}

func (w *Writer) SetCount(count int64) {
	w.count = count
}

// Write is our io.Writer implementation
func (w *Writer) Write(buffer []byte) (n int, err error) {
	oldCount := w.count

	n = len(buffer)
	w.count += int64(n)

	if w.onWrite != nil {
		w.onWrite(w.count)
	}

	if w.writer != nil {
		n, err = w.writer.Write(buffer)
		w.count = oldCount + int64(n)
	}

	return
}

// Close closes the counting writer, but DOES NOT close the underlying writer
func (w *Writer) Close() error {
	return nil
}
