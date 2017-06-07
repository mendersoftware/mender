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

package statescript

import (
	"bytes"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"

	"github.com/pkg/errors"
)

type Store struct {
	location string
}

func NewStore(destination string) *Store {
	return &Store{
		location: destination,
	}
}

func (s *Store) Clear() error {
	if s.location == "" {
		return nil
	}
	err := os.RemoveAll(s.location)
	if err == nil || (err != nil && os.IsNotExist(err)) {
		return os.MkdirAll(s.location, 0755)
	}
	return err
}

func (s *Store) StoreScript(r io.Reader, name string) error {
	sLocation := filepath.Join(s.location, name)
	f, err := os.OpenFile(sLocation, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return errors.Wrapf(err,
			"statescript: can not create script file: %v", sLocation)
	}
	defer f.Close()

	_, err = io.Copy(f, r)
	if err != nil {
		return errors.Wrapf(err,
			"statescript: can not write script file: %v", sLocation)
	}
	return nil
}

func (s Store) storeVersion(ver int) error {
	return s.StoreScript(bytes.NewBufferString(strconv.Itoa(ver)), "version")
}

func readVersion(name string) (int, error) {
	data, err := ioutil.ReadFile(name)
	if err != nil {
		return 0, err
	}

	return strconv.Atoi(string(data))
}

func (s Store) Finalize(ver int) error {
	if s.location == "" {
		return nil
	}

	// make sure we are storing the version of the scripts
	if err := s.storeVersion(ver); err != nil {
		return err
	}

	return nil
}
