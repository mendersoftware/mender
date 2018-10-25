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
	"bytes"
	"compress/gzip"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/mendersoftware/mender-artifact/artifact"
	"github.com/pkg/errors"
)

// Rootfs handles updates of type 'rootfs-image'.
type Rootfs struct {
	version           int
	update            *DataFile
	regularHeaderRead bool

	InstallHandler func(io.Reader, *DataFile) error
}

func NewRootfsV1(updFile string) *Rootfs {
	uf := &DataFile{
		Name: updFile,
	}
	return &Rootfs{
		update:  uf,
		version: 1,
	}
}

func NewRootfsV2(updFile string) *Rootfs {
	uf := &DataFile{
		Name: updFile,
	}
	return &Rootfs{
		update:  uf,
		version: 2,
	}
}

func NewRootfsV3(updFile string) *Rootfs {
	uf := &DataFile{
		Name: updFile,
	}
	return &Rootfs{
		update:  uf,
		version: 3,
	}
}

// NewRootfsInstaller is used by the artifact reader to read and install
// rootfs-image update type.
func NewRootfsInstaller() *Rootfs {
	return &Rootfs{
		update: new(DataFile),
	}
}

// Copy creates a new instance of Rootfs handler from the existing one.
func (rp *Rootfs) Copy() Installer {
	return &Rootfs{
		version:           rp.version,
		update:            new(DataFile),
		InstallHandler:    rp.InstallHandler,
		regularHeaderRead: rp.regularHeaderRead,
	}
}

func (rp *Rootfs) ReadHeader(r io.Reader, path string, version int) error {
	switch {
	case filepath.Base(path) == "files":
		files, err := parseFiles(r)
		if version == 3 {
			if !rp.regularHeaderRead {
				rp.regularHeaderRead = true
				if err == nil {
					return errors.New("ReadHeader: files-list should be empty")
				}
				return nil
			}
		}
		if err != nil {
			return err
		}
		rp.update.Name = files.FileList[0]
	case filepath.Base(path) == "type-info",
		filepath.Base(path) == "meta-data",
		match(artifact.HeaderDirectory+"/*/signatures/*", path),
		match(artifact.HeaderDirectory+"/*/scripts/*/*", path):
		// TODO: implement when needed
	case match(artifact.HeaderDirectory+"/*/checksums/*", path):
		buf := bytes.NewBuffer(nil)
		if _, err := io.Copy(buf, r); err != nil {
			return errors.Wrap(err, "update: error reading checksum")
		}
		rp.update.Checksum = buf.Bytes()
	default:
		return errors.Errorf("update: unsupported file: %v", path)
	}
	return nil
}

func (rfs *Rootfs) Install(r io.Reader, info *os.FileInfo) error {
	if rfs.InstallHandler != nil {
		if err := rfs.InstallHandler(r, rfs.update); err != nil {
			return errors.Wrap(err, "update: can not install")
		}
	}
	return nil
}

func (rfs *Rootfs) GetUpdateFiles() [](*DataFile) {
	return [](*DataFile){rfs.update}
}

func (rfs *Rootfs) GetType() string {
	return "rootfs-image"
}

func (rfs *Rootfs) ComposeHeader(args *ComposeHeaderArgs) error {

	updFiles := filepath.Base(rfs.update.Name)
	path := artifact.UpdateHeaderPath(args.No)

	switch rfs.version {
	case 1, 2:
		// first store files
		if err := writeFiles(args.TarWriter, []string{updFiles},
			path); err != nil {
			return err
		}

		if err := writeTypeInfo(args.TarWriter, "rootfs-image", path); err != nil {
			return err
		}

	case 3:
		if args.Augmented {
			if err := writeFiles(args.TarWriter, []string{updFiles},
				path); err != nil {
				return err
			}
			// Remove the typeinfov3.depends, as this should not be written in the augmented-header.
			if args.TypeInfoV3 != nil {
				args.TypeInfoV3.ArtifactProvides = nil
			}
		} else {
			// The header in a version 3 artifact will not contain the update,
			// and hence there is no files in the files list.
			if err := writeEmptyFiles(args.TarWriter, []string{updFiles},
				path); err != nil {
				return err
			}
		}

		if err := writeTypeInfoV3(&WriteInfoArgs{
			tarWriter:  args.TarWriter,
			dir:        path,
			typeinfov3: args.TypeInfoV3,
		}); err != nil {
			return errors.Wrap(err, "ComposeHeader: ")
		}

	}

	// store empty meta-data
	// the file needs to be a part of artifact even if this one is empty
	sw := artifact.NewTarWriterStream(args.TarWriter)
	if err := sw.Write(nil, filepath.Join(path, "meta-data")); err != nil {
		return errors.Wrap(err, "update: can not store meta-data")
	}

	if rfs.version == 1 {
		// store checksums
		if err := writeChecksums(args.TarWriter, [](*DataFile){rfs.update},
			filepath.Join(path, "checksums")); err != nil {
			return err
		}
	}
	return nil
}

func (rfs *Rootfs) ComposeData(tw *tar.Writer, no int) error {
	f, ferr := ioutil.TempFile("", "data")
	if ferr != nil {
		return errors.New("update: can not create temporary data file")
	}
	defer os.Remove(f.Name())

	err := func() error {
		gz := gzip.NewWriter(f)
		defer gz.Close()

		tarw := tar.NewWriter(gz)
		defer tarw.Close()

		df, err := os.Open(rfs.update.Name)
		if err != nil {
			return errors.Wrapf(err, "update: can not open data file: %v", rfs.update)
		}
		defer df.Close()

		fw := artifact.NewTarWriterFile(tarw)
		if err := fw.Write(df, filepath.Base(rfs.update.Name)); err != nil {
			return errors.Wrapf(err,
				"update: can not write tar temp data header: %v", rfs.update)
		}
		return nil
	}()

	if err != nil {
		return err
	}

	if _, err = f.Seek(0, 0); err != nil {
		return errors.Wrapf(err, "update: can not read data file: %v", rfs.update)
	}

	dfw := artifact.NewTarWriterFile(tw)
	if err = dfw.Write(f, artifact.UpdateDataPath(no)); err != nil {
		return errors.Wrapf(err, "update: can not write tar data header: %v", rfs.update)
	}
	return nil
}
