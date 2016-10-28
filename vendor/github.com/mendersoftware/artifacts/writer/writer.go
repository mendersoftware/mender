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
	aName   string

	*parser.ParseManager
	availableUpdates []parser.UpdateData
	aArchiver        *tar.Writer
	aTmpFile         *os.File

	*aHeader
}

type aHeader struct {
	hInfo       metadata.HeaderInfo
	hTmpFile    *os.File
	hArchiver   *tar.Writer
	hCompressor *gzip.Writer
	isClosed    bool
}

func newHeader() *aHeader {
	hFile, err := initHeaderFile()
	if err != nil {
		return nil
	}

	hComp := gzip.NewWriter(hFile)
	hArch := tar.NewWriter(hComp)

	return &aHeader{
		hCompressor: hComp,
		hArchiver:   hArch,
		hTmpFile:    hFile,
	}
}

func (av *Writer) init(path string) error {
	av.aHeader = newHeader()
	if av.aHeader == nil {
		return errors.New("writer: error initializing header")
	}
	var err error
	av.aTmpFile, err = createArtFile(filepath.Dir(path), "artifact.mender.tmp")
	if err != nil {
		return err
	}
	av.aArchiver = tar.NewWriter(av.aTmpFile)
	av.aName = path
	return nil
}

func (av *Writer) deinit() error {
	var errHeader error
	if av.aHeader != nil {
		errHeader = av.closeHeader()
		if av.hTmpFile != nil {
			os.Remove(av.hTmpFile.Name())
		}
	}

	if av.aTmpFile != nil {
		os.Remove(av.aTmpFile.Name())
	}

	var errArchiver error
	if av.aArchiver != nil {
		errArchiver = av.aArchiver.Close()
	}
	if errArchiver != nil || errHeader != nil {
		return errors.New("writer: error deinitializing")
	}
	return nil
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
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		return nil, errors.Wrapf(err,
			"writer: can not store artifact file in directory: %s", dir)
	}
	return f, nil
}

func initHeaderFile() (*os.File, error) {
	// we need to create a temporary file for storing the header data
	f, err := ioutil.TempFile("", "header")
	if err != nil {
		return nil, errors.Wrapf(err,
			"writer: error creating temp file for storing header")
	}
	return f, nil
}

func (av *Writer) write(updates []parser.UpdateData) error {
	av.availableUpdates = updates

	// write temporary header (we need to know the size before storing in tar)
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
	ha := archiver.NewFileArchiver(av.hTmpFile.Name(), "header.tar.gz")
	if err := ha.Archive(av.aArchiver); err != nil {
		return errors.Wrapf(err, "writer: error archiving header")
	}
	// archive data
	if err := av.WriteData(); err != nil {
		return err
	}
	// we've been storing everything in temporary file
	if err := av.aArchiver.Close(); err != nil {
		return errors.New("writer: error closing archive")
	}
	// prevent from closing archiver twice
	av.aArchiver = nil

	if err := av.aTmpFile.Close(); err != nil {
		return errors.New("writer: error closing archive temporary file")
	}
	return os.Rename(av.aTmpFile.Name(), av.aName)
}

func (av *Writer) Write(updateDir, atrifactName string) error {
	if err := av.init(atrifactName); err != nil {
		return err
	}
	defer av.deinit()

	updates, err := av.ScanUpdateDirs(updateDir)
	if err != nil {
		return err
	}
	return av.write(updates)
}

func (av *Writer) WriteKnown(updates []parser.UpdateData, atrifactName string) error {
	if err := av.init(atrifactName); err != nil {
		return err
	}
	defer av.deinit()

	return av.write(updates)
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

func (av *Writer) readDirContent(dir, cur string) (*parser.UpdateData, error) {
	tInfo, err := getTypeInfo(filepath.Join(dir, cur))
	if err != nil {
		return nil, os.ErrInvalid
	}
	p, err := av.ParseManager.GetRegistered(tInfo.Type)
	if err != nil {
		return nil, errors.Wrapf(err, "writer: error finding parser for [%v]", tInfo.Type)
	}

	data, err := getDataFiles(filepath.Join(dir, cur, "data"))
	if err != nil {
		return nil, err
	}

	upd := parser.UpdateData{
		Path:      filepath.Join(dir, cur),
		DataFiles: data,
		Type:      tInfo.Type,
		P:         p,
	}
	return &upd, nil
}

func (av *Writer) ScanUpdateDirs(dir string) ([]parser.UpdateData, error) {
	// first check  if we have update in current directory
	upd, err := av.readDirContent(dir, "")
	if err == nil {
		return []parser.UpdateData{*upd}, nil
	} else if err != os.ErrInvalid {
		return nil, err
	}

	dirs, err := ioutil.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	updates := make([]parser.UpdateData, 0, len(dirs))
	for _, uDir := range dirs {
		if uDir.IsDir() {
			upd, err := av.readDirContent(dir, uDir.Name())
			if err == os.ErrInvalid {
				continue
			} else if err != nil {
				return nil, err
			}
			updates = append(updates, *upd)
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
			append(av.hInfo.Updates, metadata.UpdateType{Type: upd.Type})
	}
	hi := archiver.NewMetadataArchiver(&av.hInfo, "header-info")
	if err := hi.Archive(av.hArchiver); err != nil {
		return errors.Wrapf(err, "writer: can not store header-info")
	}
	for cnt := 0; cnt < len(av.availableUpdates); cnt++ {
		err := av.processNextHeaderDir(&av.availableUpdates[cnt], fmt.Sprintf("%04d", cnt))
		if err == io.EOF {
			break
		} else if err != nil {
			return errors.Wrapf(err, "writer: error processing update directory")
		}
	}
	return av.aHeader.closeHeader()
}

func (av *Writer) processNextHeaderDir(upd *parser.UpdateData, order string) error {
	if err := upd.P.ArchiveHeader(av.hArchiver, filepath.Join("headers", order),
		upd); err != nil {
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
	return nil
}

func (av *Writer) processNextDataDir(upd parser.UpdateData, order string) error {
	if err := upd.P.ArchiveData(av.aArchiver,
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
