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
	"bytes"
	"fmt"
	"io"
	"os"

	"github.com/mendersoftware/log"
	"github.com/mendersoftware/mender/utils"
	"github.com/pkg/errors"
)

var (
	BlockDeviceGetSizeOf       BlockDeviceGetSizeFunc       = getBlockDeviceSize
	BlockDeviceGetSectorSizeOf BlockDeviceGetSectorSizeFunc = getBlockDeviceSectorSize
)

// BlockDeviceGetSizeFunc is a helper for obtaining the size of a block device.
type BlockDeviceGetSizeFunc func(file *os.File) (uint64, error)

// BlockDeviceGetSectorSizeFunc is a helper for obtaining the sector size of a block device.
type BlockDeviceGetSectorSizeFunc func(file *os.File) (int, error)

type ProgressCallback func(blockStartByte, blockEndByte int64)

// BlockDeviceWriter is a low-level wrapper for a block device. The wrapper implements
// io.Writer and io.Closer interfaces.
// BlockDeviceWriter writes to its underlying BlockDeviceFile using
// medium-sized writes that are aligned with the underlying device's block
// alignment (assumed to be a power of 2).  It periodically executes a Sync()
// operation to ensure the data has been persisted to the underlying storage,
// and then invokes the provided "progress" callback to indicate that a block
// of bytes has been successfully persisted to stable storage. It can also be
// Seek()-ed (as in io.Seeker), allowing data to be written starting "in the
// middle" of the underlying BlockDeviceFile.

type BlockDeviceWriter struct {
	out                BlockDeviceFile
	ImageSize          int64  // image size
	FlushIntervalBytes uint64 // Force a flush to disk each time this many bytes are written
	buf                *bytes.Buffer
	progressCallback   ProgressCallback
}

// BlockDeviceFile is an interface that represents an underlying block device (storage volume).
type BlockDeviceFile interface {
	io.Writer
	io.Closer

	Sync() error
	Seek(offset int64, whence int) (int64, error)

	Filepath() string
	Size() (uint64, error)
	SectorSize() (int, error)
}

type basicDeviceFile struct {
	*os.File
	path string
}

func newBasicDeviceFile(path string) (*basicDeviceFile, error) {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return nil, err
	}

	return &basicDeviceFile{
		File: f,
		path: path,
	}, nil
}

type ubiDeviceFile struct {
	basicDeviceFile
	lw *utils.LimitedWriter
}

func newUBIDeviceFile(path string, ubiImageSize int64) (*ubiDeviceFile, error) {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return nil, err
	}

	// This is UBI-specific stuff here.
	// From <mtd/ubi-user.h>
	//
	// UBI volume update
	// ~~~~~~~~~~~~~~~~~
	//
	// Volume update should be done via the UBI_IOCVOLUP ioctl command of the
	// corresponding UBI volume character device. A pointer to a 64-bit update
	// size should be passed to the ioctl. After this, UBI expects user to write
	// this number of bytes to the volume character device. The update is finished
	// when the claimed number of bytes is passed. So, the volume update sequence
	// is something like:
	//
	// fd = open("/dev/my_volume");
	// ioctl(fd, UBI_IOCVOLUP, &image_size);
	// write(fd, buf, image_size);
	// close(fd);
	err = setUbiUpdateVolume(f, ubiImageSize)
	if err != nil {
		log.Errorf("Failed to write images size to UBI_IOCVOLUP: %v", err)
		return nil, err
	}

	return &ubiDeviceFile{
		basicDeviceFile: basicDeviceFile{
			File: f,
			path: path,
		},
		lw: &utils.LimitedWriter{f, uint64(ubiImageSize)},
	}, nil
}

func (udf *ubiDeviceFile) Write(buf []byte) (int, error) {
	return udf.lw.Write(buf)
}

// Size queries the size of the underlying block device. Automatically opens a
// new fd in O_RDONLY mode, thus can be used in parallel to other operations.
func (bdf *basicDeviceFile) Size() (uint64, error) {
	out, err := os.OpenFile(bdf.Filepath(), os.O_RDONLY, 0)
	if err != nil {
		return 0, err
	}
	defer out.Close()

	return BlockDeviceGetSizeOf(out)
}

// SectorSize queries the logical sector size of the underlying block device. Automatically opens a
// new fd in O_RDONLY mode, thus can be used in parallel to other operations.
func (bdf *basicDeviceFile) SectorSize() (int, error) {
	out, err := os.OpenFile(bdf.Filepath(), os.O_RDONLY, 0)
	if err != nil {
		return 0, err
	}
	defer out.Close()

	return BlockDeviceGetSectorSizeOf(out)
}

func (bdf *basicDeviceFile) Filepath() string {
	return bdf.path
}

func NewBlockDeviceWriter(
	blockDeviceFile BlockDeviceFile,
	imageSize int64,
	chunkSize uint64, // Pass 0 to determine automatically
	progressCallback ProgressCallback,
) (*BlockDeviceWriter, error) {

	bd := &BlockDeviceWriter{
		out:                blockDeviceFile,
		ImageSize:          imageSize,
		FlushIntervalBytes: chunkSize,
		progressCallback:   progressCallback,
	}

	if bd.FlushIntervalBytes < 1024 {
		native_ssz, err := bd.SectorSize()
		if err != nil {
			return nil, err
		}

		chunk_size := uint64(native_ssz)

		// Pick a multiple of the sector size that's around 1 MiB.
		for chunk_size < 1*1024*1024 {
			chunk_size = chunk_size * 2 // avoid doing logarithms...
		}

		bd.FlushIntervalBytes = chunk_size
	}

	return bd, nil
}

func (bd *BlockDeviceWriter) GetCurrentOffset() (int64, error) {
	return bd.Seek(0, io.SeekCurrent) // seek to current position and report offset
}

func (bd *BlockDeviceWriter) Seek(offset int64, whence int) (int64, error) {
	newOffset, err := bd.out.Seek(offset, whence)
	return newOffset, err
}

func writeAll(b []byte, w io.Writer) (int, error) {
	totalWritten := 0
	for totalWritten < len(b) {
		written, err := w.Write(b[totalWritten:])
		totalWritten += written

		if err != nil {
			return totalWritten, err
		}
	}
	return totalWritten, nil
}

// ReadFrom reads data from r and writes to the underlying block device. Behaves like io.ReaderFrom.
func (bd *BlockDeviceWriter) ReadFrom(r io.Reader) (int64, error) {
	totalBytesRead := int64(0)

	currentOutputOffset, err := bd.GetCurrentOffset()
	if err != nil {
		return 0, errors.Wrapf(err, "unable to get current offset into device %s", bd.out.Filepath())
	}

	var remainingOutputBytes uint64
	if currentOutputOffset < bd.ImageSize {
		remainingOutputBytes = uint64(bd.ImageSize - currentOutputOffset)
	}

	if bd.buf == nil {
		bd.buf = bytes.NewBuffer(make([]byte, 0, bd.FlushIntervalBytes))
	}

	chunkSizeBytes := int64(bd.FlushIntervalBytes)

	for remainingOutputBytes > 0 {

		// Note: bd.buf might already contain some bytes here
		nextChunkBoundaryBytes := (currentOutputOffset + chunkSizeBytes) / chunkSizeBytes * chunkSizeBytes

		thisChunkBytes := nextChunkBoundaryBytes - currentOutputOffset

		bytesToRead := thisChunkBytes - int64(bd.buf.Len())

		bytesRead, readErr := io.CopyN(bd.buf, r, bytesToRead)
		totalBytesRead += bytesRead

		shouldFlush := (readErr != nil) || (int64(bd.buf.Len()) == thisChunkBytes)

		if shouldFlush {
			// Time to write, flush, and sync, and report to callback.

			bytesWritten, err := bd.buf.WriteTo(bd.out)
			if err != nil {
				return totalBytesRead, err
			}

			bd.buf.Reset() // after successful write

			// Sync output to stable storage
			err = bd.out.Sync()
			if err != nil {
				return totalBytesRead, err
			}

			//Invoke callback.
			if bd.progressCallback != nil {
				bd.progressCallback(currentOutputOffset, currentOutputOffset+bytesWritten)
			}

			// Update state
			currentOutputOffset += bytesWritten
		}

		if readErr != nil {
			if readErr == io.EOF {
				readErr = nil
			}
			return totalBytesRead, readErr
		}
	}
	return totalBytesRead, nil
}

func (bd *BlockDeviceWriter) CheckFullImageWritten() error {
	if currentOffset, err := bd.GetCurrentOffset(); err != nil || currentOffset != bd.ImageSize {
		if err != nil {
			log.Errorf("failed to get current offset from device")
			return err
		} else {
			msg := fmt.Sprintf("logic error: image size is %d but %d bytes were written to %s",
				bd.ImageSize, currentOffset, bd.out.Filepath())
			log.Error(msg)
			return errors.Errorf("%s", msg)
		}
	}
	return nil
}

// Close closes underlying block device automatically syncing any unwritten
// data. Othewise, behaves like io.Closer.
func (bd *BlockDeviceWriter) Close() error {

	fullimgErr := bd.CheckFullImageWritten()

	if bd.out != nil {
		if err := bd.out.Sync(); err != nil {
			log.Errorf("failed to fsync partition %s: %v", bd.out.Filepath(), err)
			return err
		}
		if err := bd.out.Close(); err != nil {
			log.Errorf("failed to close partition %s: %v", bd.out.Filepath(), err)
		}
		bd.out = nil
	}

	return fullimgErr
}

// Size queries the size of the underlying block device. Automatically opens a
// new fd in O_RDONLY mode, thus can be used in parallel to other operations.
func (bd *BlockDeviceWriter) Size() (uint64, error) {
	return bd.out.Size()
}

// SectorSize queries the logical sector size of the underlying block device. Automatically opens a
// new fd in O_RDONLY mode, thus can be used in parallel to other operations.
func (bd *BlockDeviceWriter) SectorSize() (int, error) {
	return bd.out.SectorSize()
}
