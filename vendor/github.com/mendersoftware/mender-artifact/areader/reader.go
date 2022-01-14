// Copyright 2021 Northern.tech AS
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
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/pkg/errors"

	"github.com/mendersoftware/mender-artifact/artifact"
	"github.com/mendersoftware/mender-artifact/handlers"
	"github.com/mendersoftware/mender-artifact/utils"
)

type SignatureVerifyFn func(message, sig []byte) error
type DevicesCompatibleFn func([]string) error
type ScriptsReadFn func(io.Reader, os.FileInfo) error

type ProgressReader interface {
	Wrap(io.Reader, int64) io.Reader
}

type Reader struct {
	CompatibleDevicesCallback DevicesCompatibleFn
	ScriptsReadCallback       ScriptsReadFn
	VerifySignatureCallback   SignatureVerifyFn
	IsSigned                  bool
	ForbidUnknownHandlers     bool

	shouldBeSigned  bool
	hInfo           artifact.HeaderInfoer
	augmentedhInfo  artifact.HeaderInfoer
	info            *artifact.Info
	r               io.Reader
	files           []handlers.DataFile
	augmentFiles    []handlers.DataFile
	handlers        map[string]handlers.Installer
	installers      map[int]handlers.Installer
	updateStorers   map[int]handlers.UpdateStorer
	manifest        *artifact.ChecksumStore
	menderTarReader *tar.Reader
	ProgressReader  ProgressReader
	compressor      artifact.Compressor
}

func NewReader(r io.Reader) *Reader {
	return &Reader{
		r:             r,
		handlers:      make(map[string]handlers.Installer, 1),
		installers:    make(map[int]handlers.Installer, 1),
		updateStorers: make(map[int]handlers.UpdateStorer),
	}
}

func NewReaderSigned(r io.Reader) *Reader {
	return &Reader{
		r:              r,
		shouldBeSigned: true,
		handlers:       make(map[string]handlers.Installer, 1),
		installers:     make(map[int]handlers.Installer, 1),
		updateStorers:  make(map[int]handlers.UpdateStorer),
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
		if errors.Cause(err) == io.EOF {
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

func (ar *Reader) readHeader(headerSum []byte, comp artifact.Compressor) error {

	r := getReader(ar.menderTarReader, headerSum)
	// header MUST be compressed
	gz, err := comp.NewReader(r)
	if err != nil {
		return errors.Wrapf(err, "readHeader: error opening %s header",
			comp.GetFileExtension())
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
	if err = ar.setInstallers(ar.GetUpdates(), false); err != nil {
		return err
	}

	// At the end read rest of the header using correct installers.
	if err = ar.readHeaderUpdate(tr, &hdr, false); err != nil {
		return err
	}

	// Empty the remaining reader
	// See (MEN-5094)
	_, _ = io.Copy(ioutil.Discard, r)

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
	case 2:
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

func (ar *Reader) readAugmentedHeader(headerSum []byte, comp artifact.Compressor) error {
	r := getReader(ar.menderTarReader, headerSum)
	// header MUST be compressed
	gz, err := comp.NewReader(r)
	if err != nil {
		return errors.Wrapf(err, "reader: error opening %s header",
			comp.GetFileExtension())
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	// first part of header must always be header-info
	hInfo := new(artifact.HeaderInfoV3)
	err = readNext(tr, hInfo, "header-info")
	if err != nil {
		return errors.Wrap(err, "readAugmentedHeader")
	}
	ar.augmentedhInfo = hInfo

	hdr, err := getNext(tr)
	if err != nil {
		return errors.Wrap(err, "readAugmentedHeader")
	}

	// Next step is setting correct installers based on update types being
	// part of the artifact.
	if err = ar.setInstallers(hInfo.Updates, true); err != nil {
		return errors.Wrap(err, "readAugmentedHeader")
	}

	// At the end read rest of the header using correct installers.
	if err = ar.readHeaderUpdate(tr, hdr, true); err != nil {
		return errors.Wrap(err, "readAugmentedHeader")
	}

	// Empty the remaining reader
	// See (MEN-5094)
	_, _ = io.Copy(ioutil.Discard, r)

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
	if _, ok := ar.handlers[handler.GetUpdateType()]; ok {
		return os.ErrExist
	}
	ar.handlers[handler.GetUpdateType()] = handler
	return nil
}

func (ar *Reader) GetHandlers() map[int]handlers.Installer {
	return ar.installers
}

func (ar *Reader) readManifest(name string) error {
	buf := bytes.NewBuffer(nil)
	if err := readNext(ar.menderTarReader, buf, name); err != nil {
		return errors.Wrap(err, "reader: can not buffer manifest")
	}
	manifest := artifact.NewChecksumStore()
	if err := manifest.ReadRaw(buf.Bytes()); err != nil {
		return errors.Wrap(err, "reader: can not read manifest")
	}
	ar.manifest = manifest
	return nil
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
	verSum, err := manifest.GetAndMark("version")
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
	// Unsigned, uncompressed
	{"manifest", "header.tar"},
	// Signed, uncompressed
	{"manifest", "manifest.sig", "header.tar"},
	// Unsigned, uncompressed, with augment header
	{"manifest", "manifest-augment", "header.tar", "header-augment.tar"},
	// Signed, uncompressed, with augment header
	{"manifest", "manifest.sig", "manifest-augment", "header.tar", "header-augment.tar"},
	// Unsigned, gzipped
	{"manifest", "header.tar.gz"},
	// Signed, gzipped
	{"manifest", "manifest.sig", "header.tar.gz"},
	// Unsigned, gzipped, with augment header
	{"manifest", "manifest-augment", "header.tar.gz", "header-augment.tar.gz"},
	// Signed, gzipped, with augment header
	{"manifest", "manifest.sig", "manifest-augment", "header.tar.gz", "header-augment.tar.gz"},
	// Unsigned, lzma-zipped
	{"manifest", "header.tar.xz"},
	// Signed, lzma-zipped
	{"manifest", "manifest.sig", "header.tar.xz"},
	// Unsigned, lzma-zipped, with augment header
	{"manifest", "manifest-augment", "header.tar.xz", "header-augment.tar.xz"},
	// Signed, lzma-zipped, with augment header
	{"manifest", "manifest.sig", "manifest-augment", "header.tar.xz", "header-augment.tar.xz"},
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

func (ar *Reader) readHeaderV3(version []byte) error {

	ar.manifest = artifact.NewChecksumStore()
	parsePath := []string{}

	for {
		hdr, err := ar.menderTarReader.Next()
		if errors.Cause(err) == io.EOF {
			return errors.New("The artifact does not contain all required fields")
		}
		if err != nil {
			return errors.Wrap(err, "readHeaderV3")
		}
		parsePath = append(parsePath, hdr.Name)
		nextParseToken, validPath, err := verifyParseOrder(parsePath)
		// Only error returned is errParseOrder.
		if err != nil {
			return fmt.Errorf(
				"Invalid structure: %s, wrong element: %s", parsePath, parsePath[len(parsePath)-1],
			)
		}
		err = ar.handleHeaderReads(nextParseToken, version)
		if err != nil {
			return errors.Wrap(err, "readHeaderV3")
		}
		if validPath {
			// Artifact should be signed, but isn't, so do not process the update.
			if ar.shouldBeSigned && !ar.IsSigned {
				return errors.New("reader: expecting signed artifact, but no signature file found")
			}
			break // return and process the /data records in ReadArtifact()
		}
	}

	// Now assign all the files we got in the manifest to the correct
	// installers. The files are indexed by their `data/xxxx` prefix.
	if err := ar.assignUpdateFiles(); err != nil {
		return err
	}

	return nil
}

func (ar *Reader) handleHeaderReads(headerName string, version []byte) error {
	var err error
	switch headerName {
	case "manifest":
		// Get the data from the manifest.
		ar.files, err = readManifestHeader(ar, ar.menderTarReader)
		if err != nil {
			return err
		}
		// verify checksums of version
		if err = verifyVersion(version, ar.manifest); err != nil {
			return err
		}
		return err
	case "manifest.sig":
		ar.IsSigned = true
		// First read and verify signature
		if err = signatureReadAndVerify(ar.menderTarReader, ar.manifest.GetRaw(),
			ar.VerifySignatureCallback, ar.shouldBeSigned); err != nil {
			return err
		}
	case "manifest-augment":
		// Get the data from the augmented manifest.
		ar.augmentFiles, err = readManifestHeader(ar, ar.menderTarReader)
		return err
	case "header.tar", "header.tar.gz", "header.tar.xz":
		// Get and verify checksums of header.
		hc, err := ar.manifest.GetAndMark(headerName)
		if err != nil {
			return err
		}

		comp, err := artifact.NewCompressorFromFileName(headerName)
		if err != nil {
			return errors.New("reader: can't get compressor")
		}
		ar.compressor = comp

		if err := ar.readHeader(hc, comp); err != nil {
			return errors.Wrap(err, "handleHeaderReads")
		}
	case "header-augment.tar", "header-augment.tar.gz", "header-augment.tar.xz":
		// Get and verify checksums of the augmented header.
		hc, err := ar.manifest.GetAndMark(headerName)
		if err != nil {
			return err
		}

		comp, err := artifact.NewCompressorFromFileName(headerName)
		if err != nil {
			return errors.New("reader: can't get compressor")
		}

		if err := ar.readAugmentedHeader(hc, comp); err != nil {
			return errors.Wrap(err, "handleHeaderReads: Failed to read the augmented header")
		}
	default:
		return errors.Errorf("reader: found unexpected file in artifact: %v",
			headerName)
	}
	return nil
}

func readManifestHeader(ar *Reader, tReader *tar.Reader) ([]handlers.DataFile, error) {
	buf := bytes.NewBuffer(nil)
	_, err := io.Copy(buf, tReader)
	if err != nil {
		return nil, errors.Wrap(
			err,
			"readHeaderV3: Failed to copy to the byte buffer, from the tar reader",
		)
	}
	err = ar.manifest.ReadRaw(buf.Bytes())
	if err != nil {
		return nil, errors.Wrap(
			err,
			"readHeaderV3: Failed to populate the manifest's checksum store",
		)
	}
	lines := bytes.Split(buf.Bytes(), []byte("\n"))
	files := make([]handlers.DataFile, 0, len(lines))
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		split := bytes.SplitN(line, []byte("  "), 2)
		if len(split) != 2 {
			return nil, fmt.Errorf("Garbled entry in manifest: '%s'", line)
		}
		files = append(files, handlers.DataFile{Name: string(split[1])})
	}
	return files, nil
}

func (ar *Reader) readHeaderV2(version []byte) error {

	// first file after version MUST contain all the checksums
	err := ar.readManifest("manifest")
	if err != nil {
		return err
	}

	// check what is the next file in the artifact
	// depending if artifact is signed or not we can have
	// either header or signature file
	hdr, err := getNext(ar.menderTarReader)
	if err != nil {
		return errors.Wrapf(err, "reader: error reading file after manifest")
	}

	// we are expecting to have a signed artifact, but the signature is missing
	if ar.shouldBeSigned && (hdr.FileInfo().Name() != "manifest.sig") {
		return errors.New("reader: expecting signed artifact, but no signature file found")
	}

	name := hdr.FileInfo().Name()
	switch {
	case name == "manifest.sig":
		ar.IsSigned = true
		// firs read and verify signature
		if err = signatureReadAndVerify(ar.menderTarReader, ar.manifest.GetRaw(),
			ar.VerifySignatureCallback, ar.shouldBeSigned); err != nil {
			return err
		}
		// verify checksums of version
		if err = verifyVersion(version, ar.manifest); err != nil {
			return err
		}

		// ...and then header
		hdr, err = getNext(ar.menderTarReader)
		if err != nil {
			return errors.New("reader: error reading header")
		}
		name = hdr.FileInfo().Name()
		if !strings.HasPrefix(name, "header.tar") {
			return errors.Errorf("reader: invalid header element: %v", hdr.Name)
		}
		fallthrough

	case strings.HasPrefix(name, "header.tar"):
		// get and verify checksums of header
		hc, err := ar.manifest.GetAndMark(name)
		if err != nil {
			return err
		}

		// verify checksums of version
		if err = verifyVersion(version, ar.manifest); err != nil {
			return err
		}

		comp, err := artifact.NewCompressorFromFileName(name)
		if err != nil {
			return errors.New("reader: can't get compressor")
		}
		ar.compressor = comp

		if err := ar.readHeader(hc, comp); err != nil {
			return err
		}

	default:
		return errors.Errorf("reader: found unexpected file in artifact: %v",
			hdr.FileInfo().Name())
	}
	return nil
}

func (ar *Reader) ReadArtifact() error {
	err := ar.ReadArtifactHeaders()
	if err != nil {
		return err
	}

	return ar.ReadArtifactData()
}

func (ar *Reader) ReadArtifactHeaders() error {
	// each artifact is tar archive
	if ar.r == nil {
		return errors.New("reader: read artifact called on invalid stream")
	}
	ar.menderTarReader = tar.NewReader(ar.r)

	// first file inside the artifact MUST be version
	ver, vRaw, err := ReadVersion(ar.menderTarReader)
	if err != nil {
		return errors.Wrapf(err, "reader: can not read version file")
	}
	ar.info = ver

	switch ver.Version {
	case 1:
		err = errors.New("reader: Mender-Artifact version 1 is no longer supported")
	case 2:
		err = ar.readHeaderV2(vRaw)
	case 3:
		err = ar.readHeaderV3(vRaw)
	default:
		return errors.Errorf("reader: unsupported version: %d", ver.Version)
	}
	if err != nil {
		return err
	}

	return nil
}

func (ar *Reader) ReadArtifactData() error {
	err := ar.initializeUpdateStorers()
	if err != nil {
		return err
	}

	err = ar.readData(ar.menderTarReader)
	if err != nil {
		return err
	}
	if ar.manifest != nil {
		notMarked := ar.manifest.FilesNotMarked()
		if len(notMarked) > 0 {
			return fmt.Errorf(
				"Files found in manifest(s), that were not part of artifact: %s",
				strings.Join(notMarked, ", "),
			)
		}
	}

	return nil
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
	return ar.hInfo.GetArtifactProvides()
}

// GetArtifactDepends is version 3 specific.
func (ar *Reader) GetArtifactDepends() *artifact.ArtifactDepends {
	return ar.hInfo.GetArtifactDepends()
}

func (ar *Reader) setInstallers(upd []artifact.UpdateType, augmented bool) error {
	for i, update := range upd {
		// set installer for given update type
		if update.Type == "" {
			if augmented {
				// Just skip empty augmented entries, which
				// means there is no augment override.
				continue
			} else {
				return errors.New("Unexpected empty Payload type")
			}
		} else if w, ok := ar.handlers[update.Type]; ok {
			if augmented {
				var err error
				ar.installers[i], err = w.NewAugmentedInstance(ar.installers[i])
				if err != nil {
					return err
				}
			} else {
				ar.installers[i] = w.NewInstance()
			}
		} else if ar.ForbidUnknownHandlers {
			errstr := fmt.Sprintf(
				"Artifact Payload type '%s' is not supported by this Mender Client",
				update.Type,
			)
			if update.Type == "rootfs-image" {
				return errors.New(
					errstr + ". Ensure that the Mender Client is fully integrated and that the" +
						" RootfsPartA/B configuration variables are set correctly in 'mender.conf'",
				)
			} else {
				return errors.New(
					errstr + ". Make sure the Update Module is installed on the device",
				)
			}
		} else {
			err := ar.makeInstallersForUnknownTypes(update.Type, i, augmented)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (ar *Reader) initializeUpdateStorers() error {
	if len(ar.updateStorers) == len(ar.installers) {
		// Already done.
		return nil
	}

	for i, update := range ar.installers {
		var err error

		ar.updateStorers[i], err = ar.installers[i].NewUpdateStorer(update.GetUpdateType(), i)
		if err != nil {
			return err
		}

		err = ar.updateStorers[i].Initialize(ar.hInfo, ar.augmentedhInfo, ar.installers[i])
		if err != nil {
			return err
		}
	}
	if len(ar.updateStorers) != len(ar.installers) {
		return errors.Errorf(
			"Mismatch between installers/updateStorers lengths (%d != %d). Should not happen!",
			len(ar.updateStorers), len(ar.installers))
	}

	return nil
}

func (ar *Reader) makeInstallersForUnknownTypes(updateType string, i int, augmented bool) error {
	if ar.info.Version < 3 && augmented {
		return errors.New(
			"augmented set when constructing installer version < 3. Should not happen",
		)
	}
	if updateType == "rootfs-image" {
		if augmented {
			ar.installers[i] = handlers.NewAugmentedRootfs(ar.installers[i], "")
		} else {
			ar.installers[i] = handlers.NewRootfsInstaller()
		}
	} else {
		// Use modules for unknown update types. We do this even for
		// artifacts whose version < 3, since this is only used to
		// display information. The Mender client will use
		// ForbidUnknownHandlers, and hence will never get here.
		if augmented {
			ar.installers[i] = handlers.NewAugmentedModuleImage(ar.installers[i], updateType)
		} else {
			ar.installers[i] = handlers.NewModuleImage(updateType)
		}
	}

	return nil
}

func (ar *Reader) buildInstallerIndexedFileLists(
	files []handlers.DataFile,
) ([][]*handlers.DataFile, error) {
	fileLists := make([][](*handlers.DataFile), len(ar.installers))
	for _, file := range files {
		if !strings.HasPrefix(file.Name, "data"+string(os.PathSeparator)) {
			continue
		}
		index, baseName, err := getUpdateNoFromManifestPath(file.Name)
		if err != nil {
			return nil, err
		}
		if index < 0 || index >= len(ar.installers) {
			return nil, fmt.Errorf(
				"File in manifest does not belong to any Payload: %s",
				file.Name,
			)
		}

		fileLists[index] = append(fileLists[index], &handlers.DataFile{Name: baseName})
	}
	return fileLists, nil
}

func (ar *Reader) assignUpdateFiles() error {
	fileLists, err := ar.buildInstallerIndexedFileLists(ar.files)
	if err != nil {
		return err
	}
	augmentedFileLists, err := ar.buildInstallerIndexedFileLists(ar.augmentFiles)
	if err != nil {
		return err
	}

	for n, inst := range ar.installers {
		if err := inst.SetUpdateFiles(fileLists[n]); err != nil {
			return err
		}
		if err := inst.SetUpdateAugmentFiles(augmentedFileLists[n]); err != nil {
			return err
		}
	}

	return nil
}

// should be `headers/0000/file` format
func getUpdateNoFromHeaderPath(path string) (int, error) {
	split := strings.Split(path, string(os.PathSeparator))
	if len(split) < 3 {
		return 0, errors.New("can not get Payload order from tar path")
	}
	return strconv.Atoi(split[1])
}

// should be 0000.tar.gz
func getUpdateNoFromDataPath(comp artifact.Compressor, path string) (int, error) {
	no := strings.TrimSuffix(filepath.Base(path), ".tar"+comp.GetFileExtension())
	return strconv.Atoi(no)
}

// should be data/0000/file
// Returns the index of the data file, converted to int, as well as the
// file name.
func getUpdateNoFromManifestPath(path string) (int, string, error) {
	components := strings.Split(path, string(os.PathSeparator))
	if len(components) != 3 || components[0] != "data" {
		return 0, "", fmt.Errorf("Malformed manifest entry: '%s'", path)
	}
	if len(components[1]) != 4 {
		return 0, "", fmt.Errorf("Manifest entry does not contain four digits: '%s'", path)
	}
	index, err := strconv.Atoi(components[1])
	if err != nil {
		return 0, "", errors.Wrapf(err, "Invalid index in manifest entry: '%s'", path)
	}
	return index, components[2], nil
}

func (ar *Reader) readHeaderUpdate(tr *tar.Reader, hdr *tar.Header, augmented bool) error {
	for {
		// Skip pure directories. mender-artifact doesn't create them,
		// but they may exist if another tool was used to create the
		// artifact.
		if hdr.Typeflag != tar.TypeDir {
			updNo, err := getUpdateNoFromHeaderPath(hdr.Name)
			if err != nil {
				return errors.Wrapf(err, "reader: error getting header Payload number")
			}

			inst, ok := ar.installers[updNo]
			if !ok {
				return errors.Errorf("reader: can not find parser for Payload: %v", hdr.Name)
			}
			if hErr := inst.ReadHeader(tr, hdr.Name, ar.info.Version, augmented); hErr != nil {
				return errors.Wrap(hErr, "reader: can not read header")
			}
		}

		var err error
		hdr, err = getNext(tr)
		if errors.Cause(err) == io.EOF {
			return nil
		} else if err != nil {
			return errors.Wrapf(err,
				"reader: can not read artifact header file: %v", hdr)
		}
	}
}

func (ar *Reader) readNextDataFile(tr *tar.Reader) error {
	hdr, err := getNext(tr)
	if errors.Cause(err) == io.EOF {
		return io.EOF
	} else if err != nil {
		return errors.Wrapf(err, "reader: error reading Payload file: [%v]", hdr)
	}
	if filepath.Dir(hdr.Name) != "data" {
		return errors.New("reader: invalid data file name: " + hdr.Name)
	}
	comp, err := artifact.NewCompressorFromFileName(hdr.Name)
	if err != nil {
		return errors.New("reader: can't get compressor")
	}

	updNo, err := getUpdateNoFromDataPath(comp, hdr.Name)
	if err != nil {
		return errors.Wrapf(err, "reader: error getting data Payload number")
	}
	inst, ok := ar.installers[updNo]
	if !ok {
		return errors.Wrapf(err,
			"reader: can not find parser for parsing data file [%v]", hdr.Name)
	}

	var r io.Reader
	if ar.ProgressReader != nil {
		r = ar.ProgressReader.Wrap(tr, hdr.Size)
	} else {
		r = tr
	}

	return ar.readAndInstall(r, inst, updNo, comp)
}

func (ar *Reader) readData(tr *tar.Reader) error {
	for {
		err := ar.readNextDataFile(tr)
		if errors.Cause(err) == io.EOF {
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

	if !strings.HasPrefix(hdr.Name, elem) {
		return os.ErrInvalid
	}

	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, tr)

	// io.Copy() is not supposed to return EOF, but it can if the EOF is a
	// wrapped error, which can happen if the underlying Reader is a network
	// stream.
	if err != nil && errors.Cause(err) != io.EOF {
		return errors.Wrap(err, "readNext: Failed to copy from tarReader to the buffer")
	}

	// The reason we did not write directly into w above is that if the
	// stream comes from a slow network socket, it may be sufficiently
	// chopped up that the JSON cannot be parsed due to being partial, and
	// the Write() call to the HeaderInfo struct does not like
	// that. Therefore we collect everything in the buffer first, and then
	// write it here in one go.
	_, err = buf.WriteTo(w)
	if err != nil {
		return errors.Wrapf(err, "readNext: Could not parse header element %s", elem)
	}

	return nil
}

func getNext(tr *tar.Reader) (*tar.Header, error) {
	hdr, err := tr.Next()
	if errors.Cause(err) == io.EOF {
		// we've reached end of archive
		return hdr, err
	} else if err != nil {
		return nil, errors.Wrapf(err, "reader: error reading archive")
	}
	return hdr, nil
}

func getDataFile(i handlers.Installer, name string) *handlers.DataFile {
	for _, file := range i.GetUpdateAllFiles() {
		if name == file.Name {
			return file
		}
	}
	return nil
}

func (ar *Reader) readAndInstall(r io.Reader, i handlers.Installer, no int,
	comp artifact.Compressor) error {

	// each data file is stored in tar.gz format
	gz, err := comp.NewReader(r)
	if err != nil {
		return errors.Wrapf(err, "Payload: can not open %s file for reading data",
			comp.GetFileExtension())
	}
	defer gz.Close()

	updateStorer := ar.updateStorers[no]

	tar := tar.NewReader(gz)

	err = updateStorer.PrepareStoreUpdate()
	if err != nil {
		return err
	}

	instErr := ar.readAndInstallDataFiles(tar, i, no, comp, updateStorer)
	err = updateStorer.FinishStoreUpdate()
	if instErr != nil {
		if err != nil {
			return errors.Wrap(instErr, err.Error())
		} else {
			return instErr
		}
	}
	return err
}

func (ar *Reader) readAndInstallDataFiles(tar *tar.Reader, i handlers.Installer,
	no int, comp artifact.Compressor, updateStorer handlers.UpdateStorer) error {

	matcher := regexp.MustCompile(`^[\w\-.,]+$`)
	for {
		hdr, err := tar.Next()
		if errors.Cause(err) == io.EOF {
			break
		} else if err != nil {
			return errors.Wrap(err, "Payload: error reading Artifact file header")
		}

		df := getDataFile(i, hdr.Name)
		if df == nil {
			return errors.Errorf("Payload: can not find data file: %s", hdr.Name)
		}
		matched := matcher.MatchString(filepath.Base(hdr.Name))

		if !matched {
			message := "Payload: data file " + hdr.Name + " contains forbidden characters"
			info := "Only letters, digits and characters in the set \".,_-\" are allowed"
			return fmt.Errorf("%s. %s", message, info)
		}

		// fill in needed data
		info := hdr.FileInfo()
		df.Size = info.Size()
		df.Date = info.ModTime()

		// we need to have a checksum either in manifest file (v2 artifact)
		// or it needs to be pre-filled after reading header
		// all the names of the data files in manifest are written with the
		// archive relative path: data/0000/update.ext4
		if ar.manifest != nil {
			df.Checksum, err = ar.manifest.GetAndMark(filepath.Join(artifact.UpdatePath(no),
				hdr.FileInfo().Name()))
			if err != nil {
				return errors.Wrapf(err, "Payload: checksum missing")
			}
		}
		if df.Checksum == nil {
			return errors.Errorf("Payload: checksum missing for file: %s", hdr.Name)
		}

		// check checksum
		ch := artifact.NewReaderChecksum(tar, df.Checksum)

		if err = updateStorer.StoreUpdate(ch, info); err != nil {
			return errors.Wrapf(err, "Payload: can not install Payload: %s", hdr.Name)
		}

		if err = ch.Verify(); err != nil {
			return errors.Wrap(err, "reader: error reading data")
		}
	}

	return nil
}

func (ar *Reader) GetUpdateStorers() ([]handlers.UpdateStorer, error) {
	err := ar.initializeUpdateStorers()
	if err != nil {
		return nil, err
	}

	length := len(ar.updateStorers)
	list := make([]handlers.UpdateStorer, length)

	for i := range ar.updateStorers {
		if i >= length {
			return nil, errors.New(
				"Update payload numbers are not in strictly increasing numbers from zero",
			)
		}
		list[i] = ar.updateStorers[i]
	}

	return list, nil
}

func (ar *Reader) MergeArtifactDepends() (map[string]interface{}, error) {

	depends := ar.GetArtifactDepends()
	if depends == nil {
		// Artifact version < 3
		return nil, nil
	}

	retMap, err := utils.MarshallStructToMap(depends)
	if err != nil {
		return nil, errors.Wrap(err,
			"error encoding struct as type map")
	}

	// No depends in the augmented header info

	for _, upd := range ar.installers {
		deps, err := upd.GetUpdateDepends()
		if err != nil {
			return nil, err
		} else if deps == nil {
			continue
		}
		for key, val := range deps.Map() {
			// Ensure there are no matching keys
			if _, ok := retMap[key]; ok {
				return nil, fmt.Errorf(
					"Conflicting keys not allowed in the provides parameters. key: %s",
					key,
				)
			}
			retMap[key] = val
		}
	}

	return retMap, nil
}

func (ar *Reader) MergeArtifactProvides() (map[string]string, error) {

	provides := ar.GetArtifactProvides()
	if provides == nil {
		// Artifact version < 3
		return nil, nil
	}
	providesMap, err := utils.MarshallStructToMap(provides)
	if err != nil {
		return nil, errors.Wrap(err,
			"error encoding struct as type map")
	}
	retMap := make(map[string]string)
	for key, value := range providesMap {
		retMap[key] = value.(string)
	}

	// No provides in the augmented header info
	for _, upd := range ar.installers {
		p, err := upd.GetUpdateProvides()
		if err != nil {
			return nil, err
		} else if p == nil {
			continue
		}

		for key, val := range p.Map() {
			// Ensure there are no matching keys
			if _, ok := retMap[key]; ok {
				return nil, fmt.Errorf(
					"Conflicting keys not allowed in the provides parameters. key: %s",
					key,
				)
			}
			retMap[key] = val
		}
	}

	return retMap, nil
}

func (ar *Reader) MergeArtifactClearsProvides() []string {
	var list []string
	for _, inst := range ar.installers {
		list = append(list, inst.GetUpdateClearsProvides()...)
	}
	return list
}

func (ar *Reader) Compressor() artifact.Compressor {
	return ar.compressor
}
