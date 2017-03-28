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

package installer

import (
	"io"
	"io/ioutil"

	"github.com/mendersoftware/log"
	"github.com/mendersoftware/mender-artifact/areader"
	"github.com/mendersoftware/mender-artifact/handlers"
	"github.com/pkg/errors"
)

type UInstaller interface {
	InstallUpdate(io.ReadCloser, int64) error
	EnableUpdatedPartition() error
}

func Install(art io.ReadCloser, dt string, device UInstaller) error {

	rootfs := handlers.NewRootfsInstaller()
	rootfs.InstallHandler = func(r io.Reader, df *handlers.DataFile) error {
		log.Debugf("installing update %v of size %v", df.Name, df.Size)
		err := device.InstallUpdate(ioutil.NopCloser(r), df.Size)
		if err != nil {
			log.Errorf("update image installation failed: %v", err)
			return err
		}
		return nil
	}

	ar := areader.NewReader(art)
	if err := ar.RegisterHandler(rootfs); err != nil {
		return errors.Wrap(err, "failed to register install handler")
	}
	ar.CompatibleDevicesCallback = func(devices []string) error {
		log.Debugf("checking if device [%s] is on compatibile device list: %v\n",
			dt, devices)
		for _, dev := range devices {
			if dev == dt {
				return nil
			}
		}
		return errors.New("installer: image not compatible with device")
	}
	if err := ar.ReadArtifact(); err != nil {
		return errors.Wrap(err, "failed to read and install update")
	}

	return nil
}
