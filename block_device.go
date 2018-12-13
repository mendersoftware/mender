// Copyright 2018 Northern.tech AS
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
	"io"
	"os"

	"github.com/mendersoftware/log"
	"github.com/mendersoftware/mender/utils"
)

var (
	BlockDeviceGetSizeOf       BlockDeviceGetSizeFunc       = getBlockDeviceSize
	BlockDeviceGetSectorSizeOf BlockDeviceGetSectorSizeFunc = getBlockDeviceSectorSize
)

// BlockDeviceGetSizeFunc is a helper for obtaining the size of a block device.
type BlockDeviceGetSizeFunc func(file *os.File) (uint64, error)

// BlockDeviceGetSectorSizeFunc is a helper for obtaining the sector size of a block device.
type BlockDeviceGetSectorSizeFunc func(file *os.File) (int, error)

// BlockDevice is a low-level wrapper for a block device. The wrapper implements
// io.Writer and io.Closer interfaces.
type BlockDevice struct {
	Path               string               // device path, ex. /dev/mmcblk0p1
	out                *os.File             // os.File for writing
	w                  *utils.LimitedWriter // wrapper for `out` limited the number of bytes written
	typeUBI            bool                 // Set to true if we are updating an UBI volume
	ImageSize          int64                // image size
	FlushIntervalBytes uint64               // Force a flush to disk each time this many bytes are written
}

// A WriteSyncer is an io.Writer that also implements a Sync() function which commits written data to stable storage.
// For instance, an os.File is a WriteSyncer.
type WriteSyncer interface {
	io.Writer
	Sync() error // Commits previously-written data to stable storage.
}

// FlushingWriter is a wrapper around a WriteSyncer which forces a Sync() to occur every so many bytes.
// FlushingWriter implements WriteSyncer.
type FlushingWriter struct {
	WF                    WriteSyncer
	FlushIntervalBytes    uint64
	unflushedBytesWritten uint64
}

// NewFlushingWriter returns a WriteSyncer which wraps the provided WriteSyncer
// and automatically flushes (calls Sync()) each time the specified number of
// bytes is written.
// Setting flushIntervalBytes == 0 causes Sync() to be called after every Write().
func NewFlushingWriter(wf WriteSyncer, flushIntervalBytes uint64) *FlushingWriter {
	return &FlushingWriter{
		WF:                    wf,
		FlushIntervalBytes:    flushIntervalBytes,
		unflushedBytesWritten: 0,
	}
}

func (fw *FlushingWriter) Write(p []byte) (int, error) {
	rv, err := fw.WF.Write(p)

	fw.unflushedBytesWritten += uint64(rv)

	if err != nil {
		return rv, err
	} else if fw.unflushedBytesWritten >= fw.FlushIntervalBytes {
		err = fw.Sync()
	}

	return rv, err
}

func (fw *FlushingWriter) Sync() error {
	err := fw.WF.Sync()
	fw.unflushedBytesWritten = 0
	return err
}

// Write writes data `p` to underlying block device. Will automatically open
// the device in a write mode. Otherwise, behaves like io.Writer.
func (bd *BlockDevice) Write(p []byte) (int, error) {
	if bd.out == nil {
		log.Infof("opening device %s for writing", bd.Path)
		out, err := os.OpenFile(bd.Path, os.O_WRONLY, 0)
		if err != nil {
			return 0, err
		}

		var wrappedOut io.Writer

		wrappedOut = out

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
		if bd.typeUBI {
			err := setUbiUpdateVolume(out, bd.ImageSize)
			if err != nil {
				log.Errorf("Failed to write images size to UBI_IOCVOLUP: %v", err)
				return 0, err
			}
		} else {
			wrappedOut = NewFlushingWriter(out, bd.FlushIntervalBytes)
		}

		size, err := BlockDeviceGetSizeOf(out)
		if err != nil {
			log.Errorf("failed to read block device size: %v", err)
			out.Close()
			return 0, err
		}
		log.Infof("partition %s size: %v", bd.Path, size)

		bd.out = out
		bd.w = &utils.LimitedWriter{
			W: wrappedOut,
			N: size,
		}
	}

	w, err := bd.w.Write(p)
	if err != nil {
		log.Errorf("written %v out of %v bytes to partition %s: %v",
			w, len(p), bd.Path, err)
	}
	return w, err
}

// Close closes underlying block device automatically syncing any unwritten
// data. Othewise, behaves like io.Closer.
func (bd *BlockDevice) Close() error {
	if bd.out != nil {
		if err := bd.out.Sync(); err != nil {
			log.Errorf("failed to fsync partition %s: %v", bd.Path, err)
			return err
		}
		if err := bd.out.Close(); err != nil {
			log.Errorf("failed to close partition %s: %v", bd.Path, err)
		}
		bd.out = nil
		bd.w = nil
	}

	return nil
}

// Size queries the size of the underlying block device. Automatically opens a
// new fd in O_RDONLY mode, thus can be used in parallel to other operations.
func (bd *BlockDevice) Size() (uint64, error) {
	out, err := os.OpenFile(bd.Path, os.O_RDONLY, 0)
	if err != nil {
		return 0, err
	}
	defer out.Close()

	return BlockDeviceGetSizeOf(out)
}

// SectorSize queries the logical sector size of the underlying block device. Automatically opens a
// new fd in O_RDONLY mode, thus can be used in parallel to other operations.
func (bd *BlockDevice) SectorSize() (int, error) {
	out, err := os.OpenFile(bd.Path, os.O_RDONLY, 0)
	if err != nil {
		return 0, err
	}
	defer out.Close()

	return BlockDeviceGetSectorSizeOf(out)
}
