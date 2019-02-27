package gzipsource

import (
	"encoding/gob"
	"fmt"

	"github.com/itchio/kompress/flate"
	"github.com/itchio/kompress/gzip"
	"github.com/itchio/savior"
	"github.com/pkg/errors"
)

type gzipSource struct {
	// input
	source savior.Source

	// params
	threshold int64

	// internal
	sr      gzip.SaverReader
	offset  int64
	bytebuf []byte

	ssc              savior.SourceSaveConsumer
	sourceCheckpoint *savior.SourceCheckpoint
}

type GzipSourceCheckpoint struct {
	Offset           int64
	SourceCheckpoint *savior.SourceCheckpoint
	GzipCheckpoint   *gzip.Checkpoint
}

var _ savior.Source = (*gzipSource)(nil)

func New(source savior.Source) *gzipSource {
	return &gzipSource{
		source:  source,
		bytebuf: []byte{0x00},
	}
}

func (gs *gzipSource) Features() savior.SourceFeatures {
	return savior.SourceFeatures{
		Name:          "gzip",
		ResumeSupport: savior.ResumeSupportBlock,
	}
}

func (gs *gzipSource) SetSourceSaveConsumer(ssc savior.SourceSaveConsumer) {
	gs.ssc = ssc
	gs.source.SetSourceSaveConsumer(&savior.CallbackSourceSaveConsumer{
		OnSave: func(checkpoint *savior.SourceCheckpoint) error {
			gs.sourceCheckpoint = checkpoint
			gs.sr.WantSave()
			return nil
		},
	})
}

func (gs *gzipSource) WantSave() {
	gs.source.WantSave()
}

func (gs *gzipSource) Resume(checkpoint *savior.SourceCheckpoint) (int64, error) {
	if checkpoint != nil {
		if ourCheckpoint, ok := checkpoint.Data.(*GzipSourceCheckpoint); ok {
			sourceOffset, err := gs.source.Resume(ourCheckpoint.SourceCheckpoint)
			if err != nil {
				return 0, errors.WithStack(err)
			}

			gc := ourCheckpoint.GzipCheckpoint
			if sourceOffset < gc.Roffset {
				delta := gc.Roffset - sourceOffset
				savior.Debugf(`gzipsource: discarding %d bytes to align source with decompressor`, delta)
				err = savior.DiscardByRead(gs.source, delta)
				if err != nil {
					return 0, errors.WithStack(err)
				}
				sourceOffset += delta
			}

			if sourceOffset == gc.Roffset {
				gs.sr, err = gc.Resume(gs.source)
				if err != nil {
					savior.Debugf(`gzipsource: could not use gzip checkpoint at R=%d`, gc.Roffset)
					// well, let's start over
					_, err = gs.source.Resume(nil)
					if err != nil {
						return 0, errors.WithStack(err)
					}
				} else {
					gs.offset = ourCheckpoint.Offset
					return gs.offset, nil
				}
			} else {
				savior.Debugf(`gzipsource: expected source to resume at %d but got %d`, gc.Roffset, sourceOffset)
			}
		}
	}

	// start from beginning
	sourceOffset, err := gs.source.Resume(nil)
	if err != nil {
		return 0, errors.WithStack(err)
	}

	if sourceOffset != 0 {
		msg := fmt.Sprintf("gzipsource: expected source to resume at start but got %d", sourceOffset)
		return 0, errors.New(msg)
	}

	gs.sr, err = gzip.NewSaverReader(gs.source)
	if err != nil {
		return 0, err
	}

	gs.offset = 0
	return 0, nil
}

func (gs *gzipSource) Read(buf []byte) (int, error) {
	if gs.sr == nil {
		return 0, errors.WithStack(savior.ErrUninitializedSource)
	}

	n, err := gs.sr.Read(buf)
	gs.offset += int64(n)

	if err == flate.ReadyToSaveError {
		err = nil

		if gs.sourceCheckpoint == nil {
			savior.Debugf("gzipsource: can't save, sourceCheckpoint is nil!")
		} else if gs.ssc == nil {
			savior.Debugf("gzipsource: can't save, ssc is nil!")
		} else {
			gzipCheckpoint, saveErr := gs.sr.Save()
			if saveErr != nil {
				return n, saveErr
			}

			savior.Debugf("gzipsource: saving, gzip rOffset = %d, sourceCheckpoint.Offset = %d", gzipCheckpoint.Roffset, gs.sourceCheckpoint.Offset)

			checkpoint := &savior.SourceCheckpoint{
				Offset: gs.offset,
				Data: &GzipSourceCheckpoint{
					Offset:           gs.offset,
					GzipCheckpoint:   gzipCheckpoint,
					SourceCheckpoint: gs.sourceCheckpoint,
				},
			}
			gs.sourceCheckpoint = nil

			err = gs.ssc.Save(checkpoint)
			savior.Debugf("gzipsource: saved checkpoint at byte %d", gs.offset)
		}
	}

	return n, err
}

func (gs *gzipSource) ReadByte() (byte, error) {
	if gs.sr == nil {
		return 0, errors.WithStack(savior.ErrUninitializedSource)
	}

	n, err := gs.Read(gs.bytebuf)
	if n == 0 {
		/* this happens when Read needs to save, but it swallows the error */
		/* we're not meant to surface them, but there's no way to handle a */
		/* short read from ReadByte, so we just read again */
		n, err = gs.Read(gs.bytebuf)
	}

	return gs.bytebuf[0], err
}

func (gs *gzipSource) Progress() float64 {
	// We can't tell how large the uncompressed stream is until we finish
	// decompressing it. The underlying's source progress is a good enough
	// approximation.
	return gs.source.Progress()
}

func (gs *gzipSource) Close() error {
	return gs.sr.Close()
}

func init() {
	gob.Register(&GzipSourceCheckpoint{})
}
