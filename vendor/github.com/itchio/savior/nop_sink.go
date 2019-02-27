package savior

import (
	"path/filepath"

	"github.com/itchio/wharf/state"
)

// NopSink does not write anything anywhere
type NopSink struct {
	Directory string
	Consumer  *state.Consumer

	writer *nopEntryWriter
}

var _ Sink = (*NopSink)(nil)

func (ns *NopSink) destPath(entry *Entry) string {
	return filepath.Join(ns.Directory, filepath.FromSlash(entry.CanonicalPath))
}

func (ns *NopSink) Mkdir(entry *Entry) error {
	return nil
}

func (ns *NopSink) GetWriter(entry *Entry) (EntryWriter, error) {
	return NewNopEntryWriter(), nil
}

func (ns *NopSink) Preallocate(entry *Entry) error {
	return nil
}

func (ns *NopSink) Symlink(entry *Entry, linkname string) error {
	return nil
}

func (ns *NopSink) Nuke() error {
	return nil
}

func (ns *NopSink) Close() error {
	return nil
}

type nopEntryWriter struct{}

var _ EntryWriter = (*nopEntryWriter)(nil)

func NewNopEntryWriter() EntryWriter {
	return &nopEntryWriter{}
}

func (ew *nopEntryWriter) Write(buf []byte) (int, error) {
	return len(buf), nil
}

func (ew *nopEntryWriter) Close() error {
	return nil
}

func (ew *nopEntryWriter) Sync() error {
	return nil
}
