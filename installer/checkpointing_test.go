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
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"log"
	"os"

	//"errors"
	"io"
	"io/ioutil"
	"strings"
	"testing"

	"github.com/pkg/errors"

	"github.com/itchio/savior"
	"github.com/itchio/savior/gzipsource"
	"github.com/itchio/savior/seeksource"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func generateTarfile() []byte {
	buf := &bytes.Buffer{}
	tw := tar.NewWriter(buf)

	addFile := func(name, content string) {
		err := tw.WriteHeader(
			&tar.Header{
				Name: name,
				Size: int64(len(content)),
			})
		if err != nil {
			panic(err)
		}

		io.Copy(
			tw,
			strings.NewReader(content),
		)
	}

	addFile(
		"file1",
		"one\ntwo\nthree\nfour\nfive\nsix",
	)

	addFile("file2", "file two content")

	tw.Flush()
	return buf.Bytes()
}

func TestTarFileSourceBasic(t *testing.T) {
	tar_bytes := generateTarfile()

	seeksrc := seeksource.FromBytes(tar_bytes)
	_, err := seeksrc.Resume(nil)
	require.NoError(t, err)

	tfs, err := NewTarFileSource(seeksrc)
	if err != nil {
		t.Fatalf("couldn't make TarFileSource")
	}

	_, err = tfs.Resume(nil)
	if err != nil {
		t.Fatalf("expected Resume() to succeed, got: %v", err)
	}

	hdr, err := tfs.Next()
	if err != nil {
		t.Fatalf("expected Next() to succeed, got: %v", err)
	}
	if hdr.Name != "file1" {
		t.Errorf("expected first file to be named file1, not %v", hdr.Name)
	}
	data, err := ioutil.ReadAll(tfs)
	if err != nil && err != io.EOF {
		t.Fatalf("unexpected error when reading: %v", err)
	}
	if !bytes.Equal(data, []byte("one\ntwo\nthree\nfour\nfive\nsix")) {
		t.Error("incorrect file1 data")
	}

	hdr, err = tfs.Next()
	if err != nil {
		t.Fatalf("expected Next() to succeed, got: %v", err)
	}
	if hdr.Name != "file2" {
		t.Errorf("expected second file to be named file2, not %v", hdr.Name)
	}
	data, err = ioutil.ReadAll(tfs)
	if err != nil && err != io.EOF {
		t.Fatalf("unexpected error when reading: %v", err)
	}
	if !bytes.Equal(data, []byte("file two content")) {
		t.Error("incorrect file1 data")
	}

	_, err = tfs.Next()

	if err == nil {
		t.Fatalf("expected Next() to fail after 2 files")
	}
}

func TestTarFileSourceBasicCheckpoint(t *testing.T) {
	tar_bytes := generateTarfile()

	var ckpt *savior.SourceCheckpoint

	seeksrc := seeksource.FromBytes(tar_bytes)
	_, err := seeksrc.Resume(nil)
	require.NoError(t, err)

	tfs, err := NewTarFileSource(seeksrc)
	if err != nil {
		t.Fatalf("couldn't make TarFileSource")
	}

	tfs.SetSourceSaveConsumer(
		&savior.CallbackSourceSaveConsumer{
			OnSave: func(checkpoint *savior.SourceCheckpoint) error {
				ckpt = checkpoint
				return nil
			},
		})

	_, err = tfs.Resume(nil)
	if err != nil {
		t.Fatalf("expected Resume() to succeed, got: %v", err)
	}

	hdr, err := tfs.Next()
	if err != nil {
		t.Fatalf("expected Next() to succeed, got: %v", err)
	}
	if hdr.Name != "file1" {
		t.Errorf("expected first file to be named file1, not %v", hdr.Name)
	}

	partial_buf := make([]byte, 6)
	_, err = io.ReadFull(tfs, partial_buf) // read 6 bytes (total 6)

	if err != nil {
		t.Fatalf("expected reading 6 bytes to succeed, got: %v", err)
	}

	if !bytes.Equal(partial_buf, []byte("one\ntw")) {
		t.Error("incorrect file1 data")
	}

	tfs.WantSave()

	_, err = io.ReadFull(tfs, partial_buf) // read 6 bytes (total 12)

	if err != nil {
		t.Fatalf("expected reading 6 bytes (second time) to succeed, got: %v", err)
	}

	if !bytes.Equal(partial_buf, []byte("o\nthre")) {
		t.Error("incorrect file1 data")
	}

	if ckpt == nil {
		t.Fatalf("no checkpoint was emitted")
	}

	if hdr = tfs.GetCurrentHeader(); hdr != nil {
		if hdr.Name != "file1" {
			t.Errorf("incorrect name from GetCurrentHeader: expected: file1 got %v", hdr.Name)
		}
	} else {
		t.Errorf("GetCurrentHeader() returned nil")
	}

	if offset := tfs.GetCurrentOffset(); offset != 12 {
		t.Errorf("GetCurrentOffset() returned %v, expected 12", offset)
	}

	// Reset to checkpoint.  Create a new TarFileSource

	tfs, err = NewTarFileSource(seeksource.FromBytes(tar_bytes))
	if err != nil {
		t.Fatalf("couldn't make TarFileSource")
	}

	tfs.SetSourceSaveConsumer(
		&savior.CallbackSourceSaveConsumer{
			OnSave: func(checkpoint *savior.SourceCheckpoint) error {
				ckpt = checkpoint
				return nil
			},
		})

	_, err = tfs.Resume(ckpt)
	if err != nil {
		t.Fatalf("expected Resume(ckpt) to succeed, got: %v", err)
	}

	if hdr = tfs.GetCurrentHeader(); hdr != nil {
		if hdr.Name != "file1" {
			t.Errorf("incorrect name from GetCurrentHeader: expected: file1 got %v", hdr.Name)
		}
	} else {
		t.Errorf("GetCurrentHeader() returned nil")
	}

	if offset := tfs.GetCurrentOffset(); offset != 6 {
		t.Errorf("GetCurrentOffset() returned %v, expected 6", offset)
	}

	for i, _ := range partial_buf {
		partial_buf[i] = 0
	}
	if !bytes.Equal(partial_buf, []byte{0, 0, 0, 0, 0, 0}) {
		t.Error("I thought I just cleared that buffer...")
	}

	_, err = io.ReadFull(tfs, partial_buf) // read 6 bytes (total 12)

	if err != nil {
		t.Fatalf("expected reading 6 bytes (second time) to succeed, got: %v", err)
	}

	if !bytes.Equal(partial_buf, []byte("o\nthre")) {
		t.Error("incorrect file1 data")
	}
}

type testTarFileContent struct {
	name    string
	content string
}
type testTarFiles []testTarFileContent

func addFilesToTar(tfiles testTarFiles, tw *tar.Writer) error {
	for _, tfile := range tfiles {
		err := addFileToTar(tfile, tw)
		if err != nil {
			return err
		}
	}
	return nil
}

func addFileToTar(tfile testTarFileContent, tw *tar.Writer) error {

	err := tw.WriteHeader(
		&tar.Header{
			Name: tfile.name,
			Size: int64(len(tfile.content)),
		})
	if err != nil {
		return errors.Wrapf(err, "Failed to add header for file %v", tfile.name)
	}

	len_written, err := io.Copy(
		tw,
		strings.NewReader(tfile.content),
	)

	if err != nil {
		return errors.Wrapf(err, "Failed to add content for file %v", tfile.name)
	} else if len_written != int64(len(tfile.content)) {
		return errors.Wrapf(err, "Expected %v bytes to be written but only wrote %v", len(tfile.content), len_written)
	}

	return nil
}

var OUTER_TAR_FILES = testTarFiles{
	{"file1", "1111111111111"},
	{"file2", "2222222222222"},
	{"file3", "3333333333333"},
}

var INNER_IMAGE_CONTENT = func() string {
	bldr := strings.Builder{}
	for i := 0; i < 100000; i++ {
		bldr.WriteString(
			fmt.Sprintf("%d", i),
		)
	}
	return bldr.String()
}()

func buildExampleTarfile(w io.Writer, t *testing.T) {
	tw1 := tar.NewWriter(w)

	bbuf := &bytes.Buffer{}
	gz := gzip.NewWriter(bbuf)
	tw2 := tar.NewWriter(gz)

	assert.Nil(t, addFilesToTar(OUTER_TAR_FILES, tw1))

	assert.Nil(t,
		addFilesToTar(
			testTarFiles{
				{"mender_repack12345", INNER_IMAGE_CONTENT},
			},
			tw2,
		),
	)
	assert.Nil(t, tw2.Flush())
	assert.Nil(t, gz.Close())

	gzipped_buf_len := int64(bbuf.Len())
	t.Logf("gzipped_buf_len: %v", gzipped_buf_len)

	//assert.Nil(t, ioutil.WriteFile("/tmp/gzipfile", bbuf.Bytes(), 0444))

	tw1.WriteHeader(
		&tar.Header{
			Name: "data/0000/testfile",
			Size: gzipped_buf_len,
		},
	)

	bytes_written, err := io.Copy(tw1, bbuf)
	assert.Nil(t, err)
	assert.Equal(t, bytes_written, gzipped_buf_len)

	assert.Nil(t, tw1.Flush())
}

type ReadSeekerLogger struct {
	prefix string
	rs     io.ReadSeeker
}

func (rsl *ReadSeekerLogger) Read(b []byte) (int, error) {
	max_readlen := len(b)
	n, err := rsl.rs.Read(b)
	log.Printf("%s: Read(max_readlen:%d) -> (%d, %v)", rsl.prefix, max_readlen, n, err)
	return n, err
}

func (rsl *ReadSeekerLogger) Seek(offset int64, whence int) (int64, error) {
	n, err := rsl.rs.Seek(offset, whence)
	log.Printf("%s: Seek(offset: %d, whence: %d) -> (%d, %v)", rsl.prefix, offset, whence, n, err)
	return n, err
}

func NewReadSeekerLogger(prefix string, rs io.ReadSeeker) *ReadSeekerLogger {
	return &ReadSeekerLogger{prefix, rs}
}

func TestTarFileSourceBasicCheckpoint2(t *testing.T) {
	artifact_buf := &bytes.Buffer{}
	buildExampleTarfile(artifact_buf, t)
	artifact_bytes := artifact_buf.Bytes()

	if false { // debugging
		f, err := os.Create("/tmp/testartifact.tar")
		assert.Nil(t, err)
		_, err = artifact_buf.WriteTo(f)
		assert.Nil(t, err)
		err = f.Close()
		assert.Nil(t, err)
	}

	seeksrc := seeksource.NewWithSize(
		NewReadSeekerLogger("art", bytes.NewReader(artifact_bytes)),
		int64(len(artifact_bytes)),
	)
	_, err := seeksrc.Resume(nil)
	require.NoError(t, err)

	outer_tfs, err := NewTarFileSource(seeksrc)
	require.Nil(t, err, "Couldn't make TarFileSource")

	_, err = outer_tfs.Resume(nil)
	require.Nil(t, err, "Couldn't Resume(nil) TarFileSource")

	var outerTarHeader *tar.Header

	for { // Move forward to the "data file"
		var err error
		outerTarHeader, err = outer_tfs.Next()
		require.Nil(t, err)
		require.NotNil(t, outerTarHeader)

		t.Logf("filename: %v", outerTarHeader.Name)

		if strings.HasPrefix(outerTarHeader.Name, "data/") {
			break
		}
	}
	require.NotNil(t, outerTarHeader)
	require.Equal(t, outerTarHeader.Name, "data/0000/testfile")

	gzr := gzipsource.New(outer_tfs)
	_, err = gzr.Resume(nil)
	require.NoError(t, err)

	inner_tfs, err := NewTarFileSource(gzr)
	require.Nil(t, err, "Couldn't create inner TarFileSource")

	_, err = inner_tfs.Resume(nil)
	require.Nil(t, err, "Couldn't Resume(nil) inner TarFileSource")

	innerTarHeader, err := inner_tfs.Next()
	assert.Nil(t, err, "Couldn't read header from inner TarFileSource")

	assert.Equal(t, innerTarHeader.Name, "mender_repack12345")

	data, err := ioutil.ReadAll(inner_tfs)

	assert.Equal(t, string(data), INNER_IMAGE_CONTENT, "inner image content mismatch")

	assert.NoError(t, gzr.Close())
}

func TestTarFileSourceBasicCheckpointMulti(t *testing.T) {
	artifact_buf := &bytes.Buffer{}
	buildExampleTarfile(artifact_buf, t)
	artifact_bytes := artifact_buf.Bytes()

	if false { // debugging
		f, err := os.Create("/tmp/testartifact.tar")
		assert.Nil(t, err)
		_, err = artifact_buf.WriteTo(f)
		assert.Nil(t, err)
		err = f.Close()
		assert.Nil(t, err)
	}

	for _, params := range []struct{ read_increment, save_after int }{{1, 3}, {5, 10}, {20, 20}} {

		func(READ_INCREMENT, SAVE_AFTER int) {

			blockDevice := make([]byte, len(INNER_IMAGE_CONTENT)) // pretend this is the state of the block device
			var savedCheckpoint *savior.SourceCheckpoint

			num_checkpoints_obtained := 0

			is_first_time := true

			for ; ; is_first_time = false {
				// We execute this block twice -- the first time we try to read up to READ_INCREMENT from the data file, then on the second time we try to read starting at the checkpoint.

				seeksrc := seeksource.NewWithSize(
					NewReadSeekerLogger("art", bytes.NewReader(artifact_bytes)),
					int64(len(artifact_bytes)),
				)
				_, err := seeksrc.Resume(nil)
				require.NoError(t, err)

				outer_tfs, err := NewTarFileSource(seeksrc)
				require.NoError(t, err, "Couldn't make TarFileSource")

				_, err = outer_tfs.Resume(nil)
				require.NoError(t, err, "Couldn't Resume(nil) TarFileSource")

				var outerTarHeader *tar.Header

				for { // Move forward to the "data file"
					var err error
					outerTarHeader, err = outer_tfs.Next()
					require.Nil(t, err)
					require.NotNil(t, outerTarHeader)

					if strings.HasPrefix(outerTarHeader.Name, "data/") {
						break
					}
				}
				require.NotNil(t, outerTarHeader)
				require.Equal(t, outerTarHeader.Name, "data/0000/testfile")

				gzr := gzipsource.New(outer_tfs)
				_, err = gzr.Resume(nil)
				require.NoError(t, err)
				inner_tfs, err := NewTarFileSource(gzr)
				require.Nil(t, err, "Couldn't create inner TarFileSource")

				inner_tfs.SetSourceSaveConsumer(
					&savior.CallbackSourceSaveConsumer{
						OnSave: func(checkpoint *savior.SourceCheckpoint) error {
							savedCheckpoint = checkpoint
							return nil
						},
					})

				if is_first_time {

					require.Nil(t, savedCheckpoint, "savedCheckpoint should be Nil")
					_, err = inner_tfs.Resume(nil)
					require.Nil(t, err, "Couldn't Resume(nil) inner TarFileSource")

					innerTarHeader, err := inner_tfs.Next()
					assert.Nil(t, err, "Couldn't read header from inner TarFileSource")
					assert.Equal(t, innerTarHeader.Name, "mender_repack12345")

				} else {

					require.NotNil(t, savedCheckpoint, "savedCheckpoint should be non-Nil")
					_, err = inner_tfs.Resume(savedCheckpoint)
					require.Nil(t, err, "Couldn't Resume(non-nil) inner TarFileSource")

					savedCheckpoint = nil
				}

				innerTarHeader := inner_tfs.GetCurrentHeader()
				assert.Nil(t, err, "Couldn't read header from inner TarFileSource")
				assert.Equal(t, innerTarHeader.Name, "mender_repack12345")

				offset := inner_tfs.GetCurrentOffset()
				if !is_first_time {
					fmt.Printf(" for (read_incr: %d, save_after: %d): resuming from offset %d\n", READ_INCREMENT, SAVE_AFTER, offset)
					require.NotEqual(t, 0, offset, "can't be resuming from offset zero on subsequent iteration!")

					// Pretend we never wrote anything after this offset:
					for i := int(offset); i < len(blockDevice); i++ {
						blockDevice[i] = 0x42
					}
				}

				save_when_offset_greater_than := offset + int64(SAVE_AFTER)

				readBuf := make([]byte, READ_INCREMENT)

				for { // copy loop
					offset := inner_tfs.GetCurrentOffset()
					bytes_read, err := inner_tfs.Read(readBuf)

					assert.Equal(t, offset+int64(bytes_read), inner_tfs.GetCurrentOffset(), "GetCurrentOffset didn't increment correctly")

					//fmt.Printf("  for (read_incr: %d, save_after: %d): read bytes %d thru %d\n", READ_INCREMENT, SAVE_AFTER, offset, offset+int64(bytes_read))

					if bytes_read > 0 {
						copy(blockDevice[offset:], readBuf[:bytes_read])
					}
					if err == io.EOF {
						// assert we got the correct full data here.
						assert.Equal(t, string(blockDevice), INNER_IMAGE_CONTENT, "inner image content mismatch (read_incr: %d, save_after: %d)", READ_INCREMENT, SAVE_AFTER)

						assert.True(t, num_checkpoints_obtained > 2, "Test failed to exercise checkpointing logic.")
						fmt.Printf("SUBTEST PASSED: for (read_incr: %d, save_after: %d): used %d checkpoints\n", READ_INCREMENT, SAVE_AFTER, num_checkpoints_obtained)

						return // test passes.
					} else {
						if offset > save_when_offset_greater_than {
							inner_tfs.WantSave()
						}
					}

					// Check to see whether we got a checkpoint yet, if so, loop around.
					if savedCheckpoint != nil {
						num_checkpoints_obtained += 1
						fmt.Printf("got a checkpoint after writing %d\n", offset+int64(bytes_read))
						break
					}
				}

				// we fell out of the loop above -- must be because we got a checkpoint
				// verify that the checkpoint can be marshalled
				require.NotNil(t, savedCheckpoint, "broke out of copy loop without a checkpoint!")

				if true {
					marshalled_checkpoint_bytes, err := CheckpointToGob(savedCheckpoint)
					require.Nil(t, err, "gob marshalling failed")

					newCheckpoint, err := GobToCheckpoint(marshalled_checkpoint_bytes)
					require.Nil(t, err, "gob unmarshalling failed")

					require.NotZero(t, newCheckpoint, "unmarshalled checkpoint is zero-value")
					savedCheckpoint = newCheckpoint
				}

			}

			// The only way to pass the test is to hit the EOF condition above.
			t.Fatalf("Fell off end of loop...")

		}(params.read_increment, params.save_after)

	}

}
