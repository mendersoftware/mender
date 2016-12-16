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

package areader

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mendersoftware/mender-artifact/metadata"
	"github.com/mendersoftware/mender-artifact/parser"
	"github.com/pkg/errors"
)

type Reader struct {
	r io.Reader
	*parser.ParseManager

	info    *metadata.Info
	tReader *tar.Reader
	*headerReader
}

type headerReader struct {
	hInfo *metadata.HeaderInfo

	hGzipReader *gzip.Reader
	hReader     *tar.Reader
	nextUpdate  int
}

func NewReader(r io.Reader) *Reader {
	return &Reader{
		r:            r,
		ParseManager: parser.NewParseManager(),
		headerReader: &headerReader{hInfo: new(metadata.HeaderInfo)},
	}
}

func isCompatibleWithDevice(current string, compatible []string) bool {
	for _, dev := range compatible {
		if strings.Compare(current, dev) == 0 {
			return true
		}
	}
	return false
}

func (ar *Reader) read(device string) (parser.Workers, error) {
	defer func() { ar.tReader = nil }()

	var err error
	ar.info, err = ar.ReadInfo()
	if err != nil {
		return nil, err
	}

	switch ar.info.Version {
	// so far we are supporting only v1
	case 1:
		var hInfo *metadata.HeaderInfo
		hInfo, err = ar.ReadHeaderInfo()
		if err != nil {
			return nil, err
		}

		// check compatibility with given device type
		if len(device) > 0 {
			if !isCompatibleWithDevice(device, hInfo.CompatibleDevices) {
				return nil, errors.Errorf(
					"unexpected device type [%v], expected to see one of [%v]",
					device, hInfo.CompatibleDevices)
			}
		}

		if _, err = ar.setWorkers(); err != nil {
			return nil, err
		}
		if _, err := ar.ReadHeader(); err != nil {
			return nil, err
		}
		if _, err := ar.ReadData(); err != nil {
			return nil, err
		}
	default:
		return nil, errors.Errorf("reader: unsupported version: %d",
			ar.info.Version)
	}

	return ar.ParseManager.GetWorkers(), nil
}

func (ar *Reader) Read() (parser.Workers, error) {
	return ar.read("")
}

func (ar *Reader) ReadCompatibleWithDevice(device string) (parser.Workers, error) {
	return ar.read(device)
}

func (ar *Reader) Close() error {
	if ar.hGzipReader != nil {
		return ar.hGzipReader.Close()
	}
	return nil
}

func (ar *Reader) GetCompatibleDevices() []string {
	return ar.hInfo.CompatibleDevices
}

func (ar *Reader) GetArtifactName() string {
	return ar.hInfo.ArtifactName
}

func (ar *Reader) GetInfo() metadata.Info {
	return *ar.info
}

func (ar *Reader) getTarReader() *tar.Reader {
	if ar.tReader == nil {
		ar.tReader = tar.NewReader(ar.r)
	}
	return ar.tReader
}

// This reads next element in main artifact tar structure.
// In v1 there are only info, header and data files available.
func (ar *Reader) readNext(w io.Writer, elem string) error {
	tr := ar.getTarReader()
	return readNext(tr, w, elem)
}

func (ar *Reader) getNext() (*tar.Header, error) {
	tr := ar.getTarReader()
	return getNext(tr)
}

func (ar *Reader) ReadHeaderInfo() (*metadata.HeaderInfo, error) {
	hdr, err := ar.getNext()
	if err != nil {
		return nil, errors.New("reader: error initializing header")
	}
	if !strings.HasPrefix(hdr.Name, "header.tar.") {
		return nil, errors.New("reader: invalid header name or elemet out of order")
	}

	gz, err := gzip.NewReader(ar.tReader)
	if err != nil {
		return nil, errors.Wrapf(err, "reader: error opening compressed header")
	}
	ar.hGzipReader = gz
	tr := tar.NewReader(gz)
	ar.hReader = tr

	if err := readNext(tr, ar.hInfo, "header-info"); err != nil {
		return nil, err
	}
	return ar.hInfo, nil
}

func (ar *Reader) setWorkers() (parser.Workers, error) {
	for cnt, update := range ar.hInfo.Updates {
		// firsrt check if we have worker for given update
		w, err := ar.ParseManager.GetWorker(fmt.Sprintf("%04d", cnt))

		if err == nil {
			if w.GetUpdateType().Type == update.Type || w.GetUpdateType().Type == "generic" {
				continue
			}

			return nil, errors.New("reader: wrong worker for given update type")
		}
		// if not just register worker for given update type
		p, err := ar.ParseManager.GetRegistered(update.Type)
		if err != nil {
			// if there is no registered one; check if we can use generic
			p = ar.ParseManager.GetGeneric(update.Type)
			if p == nil {
				return nil, errors.Wrapf(err,
					"reader: can not find parser for update type: [%v]", update.Type)
			}
		}
		ar.ParseManager.PushWorker(p, fmt.Sprintf("%04d", cnt))
	}
	return ar.ParseManager.GetWorkers(), nil
}

func (ar *Reader) ReadInfo() (*metadata.Info, error) {
	info := new(metadata.Info)
	err := ar.readNext(info, "version")
	if err != nil {
		return nil, err
	}
	return info, nil
}

func getUpdateFromHdr(hdr string) string {
	r := strings.Split(hdr, string(os.PathSeparator))
	if len(r) < 2 {
		return ""
	}
	return r[1]
}

func (ar *Reader) ReadNextHeader() (parser.Parser, error) {

	var p parser.Parser

	for {

		var hdr *tar.Header
		hdr, err := getNext(ar.hReader)
		if err == io.EOF {
			errClose := ar.Close()
			if errClose != nil {
				return nil, errors.Wrapf(errClose, "reader: error closing header reader")
			}
			return p, io.EOF
		} else if err != nil {
			return nil, errors.Wrapf(err, "reader: can not init header reading")
		}

		// make sure we are reading first header file for given update
		// some parsers might skip some header files
		upd := getUpdateFromHdr(hdr.Name)
		if upd != fmt.Sprintf("%04d", ar.headerReader.nextUpdate) {
			// make sure to increase update counter while current header is processed
			ar.headerReader.nextUpdate = ar.headerReader.nextUpdate + 1
		}

		p, err = ar.ParseManager.GetWorker(upd)
		if err != nil {
			err = errors.Wrapf(err, "reader: can not find parser for update: %v", upd)
			return nil, err
		}
		err = p.ParseHeader(ar.hReader, hdr, filepath.Join("headers", upd))
		if err != nil {
			return nil, err
		}

	}
}

func (ar *Reader) ReadHeader() (parser.Workers, error) {
	for {
		_, err := ar.ReadNextHeader()
		if err == io.EOF {
			return ar.ParseManager.GetWorkers(), nil
		} else if err != nil {
			return nil, err
		}
	}
}

func getDataFileUpdate(path string) string {
	return strings.TrimSuffix(filepath.Base(path), ".tar.gz")
}

func (ar *Reader) ReadNextDataFile() (parser.Parser, error) {
	hdr, err := ar.getNext()
	if err == io.EOF {
		return nil, io.EOF
	} else if err != nil {
		return nil, errors.Wrapf(err, "reader: error reading update file: "+hdr.Name)
	}
	if strings.Compare(filepath.Dir(hdr.Name), "data") != 0 {
		return nil, errors.New("reader: invalid data file name: " + hdr.Name)
	}
	p, err := ar.ParseManager.GetWorker(getDataFileUpdate(hdr.Name))
	if err != nil {
		return nil, errors.Wrapf(err,
			"reader: can not find parser for parsing data file [%v]", hdr.Name)
	}
	err = p.ParseData(ar.tReader)
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (ar *Reader) ReadData() (parser.Workers, error) {
	for {
		_, err := ar.ReadNextDataFile()
		if err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}
	}
	return ar.GetWorkers(), nil
}

func readNext(tr *tar.Reader, w io.Writer, elem string) error {
	if tr == nil {
		return errors.New("reader: read next called on invalid stream")
	}
	hdr, err := getNext(tr)
	if err != nil {
		return err
	}
	if strings.HasPrefix(hdr.Name, elem) {
		_, err = io.Copy(w, tr)
		if err != nil {
			return errors.Wrapf(err, "reader: error reading")
		}
		return nil
	}
	return os.ErrInvalid
}

func getNext(tr *tar.Reader) (*tar.Header, error) {
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			// we've reached end of archive
			return hdr, err
		} else if err != nil {
			return nil, errors.Wrapf(err, "reader: error reading archive")
		}
		return hdr, nil
	}
}
