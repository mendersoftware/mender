package savior

import (
	"encoding/gob"
	"fmt"

	"github.com/itchio/httpkit/progress"
	"github.com/itchio/wharf/state"
)

type ExtractorCheckpoint struct {
	SourceCheckpoint *SourceCheckpoint
	EntryIndex       int64
	Entry            *Entry
	Progress         float64
	Data             interface{}
}

type ExtractorResult struct {
	Entries []*Entry
}

// Returns a human-readable summary of the files, directories and
// symbolic links in this result.
func (er *ExtractorResult) Stats() string {
	var numFiles, numDirs, numSymlinks int
	var totalBytes int64
	for _, entry := range er.Entries {
		switch entry.Kind {
		case EntryKindFile:
			numFiles++
		case EntryKindDir:
			numDirs++
		case EntryKindSymlink:
			numSymlinks++
		}
		totalBytes += entry.UncompressedSize
	}

	return fmt.Sprintf("%s (in %d files, %d dirs, %d symlinks)", progress.FormatBytes(totalBytes), numFiles, numDirs, numSymlinks)
}

// Returns the total size of all listed entries, in bytes
func (er *ExtractorResult) Size() int64 {
	var totalBytes int64
	for _, entry := range er.Entries {
		totalBytes += entry.UncompressedSize
	}

	return totalBytes
}

type ExtractorFeatures struct {
	// Short name for the extractor, like "zip", or "tar"
	Name string
	// Level of support for resumable decompression, if any
	ResumeSupport ResumeSupport
	// Is pre-allocating files supported?
	Preallocate bool
	// Is random access supported?
	RandomAccess bool
	// Features for the underlying source
	SourceFeatures *SourceFeatures
}

func (ef ExtractorFeatures) String() string {
	res := fmt.Sprintf("%s: resume=%s", ef.Name, ef.ResumeSupport)
	if ef.Preallocate {
		res += " +preallocate"
	}

	if ef.RandomAccess {
		res += " +randomaccess"
	}

	if ef.SourceFeatures != nil {
		res += fmt.Sprintf(" (via source %s: resume=%s)", ef.SourceFeatures.Name, ef.SourceFeatures.ResumeSupport)
	}

	return res
}

type ResumeSupport int

const (
	// While the extractor exposes Save/Resume, in practice, resuming
	// will probably waste I/O and processing redoing a lot of work
	// that was already done, so it's not recommended to run it against
	// a networked resource
	ResumeSupportNone ResumeSupport = 0
	// The extractor can save/resume between each entry, but not in the middle of an entry
	ResumeSupportEntry ResumeSupport = 1
	// The extractor can save/resume within an entry, on a deflate/bzip2 block boundary for example
	ResumeSupportBlock ResumeSupport = 2
)

func (rs ResumeSupport) String() string {
	switch rs {
	case ResumeSupportNone:
		return "none"
	case ResumeSupportEntry:
		return "entry"
	case ResumeSupportBlock:
		return "block"
	default:
		return "unknown resume support"
	}
}

type AfterSaveAction int

const (
	// Continue decompressing after the checkpoint has been emitted
	AfterSaveContinue AfterSaveAction = 1
	// Stop decompression after the checkpoint has been emitted (returns ErrStop)
	AfterSaveStop AfterSaveAction = 2
)

type SaveConsumer interface {
	// Returns true if a checkpoint should be emitted. `copiedBytes` is the
	// amount of bytes extracted since the last time ShouldSave was called.
	ShouldSave(copiedBytes int64) bool
	// Should persist a checkpoint and return instructions on whether to continue
	// or stop decompression.
	Save(checkpoint *ExtractorCheckpoint) (AfterSaveAction, error)
}

// Returns a *state.Consumer that prints nothing at all.
func NopConsumer() *state.Consumer {
	return &state.Consumer{
		OnMessage:        func(lvl string, msg string) {},
		OnProgressLabel:  func(label string) {},
		OnPauseProgress:  func() {},
		OnResumeProgress: func() {},
		OnProgress:       func(progress float64) {},
	}
}

// An extractor is able to decompress entries of an archive format
// (like .zip, .tar, .7z, etc.), preferably in a resumable fashion.
type Extractor interface {
	// Set save consumer for determining checkpoint frequency and persisting them.
	SetSaveConsumer(saveConsumer SaveConsumer)
	// Set *state.Consumer for logging
	SetConsumer(consumer *state.Consumer)
	// Perform extraction, optionally resuming from a checkpoint (if non-nil)
	// Sink is not closed, it should be closed by the caller, see simple_extract
	// for an example.
	Resume(checkpoint *ExtractorCheckpoint, sink Sink) (*ExtractorResult, error)
	// Returns the supported features for this extractor
	Features() ExtractorFeatures
}

func init() {
	gob.Register(&ExtractorCheckpoint{})
}

type nopSaveConsumer struct{}

var _ SaveConsumer = (*nopSaveConsumer)(nil)

// Returns a SaveConsumer that never asks for a checkpoint, ignores
// any emitted checkpoints, and always tells the extractor to continue
// decompressing.
func NopSaveConsumer() SaveConsumer {
	return &nopSaveConsumer{}
}

func (nsc *nopSaveConsumer) ShouldSave(n int64) bool {
	return false
}

func (nsc *nopSaveConsumer) Save(checkpoint *ExtractorCheckpoint) (AfterSaveAction, error) {
	return AfterSaveContinue, nil
}
