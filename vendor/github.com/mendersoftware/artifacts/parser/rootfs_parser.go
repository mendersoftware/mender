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
	"encoding/json"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/mendersoftware/artifacts/archiver"
	"github.com/mendersoftware/artifacts/metadata"
	"github.com/pkg/errors"
)

// DataHandlerFunc is a user provided update data stream handler. Parameter `r`
// is a decompressed data stream, `dt` holds current device type, `uf` contains
// basic information about update. The handler shall return nil if no errors
// occur.
type DataHandlerFunc func(r io.Reader, uf UpdateFile) error

// RootfsParser handles updates of type 'rootfs-image'. The parser can be
// initialized setting `W` (io.Writer the update data gets written to), or
// `DataFunc` (user provided callback that handlers the update data stream).
type RootfsParser struct {
	W         io.Writer       // output stream the update gets written to
	ScriptDir string          // output directory for scripts
	DataFunc  DataHandlerFunc // custom update data handler

	metadata metadata.Metadata
	update   UpdateFile // we are supporting ONLY one update file for rootfs-image
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
	return map[string]UpdateFile{withoutExt(rp.update.Name): rp.update}
}

func (rp *RootfsParser) GetMetadata() *metadata.Metadata {
	return &rp.metadata
}

func (rp *RootfsParser) archiveToTmp(tw *tar.Writer, f *os.File) (err error) {
	gz := gzip.NewWriter(f)
	defer func() { err = gz.Close() }()
	dtw := tar.NewWriter(gz)
	defer func() { err = dtw.Close() }()

	a := archiver.NewFileArchiver(rp.update.Path, rp.update.Name)
	if err = a.Archive(dtw); err != nil {
		return err
	}
	return err
}

func (rp *RootfsParser) ArchiveData(tw *tar.Writer, dst string) error {
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

func archiveChecksums(tw *tar.Writer, upd []string, dir string) error {
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

type HeaderElems struct {
	Metadata []byte
	TypeInfo []byte
	Scripts  []string
}

func (rp *RootfsParser) ArchiveHeader(tw *tar.Writer, dstDir string, update *UpdateData) error {
	if update == nil {
		return errors.New("paser: empty update")
	}

	e := new(HeaderElems)
	if update.Data != nil {
		var ok bool
		e, ok = update.Data.(*HeaderElems)
		if !ok {
			return errors.New("invalid header elements type")
		}
	}

	if len(update.DataFiles) != 1 {
		return errors.Errorf("parser: only one update file supported for "+
			"rootfs-image; %d found", len(update.DataFiles))
	}

	// we have ONLY one update file below
	for _, f := range update.DataFiles {
		rp.update = UpdateFile{
			Name: filepath.Base(f),
			Path: f,
		}
	}
	if err := archiveFiles(tw, update.DataFiles, dstDir); err != nil {
		return errors.Wrapf(err, "parser: can not store files")
	}

	if e.TypeInfo == nil {
		tInfo := metadata.TypeInfo{Type: update.Type}
		info, err := json.Marshal(&tInfo)
		if err != nil {
			return errors.Wrapf(err, "parser: can not create type-info")
		}
		e.TypeInfo = info
	}

	a := archiver.NewStreamArchiver(e.TypeInfo, filepath.Join(dstDir, "type-info"))
	if err := a.Archive(tw); err != nil {
		return errors.Wrapf(err, "parser: can not store type-info")
	}

	// if metadata info is not provided we need to have one stored in file
	if e.Metadata == nil {
		a := archiver.NewFileArchiver(filepath.Join(update.Path, "meta-data"),
			filepath.Join(dstDir, "meta-data"))
		if err := a.Archive(tw); err != nil {
			return errors.Wrapf(err, "parser: can not store meta-data")
		}
	} else {
		a = archiver.NewStreamArchiver(e.Metadata, filepath.Join(dstDir, "meta-data"))
		if err := a.Archive(tw); err != nil {
			return errors.Wrapf(err, "parser: can not store meta-data")
		}
	}

	if err := archiveChecksums(tw, update.DataFiles,
		filepath.Join(dstDir, "checksums")); err != nil {
		return err
	}

	// scripts
	if len(e.Scripts) > 0 {
		for _, scr := range e.Scripts {
			scrRelPath, err := filepath.Rel(scr, filepath.Dir(filepath.Dir(scr)))
			if err != nil {
				return err
			}
			a := archiver.NewFileArchiver(scr, filepath.Join(dstDir, "scripts", scrRelPath))
			if err := a.Archive(tw); err != nil {
				return err
			}
		}
	} else {
		sa := scriptArch{
			w:   tw,
			dst: filepath.Join(dstDir, "scripts"),
		}
		if err := filepath.Walk(filepath.Join(update.Path, "scripts"),
			sa.archScrpt); err != nil {
			return errors.Wrapf(err, "parser: can not archive scripts")
		}
	}
	return nil
}

type scriptArch struct {
	w   *tar.Writer
	dst string
}

func (s *scriptArch) archScrpt(path string, info os.FileInfo, err error) error {
	if err != nil {
		// if there is no `scripts` directory
		if pErr, ok := err.(*os.PathError); ok && pErr.Err == syscall.ENOENT {
			return nil
		}
		return err
	}
	// store only files
	if info.IsDir() {
		return nil
	}
	// scripts should be always stored in `./scripts/{pre,post,check}/` directory
	sPath, err := filepath.Rel(filepath.Join(path, "..", ".."), path)
	if err != nil {
		return errors.Wrapf(err, "parser: can not archive scripts")
	}
	a := archiver.NewFileArchiver(path, filepath.Join(s.dst, sPath))
	return a.Archive(s.w)
}

func (rp *RootfsParser) ParseHeader(tr *tar.Reader, hdr *tar.Header, hPath string) error {
	relPath, err := filepath.Rel(hPath, hdr.Name)
	if err != nil {
		return err
	}

	switch {
	case strings.Compare(relPath, "files") == 0:
		updates := map[string]UpdateFile{}
		if err = parseFiles(tr, updates); err != nil {
			return err
		}
		if len(updates) != 1 {
			return errors.Wrapf(err, "parser: only one update file supported for "+
				"rootfs-image; %d found", len(updates))
		}

		// it is OK; we are having ONLY one update file
		for _, upd := range updates {
			rp.update = upd
		}
	case strings.Compare(relPath, "type-info") == 0:
		// we can skip this one for now
	case strings.Compare(relPath, "meta-data") == 0:
		if _, err = io.Copy(&rp.metadata, tr); err != nil {
			return errors.Wrapf(err, "parser: error reading metadata")
		}
	case strings.HasPrefix(relPath, "checksums"):
		updates := map[string]UpdateFile{withoutExt(rp.update.Name): rp.update}
		if err = processChecksums(tr, hdr.Name, updates); err != nil {
			return err
		}

		for _, upd := range updates {
			rp.update.Checksum = upd.Checksum
		}
	case strings.HasPrefix(relPath, "signatures"):
		//TODO:
	case strings.HasPrefix(relPath, "scripts"):
		//TODO

	default:
		return errors.New("parser: unsupported element '" + relPath + "' in header")
	}
	return nil
}

// data files are stored in tar.gz format
func (rp *RootfsParser) ParseData(r io.Reader) error {
	if rp.W == nil {
		rp.W = ioutil.Discard
	}

	updates := map[string]UpdateFile{}
	updates[withoutExt(rp.update.Name)] = rp.update

	if rp.DataFunc != nil {
		// run with user provided callback
		err := parseDataWithHandler(
			r,
			func(dr io.Reader, uf UpdateFile) error {
				return rp.DataFunc(dr, uf)
			},
			updates,
		)
		rp.update = updates[withoutExt(rp.update.Name)]
		return err
	}

	err := parseData(r, rp.W, updates)
	rp.update = updates[withoutExt(rp.update.Name)]
	return err
}

func withoutExt(name string) string {
	bName := filepath.Base(name)
	return strings.TrimSuffix(bName, filepath.Ext(bName))
}
