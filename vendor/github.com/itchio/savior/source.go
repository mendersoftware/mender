package savior

import (
	"encoding/gob"
	"io"

	"github.com/pkg/errors"
)

// SourceCheckpoint contains all the information needed for a source
// to resume from a given offset.
type SourceCheckpoint struct {
	// Offset is the position in the stream, in bytes
	// It should be non-zero, as the checkpoint for offset 0 is simply nil
	Offset int64

	// Data is a source-specific pointer to a struct, which must be
	// registered with `gob` so that it can be serialized and deserialized
	Data interface{}
}

var ErrUninitializedSource = errors.New("tried to read from source before Resume() was called")

// A Source represents a data stream that does not provide random access,
// is not seekable, but for which checkpoints can be emitted, allowing
// the consumer to resume reading from the stream later.
//
// Sources typically are either a limited interface for a more powerful
// resource (*os.File, eos.File, both of which provide seeking and random
// access), or a more powerful interface to resources typically exposed
// as simply an `io.Reader` in the golang standard library (flate streams,
// gzip streams, bzip2 streams)
//
// Sources that expose a random access resource tend to be able to `Save()`
// at any given byte, whereas sources that are decompressors are typically
// only able to save on a block boundary.
type Source interface {
	// Resume tries to use a checkpoint to start reading again at the checkpoint.
	// It *must be called* before using the source, whether or not checkpoint is
	// an actual mid-stream checkpoint or just the nil checkpoint (for Offset=0)
	Resume(checkpoint *SourceCheckpoint) (int64, error)

	// Register a source save consumer for this source
	SetSourceSaveConsumer(ssc SourceSaveConsumer)

	// Let the source know that it should emit a checkpoint as soon as it can.
	WantSave()

	// Progress returns how much of the stream has been consumed, in a [0,1] range.
	// If this source does not support progress reporting (ie. the total size of
	// the stream is unknown), then Progress returns a negative value (typically -1).
	Progress() float64

	Features() SourceFeatures

	io.Reader

	// io.ByteReader is embedded in Source so it can be used by the `flate` package
	// without it wrapping it in a `bufio.Reader`
	io.ByteReader
}

type SourceFeatures struct {
	Name          string
	ResumeSupport ResumeSupport
}

// SeekSource is a Source with extra powers: you can know its size,
// tell which offset it's currently at, and ask for a view of a subsection of it.
type SeekSource interface {
	Source

	// Tell returns the current offset of the seeksource
	Tell() int64
	// Size returns the total number of bytes the seeksource reads
	Size() int64
	// Section returns a SeekSource that reads from start to start+size
	// Note that the returned SeekSource will use the same underlying
	// io.ReadSeeker, so the original SeekSource cannot be used anymore.
	// The returned SeekSource should be Resume()'d before being used
	Section(start int64, size int64) (SeekSource, error)
}

// FileSource is a SeekSource that can be closed (to release associated resources)
type FileSource interface {
	SeekSource

	Close() error
}

type SourceSaveConsumer interface {
	// Send a checkpoint to the consumer. The consumer may
	// retain the checkpoint, so its contents must not change
	// after it is sent.
	Save(checkpoint *SourceCheckpoint) error
}

type CallbackSourceSaveConsumer struct {
	OnSave func(checkpoint *SourceCheckpoint) error
}

var _ SourceSaveConsumer = (*CallbackSourceSaveConsumer)(nil)

func (cssc *CallbackSourceSaveConsumer) Save(checkpoint *SourceCheckpoint) error {
	return cssc.OnSave(checkpoint)
}

// DiscardByRead advances a source by `delta` bytes by reading
// data then throwing it away. This is useful in case a source made a checkpoint
// shortly before the offset we actually need to resume from.
func DiscardByRead(source Source, delta int64) error {
	buf := make([]byte, 4096)
	for delta > 0 {
		toRead := delta
		if toRead > int64(len(buf)) {
			toRead = int64(len(buf))
		}

		n, err := source.Read(buf[:toRead])
		if err != nil {
			return errors.WithStack(err)
		}

		delta -= int64(n)
	}

	return nil
}

func init() {
	gob.Register(&SourceCheckpoint{})
}
