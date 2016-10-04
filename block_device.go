// Copyright 2016 Mender Software AS
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
	"os"

	"github.com/mendersoftware/log"
	"github.com/mendersoftware/mender/utils"
)

var (
	BlockDeviceGetSizeOf BlockDeviceGetSizeFunc = getBlockDeviceSize
)

// BlockDeviceGetSizeFunc is a helper for obtaining the size of a block device.
type BlockDeviceGetSizeFunc func(file *os.File) (uint64, error)

// BlockDevice is a low-level wrapper for a block device. The wrapper implements
// io.Writer and io.Closer interfaces.
type BlockDevice struct {
	Path string               // device path, ex. /dev/mmcblk0p1
	out  *os.File             // os.File for writing
	w    *utils.LimitedWriter // wrapper for `out` limited the number of bytes written
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

		size, err := BlockDeviceGetSizeOf(out)
		if err != nil {
			log.Errorf("failed to read block device size: %v", err)
			out.Close()
			return 0, err
		}
		log.Infof("partition %s size: %v", bd.Path, size)

		bd.out = out
		bd.w = &utils.LimitedWriter{out, size}
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
