// Copyright 2021 Northern.tech AS
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
	"os"
	"path/filepath"
	"syscall"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/mendersoftware/mender/system"
	"github.com/mendersoftware/mender/utils"
)

var (
	BlockDeviceGetSizeOf       BlockDeviceGetSizeFunc       = system.GetBlockDeviceSize
	BlockDeviceGetSectorSizeOf BlockDeviceGetSectorSizeFunc = system.GetBlockDeviceSectorSize
)

// BlockDevicer is a file-like interface for the block-device.
type BlockDevicer interface {
	io.Reader
	io.Writer
	io.Closer
	io.Seeker
	Sync() error // Commits previously-written data to stable storage.
}

// BlockDeviceGetSizeFunc is a helper for obtaining the size of a block device.
type BlockDeviceGetSizeFunc func(file *os.File) (uint64, error)

// BlockDeviceGetSectorSizeFunc is a helper for obtaining the sector size of a block device.
type BlockDeviceGetSectorSizeFunc func(file *os.File) (int, error)

// BlockDevice is a low-level wrapper for a block device. The wrapper implements
// the io.Writer and io.Closer interfaces, which is all that is needed by the
// mender-client.
type BlockDevice struct {
	Path string // Device path, ex. /dev/mmcblk0p1
	w    io.WriteCloser
}

type bdevice int

// Give the block-device a package-like interface,
// i.e., blockdevice.Open(partition, size)
var blockdevice bdevice

// Open tries to open the 'device' (/dev/<device> usually), and returns a
// BlockDevice.
func (bd bdevice) Open(device string, size int64) (*BlockDevice, error) {
	log.Infof("Opening device %q for writing", device)

	var out *os.File
	var err error

	log.Debugf("Installing update of size: %d", size)
	if size < 0 {
		return nil, errors.New("Have invalid update. Aborting.")
	}

	// Make sure the file system is not mounted (MEN-2084)
	if mntPt := checkMounted(device); mntPt != "" {
		log.Warnf("Inactive partition %q is mounted at %q. "+
			"This might be caused by some \"auto mount\" service "+
			"(e.g udisks2) that mounts all block devices. It is "+
			"recommended to blacklist the partitions used by "+
			"Mender to avoid any issues.", device, mntPt)
		log.Warnf("Performing umount on %q.", mntPt)
		err = syscall.Unmount(device, 0)
		if err != nil {
			log.Errorf("Error unmounting partition %s",
				device)
			return nil, err
		}
	}

	typeUBI := system.IsUbiBlockDevice(device)

	log.Debugf("Device: %s is a ubi device: %t", device, typeUBI)

	var flag int

	if typeUBI {
		// UBI block devices are not prefixed with /dev due to the fact
		// that the kernel root= argument does not handle UBI block
		// devices which are prefixed with /dev
		//
		// Kernel root= only accepts:
		// - ubi0_0
		// - ubi:rootfsa
		device = filepath.Join("/dev", device)
		flag = os.O_WRONLY
	} else {
		flag = os.O_RDWR
	}

	b := &BlockDevice{
		Path: device,
	}

	if bsz, err := b.Size(); err != nil {
		log.Errorf("Failed to read size of block device %s: %v",
			device, err)
		return nil, err
	} else if bsz < uint64(size) {
		log.Errorf("Update (%v bytes) is larger than the size of device %s (%v bytes)",
			size, device, bsz)
		return nil, syscall.ENOSPC
	}

	nativeSsz, err := b.SectorSize()
	if err != nil {
		log.Errorf("Failed to read sector size of block device %s: %v",
			device, err)
		return nil, err
	}

	// The size of an individual sector tends to be quite small. Rather than
	// doing a zillion small writes, do medium-size-ish writes that are
	// still sector aligned. (Doing too many small writes can put pressure
	// on the DMA subsystem (unless writes are able to be coalesced) by
	// requiring large numbers of scatter-gather descriptors to be
	// allocated.)
	chunkSize := nativeSsz

	// Pick a multiple of the sector size that's around 1 MiB.
	for chunkSize < 1*1024*1024 {
		chunkSize = chunkSize * 2
	}

	log.Infof(
		"Native sector size of block device %s is %v bytes."+
			" Mender will write in chunks of %v bytes",
		device,
		nativeSsz,
		chunkSize,
	)

	log.Debugf("Opening device: %s for writing with flag: %d", device, flag)
	out, err = os.OpenFile(device, flag, 0)
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to open the device: %q", device)
	}

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
	if typeUBI {
		err := system.SetUbiUpdateVolume(out, uint64(size))
		if err != nil {
			log.Errorf("Failed to write images size to UBI_IOCVOLUP: %v", err)
			return nil, err
		}
	}

	var bdw io.WriteCloser
	if !typeUBI {
		//
		// FlushingWriter is needed due to a driver bug in the linux emmc driver
		// OOM errors.
		//
		// Implements 'BlockDevicer' interface, and is hence the owner of the file
		//
		fw := NewFlushingWriter(out, uint64(nativeSsz))

		//
		// bdw owns the block-device.
		// No-one else is allowed to touch it!
		//
		odw := &OptimizedBlockDeviceWriter{
			blockDevice: fw,
		}

		//
		// Buffers writes, and writes to the underlying writer
		// once a buffer of size 'frameSize' is full.
		//
		bdw = &BlockFrameWriter{
			frameSize: chunkSize,
			buf:       bytes.NewBuffer(nil),
			w:         odw,
		}
	} else {
		// No optimized writes possible on UBI (Mirza)
		// All the bytes have to be written
		bdw = out
	}

	//
	// The outermost writer. Makes sure that we never(!) write
	// more than the size of the image.
	//
	b.w = &utils.LimitedWriteCloser{
		W: bdw,
		N: uint64(size),
	}

	return b, nil
}

// Write writes data 'b' to the underlying writer. Although this is just one
// line, the underlying implementation is currently slightly more involved. The
// BlockDevice writer will write to a chain of writers as follows:
//
//                LimitWriter
//       Make sure that no more than image-size
//       bytes are written to the  block-device.
//                   |
//                   |
//                   v
//              BlockFrameWriter
//       Buffers the writes into 'chunkSize' frames
//       for writing to the underlying writer.
//                   |
//                   |
//                   v
//             OptimizedBlockDeviceWriter
//       Only writes dirty frames to the underlying block-device.
//       Note: This is not done for UBI volumes
//                   |
//                   |
//                   v
//               BlockDevicer
//        This is an interface with all the main functionality
//        of a file, and is in this case a FlushingWriter,
//        which writes a chunk to the underlying file-descriptor,
//        and then calls Sync() on every 'FlushIntervalBytes' written.
//
// Due to the underlying writer caching writes, the block-device needs to be
// closed, in order to make sure that all data has been flushed to the device.
func (bd *BlockDevice) Write(b []byte) (n int, err error) {
	if bd.w == nil {
		return 0, errors.New("No device")
	}
	n, err = bd.w.Write(b)
	return n, err
}

// Close closes the underlying block device, thus automatically syncing any
// unwritten data. Othewise, behaves like io.Closer.
func (bd *BlockDevice) Close() error {
	if bd.w == nil {
		return nil
	}
	return bd.w.Close()
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

type BlockFrameWriter struct {
	buf       *bytes.Buffer
	frameSize int
	w         io.WriteCloser
}

// Write buffers the writes into a buffer of size 'frameSize'. Then, when this
// buffer is full, it writes 'frameSize' bytes to the underlying writer.
func (bw *BlockFrameWriter) Write(b []byte) (n int, err error) {

	// Fill the frame buffer first
	n, err = bw.buf.Write(b)
	if err != nil {
		return n, err
	}

	if bw.buf.Len() < bw.frameSize {
		return n, nil // Chunk buffer not full
	}

	nFrames := bw.buf.Len() / bw.frameSize
	for i := 0; i < nFrames; i++ {
		_, err = bw.w.Write(bw.buf.Next(bw.frameSize))
		if err != nil {
			return 0, err
		}
	}

	// Write a full frame, but report only the last byte chunk as written
	return len(b), nil
}

// Close flushes the remaining cached bytes -- if any.
func (bw *BlockFrameWriter) Close() error {
	_, err := bw.w.Write(bw.buf.Bytes())
	if cerr := bw.w.Close(); cerr != nil {
		return cerr
	}
	if err == io.EOF {
		return nil
	}
	return err
}

// OptimizedBlockDeviceWriter wraps an underlying blockDevice write, however,
// with the optimization that it compares the bytes passed in to the Write
// method, with the next len([]byte) bytes (considered a frame) on the block
// device, and if they match, the write is discarded. The lingo is that only
// dirty frames are written. Clean ones are discarded.
type OptimizedBlockDeviceWriter struct {
	blockDevice BlockDevicer
	totalFrames int
	dirtyFrames int
}

// Write only write 'dirty' frames.
// Note: a frame-size is always the size 'len(b)'
func (bd *OptimizedBlockDeviceWriter) Write(b []byte) (n int, err error) {

	frameSize := int64(len(b))
	payloadBuf := make([]byte, frameSize)

	//
	// Read len(b) bytes from the block-device
	//
	n, err = io.ReadFull(bd.blockDevice, payloadBuf)
	if err != nil {
		log.Errorf("Failed to read a full frame of size: %d from the block-device", err)
		return 0, err
	}

	//
	// Write the frame if it is dirty.
	//
	if !bytes.Equal(payloadBuf, b) {
		// In order to write, we need to seek back to
		// the start of the chunk.
		if _, err = bd.blockDevice.Seek(-int64(frameSize), io.SeekCurrent); err != nil {
			log.Errorf("Failed to seek back to the start of the frame. Err: %v", err)
			return 0, err
		}
		bd.totalFrames += 1
		bd.dirtyFrames += 1
		return bd.blockDevice.Write(b)
	}

	// No need to write a clean frame
	bd.totalFrames += 1
	return n, err
}

func (obw *OptimizedBlockDeviceWriter) Close() error {
	s := "The optimized block-device writer wrote a total of %d frames, " +
		"where %d frames did need to be rewritten (i.e., skipped)"
	log.Infof(s, obw.totalFrames, obw.dirtyFrames)
	return obw.blockDevice.Close()
}

// FlushingWriter is a wrapper around a BlockDevice which forces a Sync() to occur
// every FlushIntervalBytes.
type FlushingWriter struct {
	BlockDevicer
	FlushIntervalBytes    uint64
	unflushedBytesWritten uint64
}

// NewFlushingWriter returns a FlushingWriter which wraps the provided
// block-device (BlockDevicer) and automatically flushes (calls Sync()) each
// time the specified number of bytes is written. Setting flushIntervalBytes == 0
// causes Sync() to be called after every Write().
func NewFlushingWriter(wf *os.File, flushIntervalBytes uint64) *FlushingWriter {
	return &FlushingWriter{
		BlockDevicer:          wf,
		FlushIntervalBytes:    flushIntervalBytes,
		unflushedBytesWritten: 0,
	}
}

func (fw *FlushingWriter) Write(p []byte) (int, error) {
	rv, err := fw.BlockDevicer.Write(p)

	fw.unflushedBytesWritten += uint64(rv)

	if err != nil {
		return rv, err
	} else if fw.unflushedBytesWritten >= fw.FlushIntervalBytes {
		err = fw.Sync()
	}

	return rv, err
}

func (fw *FlushingWriter) Sync() error {
	err := fw.BlockDevicer.Sync()
	fw.unflushedBytesWritten = 0
	return err
}
