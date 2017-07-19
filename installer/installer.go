// Copyright 2017 Northern.tech AS
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
	"github.com/mendersoftware/mender-artifact/artifact"
	"github.com/mendersoftware/mender-artifact/handlers"
	"github.com/pkg/errors"
)

type UInstaller interface {
	InstallUpdate(io.ReadCloser, int64) error
	EnableUpdatedPartition() error
}

func Install(art io.ReadCloser, dt string, key []byte, device UInstaller) error {

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

	var ar *areader.Reader
	// if there is a verification key artifact must be signed
	if key != nil {
		ar = areader.NewReaderSigned(art)
	} else {
		ar = areader.NewReader(art)
	}

	if err := ar.RegisterHandler(rootfs); err != nil {
		return errors.Wrap(err, "failed to register install handler")
	}

	ar.CompatibleDevicesCallback = func(devices []string) error {
		log.Debugf("checking if device [%s] is on compatibile device list: %v\n",
			dt, devices)
		if dt == "" {
			log.Errorf("Unknown device_type. Continuing with update")
			return nil
		}
		for _, dev := range devices {
			if dev == dt {
				return nil
			}
		}
		return errors.Errorf("installer: image (device types %v) not compatible with device %v",
			devices, dt)
	}

	// VerifySignatureCallback needs to be registered both for
	// NewReader and NewReaderSigned to print a warning if artifact is signed
	// but no verification key is provided.
	ar.VerifySignatureCallback = func(message, sig []byte) error {
		// MEN-1196 skip verification of the signature if there is no key
		// provided. This means signed artifact will be installed on all
		// devices having no key specified.
		if key == nil {
			log.Warn("installer: installing signed artifact without verification " +
				"as verification key is missing")
			return nil
		}

		// Do the verification only if the key is provided.
		s := artifact.NewVerifier(key)
		return s.Verify(message, sig)
	}

	// read the artifact
	if err := ar.ReadArtifact(); err != nil {
		return errors.Wrap(err, "installer: failed to read and install update")
	}

	log.Debugf(
		"installer: successfully read artifact [name: %v; version: %v; compatible devices: %v]",
		ar.GetArtifactName(), ar.GetInfo().Version, ar.GetCompatibleDevices())

	return nil
}
