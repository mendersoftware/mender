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

package handlers

import (
	"archive/tar"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/mendersoftware/mender-artifact/artifact"
	"github.com/pkg/errors"
)

// DataFile represents the minimum set of attributes each update file
// must contain. Some of those might be empty though for specific update types.
type DataFile struct {
	// name of the update file
	Name string
	// size of the update file
	Size int64
	// last modification time
	Date time.Time
	// checksum of the update file
	Checksum []byte
}

type Composer interface {
	GetUpdateFiles() [](*DataFile)
	GetType() string
	ComposeHeader(tw *tar.Writer, no int) error
	ComposeData(tw *tar.Writer, no int) error
}

type Installer interface {
	GetUpdateFiles() [](*DataFile)
	GetType() string
	ReadHeader(r io.Reader, path string) error
	Install(r io.Reader, info *os.FileInfo) error
	Copy() Installer
}

func parseFiles(r io.Reader) (*artifact.Files, error) {
	files := new(artifact.Files)
	if _, err := io.Copy(files, r); err != nil {
		return nil, errors.Wrap(err, "update: error reading files")
	}
	if err := files.Validate(); err != nil {
		return nil, err
	}
	return files, nil
}

func match(pattern, name string) bool {
	match, err := filepath.Match(pattern, name)
	if err != nil {
		return false
	}
	return match
}

func writeFiles(tw *tar.Writer, updFiles []string, dir string) error {
	files := new(artifact.Files)
	for _, u := range updFiles {
		files.FileList = append(files.FileList, u)
	}

	sa := artifact.NewTarWriterStream(tw)
	if err := sa.Write(artifact.ToStream(files),
		filepath.Join(dir, "files")); err != nil {
		return errors.Wrapf(err, "writer: can not tar files")
	}
	return nil
}

func writeTypeInfo(tw *tar.Writer, updateType string, dir string) error {
	tInfo := artifact.TypeInfo{Type: updateType}
	info, err := json.Marshal(&tInfo)
	if err != nil {
		return errors.Wrapf(err, "update: can not create type-info")
	}

	w := artifact.NewTarWriterStream(tw)
	if err := w.Write(info, filepath.Join(dir, "type-info")); err != nil {
		return errors.Wrapf(err, "update: can not tar type-info")
	}
	return nil
}

func writeChecksums(tw *tar.Writer, files [](*DataFile), dir string) error {
	for _, f := range files {
		w := artifact.NewTarWriterStream(tw)
		if err := w.Write(f.Checksum,
			filepath.Join(dir, filepath.Base(f.Name)+".sha256sum")); err != nil {
			return errors.Wrapf(err, "update: can not tar checksum for %v", f)
		}
	}
	return nil
}
