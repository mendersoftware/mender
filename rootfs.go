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
import "io"
import "os"

func doRootfs(imageFile string) error {
	inact, err := getInactivePartition()
	if err != nil {
		return fmt.Errorf("Not able to determine inactive partition: "+
			"%s\n", err.Error())
	}

	image_fd, err := os.Open(imageFile)
	if err != nil {
		return fmt.Errorf("Not able to open image file: %s: %s\n",
			imageFile, err.Error())
	}
	defer image_fd.Close()

	part_fd, err := os.OpenFile(inact, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("Not able to open partition: %s: %s\n",
			inact, err.Error())
	}
	defer part_fd.Close()

	// Size check on partition: Don't try to write into a partition which is
	// smaller than the image file.
	image_info, err := image_fd.Stat()
	if err != nil {
		return fmt.Errorf("Unable to stat() file: %s: %s\n",
			imageFile, err.Error())
	}

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

	if partSize < image_info.Size() {
		return fmt.Errorf("Partition is smaller than the given image " +
			"file.")
	}

	// Write image file into partition.
	buf := make([]byte, 4096)
	for {
		read, read_err := image_fd.Read(buf)

		if read_err != nil && read_err != io.EOF {
			return fmt.Errorf("Error while reading image file: "+
				"%s: %s\n", imageFile, read_err.Error())
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

	err = enableUpdatedPartition()
	if err != nil {
		return fmt.Errorf("Unable to activate partition after update: "+
			"%s", err.Error())
	}

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
