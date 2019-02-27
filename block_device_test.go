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
package main

import (
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBlockDeviceFail(t *testing.T) {
	_, err := newBasicDeviceFile("/dev/somefile")
	assert.Error(t, err)
}

type MethodType int

const Method_Write = 1
const Method_Sync = 2
const Method_Seek = 3
const Method_Close = 4
const Method_ProgressCallback = 5

type Call struct {
	methodType MethodType
	writeData  []byte // valid if methodType = Method_Write

	seekOffset int64 // valid of methodType = Method_Sync
	seekWhence int   // valid of methodType = Method_Sync

	progressBlockStartByte, progressBlockEndByte int64
}
type CallRecorder struct {
	Calls      []Call
	byteOffset int64
}

func (cr *CallRecorder) Write(b []byte) (int, error) {
	cr.Calls = append(cr.Calls, Call{
		methodType: Method_Write,
		writeData:  defensiveCopy(b),
	})
	cr.byteOffset += int64(len(b))
	return len(b), nil
}

func defensiveCopy(p []byte) []byte {
	rv := make([]byte, len(p))
	copy(rv, p)
	return rv
}

func (cr *CallRecorder) Sync() error {
	cr.Calls = append(cr.Calls, Call{
		methodType: Method_Sync,
	})
	return nil
}

func (cr *CallRecorder) Seek(offset int64, whence int) (int64, error) {
	cr.Calls = append(cr.Calls, Call{
		methodType: Method_Seek,
		seekOffset: offset,
		seekWhence: whence,
	})

	if whence == io.SeekStart {
		cr.byteOffset = offset
	} else if whence == io.SeekCurrent {
		cr.byteOffset += offset
	} else {
		panic("not implemented")
	}

	return cr.byteOffset, nil
}

func (cr *CallRecorder) Close() error {
	cr.Calls = append(cr.Calls, Call{
		methodType: Method_Close,
	})
	return nil
}

type FakeBlockDeviceFile struct {
	*CallRecorder
	filepath   string
	size       uint64
	sectorsize int
}

func (fb *FakeBlockDeviceFile) Filepath() string {
	return fb.filepath
}

func (fb *FakeBlockDeviceFile) Size() (uint64, error) {
	return fb.size, nil
}

func (fb *FakeBlockDeviceFile) SectorSize() (int, error) {
	return fb.sectorsize, nil
}

type JunkReader struct {
	val byte
}

func (jr JunkReader) Read(b []byte) (int, error) {
	for idx := range b {
		b[idx] = jr.val
	}
	return len(b), nil
}

func TestBlockDeviceWriter(t *testing.T) {
	type args struct {
		initialOffset        int64
		imageSize            int64
		flushSize            uint64
		additionalImageBytes int64 // added to the total bytes returned by the Reader, making the image longer or shorter than it "should" be
	}
	tests := []args{
		{
			initialOffset: 0,
			imageSize:     1*1024*1024 + 17,
			flushSize:     4096,
		},
		{
			initialOffset: 1000,
			imageSize:     1*1024*1024 + 17,
			flushSize:     4096,
		},
		{
			initialOffset: 10000,
			imageSize:     2*1024*1024 + 17,
			flushSize:     16 * 1024,
		},
		{
			initialOffset:        10000,
			imageSize:            2*1024*1024 + 17,
			flushSize:            16 * 1024,
			additionalImageBytes: 10, // should trigger an error
		},
		{
			initialOffset:        10000,
			imageSize:            2*1024*1024 + 17,
			flushSize:            16 * 1024,
			additionalImageBytes: -10, // should trigger an error
		},
	}

	for testIdx, tt := range tests {
		t.Run(fmt.Sprintf("subtest%02d", testIdx), func(t *testing.T) {
			DoTestNewBlockDeviceWriter(t, tt.initialOffset, tt.imageSize, tt.flushSize, tt.additionalImageBytes)
		})
	}
}

func DoTestNewBlockDeviceWriter(t *testing.T, INITIAL_OFFSET int64, IMAGE_SIZE int64, FLUSH_SIZE uint64, ADDITIONAL_IMAGE_BYTES int64) {

	cr := &CallRecorder{}
	fbd := &FakeBlockDeviceFile{
		CallRecorder: cr,
		filepath:     "/dev/fakep1",
		size:         100 * 1024 * 1024,
		sectorsize:   512,
	}

	progressCallback := func(blockStartByte, blockEndByte int64) {
		cr.Calls = append(cr.Calls, Call{
			methodType:             Method_ProgressCallback,
			progressBlockStartByte: blockStartByte,
			progressBlockEndByte:   blockEndByte,
		})
	}

	bdw, err := NewBlockDeviceWriter(
		fbd,
		IMAGE_SIZE,
		FLUSH_SIZE,
		progressCallback,
	)

	_, err = bdw.Seek(INITIAL_OFFSET, io.SeekStart)
	require.NoError(t, err)

	reader := io.LimitReader(JunkReader{val: 42}, IMAGE_SIZE-INITIAL_OFFSET+ADDITIONAL_IMAGE_BYTES)
	bytes_read, err := bdw.ReadFrom(reader)

	require.NoError(t, err)

	if ADDITIONAL_IMAGE_BYTES != 0 {
		// we expect an error in this case
		require.Error(t, bdw.CheckFullImageWritten())
		require.Error(t, bdw.Close())
	} else {
		// we expect no error in this case
		require.Equal(t, bytes_read, IMAGE_SIZE-INITIAL_OFFSET, "didn't read whole image")
		require.NoError(t, bdw.CheckFullImageWritten())
		require.NoError(t, bdw.Close())
	}

	// Check a bunch of invariants
	totalBytesSeenOnBlockDevice := int64(0)
	totalWrites := 0
	totalSyncs := 0
	totalProgressCallbacks := 0
	bytesSinceLastSync := 0

	allWriteSizes := []int{}
	lastSyncedOffset := int64(0)

	for idx, call := range cr.Calls {
		switch call.methodType {
		case Method_Write:
			writeLen := len(call.writeData)
			allWriteSizes = append(allWriteSizes, writeLen)
			totalWrites += 1
			totalBytesSeenOnBlockDevice += int64(writeLen)
			bytesSinceLastSync += writeLen
		case Method_Sync:
			assert.True(t, uint64(bytesSinceLastSync) <= FLUSH_SIZE,
				"should sync at least once every %d bytes, but it's been %d bytes",
				FLUSH_SIZE, bytesSinceLastSync)
			lastSyncedOffset = INITIAL_OFFSET + totalBytesSeenOnBlockDevice
			bytesSinceLastSync = 0
			totalSyncs += 1
		case Method_Seek:
			didAnything := !(call.seekWhence == io.SeekCurrent && call.seekOffset == 0)
			if didAnything && idx != 0 {
				t.Errorf("Seek() must be first call (idx 0, not %d)", idx)
			}
		case Method_Close:
			if idx != len(cr.Calls)-1 {
				t.Errorf("Close() must be last call (seen at idx %d, of %d)", idx, len(cr.Calls)-1)
			}
		case Method_ProgressCallback:
			totalProgressCallbacks += 1
			if call.progressBlockEndByte > lastSyncedOffset {
				t.Errorf("progress callback invoked too early: blockEndBytes is %d but lastSyncedOffset is %d",
					call.progressBlockEndByte, lastSyncedOffset,
				)
			}
		}
	}

	for idx, writeSize := range allWriteSizes[1 : len(allWriteSizes)-1] {
		assert.Equal(t, uint64(writeSize), FLUSH_SIZE, "only the first and last chunks can be incomplete, but idx %d is %d", idx+1, writeSize)
	}
	assert.True(t, uint64(allWriteSizes[0]) <= FLUSH_SIZE, "all write sizes must be at most FLUSH_SIZE (%d), but first was %d", FLUSH_SIZE, allWriteSizes[0])
	lastWriteSize := uint64(allWriteSizes[len(allWriteSizes)-1])
	assert.True(t, lastWriteSize <= FLUSH_SIZE, "all write sizes must be at most FLUSH_SIZE (%d), but last was %d", FLUSH_SIZE, lastWriteSize)

	assert.True(t, totalSyncs > 0, "Sync() was never called")
	assert.True(t, totalProgressCallbacks > 0, "progress callback was never called")

	assert.Equal(t, IMAGE_SIZE-INITIAL_OFFSET+ADDITIONAL_IMAGE_BYTES, totalBytesSeenOnBlockDevice, "bytes written mismatch")
}
