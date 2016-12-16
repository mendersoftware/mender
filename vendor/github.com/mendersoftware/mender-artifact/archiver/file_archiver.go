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

package archiver

import (
	"archive/tar"
	"io"
	"os"

	"github.com/pkg/errors"
)

type FileArchiver struct {
	path        string
	archivePath string
	*os.File
}

// NewFileArchiver creates fileArchiver used for storing plain files
// inside tar archive.
// path is the absolute path to the file that will be archived and
// archivePath is the relatve path inside the archive (see tar.Header.Name)
func NewFileArchiver(path, archivePath string) *FileArchiver {
	return &FileArchiver{path, archivePath, nil}
}

func (f *FileArchiver) Archive(tw *tar.Writer) error {
	info, err := os.Stat(f.path)
	if err != nil {
		return err
	}
	hdr, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return errors.Wrapf(err, "arch: invalid file info header")
	}

	fd, err := os.Open(f.path)
	if err != nil {
		return errors.Wrapf(err, "arch: can not open file")
	}
	defer fd.Close()

	hdr.Name = f.archivePath
	if err = tw.WriteHeader(hdr); err != nil {
		return errors.Wrapf(err, "arch: error writing header")
	}

	_, err = io.Copy(tw, fd)
	if err != nil {
		return errors.Wrapf(err, "arch: error writing archive data")
	}
	return nil
}
