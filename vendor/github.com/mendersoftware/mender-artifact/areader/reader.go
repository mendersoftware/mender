// Copyright 2017 Northern.tech AS
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
	"bytes"
	"compress/gzip"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mendersoftware/mender-artifact/artifact"
	"github.com/mendersoftware/mender-artifact/handlers"
	"github.com/pkg/errors"
)

type SignatureVerifyFn func(message, sig []byte) error
type DevicesCompatibleFn func([]string) error
type ScriptsReadFn func(io.Reader, os.FileInfo) error

// TarReaderish is an interface that mimics the interface of tar.Reader.
type TarReaderish interface {
	Next() (*tar.Header, error)
	Read([]byte) (int, error)
}

// GzipReaderish is an interface that mimics the interface of gzip.Reader.
type GzipReaderish interface {
	io.ReadCloser
}

// GzipReaderish is an interface that mimics the interface of artifact.ReaderChecksum
type ReaderChecksumish interface {
	Read([]byte) (int, error)
	Checksum() []byte
	Verify() error
}

// CustomNewOuterTarReaderFn is a function prototype that returns a TarReaderish which will be used to parse the "outer" artifact tar file.
type CustomNewOuterTarReaderFn func(io.Reader) (TarReaderish, error)

// CustomNewGzipReaderFn is a function prototype that returns a GzipReaderish which will be used to decompress the "inner" "data/*" files in the artifact.
type CustomNewGzipReaderFn func(io.Reader) (GzipReaderish, error)

// CustomNewInnerTarReaderFn is a function prototype that returns a TarReaderish which will be used to parse the "inner" "data/*" tar file.
// The function should return a TarReaderish.  Optionally, a "file in progress" tar.Header may be returned along with the offset into the current file.
type CustomNewInnerTarReaderFn func(io.Reader, int, *artifact.ChecksumStore) (TarReaderish, *tar.Header, int64, error)

// CustomNewInnerTarReaderFn is a function prototype that returns a ReaderChecksumish which will be used to verify the checksum of the data files extracted from the "inner" "data/*" tar file.
type CustomNewReaderChecksumFn func(io.Reader, []byte) (ReaderChecksumish, error)

type Reader struct {
	CompatibleDevicesCallback DevicesCompatibleFn
	ScriptsReadCallback       ScriptsReadFn
	VerifySignatureCallback   SignatureVerifyFn

	newOuterTarReaderFn CustomNewOuterTarReaderFn
	newInnerTarReaderFn CustomNewInnerTarReaderFn
	newGzipReaderFn     CustomNewGzipReaderFn
	newReaderChecksumFn CustomNewReaderChecksumFn

	signed     bool
	hInfo      *artifact.HeaderInfo
	info       *artifact.Info
	r          io.Reader
	handlers   map[string]handlers.Installer
	installers map[int]handlers.Installer

	rawReader TarReaderish
}

func DefaultNewOuterTarReaderish(r io.Reader) (TarReaderish, error) {
	return tar.NewReader(r), nil
}

func DefaultNewInnerTarReaderish(r io.Reader, datafileNo int, manifest *artifact.ChecksumStore) (TarReaderish, *tar.Header, int64, error) {
	return tar.NewReader(r), nil, 0, nil
}

func DefaultNewGzipReaderish(r io.Reader) (GzipReaderish, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, errors.Wrapf(err, "reader: error opening compressed header")
	}
	return gz, nil
}

func DefaultNewReaderCheckSumish(r io.Reader, expectedSum []byte) (ReaderChecksumish, error) {
	return artifact.NewReaderChecksum(r, expectedSum), nil
}

func NewReader(r io.Reader) *Reader {
	return &Reader{
		r:                   r,
		handlers:            make(map[string]handlers.Installer, 1),
		installers:          make(map[int]handlers.Installer, 1),
		newOuterTarReaderFn: DefaultNewOuterTarReaderish,
		newInnerTarReaderFn: DefaultNewInnerTarReaderish,
		newGzipReaderFn:     DefaultNewGzipReaderish,
		newReaderChecksumFn: DefaultNewReaderCheckSumish,
	}
}

func NewReaderSigned(r io.Reader) *Reader {
	return &Reader{
		r:                   r,
		signed:              true,
		handlers:            make(map[string]handlers.Installer, 1),
		installers:          make(map[int]handlers.Installer, 1),
		newOuterTarReaderFn: DefaultNewOuterTarReaderish,
		newInnerTarReaderFn: DefaultNewInnerTarReaderish,
		newGzipReaderFn:     DefaultNewGzipReaderish,
		newReaderChecksumFn: DefaultNewReaderCheckSumish,
	}
}

// NewReaderCustom returns a new Reader which uses the specified functions to create objects implementing the TarReaderish, GzipReaderish, and ReaderCheckumish interfaces, which will be used during ArtifactRead().
func NewReaderCustom(
	r io.Reader,
	newOuterTarReaderFn CustomNewOuterTarReaderFn,
	newInnerTarReaderFn CustomNewInnerTarReaderFn,
	newGzipReaderFn CustomNewGzipReaderFn,
	newReaderChecksumFn CustomNewReaderChecksumFn,
	isSigned bool,
) *Reader {
	return &Reader{
		r:          r,
		signed:     isSigned,
		handlers:   make(map[string]handlers.Installer, 1),
		installers: make(map[int]handlers.Installer, 1),

		newOuterTarReaderFn: newOuterTarReaderFn,
		newInnerTarReaderFn: newInnerTarReaderFn,
		newGzipReaderFn:     newGzipReaderFn,
		newReaderChecksumFn: newReaderChecksumFn,
	}
}

func getReader(tReader io.Reader, headerSum []byte) io.Reader {

	if headerSum != nil {
		// If artifact is signed we need to calculate header checksum to be
		// able to validate it later.
		return artifact.NewReaderChecksum(tReader, headerSum)
	} else {
		return tReader
	}
}

func readStateScripts(tr TarReaderish, header *tar.Header, cb ScriptsReadFn) error {

	for {
		hdr, err := getNext(tr)
		if err == io.EOF {
			return nil
		} else if err != nil {
			return errors.Wrapf(err,
				"reader: error reading artifact header file: %v", hdr)
		}
		if filepath.Dir(hdr.Name) == "scripts" {
			if cb != nil {
				if err = cb(tr, hdr.FileInfo()); err != nil {
					return err
				}
			}
		} else {
			// if there are no more scripts to read leave the loop
			*header = *hdr
			break
		}
	}

	return nil
}

func (ar *Reader) readHeader(tReader io.Reader, headerSum []byte) error {

	r := getReader(tReader, headerSum)
	// header MUST be compressed
	gz, err := gzip.NewReader(r)
	if err != nil {
		return errors.Wrapf(err, "reader: error opening compressed header")
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	// first part of header must always be header-info
	hInfo := new(artifact.HeaderInfo)
	if err = readNext(tr, hInfo, "header-info"); err != nil {
		return err
	}
	ar.hInfo = hInfo

	// after reading header-info we can check device compatibility
	if ar.CompatibleDevicesCallback != nil {
		if err = ar.CompatibleDevicesCallback(hInfo.CompatibleDevices); err != nil {
			return err
		}
	}

	var hdr tar.Header

	// Next we need to read and process state scripts.
	if err = readStateScripts(tr, &hdr, ar.ScriptsReadCallback); err != nil {
		return err
	}

	// Next step is setting correct installers based on update types being
	// part of the artifact.
	if err = ar.setInstallers(hInfo.Updates); err != nil {
		return err
	}

	// At the end read rest of the header using correct installers.
	if err = ar.readHeaderUpdate(tr, &hdr); err != nil {
		return err
	}

	// Check if header checksum is correct.
	if cr, ok := r.(*artifact.Checksum); ok {
		if err = cr.Verify(); err != nil {
			return errors.Wrap(err, "reader: reading header error")
		}
	}

	return nil
}

func readVersion(tr TarReaderish) (*artifact.Info, []byte, error) {
	buf := bytes.NewBuffer(nil)
	// read version file and calculate checksum
	if err := readNext(tr, buf, "version"); err != nil {
		return nil, nil, err
	}
	raw := buf.Bytes()
	info := new(artifact.Info)
	if _, err := io.Copy(info, buf); err != nil {
		return nil, nil, err
	}
	return info, raw, nil
}

func (ar *Reader) RegisterHandler(handler handlers.Installer) error {
	if handler == nil {
		return errors.New("reader: invalid handler")
	}
	if _, ok := ar.handlers[handler.GetType()]; ok {
		return os.ErrExist
	}
	ar.handlers[handler.GetType()] = handler
	return nil
}

func (ar *Reader) GetHandlers() map[int]handlers.Installer {
	return ar.installers
}

func (ar *Reader) readHeaderV1(tReader TarReaderish) error {
	if ar.signed {
		return errors.New("reader: expecting signed artifact; " +
			"v1 is not supporting signatures")
	}
	hdr, err := getNext(tReader)
	if err != nil {
		return errors.New("reader: error reading header")
	}
	if !strings.HasPrefix(hdr.Name, "header.tar.") {
		return errors.Errorf("reader: invalid header element: %v", hdr.Name)
	}

	if err = ar.readHeader(tReader, nil); err != nil {
		return err
	}
	return nil
}

func readManifest(tReader TarReaderish) (*artifact.ChecksumStore, error) {
	buf := bytes.NewBuffer(nil)
	if err := readNext(tReader, buf, "manifest"); err != nil {
		return nil, errors.Wrap(err, "reader: can not buffer manifest")
	}
	manifest := artifact.NewChecksumStore()
	if err := manifest.ReadRaw(buf.Bytes()); err != nil {
		return nil, errors.Wrap(err, "reader: can not read manifest")
	}
	return manifest, nil
}

func signatureReadAndVerify(tReader TarReaderish, message []byte,
	verify SignatureVerifyFn, signed bool) error {

	// verify signature
	if verify == nil && signed {
		return errors.New("reader: verify signature callback not registered")
	} else if verify != nil {
		// first read signature...
		sig := bytes.NewBuffer(nil)
		if _, err := io.Copy(sig, tReader); err != nil {
			return errors.Wrapf(err, "reader: can not read signature file")
		}

		if err := verify(message, sig.Bytes()); err != nil {
			return errors.Wrap(err, "reader: invalid signature")
		}
	}
	return nil
}

func verifyVersion(ver []byte, manifest *artifact.ChecksumStore) error {
	verSum, err := manifest.Get("version")
	if err != nil {
		return err
	}
	buf := bytes.NewBuffer(ver)
	c := artifact.NewReaderChecksum(buf, verSum)
	_, err = io.Copy(ioutil.Discard, c)
	return err
}

func (ar *Reader) readHeaderV2(tReader TarReaderish,
	version []byte) (*artifact.ChecksumStore, error) {
	// first file after version MUST contain all the checksums
	manifest, err := readManifest(tReader)
	if err != nil {
		return nil, err
	}

	// check what is the next file in the artifact
	// depending if artifact is signed or not we can have
	// either header or signature file
	hdr, err := getNext(tReader)
	if err != nil {
		return nil, errors.Wrapf(err, "reader: error reading file after manifest")
	}

	// we are expecting to have a signed artifact, but the signature is missing
	if ar.signed && (hdr.FileInfo().Name() != "manifest.sig") {
		return nil,
			errors.New("reader: expecting signed artifact, but no signature file found")
	}

	switch hdr.FileInfo().Name() {
	case "manifest.sig":
		// firs read and verify signature
		if err = signatureReadAndVerify(tReader, manifest.GetRaw(),
			ar.VerifySignatureCallback, ar.signed); err != nil {
			return nil, err
		}
		// verify checksums of version
		if err = verifyVersion(version, manifest); err != nil {
			return nil, err
		}

		// ...and then header
		hdr, err = getNext(tReader)
		if err != nil {
			return nil, errors.New("reader: error reading header")
		}
		if !strings.HasPrefix(hdr.Name, "header.tar.gz") {
			return nil, errors.Errorf("reader: invalid header element: %v", hdr.Name)
		}
		fallthrough

	case "header.tar.gz":
		// get and verify checksums of header
		hc, err := manifest.Get("header.tar.gz")
		if err != nil {
			return nil, err
		}

		if err := ar.readHeader(tReader, hc); err != nil {
			return nil, err
		}

	default:
		return nil, errors.Errorf("reader: found unexpected file in artifact: %v",
			hdr.FileInfo().Name())
	}
	return manifest, nil
}

func (ar *Reader) ReadRaw() (*artifact.Raw, error) {
	// each artifact is tar archive
	if ar.r == nil {
		return nil, errors.New("reader: read raw called on invalid stream")
	}
	if ar.rawReader == nil {
		ar.rawReader = tar.NewReader(ar.r)
	}
	hdr, err := getNext(ar.rawReader)
	if err != nil {
		return nil, errors.Wrap(err, "reader: error reading raw data")
	}
	return artifact.NewRaw(hdr.Name, hdr.Size, ar.rawReader), nil
}

func (ar *Reader) ReadRawVersion() (int, *artifact.Raw, error) {
	// each artifact is tar archive
	if ar.r == nil {
		return 0, nil,
			errors.New("reader: read raw version called on invalid stream")
	}

	// check if reader is initialized
	if ar.rawReader == nil {
		ar.rawReader = tar.NewReader(ar.r)
	}

	ver, vRaw, err := readVersion(ar.rawReader)
	if err != nil {
		return 0, nil, errors.Wrapf(err, "reader: can not read version file")
	}
	return ver.Version,
		artifact.NewRaw("version", int64(len(vRaw)), bytes.NewBuffer(vRaw)), nil
}

func (ar *Reader) ReadArtifact() error {
	// each artifact is tar archive
	if ar.r == nil {
		return errors.New("reader: read artifact called on invalid stream")
	}

	tReader, err := ar.newOuterTarReaderFn(ar.r)
	if err != nil {
		return errors.Wrapf(err, "reader: Failed to create TarReaderish")
	}

	// first file inside the artifact MUST be version
	ver, vRaw, err := readVersion(tReader)
	if err != nil {
		return errors.Wrapf(err, "reader: can not read version file")
	}
	ar.info = ver

	var s *artifact.ChecksumStore

	switch ver.Version {
	case 1:
		if err = ar.readHeaderV1(tReader); err != nil {
			return err
		}
	case 2:
		s, err = ar.readHeaderV2(tReader, vRaw)
		if err != nil {
			return err
		}
	default:
		return errors.Errorf("reader: unsupported version: %d", ver.Version)
	}
	return ar.readData(tReader, s)
}

func (ar *Reader) GetCompatibleDevices() []string {
	return ar.hInfo.CompatibleDevices
}

func (ar *Reader) GetArtifactName() string {
	return ar.hInfo.ArtifactName
}

func (ar *Reader) GetInfo() artifact.Info {
	return *ar.info
}

func (ar *Reader) setInstallers(upd []artifact.UpdateType) error {
	for i, update := range upd {
		// set installer for given update type
		if w, ok := ar.handlers[update.Type]; ok {
			ar.installers[i] = w.Copy()
			continue
		}
		// if nothing else worked set generic installer for given update
		ar.installers[i] = handlers.NewGeneric(update.Type)
	}
	return nil
}

// should be `headers/0000/file` format
func getUpdateNoFromHeaderPath(path string) (int, error) {
	split := strings.Split(path, string(os.PathSeparator))
	if len(split) < 3 {
		return 0, errors.New("can not get update order from tar path")
	}
	return strconv.Atoi(split[1])
}

// should be 0000.tar.gz
func getUpdateNoFromDataPath(path string) (int, error) {
	no := strings.TrimSuffix(filepath.Base(path), ".tar.gz")
	return strconv.Atoi(no)
}

func (ar *Reader) readHeaderUpdate(tr TarReaderish, hdr *tar.Header) error {
	for {
		updNo, err := getUpdateNoFromHeaderPath(hdr.Name)
		if err != nil {
			return errors.Wrapf(err, "reader: error getting header update number")
		}

		inst, ok := ar.installers[updNo]
		if !ok {
			return errors.Errorf("reader: can not find parser for update: %v", hdr.Name)
		}
		if hErr := inst.ReadHeader(tr, hdr.Name); hErr != nil {
			return errors.Wrap(hErr, "reader: can not read header")
		}

		hdr, err = getNext(tr)
		if err == io.EOF {
			return nil
		} else if err != nil {
			return errors.Wrapf(err,
				"reader: can not read artifact header file: %v", hdr)
		}
	}
}

func (ar *Reader) readNextDataFile(tr TarReaderish,
	manifest *artifact.ChecksumStore) error {
	hdr, err := getNext(tr)
	if err == io.EOF {
		return io.EOF
	} else if err != nil {
		return errors.Wrapf(err, "reader: error reading update file: [%v]", hdr)
	}
	if filepath.Dir(hdr.Name) != "data" {
		return errors.New("reader: invalid data file name: " + hdr.Name)
	}
	updNo, err := getUpdateNoFromDataPath(hdr.Name)
	if err != nil {
		return errors.Wrapf(err, "reader: error getting data update number")
	}
	inst, ok := ar.installers[updNo]
	if !ok {
		return errors.Wrapf(err,
			"reader: can not find parser for parsing data file [%v]", hdr.Name)
	}
	return readAndInstall(tr, inst, manifest, updNo, ar.newGzipReaderFn, ar.newInnerTarReaderFn, ar.newReaderChecksumFn)
}

func (ar *Reader) readData(tr TarReaderish, manifest *artifact.ChecksumStore) error {
	for {
		err := ar.readNextDataFile(tr, manifest)
		if err == io.EOF {
			break
		} else if err != nil {
			return err
		}
	}
	return nil
}

func readNext(tr TarReaderish, w io.Writer, elem string) error {
	if tr == nil {
		return errors.New("reader: read next called on invalid stream")
	}
	hdr, err := getNext(tr)
	if err != nil {
		return err
	}
	if strings.HasPrefix(hdr.Name, elem) {
		_, err := io.Copy(w, tr)
		return err
	}
	return os.ErrInvalid
}

func getNext(tr TarReaderish) (*tar.Header, error) {
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

func getDataFile(i handlers.Installer, name string) *handlers.DataFile {
	for _, file := range i.GetUpdateFiles() {
		if name == file.Name {
			return file
		}
	}
	return nil
}

func readAndInstall(r io.Reader, i handlers.Installer,
	manifest *artifact.ChecksumStore, no int,
	newGzipReaderFn CustomNewGzipReaderFn,
	newInnerTarReaderFn CustomNewInnerTarReaderFn,
	newReaderChecksumFn CustomNewReaderChecksumFn,
) error {
	// each data file is stored in tar.gz format
	gz, err := newGzipReaderFn(r)
	if err != nil {
		return errors.Wrapf(err, "update: can not open gz for reading data")
	}
	defer gz.Close()

	tf, currentFileHeader, _, err := newInnerTarReaderFn(gz, no, manifest)
	if err != nil {
		return errors.Wrapf(err, "reader: Failed to create TarReaderish for data file")
	}

	for {
		var hdr *tar.Header
		if currentFileHeader == nil {
			hdr, err = tf.Next()
			if err == io.EOF {
				break
			} else if err != nil {
				return errors.Wrap(err, "update: error reading update file header")
			}
		} else {
			hdr = currentFileHeader
			currentFileHeader = nil
		}

		df := getDataFile(i, hdr.Name)
		if df == nil {
			return errors.Errorf("update: can not find data file: %s", hdr.Name)
		}

		// fill in needed data
		info := hdr.FileInfo()
		df.Size = info.Size()
		df.Date = info.ModTime()

		// we need to have a checksum either in manifest file (v2 artifact)
		// or it needs to be pre-filled after reading header
		// all the names of the data files in manifest are written with the
		// archive relative path: data/0000/update.ext4
		if manifest != nil {
			df.Checksum, err = manifest.Get(filepath.Join(artifact.UpdatePath(no),
				hdr.FileInfo().Name()))
			if err != nil {
				return errors.Wrapf(err, "update: checksum missing")
			}
		}
		if df.Checksum == nil {
			return errors.Errorf("update: checksum missing for file: %s", hdr.Name)
		}

		// check checksum
		var rdr io.Reader
		var ch ReaderChecksumish = nil

		if newReaderChecksumFn != nil {
			ch, err := newReaderChecksumFn(tf, df.Checksum)
			if err != nil {
				return errors.Wrapf(err, "reader: could not create ReaderChecksumish")
			}
			rdr = ch
		} else {
			// NOTE: artifact checksums are disabled!
			rdr = tf
		}

		if err = i.Install(rdr, &info); err != nil {
			return errors.Wrapf(err, "update: can not install update: %v", hdr)
		}

		if ch != nil {
			if err = ch.Verify(); err != nil {
				return errors.Wrap(err, "reader: error reading data")
			}
		}
	}
	return nil
}
