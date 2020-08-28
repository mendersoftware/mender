// Copyright 2020 Northern.tech AS
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
package store

import (
	"io"
	"io/ioutil"
	"os"
	"path"

	log "github.com/sirupsen/logrus"
)

type DirStore struct {
	basepath string
}

type DirFile struct {
	io.WriteCloser
	name     string
	dirstore *DirStore
}

func NewDirStore(path string) *DirStore {
	return &DirStore{
		basepath: path,
	}
}

func (d DirStore) Close() error {
	// nop
	return nil
}

func (d DirStore) ReadAll(name string) ([]byte, error) {
	in, err := d.OpenRead(name)
	if err != nil {
		return nil, err
	}
	defer in.Close()
	return ioutil.ReadAll(in)
}

func (d DirStore) WriteAll(name string, data []byte) error {
	out, err := d.OpenWrite(name)
	if err != nil {
		return err
	}
	// we should not call/defer out.Close() here as we'll be committing the file
	// later on, although it's most of the time ok to rename(3) a file while it's
	// opened, don't assume that it works always

	_, err = out.Write(data)
	if err != nil {
		out.Close()
		return err
	}

	// this could return an error in theory
	out.Close()

	return out.Commit()
}

// Open an entry for reading.
func (d DirStore) OpenRead(name string) (io.ReadCloser, error) {
	var p string
	if path.IsAbs(name) {
		p = name
	} else {
		p = path.Join(d.basepath, name)
	}
	f, err := os.Open(p)
	if err != nil {
		log.Debugf("I/O read error for entry %v: %v", name, err)
		return nil, err
	}
	return f, nil
}

// Open an entry for writing. Under the hood, opens a temporary file (with
// 'name~' name) using os.O_WRONLY|os.O_CREAT flags, with default mode 0600.
// Once writing to temp file is done, the caller should run Commit() method of
// the WriteCloserCommitter interface.
func (d DirStore) OpenWrite(name string) (WriteCloserCommitter, error) {
	f, err := os.OpenFile(d.getTempPath(name), os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		log.Errorf("I/O write error for entry %v: %v", name, err)
		return nil, err
	}

	wrc := &DirFile{
		WriteCloser: f,
		name:        name,
		dirstore:    &d,
	}
	return wrc, nil
}

// Return an actual path in DirStore
func (d DirStore) getPath(name string) string {
	return path.Join(d.basepath, name)
}

// Return a temporary path in DirStore
func (d DirStore) getTempPath(name string) string {
	return d.getPath(name) + "~"
}

// Commit a file from temporary copy to the actual name. Under the hood, does a
// os.Rename() from a temp file (one with ~ suffix) to the actual name
func (d DirStore) CommitFile(name string) error {
	from := d.getTempPath(name)
	to := d.getPath(name)

	err := os.Rename(from, to)
	if err != nil {
		log.Errorf("I/O commit error for entry %v: %v", name, err)
	}
	return err
}

func (df DirFile) Commit() error {
	return df.dirstore.CommitFile(df.name)
}

func (d DirStore) Remove(name string) error {
	return os.Remove(d.getPath(name))
}

func (d *DirStore) WriteTransaction(txnFunc func(txn Transaction) error) error {
	return NoTransactionSupport
}
func (d *DirStore) ReadTransaction(txnFunc func(txn Transaction) error) error {
	return NoTransactionSupport
}
