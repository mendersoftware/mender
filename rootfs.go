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

import "fmt"
import "net/http"
import "io"
import "os"
import "strings"

const minimumImageSize int64 = 4096

func doRootfs(imageFile string) error {
	var imageFd io.ReadCloser
	var imageSize int64
	var err error

	if strings.HasPrefix(imageFile, "http:") ||
		strings.HasPrefix(imageFile, "https:") {
		// Network based update.
		imageFd, imageSize, err = getHttpStream(imageFile)
		if err != nil {
			return err
		}

	} else {
		// File based update.
		imageFd, imageSize, err = getFileStream(imageFile)
		if err != nil {
			return err
		}
	}
	defer imageFd.Close()

	if err = writeToPartition(imageFd, imageSize); err != nil {
		return err
	}

	if err = enableUpdatedPartition(); err != nil {
		return fmt.Errorf("Unable to activate partition after update: "+
			"%s", err.Error())
	}

	return nil
}

// Returns a byte stream which is a download of the given link, and also returns
// the length of the file being downloaded.
func getHttpStream(link string) (io.ReadCloser, int64, error) {
	// Network based update.
	resp, err := http.Get(link)
	if err != nil {
		return nil, 0, err
	}

	imageSize := resp.ContentLength
	if imageSize < 0 {
		return nil, 0, fmt.Errorf("Image size from '%s' is unknown. "+
			"Will not continue with unknown image size.",
			link)
	} else if imageSize < minimumImageSize {
		return nil, 0, fmt.Errorf("Less than 4KiB image update (%d "+
			"bytes)? Something is wrong, aborting.",
			imageSize)
	}

	return resp.Body, imageSize, nil
}

// Returns a byte stream of the fiven file, and also returns the size of the
// file.
func getFileStream(file string) (io.ReadCloser, int64, error) {
	fd, err := os.Open(file)
	if err != nil {
		return nil, 0, fmt.Errorf("Not able to open image file: %s: %s\n",
			file, err.Error())
	}

	imageInfo, err := fd.Stat()
	if err != nil {
		return nil, 0, fmt.Errorf("Unable to stat() file: %s: %s\n",
			file, err.Error())
	}

	return fd, imageInfo.Size(), nil
}

func writeToPartition(imageFd io.Reader, imageSize int64) error {
	inact, err := getInactivePartition()
	if err != nil {
		return fmt.Errorf("Not able to determine inactive partition: "+
			"%s\n", err.Error())
	}

	part_fd, err := os.OpenFile(inact, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("Not able to open partition: %s: %s\n",
			inact, err.Error())
	}
	defer part_fd.Close()

	// Size check on partition: Don't try to write into a partition which is
	// smaller than the image file.
	var partSizeU uint64
	var partSize int64

	partSizeU, notBlockDevice, err := getBlockDeviceSize(part_fd)
	if notBlockDevice {
		part_info, err := part_fd.Stat()
		if err != nil {
			return fmt.Errorf("Unable to stat() partition: "+
				"%s: %s\n", inact, err.Error())
		}
		partSize = part_info.Size()
	} else if err != nil {
		return fmt.Errorf("Unable to determine size of partition "+
			"%s: %s", inact, err.Error())
	} else {
		partSize = int64(partSizeU)
	}

	if partSize < imageSize {
		return fmt.Errorf("Partition is smaller than the given image " +
			"file.")
	}

	// Write image file into partition.
	buf := make([]byte, 4096)
	for {
		read, read_err := imageFd.Read(buf)

		if read_err != nil && read_err != io.EOF {
			return fmt.Errorf("Error while reading image file: "+
				"%s\n", read_err.Error())
		}

		if read > 0 {
			_, write_err := part_fd.Write(buf[0:read])
			if write_err != nil {
				return fmt.Errorf("Error while writing to "+
					"partition: %s: %s\n",
					inact, write_err.Error())
			}
		}

		if read_err == io.EOF {
			break
		}
	}

	part_fd.Sync()

	return nil
}

func enableUpdatedPartition() error {
	inact, err := getInactivePartitionNumber()
	if err != nil {
		return err
	}

	err = setBootEnv("upgrade_available", "1")
	if err != nil {
		return err
	}
	err = setBootEnv("boot_part", inact)
	if err != nil {
		return err
	}
	// TODO REMOVE?
	err = setBootEnv("bootcount", "0")
	if err != nil {
		return err
	}

	return nil
}

func doCommitRootfs() error {
	err := setBootEnv("upgrade_available", "0")
	if err != nil {
		return err
	}
	// TODO REMOVE?
	err = setBootEnv("bootcount", "0")
	if err != nil {
		return err
	}

	return nil
}
