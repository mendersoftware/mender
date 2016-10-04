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
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/ioutil"
	"path/filepath"
	"strings"

	"github.com/mendersoftware/artifacts/metadata"
	"github.com/pkg/errors"
)

type GenericParser struct {
	metadata metadata.Metadata
	updates  map[string]UpdateFile
}

func (rp *GenericParser) GetUpdateType() *metadata.UpdateType {
	return &metadata.UpdateType{Type: "generic"}
}

func (rp *GenericParser) GetUpdateFiles() map[string]UpdateFile {
	return rp.updates
}
func (rp *GenericParser) GetDeviceType() string {
	return rp.metadata.Required.DeviceType
}
func (rp *GenericParser) GetMetadata() *metadata.AllMetadata {
	return &rp.metadata.All
}

func parseFiles(tr *tar.Reader, uFiles map[string]UpdateFile) error {
	files := new(metadata.Files)
	if _, err := io.Copy(files, tr); err != nil {
		return errors.Wrapf(err, "parser: error reading files")
	}
	for _, file := range files.FileList {
		uFiles[withoutExt(file)] = UpdateFile{Name: file}
	}
	return nil
}

func processChecksums(tr *tar.Reader, name string, uFiles map[string]UpdateFile) error {
	update, ok := uFiles[withoutExt(name)]
	if !ok {
		return errors.New("parser: found checksum for non existing update file")
	}
	buf := bytes.NewBuffer(nil)
	if _, err := io.Copy(buf, tr); err != nil {
		return errors.Wrapf(err, "rparser: error reading checksum")
	}
	update.Checksum = buf.Bytes()
	uFiles[withoutExt(name)] = update
	return nil
}

func (rp *GenericParser) ParseHeader(tr *tar.Reader, hdr *tar.Header, hPath string) error {
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
	case strings.Compare(relPath, "meta-data") == 0:
		if _, err = io.Copy(&rp.metadata, tr); err != nil {
			return errors.Wrapf(err, "parser: error reading metadata")
		}
	case strings.HasPrefix(relPath, "checksums"):
		if err = processChecksums(tr, hdr.Name, rp.updates); err != nil {
			return err
		}
	}
	return nil
}

// Default data parser what writes the uncompressed data to `w`.
func parseData(r io.Reader, w io.Writer, uFiles map[string]UpdateFile) error {
	return parseDataWithHandler(
		r,
		func(dr io.Reader, uf UpdateFile) error {
			if _, err := io.Copy(w, dr); err != nil {
				return errors.Wrapf(err, "parser: can not read data: %v", uf.Name)
			}
			return nil
		},
		uFiles,
	)
}

// Caller provided update data stream handler. Parameter `r` is a decompressed
// data stream, `uf` contains basic information about update. The handler shall
// return nil if no errors occur.
type parseDataHandlerFunc func(r io.Reader, uf UpdateFile) error

func parseDataWithHandler(r io.Reader, handler parseDataHandlerFunc, uFiles map[string]UpdateFile) error {
	if r == nil {
		return errors.New("rootfs updater: uninitialized tar reader")
	}
	//[data.tar].gz
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer gz.Close()

	//data[.tar].gz
	tar := tar.NewReader(gz)
	// iterate over the files in tar archive
	for {
		hdr, err := tar.Next()
		if err == io.EOF {
			// once we reach end of archive break the loop
			break
		} else if err != nil {
			return errors.Wrapf(err, "parser: error reading archive")
		}
		fh, ok := uFiles[withoutExt(hdr.Name)]
		if !ok {
			return errors.New("parser: can not find header info for data file")
		}
		fh.Date = hdr.ModTime
		fh.Size = hdr.Size

		h := sha256.New()
		// use tee reader to pass read data for checksum calculation
		tr := io.TeeReader(tar, h)

		if err := handler(tr, fh); err != nil {
			return errors.Wrapf(err, "parser: data handler failed")
		}

		sum := h.Sum(nil)
		hSum := make([]byte, hex.EncodedLen(len(sum)))
		hex.Encode(hSum, h.Sum(nil))

		if bytes.Compare(hSum, fh.Checksum) != 0 {
			return errors.New("parser: invalid data file checksum")
		}

		uFiles[withoutExt(hdr.Name)] = fh
	}
	return nil
}

func (rp *GenericParser) Copy() Parser {
	return &GenericParser{}
}

// data files are stored in tar.gz format
func (rp *GenericParser) ParseData(r io.Reader) error {
	return parseData(r, ioutil.Discard, rp.updates)
}

func (rp *GenericParser) ArchiveData(tw *tar.Writer, src, dst string) error {
	return errors.New("generic: can not use generic parser for writing artifact")
}

func (rp *GenericParser) ArchiveHeader(tw *tar.Writer, src, dst string, updFiles []string) error {
	return errors.New("generic: can not use generic parser for writing artifact")
}
