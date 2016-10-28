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

	"github.com/mendersoftware/artifacts/parser"
	"github.com/mendersoftware/artifacts/reader"
	"github.com/mendersoftware/log"
	"github.com/pkg/errors"
)

type UInstaller interface {
	InstallUpdate(io.ReadCloser, int64) error
	EnableUpdatedPartition() error
}

func InstallRootfs(device UInstaller, dt string) parser.DataHandlerFunc {
	return func(r io.Reader, dev string, uf parser.UpdateFile) error {
		if dev != dt {
			return errors.Errorf("unexpected device type [%v], expected to see [%v]",
				dev, dt)
		}
		log.Infof("installing update %v of size %v", uf.Name, uf.Size)
		err := device.InstallUpdate(ioutil.NopCloser(r), uf.Size)
		if err != nil {
			log.Errorf("update image installation failed: %v", err)
			return err
		}
		return device.EnableUpdatedPartition()
	}
}

func Install(artifact io.ReadCloser, dt string, device UInstaller) error {
	rp := parser.RootfsParser{
		DataFunc: InstallRootfs(device, dt),
	}

	ar := areader.NewReader(artifact)
	defer ar.Close()

	ar.Register(&rp)

	_, err := ar.Read()
	if err != nil {
		return errors.Wrapf(err, "failed to read and install update")
	}

	return nil
}
