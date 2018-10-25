// Copyright 2018 Northern.tech AS
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

type ComposeHeaderArgs struct {
	TarWriter  *tar.Writer
	No         int
	Version    int
	Augmented  bool
	TypeInfoV3 *artifact.TypeInfoV3
}

type Composer interface {
	GetUpdateFiles() [](*DataFile)
	GetType() string
	ComposeHeader(args *ComposeHeaderArgs) error
	ComposeData(tw *tar.Writer, no int) error
}

type Installer interface {
	GetUpdateFiles() [](*DataFile)
	GetType() string
	ReadHeader(r io.Reader, path string, version int) error
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
	stream, err := artifact.ToStream(files)
	if err != nil {
		return errors.Wrap(err, "writeFiles: ")
	}
	if err := sa.Write(stream,
		filepath.Join(dir, "files")); err != nil {
		return errors.Wrapf(err, "writer: can not tar files")
	}
	return nil
}

// writeEmptyFiles Writes an empty files list to the update header, as
// the V3 format contains the updates in the augmented header, the regular
// header will not contain any update, thus this method is needed to bypass
// the empty files list check in version 1 and 2.
func writeEmptyFiles(tw *tar.Writer, updFiles []string, dir string) error {
	files := new(artifact.FilesV3)
	sa := artifact.NewTarWriterStream(tw)
	stream, err := artifact.ToStream(files)
	if err != nil {
		return errors.Wrap(err, "writeFiles: ")
	}
	if err := sa.Write(stream,
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

type WriteInfoArgs struct {
	tarWriter  *tar.Writer
	dir        string
	typeinfov3 *artifact.TypeInfoV3
}

func writeTypeInfoV3(args *WriteInfoArgs) error {
	info, err := json.Marshal(args.typeinfov3)
	if err != nil {
		return errors.Wrapf(err, "update: can not create type-info")
	}

	w := artifact.NewTarWriterStream(args.tarWriter)
	if err := w.Write(info, filepath.Join(args.dir, "type-info")); err != nil {
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
