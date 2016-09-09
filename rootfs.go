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
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mendersoftware/log"
	"github.com/mendersoftware/mender/client"
)

// This will be run manually from command line ONLY
func doRootfs(device UInstaller, args runOptionsType) error {
	var image io.ReadCloser
	var imageSize int64
	var err error
	var upclient client.Updater

	if args == (runOptionsType{}) {
		return errors.New("rootfs called without needed parameters")
	}

	log.Debug("Starting device update.")

	updateLocation := *args.imageFile
	if strings.HasPrefix(updateLocation, "http:") ||
		strings.HasPrefix(updateLocation, "https:") {
		log.Infof("Performing remote update from: [%s].", updateLocation)

		// we are having remote update
		ac, err := client.New(args.Config)
		if err != nil {
			return errors.New("Can not initialize client for performing network update.")
		}
		upclient = client.NewUpdate()

		log.Debug("Client initialized. Start downloading image.")

		image, imageSize, err = upclient.FetchUpdate(ac, updateLocation)
		log.Debugf("Image downloaded: %d [%v] [%v]", imageSize, image, err)
	} else {
		// perform update from local file
		log.Infof("Start updating from local image file: [%s]", updateLocation)
		image, imageSize, err = FetchUpdateFromFile(updateLocation)

		log.Debugf("Feting update from file results: [%v], %d, %v", image, imageSize, err)
	}

	if image != nil {
		defer image.Close()
	}

	if err != nil {
		return errors.New("Error while updateing image from command line: " + err.Error())
	}

	if err = device.InstallUpdate(image, imageSize); err != nil {
		return err
	}
	log.Info("Image correctly installed to inactive partition. " +
		"Marking inactive partition as the new boot candidate.")

	return device.EnableUpdatedPartition()
}

// FetchUpdateFromFile returns a byte stream of the given file, size of the file
// and an error if one occurred.
func FetchUpdateFromFile(file string) (io.ReadCloser, int64, error) {
	fd, err := os.Open(file)
	if err != nil {
		return nil, 0, fmt.Errorf("Not able to open image file: %s: %s\n",
			file, err.Error())
	}

	imageInfo, err := fd.Stat()
	if err != nil {
		return nil, 0, fmt.Errorf("Unable to stat() file: %s: %s\n", file, err.Error())
	}

	return fd, imageInfo.Size(), nil
}
