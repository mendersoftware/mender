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
	"bytes"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"

	"github.com/mendersoftware/log"
	"github.com/pkg/errors"
)

const scriptLocation = "/var/lib/mender/scripts"

type Scripts struct {
	tmpStore string
	store    string
}

func NewScriptsInstaller(destination string) *Scripts {
	return &Scripts{
		store: destination,
	}
}

func (s *Scripts) StoreScript(r io.Reader, name string) error {
	// first create a temp directory for storing the scripts
	if s.tmpStore == "" {
		if tmpDir, err := ioutil.TempDir("", "scripts"); err != nil {
			return errors.Errorf(
				"installer: can not create directory for storing scripts: %v", err)
		} else {
			s.tmpStore = tmpDir
		}
	}

	sLocation := filepath.Join(s.tmpStore, name)
	f, err := os.Create(sLocation)
	if err != nil {
		return errors.Wrapf(err,
			"installer: can not create script file: %v", sLocation)
	}
	defer f.Close()

	_, err = io.Copy(f, r)
	if err != nil {
		return errors.Wrapf(err,
			"installer: can not write script file: %v", sLocation)
	}
	return nil
}

func (s Scripts) storeVersion(ver int) error {
	if s.store == "" {
		return nil
	}
	return s.StoreScript(bytes.NewBufferString(strconv.Itoa(ver)), "version")
}

func (s Scripts) CleanUp() {
	os.RemoveAll(s.tmpStore)
}

func (s Scripts) Finalize(ver int) error {
	if s.tmpStore != "" {
		// first make sure we are storing the version of the scripts
		if err := s.storeVersion(ver); err != nil {
			return err
		}
		// we try to make moving operation robust
		// first we are checking if directory where the scripts should be stored
		// exists
		// then we are renaming existing directory adding `-bkp` suffix
		// at the end we are moving the `s.tmpStore` to desired location and we
		// are removing previous scripts directory
		_, err := os.Stat(s.store)
		if err != nil && os.IsNotExist(err) {
			return os.Rename(s.tmpStore, s.store)
		} else if err != nil {
			return err
		}
		scrBkpDir := s.store + "-bkp"
		err = os.Rename(s.store, scrBkpDir)
		if err != nil {
			return err
		}

		err = os.Rename(s.tmpStore, s.store)
		if err != nil {
			return err
		}
		if err := os.RemoveAll(scrBkpDir); err != nil {
			// here we are JUST warning as the whole operation was successful
			// but we didn't manage to remove previous scripts
			// WARNING: this migh cause disk full after significant amount of
			// errors and large scripts
			log.Warnf("installer: can not remove old scripts directory: %s",
				scrBkpDir)
		}

	}
	return nil
}
