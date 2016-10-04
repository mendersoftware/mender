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

package parser

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/mendersoftware/artifacts/archiver"
	"github.com/mendersoftware/artifacts/metadata"
	"github.com/pkg/errors"
)

// DataHandlerFunc is a user provided update data stream handler. Parameter `r`
// is a decompressed data stream, `dt` holds current device type, `uf` contains
// basic information about update. The handler shall return nil if no errors
// occur.
type DataHandlerFunc func(r io.Reader, dt string, uf UpdateFile) error

// RootfsParser handles updates of type 'image-rootfs'. The parser can be
// initialized setting `W` (io.Writer the update data gets written to), or
// `DataFunc` (user provided callback that handlers the update data stream).
type RootfsParser struct {
	W         io.Writer       // output stream the update gets written to
	ScriptDir string          // output directory for scripts
	DataFunc  DataHandlerFunc // custom update data handler

	metadata metadata.Metadata
	updates  map[string]UpdateFile
}

func (rp *RootfsParser) Copy() Parser {
	return &RootfsParser{
		W:         rp.W,
		ScriptDir: rp.ScriptDir,
		DataFunc:  rp.DataFunc,
	}
}

func (rp *RootfsParser) GetUpdateType() *metadata.UpdateType {
	return &metadata.UpdateType{Type: "rootfs-image"}
}

func (rp *RootfsParser) GetUpdateFiles() map[string]UpdateFile {
	return rp.updates
}
func (rp *RootfsParser) GetDeviceType() string {
	return rp.metadata.Required.DeviceType
}
func (rp *RootfsParser) GetImageID() string {
	return rp.metadata.Required.ImageID
}
func (rp *RootfsParser) GetMetadata() *metadata.AllMetadata {
	return &rp.metadata.All
}

func (rp *RootfsParser) archiveToTmp(tw *tar.Writer, f *os.File) (err error) {
	gz := gzip.NewWriter(f)
	defer func() { err = gz.Close() }()
	dtw := tar.NewWriter(gz)
	defer func() { err = dtw.Close() }()

	for _, data := range rp.updates {
		a := archiver.NewFileArchiver(data.Path, data.Name)
		if err = a.Archive(dtw); err != nil {
			return err
		}
	}
	return err
}

func (rp *RootfsParser) ArchiveData(tw *tar.Writer, src, dst string) error {
	f, err := ioutil.TempFile("", "data")
	if err != nil {
		return errors.Wrapf(err, "parser: can not create tmp data file")
	}
	defer os.Remove(f.Name())

	if err := rp.archiveToTmp(tw, f); err != nil {
		return errors.Wrapf(err, "parser: error archiving data to tmp file")
	}

	a := archiver.NewFileArchiver(f.Name(), dst)
	if err := a.Archive(tw); err != nil {
		return err
	}

	return nil
}

func archiveFiles(tw *tar.Writer, upd []string, dir string) error {
	files := new(metadata.Files)
	for _, u := range upd {
		files.FileList = append(files.FileList, filepath.Base(u))
	}
	a := archiver.NewMetadataArchiver(files, filepath.Join(dir, "files"))
	return a.Archive(tw)
}

func calcChecksum(file string) ([]byte, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil,
			errors.Wrapf(err, "can not open file for calculating checksum")
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, errors.Wrapf(err, "error calculating checksum")
	}

	sum := h.Sum(nil)
	checksum := make([]byte, hex.EncodedLen(len(sum)))
	hex.Encode(checksum, h.Sum(nil))
	return checksum, nil
}

func archiveChecksums(tw *tar.Writer, upd []string, src, dir string) error {
	for _, u := range upd {
		sum, err := calcChecksum(u)
		if err != nil {
			return err
		}
		a := archiver.NewStreamArchiver(sum, filepath.Join(dir, withoutExt(u)+".sha256sum"))
		if err := a.Archive(tw); err != nil {
			return errors.Wrapf(err, "parser: error storing checksum")
		}
	}
	return nil
}

type scriptArchiver struct {
	tw  *tar.Writer
	src string
	dst string
}

func (sa *scriptArchiver) archiveScrpt(path string, info os.FileInfo, err error) error {
	if info.IsDir() {
		return nil
	}
	sPath, err := filepath.Rel(sa.src, path)
	if err != nil {
		return errors.Wrapf(err, "parser: error getting path for storing scripts")
	}

	a := archiver.NewFileArchiver(path, filepath.Join(sa.dst, sPath))
	return a.Archive(sa.tw)
}

func (rp *RootfsParser) ArchiveHeader(tw *tar.Writer,
	src, dst string, updFiles []string) error {
	if err := hFormatPreWrite.CheckHeaderStructure(src); err != nil {
		return err
	}

	rp.updates = map[string]UpdateFile{}
	for _, f := range updFiles {
		rp.updates[withoutExt(f)] =
			UpdateFile{
				Name: filepath.Base(f),
				Path: f,
			}
	}
	if err := archiveFiles(tw, updFiles, dst); err != nil {
		return errors.Wrapf(err, "parser: can not store files")
	}

	a := archiver.NewFileArchiver(filepath.Join(src, "type-info"),
		filepath.Join(dst, "type-info"))
	if err := a.Archive(tw); err != nil {
		return errors.Wrapf(err, "parser: can not store type-info")
	}

	a = archiver.NewFileArchiver(filepath.Join(src, "meta-data"),
		filepath.Join(dst, "meta-data"))
	if err := a.Archive(tw); err != nil {
		return errors.Wrapf(err, "parser: can not store meta-data")
	}

	if err := archiveChecksums(tw, updFiles,
		filepath.Join(src, "data"),
		filepath.Join(dst, "checksums")); err != nil {
		return err
	}

	// copy signatures
	for _, u := range updFiles {
		a = archiver.NewFileArchiver(
			filepath.Join(src, "signatures", withoutExt(u)+".sig"),
			filepath.Join(dst, "signatures", withoutExt(u)+".sig"))
		if err := a.Archive(tw); err != nil {
			// TODO: for now we are skipping storing signatures
			return nil
		}
	}

	// scripts
	sa := scriptArchiver{tw, src, dst}
	if err := filepath.Walk(filepath.Join(src, "scripts"),
		sa.archiveScrpt); err != nil {
		return errors.Wrapf(err, "parser: can not store scripts")
	}
	return nil
}

func (rp *RootfsParser) ParseHeader(tr *tar.Reader, hdr *tar.Header, hPath string) error {

	relPath, err := filepath.Rel(hPath, hdr.Name)
	if err != nil {
		return err
	}

	switch {
	case strings.Compare(relPath, "files") == 0:
		if rp.updates == nil {
			rp.updates = map[string]UpdateFile{}
		}
		if err = parseFiles(tr, rp.updates); err != nil {
			return err
		}
	case strings.Compare(relPath, "type-info") == 0:
		// we can skip this one for now
	case strings.Compare(relPath, "meta-data") == 0:
		if _, err = io.Copy(&rp.metadata, tr); err != nil {
			return errors.Wrapf(err, "parser: error reading metadata")
		}
	case strings.HasPrefix(relPath, "checksums"):
		if err = processChecksums(tr, hdr.Name, rp.updates); err != nil {
			return err
		}
	case strings.HasPrefix(relPath, "signatures"):
		//TODO:
	case strings.HasPrefix(relPath, "scripts"):
		//TODO

	default:
		return errors.New("parser: unsupported element in header")
	}
	return nil
}

// data files are stored in tar.gz format
func (rp *RootfsParser) ParseData(r io.Reader) error {
	if rp.W == nil {
		rp.W = ioutil.Discard
	}

	if rp.DataFunc != nil {
		// run with user provided callback
		return parseDataWithHandler(
			r,
			func(dr io.Reader, uf UpdateFile) error {
				return rp.DataFunc(dr, rp.GetDeviceType(), uf)
			},
			rp.updates,
		)
	}
	return parseData(r, rp.W, rp.updates)
}

var hFormatPreWrite = metadata.ArtifactHeader{
	// while calling filepath.Walk() `.` (root) directory is included
	// when iterating throug entries in the tree
	".": {Path: ".", IsDir: true, Required: false},
	// temporary artifact file
	"artifact.mender": {Path: "artifact.mender", IsDir: false, Required: false},
	"files":           {Path: "files", IsDir: false, Required: false},
	"meta-data":       {Path: "meta-data", IsDir: false, Required: true},
	"type-info":       {Path: "type-info", IsDir: false, Required: true},
	"checksums":       {Path: "checksums", IsDir: true, Required: false},
	"checksums/*":     {Path: "checksums", IsDir: false, Required: false},
	"signatures":      {Path: "signatures", IsDir: true, Required: false},
	"signatures/*":    {Path: "signatures", IsDir: false, Required: false},
	"scripts":         {Path: "scripts", IsDir: true, Required: false},
	"scripts/pre":     {Path: "scripts/pre", IsDir: true, Required: false},
	"scripts/pre/*":   {Path: "scripts/pre", IsDir: false, Required: false},
	"scripts/post":    {Path: "scripts/post", IsDir: true, Required: false},
	"scripts/post/*":  {Path: "scripts/post", IsDir: false, Required: false},
	"scripts/check":   {Path: "scripts/check", IsDir: true, Required: false},
	"scripts/check/*": {Path: "scripts/check", IsDir: false, Required: false},
	// we can have data directory containing update
	"data":   {Path: "data", IsDir: true, Required: false},
	"data/*": {Path: "data/*", IsDir: false, Required: false},
}

func withoutExt(name string) string {
	bName := filepath.Base(name)
	return strings.TrimSuffix(bName, filepath.Ext(bName))
}
