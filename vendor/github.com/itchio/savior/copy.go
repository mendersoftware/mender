package savior

import (
	"io"

	"github.com/pkg/errors"
)

// ErrStop is returned when decompression has been stopped by a SaveConsumer returning
// AfterActionStop.
var ErrStop = errors.New("copy was stopped after save!")

type EmitProgressFunc func()

type Savable interface {
	WantSave()
}

type CopyParams struct {
	Src   io.Reader
	Dst   io.Writer
	Entry *Entry

	Savable Savable

	EmitProgress EmitProgressFunc
}

const progressThreshold = 512 * 1024

type Copier struct {
	// params
	SaveConsumer SaveConsumer

	// internal
	buf  []byte
	stop bool
}

func NewCopier(SaveConsumer SaveConsumer) *Copier {
	return &Copier{
		SaveConsumer: SaveConsumer,
		buf:          make([]byte, 32*1024),
	}
}

func (c *Copier) Do(params *CopyParams) error {
	if params == nil {
		return errors.New("CopyWithSaver called with nil params")
	}

	c.stop = false

	var progressCounter int64

	for !c.stop {
		n, readErr := params.Src.Read(c.buf)

		m, err := params.Dst.Write(c.buf[:n])
		if err != nil {
			return errors.WithStack(err)
		}

		progressCounter += int64(m)
		if progressCounter > progressThreshold {
			progressCounter = 0
			if params.EmitProgress != nil {
				params.EmitProgress()
			}
		}

		if readErr != nil {
			if readErr == io.EOF {
				// cool, we're done!
				return nil
			}
			return errors.WithStack(readErr)
		}

		if c.SaveConsumer.ShouldSave(int64(n)) {
			params.Savable.WantSave()
		}
	}

	return nil
}

func (c *Copier) Stop() {
	c.stop = true
}
