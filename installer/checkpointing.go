// Copyright 2019 Northern.tech AS
//
//    Licensed under the Apache License, Version 2.0 (the "License");
//    you may not use this file except in compliance with the License.
//    You may obtain a copy of the License at
//
//        http://www.apache.org/licenses/LICENSE-2.0
//
//    Unless required by applicable law or agreed to in writing, software
//    distributed under the License is distributed on an "AS IS" BASIS,
//    WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//    See the License for the specific language governing permissions and
//    limitations under the License.

package installer

import (
	gotar "archive/tar"
	"bytes"
	"encoding/gob"

	arktar "github.com/itchio/arkive/tar"
	"github.com/itchio/savior"
	"github.com/pkg/errors"
)

type TarFileSourceSaveState struct {
	SourceCheckpoint *savior.SourceCheckpoint
	ArkCheckpoint    *arktar.Checkpoint

	GoTarHeader *gotar.Header
}

// TarFileSource is a tar file reader with a tar.Reader-like interface, that
// complies with the savior.Source interface.
type TarFileSource struct {
	// input
	source savior.Source

	// internal
	ark            *arktar.SaverReader
	current_gohdr  *gotar.Header
	current_offset int64 // offset within the current file (if current_gohdr != nil)

	ssc savior.SourceSaveConsumer
	//source_checkpoint_in_waiting *savior.SourceCheckpoint
}

// NewTarFileSource creates a new TarFileSource from the given savior.Source
func NewTarFileSource(source savior.Source) (*TarFileSource, error) {

	ark, err := arktar.NewSaverReader(source)

	if err != nil {
		return nil, errors.Wrapf(err, "Failed to create arkive tar reader.")
	}

	tfs := &TarFileSource{
		source: source,
		ark:    &ark,
	}

	source.SetSourceSaveConsumer(
		&savior.CallbackSourceSaveConsumer{
			OnSave: tfs.handleSourceSaveFromUpstream,
		})

	return tfs, nil
}

// Read is required to implement io.Reader
func (tfs *TarFileSource) Read(r []byte) (int, error) {
	if tfs.ark == nil {
		return 0, savior.ErrUninitializedSource
	}

	bytesRead, err := (*(tfs.ark)).Read(r)

	tfs.current_offset += int64(bytesRead)

	return bytesRead, err
}

// Next is required to look like tar.Reader
func (tfs *TarFileSource) Next() (*gotar.Header, error) {
	if tfs.ark == nil {
		return nil, savior.ErrUninitializedSource
	}

	ark_hdr, err := (*(tfs.ark)).Next()

	if err != nil {
		return nil, err
	}

	go_hdr := &gotar.Header{
		Typeflag:   ark_hdr.Typeflag,
		Name:       ark_hdr.Name,
		Linkname:   ark_hdr.Linkname,
		Size:       ark_hdr.Size,
		Mode:       ark_hdr.Mode,
		Uid:        ark_hdr.Uid,
		Gid:        ark_hdr.Gid,
		Uname:      ark_hdr.Uname,
		Gname:      ark_hdr.Gname,
		ModTime:    ark_hdr.ModTime,
		AccessTime: ark_hdr.AccessTime,
		ChangeTime: ark_hdr.ChangeTime,
		Devmajor:   ark_hdr.Devmajor,
		Devminor:   ark_hdr.Devminor,
		Xattrs:     ark_hdr.Xattrs,
	}

	tfs.current_gohdr = go_hdr
	tfs.current_offset = 0

	return go_hdr, nil
}

// GetCurrentHeader is a convenience function that returns a the current
// gotar.Header.  Please do not modify the returned object.
func (tfs *TarFileSource) GetCurrentHeader() *gotar.Header {
	return tfs.current_gohdr
}

// GetCurrentOffset returns the current offset into the current tar file.
func (tfs *TarFileSource) GetCurrentOffset() int64 {
	return tfs.current_offset
}

// ReadByte is required to implement io.ByteReader
func (tfs *TarFileSource) ReadByte() (byte, error) {
	if tfs.ark == nil {
		return 0, savior.ErrUninitializedSource
	}

	one_byte_buf := make([]byte, 1)

	bytes_read, err := (*(tfs.ark)).Read(one_byte_buf)
	if bytes_read == 1 {
		tfs.current_offset += 1
	}

	return one_byte_buf[0], err
}

func (tfs *TarFileSource) Resume(checkpoint *savior.SourceCheckpoint) (int64, error) {

	if checkpoint != nil {
		my_checkpoint, ok := checkpoint.Data.(*TarFileSourceSaveState)

		if !ok {
			return 0, errors.New("Couldn't retrieve TarFileSourceSaveState")
		}

		// This will get used below.
		sourceCheckpoint := my_checkpoint.SourceCheckpoint

		_, src_err := tfs.source.Resume(sourceCheckpoint) // sourceCheckpoint might be nil

		if src_err != nil {
			return 0, errors.Wrap(src_err, "Failed to Resume upstream source")
		}

		newArk, ark_err := my_checkpoint.ArkCheckpoint.Resume(tfs.source)

		if ark_err != nil {
			return 0, errors.Wrap(ark_err, "Failed to Resume arkive tar")
		}

		tfs.ark = &newArk
		tfs.current_gohdr = my_checkpoint.GoTarHeader
		tfs.current_offset = checkpoint.Offset

	}
	// else: if checkpoint is nil, leave everything as-is, don't modify upstream source, which should have already had Resume() called on it at least once.

	return tfs.current_offset, nil
}

func (tfs *TarFileSource) handleSourceSaveFromUpstream(upstreamCheckpoint *savior.SourceCheckpoint) error {
	if tfs.ssc != nil {

		arkCheckpoint, err := (*(tfs.ark)).Save()
		if err != nil {
			return errors.Wrapf(err, "failed to checkpoint arkive tar")
		}

		// Generate my own checkpoint, combining these two
		my_checkpoint := &TarFileSourceSaveState{
			SourceCheckpoint: upstreamCheckpoint,
			ArkCheckpoint:    arkCheckpoint,
			GoTarHeader:      tfs.current_gohdr,
		}

		general_checkpoint := &savior.SourceCheckpoint{
			Offset: tfs.current_offset,
			Data:   my_checkpoint,
		}

		return tfs.ssc.Save(general_checkpoint)
	}
	return errors.New("No SourceSaveConsumer specified")
}

func (tfs *TarFileSource) SetSourceSaveConsumer(ssc savior.SourceSaveConsumer) {
	tfs.ssc = ssc
}

func (tfs *TarFileSource) WantSave() {
	tfs.source.WantSave()
}

func (tfs *TarFileSource) Progress() float64 {
	return -1 // TODO: maybe implement this later
}

func (tfs *TarFileSource) Features() savior.SourceFeatures {
	upstreamFeatures := tfs.source.Features()

	var res_support savior.ResumeSupport
	switch upstreamFeatures.ResumeSupport {
	case savior.ResumeSupportBlock:
		res_support = savior.ResumeSupportBlock
	case savior.ResumeSupportEntry:
		res_support = savior.ResumeSupportEntry
	default:
		res_support = savior.ResumeSupportNone
	}

	return savior.SourceFeatures{
		Name:          "tarish",
		ResumeSupport: res_support,
	}
}

// Magic Go GOB support
func init() {
	gob.Register(&TarFileSourceSaveState{})
}

func CheckpointToGob(ckpt *savior.SourceCheckpoint) ([]byte, error) {
	buf := &bytes.Buffer{}
	enc := gob.NewEncoder(buf)

	err := enc.Encode(*ckpt)
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func GobToCheckpoint(blob []byte) (*savior.SourceCheckpoint, error) {
	dec := gob.NewDecoder(bytes.NewBuffer(blob))

	var ckpt savior.SourceCheckpoint

	err := dec.Decode(&ckpt)
	if err != nil {
		return nil, err
	}

	return &ckpt, err
}
