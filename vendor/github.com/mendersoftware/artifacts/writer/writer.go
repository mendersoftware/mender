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

package awriter

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/mendersoftware/artifacts/archiver"
	"github.com/mendersoftware/artifacts/metadata"
	"github.com/mendersoftware/artifacts/parser"
	"github.com/pkg/errors"
)

// Writer provides on the fly writing of artifacts metadata file used by
// Mender client and server.
// Call Write to start writing artifacts file.
type Writer struct {
	format  string
	version int

	*parser.ParseManager
	availableUpdates []hdrData

	aName  string
	updDir string

	aArchiver *tar.Writer
	aFile     *os.File
	isClosed  bool

	*aHeader
}

type hdrData struct {
	path      string
	dataFiles []string
	tInfo     string
	p         parser.Parser
}

type aHeader struct {
	hInfo        metadata.HeaderInfo
	hTmpFile     *os.File
	hTmpFilePath string
	hArchiver    *tar.Writer
	hCompressor  *gzip.Writer
	isClosed     bool
}

func newHeader() *aHeader {
	hFile, err := initHeaderFile()
	if err != nil {
		return nil
	}

	hComp := gzip.NewWriter(hFile)
	hArch := tar.NewWriter(hComp)

	return &aHeader{
		hCompressor:  hComp,
		hArchiver:    hArch,
		hTmpFile:     hFile,
		hTmpFilePath: hFile.Name(),
	}
}

func (av *Writer) init(path string) (err error) {
	av.aFile, err = createArtFile(filepath.Dir(path), "artifact.mender")
	if err != nil {
		return
	}
	av.aArchiver = tar.NewWriter(av.aFile)
	av.aName = path
	av.aHeader = newHeader()
	if av.aHeader == nil {
		err = errors.New("writer: error initializing header")
	}
	return
}

func NewWriter(format string, version int) *Writer {
	return &Writer{
		format:       format,
		version:      version,
		ParseManager: parser.NewParseManager(),
	}
}

func createArtFile(dir, name string) (*os.File, error) {
	// here we should have header stored in temporary location
	fPath := filepath.Join(dir, name)
	f, err := os.Create(fPath)
	if err != nil {
		return nil, errors.Wrapf(err, "writer: can not create artifact file: %v", fPath)
	}
	return f, nil
}

func initHeaderFile() (*os.File, error) {
	// we need to create a file for storing header
	f, err := ioutil.TempFile("", "header")
	if err != nil {
		return nil, errors.Wrapf(err,
			"writer: error creating temp file for storing header")
	}

	return f, nil
}

func (av *Writer) write(updates []hdrData) error {
	av.availableUpdates = updates

	// write header
	if err := av.WriteHeader(); err != nil {
		return err
	}

	// archive info
	info := av.getInfo()
	ia := archiver.NewMetadataArchiver(&info, "info")
	if err := ia.Archive(av.aArchiver); err != nil {
		return errors.Wrapf(err, "writer: error archiving info")
	}
	// archive header
	ha := archiver.NewFileArchiver(av.hTmpFilePath, "header.tar.gz")
	if err := ha.Archive(av.aArchiver); err != nil {
		return errors.Wrapf(err, "writer: error archiving header")
	}
	// archive data
	if err := av.WriteData(); err != nil {
		return err
	}
	return nil
}

func (av *Writer) Write(updateDir, atrifactName string) error {
	if err := av.init(atrifactName); err != nil {
		return err
	}

	updates, err := av.ScanUpdateDirs(updateDir)
	if err != nil {
		return err
	}
	return av.write(updates)
}

func (av *Writer) WriteSingle(header, data, updateType, atrifactName string) error {
	if err := av.init(atrifactName); err != nil {
		return err
	}

	worker, err := av.GetRegistered(updateType)
	if err != nil {
		return err
	}

	hdr := hdrData{
		path:      header,
		dataFiles: []string{data},
		tInfo:     updateType,
		p:         worker,
	}
	typeInfo := metadata.TypeInfo{Type: "rootfs-image"}
	info, err := json.Marshal(&typeInfo)
	if err != nil {
		return errors.Wrapf(err, "reader: can not create type-info")
	}
	if err := ioutil.WriteFile(filepath.Join(header, "type-info"), info, os.ModePerm); err != nil {
		return errors.Wrapf(err, "reader: can not create type-info file")
	}
	return av.write([]hdrData{hdr})
}

func (av *Writer) Close() (err error) {
	if av.isClosed {
		return nil
	}

	errHeader := av.closeHeader()

	if av.hTmpFilePath != "" {
		os.Remove(av.hTmpFilePath)
	}

	var errArch error
	if av.aArchiver != nil {
		errArch = av.aArchiver.Close()
	}

	var errFile error
	if av.aFile != nil {
		errFile = av.aFile.Close()
	}

	if errHeader != nil || errArch != nil || errFile != nil {
		err = errors.New("writer: close error")
	} else {
		if av.aFile != nil {
			os.Rename(av.aFile.Name(), av.aName)
		}
		av.isClosed = true
	}
	return
}

// This reads `type-info` file in provided directory location.
func getTypeInfo(dir string) (*metadata.TypeInfo, error) {
	iPath := filepath.Join(dir, "type-info")
	f, err := os.Open(iPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info := new(metadata.TypeInfo)
	_, err = io.Copy(info, f)
	if err != nil {
		return nil, err
	}

	if err = info.Validate(); err != nil {
		return nil, err
	}
	return info, nil
}

func getDataFiles(dir string) ([]string, error) {
	info, err := os.Stat(dir)
	if os.IsNotExist(err) {
		// we have no data file(s) associated with given header
		return nil, nil
	} else if err != nil {
		return nil, errors.Wrapf(err, "writer: error reading data directory")
	}
	if info.IsDir() {
		updFiles, err := ioutil.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		var updates []string
		for _, f := range updFiles {
			updates = append(updates, filepath.Join(dir, f.Name()))
		}
		return updates, nil
	}
	return nil, errors.New("writer: broken data directory")
}

func (av *Writer) readDirContent(dir string) (*hdrData, error) {
	tInfo, err := getTypeInfo(filepath.Join(av.updDir, dir))
	if err != nil {
		return nil, os.ErrInvalid
	}
	p, err := av.ParseManager.GetRegistered(tInfo.Type)
	if err != nil {
		return nil, errors.Wrapf(err, "writer: error finding parser for [%v]", tInfo.Type)
	}

	data, err := getDataFiles(filepath.Join(av.updDir, dir, "data"))
	if err != nil {
		return nil, err
	}

	hdr := hdrData{
		path:      filepath.Join(av.updDir, dir),
		dataFiles: data,
		tInfo:     tInfo.Type,
		p:         p,
	}
	return &hdr, nil
}

func (av *Writer) ScanUpdateDirs(dir string) ([]hdrData, error) {
	av.updDir = dir
	// first check  if we have plain dir update
	hdr, err := av.readDirContent("")
	if err == nil {
		return []hdrData{*hdr}, nil
	} else if err != os.ErrInvalid {
		return nil, err
	}

	dirs, err := ioutil.ReadDir(av.updDir)
	if err != nil {
		return nil, err
	}

	var updates []hdrData

	for _, uDir := range dirs {
		if uDir.IsDir() {
			hdr, err := av.readDirContent(uDir.Name())
			if err == os.ErrInvalid {
				continue
			} else if err != nil {
				return nil, err
			}
			updates = append(updates, *hdr)
		}
	}

	if len(updates) == 0 {
		return nil, errors.New("writer: no update data detected")
	}
	return updates, nil
}

func (h *aHeader) closeHeader() (err error) {
	// We have seen some of header components to cause crash while
	// closing. That's why we are trying to close and clean up as much
	// as possible here and recover() if crash happens.
	defer func() {
		if r := recover(); r != nil {
			err = errors.New("error closing header")
		}
		if err == nil {
			h.isClosed = true
		}
	}()

	if !h.isClosed {
		errArch := h.hArchiver.Close()
		errComp := h.hCompressor.Close()
		errFile := h.hTmpFile.Close()

		if errArch != nil || errComp != nil || errFile != nil {
			err = errors.New("writer: error closing header")
		}
	}
	return
}

func (av *Writer) WriteHeader() error {
	// store header info
	for _, upd := range av.availableUpdates {
		av.hInfo.Updates =
			append(av.hInfo.Updates, metadata.UpdateType{Type: upd.tInfo})
	}
	hi := archiver.NewMetadataArchiver(&av.hInfo, "header-info")
	if err := hi.Archive(av.hArchiver); err != nil {
		return errors.Wrapf(err, "writer: can not store header-info")
	}
	for cnt := 0; cnt < len(av.availableUpdates); cnt++ {
		err := av.processNextHeaderDir(av.availableUpdates[cnt], fmt.Sprintf("%04d", cnt))
		if err == io.EOF {
			break
		} else if err != nil {
			return errors.Wrapf(err, "writer: error processing update directory")
		}
	}
	return av.aHeader.closeHeader()
}

func (av *Writer) processNextHeaderDir(hdr hdrData, order string) error {
	if err := hdr.p.ArchiveHeader(av.hArchiver, hdr.path,
		filepath.Join("headers", order), hdr.dataFiles); err != nil {
		return err
	}
	return nil
}

func (av *Writer) WriteData() error {
	for cnt := 0; cnt < len(av.availableUpdates); cnt++ {
		err := av.processNextDataDir(av.availableUpdates[cnt], fmt.Sprintf("%04d", cnt))
		if err != nil {
			return errors.Wrapf(err, "writer: error writing data files")
		}
	}
	return av.Close()
}

func (av *Writer) processNextDataDir(hdr hdrData, order string) error {
	if err := hdr.p.ArchiveData(av.aArchiver, hdr.path,
		filepath.Join("data", order+".tar.gz")); err != nil {
		return err
	}
	return nil
}

func (av Writer) getInfo() metadata.Info {
	return metadata.Info{
		Format:  av.format,
		Version: av.version,
	}
}
