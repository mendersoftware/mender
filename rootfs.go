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
	"io"
	"strings"

	"github.com/mendersoftware/log"
)

// This will be run manually from command line ONLY
func doRootfs(device UInstaller, args runOptionsType) error {
	var image io.ReadCloser
	var imageSize int64
	var err error
	var client Updater

	if args == (runOptionsType{}) {
		return errors.New("rootfs called without needed parameters")
	}

	updateLocation := *args.imageFile
	if strings.HasPrefix(updateLocation, "http:") ||
		strings.HasPrefix(updateLocation, "https:") {
		// we are having remote update
		client, err = NewUpdater(args.httpsClientConfig)

		if err != nil {
			return errors.New("Can not initialize client for performing network update.")
		}

		image, imageSize, err = client.FetchUpdate(updateLocation)
		log.Debugf("Image downloaded: %d [%v] [%v]", imageSize, image, err)
	} else {
		// perform update from local file
		image, imageSize, err = FetchUpdateFromFile(updateLocation)
	}

	if image != nil {
		defer image.Close()
	}

	if err != nil {
		return err
	}

	if err = device.InstallUpdate(image, imageSize); err != nil {
		return err
	}
	return device.EnableUpdatedPartition()
}
