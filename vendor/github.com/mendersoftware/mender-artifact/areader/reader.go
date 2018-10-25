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

package areader

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
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

type Reader struct {
	CompatibleDevicesCallback DevicesCompatibleFn
	ScriptsReadCallback       ScriptsReadFn
	VerifySignatureCallback   SignatureVerifyFn
	IsSigned                  bool
	shouldBeSigned            bool
	hInfo                     artifact.HeaderInfoer
	augmentedhInfo            *artifact.AugmentedHeaderInfoV3
	info                      *artifact.Info
	r                         io.Reader
	handlers                  map[string]handlers.Installer
	installers                map[int]handlers.Installer
}

func NewReader(r io.Reader) *Reader {
	return &Reader{
		r:          r,
		handlers:   make(map[string]handlers.Installer, 1),
		installers: make(map[int]handlers.Installer, 1),
	}
}

func NewReaderSigned(r io.Reader) *Reader {
	return &Reader{
		r:              r,
		shouldBeSigned: true,
		handlers:       make(map[string]handlers.Installer, 1),
		installers:     make(map[int]handlers.Installer, 1),
	}
}

func getReader(tReader io.Reader, headerSum []byte) io.Reader {

	if headerSum != nil {
		// If artifact is signed we need to calculate header checksum to be
		// able to validate it later.
		return artifact.NewReaderChecksum(tReader, headerSum)
	}
	return tReader
}

func readStateScripts(tr *tar.Reader, header *tar.Header, cb ScriptsReadFn) error {

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
		return errors.Wrapf(err, "readHeader: error opening compressed header")
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	// Populate the artifact info fields.
	if err = ar.populateArtifactInfo(ar.info.Version, tr); err != nil {
		return errors.Wrap(err, "readHeader")
	}
	// after reading header-info we can check device compatibility
	if ar.CompatibleDevicesCallback != nil {
		if err = ar.CompatibleDevicesCallback(ar.GetCompatibleDevices()); err != nil {
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
	if err = ar.setInstallers(ar.GetUpdates()); err != nil {
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

func (ar *Reader) populateArtifactInfo(version int, tr *tar.Reader) error {
	var hInfo artifact.HeaderInfoer
	switch version {
	case 1, 2:
		hInfo = new(artifact.HeaderInfo)
	case 3:
		hInfo = new(artifact.HeaderInfoV3)
	}
	// first part of header must always be header-info
	if err := readNext(tr, hInfo, "header-info"); err != nil {
		return err
	}
	ar.hInfo = hInfo
	return nil
}

func (ar *Reader) readAugmentedHeader(tReader io.Reader, headerSum []byte) error {
	r := getReader(tReader, headerSum)
	// header MUST be compressed
	gz, err := gzip.NewReader(r)
	if err != nil {
		return errors.Wrapf(err, "reader: error opening compressed header")
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	// first part of header must always be header-info
	hInfo := new(artifact.AugmentedHeaderInfoV3)
	if err = readNext(tr, hInfo, "header-info"); err != nil {
		return err
	}
	ar.augmentedhInfo = hInfo

	hdr, err := getNext(tr)
	if err != nil {
		return err
	}

	// Next step is setting correct installers based on update types being
	// part of the artifact.
	if err = ar.setInstallers(hInfo.Updates); err != nil {
		return err
	}

	// At the end read rest of the header using correct installers.
	if err = ar.readHeaderUpdate(tr, hdr); err != nil {
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

func ReadVersion(tr *tar.Reader) (*artifact.Info, []byte, error) {
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

func (ar *Reader) readHeaderV1(tReader *tar.Reader) error {
	if ar.shouldBeSigned {
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

func readManifest(tReader *tar.Reader, name string) (*artifact.ChecksumStore, error) {
	buf := bytes.NewBuffer(nil)
	if err := readNext(tReader, buf, name); err != nil {
		return nil, errors.Wrap(err, "reader: can not buffer manifest")
	}
	manifest := artifact.NewChecksumStore()
	if err := manifest.ReadRaw(buf.Bytes()); err != nil {
		return nil, errors.Wrap(err, "reader: can not read manifest")
	}
	return manifest, nil
}

func signatureReadAndVerify(tReader *tar.Reader, message []byte,
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
			return errors.Wrapf(err, "reader: invalid signature")
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

// The artifact parser needs to take one of the paths listed below when reading
// the artifact version 3.
var artifactV3ParseGrammar = [][]string{
	// Version is already read in ReadArtifact().
	{"manifest", "manifest-augment", "header.tar.gz", "header-augment.tar.gz"},                 // Unsigned.
	{"manifest", "manifest.sig", "manifest-augment", "header.tar.gz", "header-augment.tar.gz"}, // Signed.
	// Data is processed in ReadArtifact()
}

var errParseOrder = errors.New("Parse error: The artifact seems to have the wrong structure")

// verifyParseOrder compares the parseOrder against the allowed parse paths through an artifact.
func verifyParseOrder(parseOrder []string) (validToken string, validPath bool, err error) {
	// Do a substring search for the parseOrder sent in on each of the valid grammars.
	for _, validPath := range artifactV3ParseGrammar {
		if len(parseOrder) > len(validPath) {
			continue
		}
		// Check for a submatch in the current validPath.
		for i := range parseOrder {
			if validPath[i] != parseOrder[i] {
				break // Check the next validPath against the parseOrder.
			}
			// We have a submatch. Check if the entire length matches.
			if i == len(parseOrder)-1 {
				if len(parseOrder) == len(validPath) {
					return parseOrder[i], true, nil // Full match.
				}
				return parseOrder[i], false, nil
			}
		}
	}
	return "", false, errParseOrder
}

func (ar *Reader) readHeaderV3(tReader *tar.Reader,
	version []byte) (*artifact.ChecksumStore, error) {
	manifestChecksumStore := artifact.NewChecksumStore()
	parsePath := []string{}

	for {
		hdr, err := tReader.Next()
		if err == io.EOF {
			return nil, errors.New("The artifact does not contain all required fields")
		}
		if err != nil {
			return nil, errors.Wrap(err, "readHeaderV3")
		}
		parsePath = append(parsePath, hdr.Name)
		nextParseToken, validPath, err := verifyParseOrder(parsePath)
		// Only error returned is errParseOrder.
		if err != nil {
			return nil, fmt.Errorf("Invalid structure: %s, wrong element: %s", parsePath, parsePath[len(parsePath)-1])
		}
		err = handleHeaderReads(nextParseToken, tReader, manifestChecksumStore, ar, version)
		if err != nil {
			return nil, errors.Wrap(err, "readHeaderV3")
		}
		if validPath {
			// Artifact should be signed, but isn't, so do not process the update.
			if ar.shouldBeSigned && !ar.IsSigned {
				return nil,
					errors.New("reader: expecting signed artifact, but no signature file found")
			}
			break // return and process the /data records in ReadArtifact()
		}
	}
	return manifestChecksumStore, nil
}

func handleHeaderReads(headerName string, tReader *tar.Reader, manifestChecksumStore *artifact.ChecksumStore, ar *Reader, version []byte) (err error) {
	switch headerName {
	case "manifest":
		// Get the data from the manifest.
		return readManifestHeader(ar, tReader, manifestChecksumStore)
	case "manifest.sig":
		ar.IsSigned = true
		// First read and verify signature
		if err = signatureReadAndVerify(tReader, manifestChecksumStore.GetRaw(),
			ar.VerifySignatureCallback, ar.shouldBeSigned); err != nil {
			return err
		}
		// verify checksums of version
		if err = verifyVersion(version, manifestChecksumStore); err != nil {
			return err
		}
	case "manifest-augment":
		// Get the data from the augmented manifest.
		return readManifestHeader(ar, tReader, manifestChecksumStore)
	case "header.tar.gz":
		// Get and verify checksums of header.
		hc, err := manifestChecksumStore.Get("header.tar.gz")
		if err != nil {
			return err
		}

		if err := ar.readHeader(tReader, hc); err != nil {
			return errors.Wrap(err, "handleHeaderReads")
		}
	case "header-augment.tar.gz":
		// Get and verify checksums of the augmented header.
		hc, err := manifestChecksumStore.Get("header-augment.tar.gz")
		if err != nil {
			return err
		}
		if err := ar.readAugmentedHeader(tReader, hc); err != nil {
			return errors.Wrap(err, "handleHeaderReads: Failed to read the augmented header")
		}
	default:
		return errors.Errorf("reader: found unexpected file in artifact: %v",
			headerName)
	}
	return nil
}

func readManifestHeader(ar *Reader, tReader *tar.Reader, manifestChecksumStore *artifact.ChecksumStore) error {
	buf := bytes.NewBuffer(nil)
	_, err := io.Copy(buf, tReader)
	if err != nil {
		return errors.Wrap(err, "readHeaderV3: Failed to copy to the byte buffer, from the tar reader")
	}
	err = manifestChecksumStore.ReadRaw(buf.Bytes())
	if err != nil {
		return errors.Wrap(err, "readHeaderV3: Failed to populate the manifest's checksum store")
	}
	return nil
}

func (ar *Reader) readHeaderV2(tReader *tar.Reader,
	version []byte) (*artifact.ChecksumStore, error) {
	// first file after version MUST contain all the checksums
	manifest, err := readManifest(tReader, "manifest")
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
	if ar.shouldBeSigned && (hdr.FileInfo().Name() != "manifest.sig") {
		return nil,
			errors.New("reader: expecting signed artifact, but no signature file found")
	}

	switch hdr.FileInfo().Name() {
	case "manifest.sig":
		ar.IsSigned = true
		// firs read and verify signature
		if err = signatureReadAndVerify(tReader, manifest.GetRaw(),
			ar.VerifySignatureCallback, ar.shouldBeSigned); err != nil {
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

func (ar *Reader) ReadArtifact() error {
	// each artifact is tar archive
	if ar.r == nil {
		return errors.New("reader: read artifact called on invalid stream")
	}
	tReader := tar.NewReader(ar.r)

	// first file inside the artifact MUST be version
	ver, vRaw, err := ReadVersion(tReader)
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
	case 3:
		s, err = ar.readHeaderV3(tReader, vRaw)
		if err != nil {
			return err
		}
	default:
		return errors.Errorf("reader: unsupported version: %d", ver.Version)
	}
	return ar.readData(tReader, s)
}

func (ar *Reader) GetCompatibleDevices() []string {
	if ar.hInfo == nil {
		return nil
	}
	return ar.hInfo.GetCompatibleDevices()
}

func (ar *Reader) GetArtifactName() string {
	if ar.hInfo == nil {
		return ""
	}
	return ar.hInfo.GetArtifactName()
}

func (ar *Reader) GetInfo() artifact.Info {
	return *ar.info
}

func (ar *Reader) GetUpdates() []artifact.UpdateType {
	if ar.hInfo == nil {
		return nil
	}
	return ar.hInfo.GetUpdates()
}

// GetArtifactProvides is version 3 specific.
func (ar *Reader) GetArtifactProvides() *artifact.ArtifactProvides {
	h3, ok := ar.hInfo.(*artifact.HeaderInfoV3)
	if !ok {
		return nil
	}
	return h3.ArtifactProvides
}

// GetArtifactDepends is version 3 specific.
func (ar *Reader) GetArtifactDepends() *artifact.ArtifactDepends {
	h3, ok := ar.hInfo.(*artifact.HeaderInfoV3)
	if !ok {
		return nil
	}
	return h3.ArtifactDepends
}

func (ar *Reader) setInstallers(upd []artifact.UpdateType) error {
	for i, update := range upd {
		// set installer for given update type
		if w, ok := ar.handlers[update.Type]; ok {
			// NOTE ArtifactV3 specific:
			// If the update-type has not changed, do not update the installer.
			// The installer has internal state in order to handle the diffence between a read in an
			// augmented and a regular header.
			if installer, ok := ar.installers[i]; ok {
				if installer.GetType() == update.Type {
					continue
				}
			}
			ar.installers[i] = w.Copy()
			continue
		}
		// NOTE: ArtifactV3 specific:
		// Do not update the installer between the reads of the regular and augmented header,
		// as the installer has internal state.
		if ar.installers[i] != nil {
			return nil
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

func (ar *Reader) readHeaderUpdate(tr *tar.Reader, hdr *tar.Header) error {
	for {
		updNo, err := getUpdateNoFromHeaderPath(hdr.Name)
		if err != nil {
			return errors.Wrapf(err, "reader: error getting header update number")
		}

		inst, ok := ar.installers[updNo]
		if !ok {
			return errors.Errorf("reader: can not find parser for update: %v", hdr.Name)
		}
		if hErr := inst.ReadHeader(tr, hdr.Name, ar.info.Version); hErr != nil {
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

func (ar *Reader) readNextDataFile(tr *tar.Reader,
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
	return readAndInstall(tr, inst, manifest, updNo)
}

func (ar *Reader) readData(tr *tar.Reader, manifest *artifact.ChecksumStore) error {
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

func readNext(tr *tar.Reader, w io.Writer, elem string) error {
	if tr == nil {
		return errors.New("reader: read next called on invalid stream")
	}
	hdr, err := getNext(tr)
	if err != nil {
		return errors.Wrap(err, "readNext: Failed to get next header")
	}
	if strings.HasPrefix(hdr.Name, elem) {
		_, err := io.Copy(w, tr)
		return errors.Wrap(err, "readNext: Failed to copy from tarReader to the writer")
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

func getDataFile(i handlers.Installer, name string) *handlers.DataFile {
	for _, file := range i.GetUpdateFiles() {
		if name == file.Name {
			return file
		}
	}
	return nil
}

func readAndInstall(r io.Reader, i handlers.Installer,
	manifest *artifact.ChecksumStore, no int) error {
	// each data file is stored in tar.gz format
	gz, err := gzip.NewReader(r)
	if err != nil {
		return errors.Wrapf(err, "update: can not open gz for reading data")
	}
	defer gz.Close()

	tar := tar.NewReader(gz)

	for {
		hdr, err := tar.Next()
		if err == io.EOF {
			break
		} else if err != nil {
			return errors.Wrap(err, "update: error reading update file header")
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
		ch := artifact.NewReaderChecksum(tar, df.Checksum)

		if err = i.Install(ch, &info); err != nil {
			return errors.Wrapf(err, "update: can not install update: %v", hdr)
		}

		if err = ch.Verify(); err != nil {
			return errors.Wrap(err, "reader: error reading data")
		}
	}
	return nil
}
