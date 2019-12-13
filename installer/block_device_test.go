// Copyright 2020 Northern.tech AS
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
	"bytes"
	"io"
	"io/ioutil"
	"os"
	"path"
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBlockDeviceFail(t *testing.T) {
	bd := BlockDevice{Path: "/dev/somefile"}

	// closing unopened device should not fail
	err := bd.Close()
	assert.NoError(t, err)

	w, err := bd.Write([]byte("foo"))
	assert.Equal(t, 0, w)
	assert.Error(t, err)

	err = bd.Close()
	assert.NoError(t, err)
}

func makeBlockDeviceSize(t *testing.T, sz uint64, err error, name string) BlockDeviceGetSizeFunc {
	return func(file *os.File) (uint64, error) {
		t.Logf("block device size called: %v", file)
		if assert.NotNil(t, file) {
			assert.Equal(t, name, file.Name())
		}
		return sz, err
	}
}

func makeBlockDeviceSectorSize(t *testing.T, sz uint64, err error, name string) BlockDeviceGetSectorSizeFunc {
	return func(file *os.File) (int, error) {
		t.Logf("block device sector-size called: %v", file)
		if assert.NotNil(t, file) {
			assert.Equal(t, name, file.Name())
		}
		return int(sz), err
	}
}

func createFile(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return nil
}

func TestBlockDeviceWrite(t *testing.T) {
	td, err := ioutil.TempDir("", "mender-block-device-")
	assert.NoError(t, err)
	defer os.RemoveAll(td)

	// prepare fake block device file
	bdpath := path.Join(td, "foo")

	// Write some junk to the file
	err = ioutil.WriteFile(bdpath, []byte("abxdrz1234"), 0644)
	require.Nil(t, err)

	// temporarily override helper for getting block device size
	old := BlockDeviceGetSizeOf

	// pretend the device is only 10 bytes in size
	BlockDeviceGetSizeOf = makeBlockDeviceSize(t, 10, nil, bdpath)

	oldSectorSize := BlockDeviceGetSectorSizeOf
	BlockDeviceGetSectorSizeOf = makeBlockDeviceSectorSize(t, 5, nil, bdpath)

	// test simple write
	bd, err := blockdevice.Open(bdpath, 10)
	require.NoError(t, err, "Failed to open the blockdevice: %q", bdpath)

	// Ensure that a standard write of < 10 bytes succeeds
	payloadBuf := bytes.NewBuffer([]byte("foobar"))
	expectedBytesWritten := payloadBuf.Len()
	n, err := io.Copy(bd, payloadBuf)
	assert.NoError(t, err, "Failed to write the payload to the block-device")
	assert.Equal(t, expectedBytesWritten, int(n))

	// Close the device, in order to flush the last writes
	assert.NoError(t, bd.Close())

	// // Compare the actual data written
	actualData, err := ioutil.ReadFile(bdpath)
	assert.NoError(t, err, "Failed to read all the data from the fake block-device")

	assert.True(t, bytes.Equal(actualData, []byte("foobar1234")), string(actualData))

	BlockDeviceGetSizeOf = old
	BlockDeviceGetSectorSizeOf = oldSectorSize
}

func TestBlockDeviceSize(t *testing.T) {
	td, err := ioutil.TempDir("", "mender-block-device-")
	assert.NoError(t, err)
	defer os.RemoveAll(td)

	// prepare fake block device file
	bdpath := path.Join(td, "foo")
	err = createFile(bdpath)
	assert.NoError(t, err)

	// temporarily override helper for getting block device size
	old := BlockDeviceGetSizeOf

	// pretend the device is only 10 bytes in size
	BlockDeviceGetSizeOf = makeBlockDeviceSize(t, 10, nil, bdpath)

	bd := BlockDevice{Path: bdpath}
	sz, err := bd.Size()
	assert.Equal(t, uint64(10), sz)
	assert.NoError(t, err)

	BlockDeviceGetSizeOf = makeBlockDeviceSize(t, 10, errors.New("failed"), bdpath)

	bd = BlockDevice{Path: bdpath}
	_, err = bd.Size()
	assert.EqualError(t, err, "failed")

	BlockDeviceGetSizeOf = old
}

type testWriter struct {
	*bytes.Buffer
}

// NopCloser
func (t *testWriter) Close() error {
	return nil
}

func TestBlockFrameWriter(t *testing.T) {

	tests := map[string]struct {
		frameSize            int
		input                []byte
		expected             []byte
		expectedBytesWritten int
		expectedBytesCached  int
	}{
		"write one full frame": {
			frameSize:            2,
			input:                []byte("fo"),
			expected:             []byte("fo"),
			expectedBytesWritten: 2,
			expectedBytesCached:  0,
		},

		"half a frame - No underlying writes": {
			frameSize:            6,
			input:                []byte("foo"),
			expected:             []byte(nil),
			expectedBytesWritten: 3,
			expectedBytesCached:  3,
		},

		"one and a half frame - only one full underlying write": {
			frameSize:            4,
			input:                []byte("foobar"),
			expected:             []byte("foob"),
			expectedBytesWritten: 6,
			expectedBytesCached:  2,
		},
	}

	for _, test := range tests {

		buf := bytes.NewBuffer(nil)
		b := BlockFrameWriter{
			buf:       bytes.NewBuffer(nil),
			frameSize: test.frameSize,
			w:         &testWriter{buf},
		}

		n, err := b.Write(test.input)
		assert.NoError(t, err)

		// Verify that the bytes written to the underlying writer matches
		assert.Equal(t, n, test.expectedBytesWritten)

		// Verify that the bytes cached internally matches
		assert.Equal(t, b.buf.Len(), test.expectedBytesCached, "%s", string(b.buf.Bytes()))

		assert.EqualValues(t, buf.Bytes(), test.expected, "Written and expected output do not match")
	}
}

// Implements Blockdevicer
type TestBlockDevice struct {
	*bytes.Buffer
	synced bool
}

func (td *TestBlockDevice) Close() error { return nil }

func (td *TestBlockDevice) Seek(offset int64, whence int) (int64, error) { return offset, nil }

func (td *TestBlockDevice) Sync() error { td.synced = true; return nil }

func TestFlushingWriter(t *testing.T) {

	tests := map[string]struct {
		input                    []byte
		expectedOutput           []byte
		flushInterval            uint64
		expectedNrUnflushedBytes uint64
	}{
		"Write less than the flush-limit": {
			flushInterval:            7,
			input:                    []byte("foobar"),
			expectedOutput:           []byte("foobar"),
			expectedNrUnflushedBytes: 6,
		},

		"Write the limit exactly": {
			flushInterval:            6,
			input:                    []byte("foobar"),
			expectedOutput:           []byte("foobar"),
			expectedNrUnflushedBytes: 0,
		},

		"Write more than the limit": {
			flushInterval:            5,
			input:                    []byte("foobar"),
			expectedOutput:           []byte("foobar"),
			expectedNrUnflushedBytes: 0,
		},
	}

	for _, test := range tests {
		buf := bytes.NewBuffer(nil)
		td := &TestBlockDevice{buf, false}
		w := FlushingWriter{
			BlockDevicer:       td,
			FlushIntervalBytes: test.flushInterval,
		}

		n, err := w.Write(test.input)

		assert.NoError(t, err)

		assert.Equal(t, n, len(test.input))

		assert.Equal(t, test.expectedNrUnflushedBytes, w.unflushedBytesWritten)

		if test.expectedNrUnflushedBytes == 0 {
			assert.True(t, td.synced)
		}
		assert.Equal(t, test.input, td.Bytes())
	}
}
