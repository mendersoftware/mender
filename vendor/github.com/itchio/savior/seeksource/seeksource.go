package seeksource

import (
	"bufio"
	"bytes"
	"fmt"
	"io"

	"github.com/itchio/savior"
	"github.com/itchio/wharf/eos"
	"github.com/pkg/errors"
)

type seekSource struct {
	rs io.ReadSeeker

	br *bufio.Reader

	ssc      savior.SourceSaveConsumer
	wantSave bool

	sectionStart int64
	offset       int64
	size         int64
}

var _ savior.SeekSource = (*seekSource)(nil)

func FromFile(file eos.File) savior.SeekSource {
	res := &seekSource{
		rs: file,
	}

	stats, err := file.Stat()
	if err == nil {
		res.size = stats.Size()
	}

	return res
}

func FromBytes(buf []byte) savior.SeekSource {
	return NewWithSize(bytes.NewReader(buf), int64(len(buf)))
}

// NewWithSize returns a new source that reads up to 'size' bytes
// from an io.ReadSeeker. If size is zero, the SeekSource will immediately EOF.
func NewWithSize(rs io.ReadSeeker, size int64) savior.SeekSource {
	return &seekSource{
		rs:   rs,
		size: size,
	}
}

func (ss *seekSource) Features() savior.SourceFeatures {
	return savior.SourceFeatures{
		Name:          "seek",
		ResumeSupport: savior.ResumeSupportBlock,
	}
}

func (ss *seekSource) SetSourceSaveConsumer(ssc savior.SourceSaveConsumer) {
	savior.Debugf("seeksource: set source save consumer!")
	ss.ssc = ssc
}

func (ss *seekSource) WantSave() {
	savior.Debugf("seeksource: want save!")
	ss.wantSave = true
}

func (ss *seekSource) Resume(c *savior.SourceCheckpoint) (int64, error) {
	if c == nil {
		ss.offset = 0
	} else {
		if c.Offset < 0 {
			return 0, errors.New("cannot resume from negative offset (corrupted checkpoint?)")
		}
		ss.offset = c.Offset
	}

	newOffset, err := ss.rs.Seek(ss.sectionStart+ss.offset, io.SeekStart)
	if err != nil {
		return newOffset, errors.WithStack(err)
	}

	if ss.br == nil {
		ss.br = bufio.NewReader(ss.rs)
	} else {
		ss.br.Reset(ss.rs)
	}

	return ss.offset, nil
}

func (ss *seekSource) Tell() int64 {
	return ss.offset
}

func (ss *seekSource) Size() int64 {
	return ss.size
}

func (ss *seekSource) Section(start int64, size int64) (savior.SeekSource, error) {
	if start < 0 {
		return nil, errors.WithStack(fmt.Errorf("can't make section with negative start"))
	}

	if size < 0 {
		return nil, errors.WithStack(fmt.Errorf("can't make section with negative size"))
	}

	if start+size > ss.size {
		return nil, errors.WithStack(fmt.Errorf("section too large: start+size (%d) > original size (%d)", start+size, ss.size))
	}

	sectionSeekSource := &seekSource{
		rs:           ss.rs,
		size:         size,
		sectionStart: ss.sectionStart + start,
	}
	return sectionSeekSource, nil
}

func (ss *seekSource) Read(buf []byte) (int, error) {
	if ss.br == nil {
		return 0, errors.WithStack(savior.ErrUninitializedSource)
	}

	if len(buf) == 0 {
		return 0, nil
	}

	remaining := ss.size - ss.offset
	if remaining == 0 {
		return 0, io.EOF
	}
	if int64(len(buf)) > remaining {
		buf = buf[:remaining]
	}

	ss.handleSave()
	n, err := ss.br.Read(buf)
	ss.offset += int64(n)
	return n, err
}

func (ss *seekSource) ReadByte() (byte, error) {
	if ss.br == nil {
		return 0, errors.WithStack(savior.ErrUninitializedSource)
	}

	if ss.offset == ss.size {
		return 0, io.EOF
	}

	ss.handleSave()
	b, err := ss.br.ReadByte()
	if err == nil {
		ss.offset++
	}
	return b, err
}

func (ss *seekSource) handleSave() {
	if ss.wantSave {
		ss.wantSave = false
		if ss.ssc != nil {
			c := &savior.SourceCheckpoint{
				Offset: ss.offset,
			}
			savior.Debugf("seeksource: emitting checkpoint at %d!", c.Offset)
			ss.ssc.Save(c)
		}
	}
}

func (ss *seekSource) Progress() float64 {
	// avoid NaNs
	if ss.size > 0 {
		return float64(ss.offset) / float64(ss.size)
	}

	return 0
}
