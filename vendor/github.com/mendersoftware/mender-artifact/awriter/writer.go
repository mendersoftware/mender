// Copyright 2020 Northern.tech AS
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
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"

	"github.com/mendersoftware/mender-artifact/artifact"
	"github.com/mendersoftware/mender-artifact/handlers"
	"github.com/pkg/errors"
)

// Writer provides on the fly writing of artifacts metadata file used by
// the Mender client and the server.
type Writer struct {
	w      io.Writer // underlying writer
	signer artifact.Signer
	c      artifact.Compressor
}

func NewWriter(w io.Writer, c artifact.Compressor) *Writer {
	return &Writer{
		w: w,
		c: c,
	}
}

func NewWriterSigned(w io.Writer, c artifact.Compressor, manifestChecksumStore artifact.Signer) *Writer {
	return &Writer{
		w:      w,
		c:      c,
		signer: manifestChecksumStore,
	}
}

type Updates struct {
	// Both of these are indexed the same, so Augment at index X corresponds
	// to Update at index X.
	Updates  []handlers.Composer
	Augments []handlers.Composer
}

// Iterate through all data files inside `upd` and calculate checksums.
func calcDataHash(manifestChecksumStore *artifact.ChecksumStore, upd *Updates, augmented bool) error {
	var updates []handlers.Composer
	if augmented {
		updates = upd.Augments
	} else {
		updates = upd.Updates
	}
	for i, u := range updates {
		var files [](*handlers.DataFile)
		if augmented {
			if u == nil {
				// Can happen if there is no augmented part.
				continue
			}
			files = u.GetUpdateAugmentFiles()
		} else {
			files = u.GetUpdateFiles()
		}
		for _, f := range files {
			ch := artifact.NewWriterChecksum(ioutil.Discard)
			df, err := os.Open(f.Name)
			if err != nil {
				return errors.Wrapf(err, "writer: can not open data file: %s", f.Name)
			}
			defer df.Close()
			if _, err := io.Copy(ch, df); err != nil {
				return errors.Wrapf(err, "writer: can not calculate checksum: %s", f.Name)
			}
			sum := ch.Checksum()
			f.Checksum = sum
			manifestChecksumStore.Add(filepath.Join(artifact.UpdatePath(i), filepath.Base(f.Name)), sum)
		}
	}
	return nil
}

// writeTempHeader can write both the standard and the augmented header
func writeTempHeader(c artifact.Compressor, manifestChecksumStore *artifact.ChecksumStore,
	name string, args *WriteArtifactArgs, augmented bool) (*os.File, error) {

	// create temporary header file
	f, err := ioutil.TempFile("", name)
	if err != nil {
		return nil, errors.New("writer: can not create temporary header file")
	}

	ch := artifact.NewWriterChecksum(f)
	// use function to make sure to close gz and tar before
	// calculating checksum
	err = func() error {
		gz, err := c.NewWriter(ch)
		if err != nil {
			return errors.Wrapf(err, "writer: can not open compressor")
		}
		defer gz.Close()

		htw := tar.NewWriter(gz)
		defer htw.Close()

		// Header differs in version 3 from version 2.
		if err = writeHeader(htw, args, augmented); err != nil {
			return errors.Wrapf(err, "writer: error writing header")
		}
		return nil
	}()

	if err != nil {
		os.Remove(f.Name())
		return nil, err
	}
	fullName := fmt.Sprintf("%s.tar%s", name, c.GetFileExtension())
	manifestChecksumStore.Add(fullName, ch.Checksum())

	return f, nil
}

func WriteSignature(tw *tar.Writer, message []byte,
	signer artifact.Signer) error {
	if signer == nil {
		return nil
	}

	sig, err := signer.Sign(message)
	if err != nil {
		return errors.Wrap(err, "writer: can not sign artifact")
	}
	sw := artifact.NewTarWriterStream(tw)
	if err := sw.Write(sig, "manifest.sig"); err != nil {
		return errors.Wrap(err, "writer: can not tar signature")
	}
	return nil
}

type WriteArtifactArgs struct {
	Format            string
	Version           int
	Devices           []string
	Name              string
	Updates           *Updates
	Scripts           *artifact.Scripts
	Depends           *artifact.ArtifactDepends
	Provides          *artifact.ArtifactProvides
	TypeInfoV3        *artifact.TypeInfoV3
	MetaData          map[string]interface{} // Generic JSON
	AugmentTypeInfoV3 *artifact.TypeInfoV3
	AugmentMetaData   map[string]interface{} // Generic JSON
}

func (aw *Writer) WriteArtifact(args *WriteArtifactArgs) (err error) {

	if args.Version == 1 {
		return errors.New("writer: The Mender-Artifact version 1 is outdated. Refusing to create artifact.")
	}

	if !(args.Version == 2 || args.Version == 3) {
		return errors.New("Unsupported artifact version")
	}

	if args.Version == 3 {
		return aw.writeArtifactV3(args)
	}

	return aw.writeArtifactV2(args)
}

func (aw *Writer) writeArtifactV2(args *WriteArtifactArgs) error {

	manifestChecksumStore := artifact.NewChecksumStore()
	// calculate checksums of all data files
	// we need this regardless of which artifact version we are writing
	if err := calcDataHash(manifestChecksumStore, args.Updates, false); err != nil {
		return err
	}
	// mender archive writer
	tw := tar.NewWriter(aw.w)
	defer tw.Close()

	tmpHdr, err := writeTempHeader(aw.c, manifestChecksumStore, "header", args, false)

	if err != nil {
		return err
	}
	defer os.Remove(tmpHdr.Name())

	// write version file
	inf, err := artifact.ToStream(&artifact.Info{Version: args.Version, Format: args.Format})
	if err != nil {
		return err
	}
	sa := artifact.NewTarWriterStream(tw)
	if err := sa.Write(inf, "version"); err != nil {
		return errors.Wrapf(err, "writer: can not write version tar header")
	}

	if err = writeManifestVersion(args.Version, aw.signer, tw, manifestChecksumStore, nil, inf); err != nil {
		return errors.Wrap(err, "WriteArtifact")
	}

	// write header
	if _, err := tmpHdr.Seek(0, 0); err != nil {
		return errors.Wrapf(err, "writer: error preparing tmp header for writing")
	}
	fw := artifact.NewTarWriterFile(tw)
	if err := fw.Write(tmpHdr, "header.tar"+aw.c.GetFileExtension()); err != nil {
		return errors.Wrapf(err, "writer: can not tar header")
	}

	// write data files
	return writeData(tw, aw.c, args.Updates)
}

func (aw *Writer) writeArtifactV3(args *WriteArtifactArgs) (err error) {
	augmentedDataPresent := (len(args.Updates.Augments) > 0)

	// Holds the checksum for the update, and 'header.tar.gz', and the 'version' file.
	manifestChecksumStore := artifact.NewChecksumStore()

	// Holds the checksum for 'header-augment.tar.gz'.
	augManifestChecksumStore := artifact.NewChecksumStore()
	if err := calcDataHash(manifestChecksumStore, args.Updates, false); err != nil {
		return err
	}
	if augmentedDataPresent {
		if err := calcDataHash(augManifestChecksumStore, args.Updates, true); err != nil {
			return err
		}
	}
	tw := tar.NewWriter(aw.w)
	defer tw.Close()

	// The header in version 3 will have the original rootfs-checksum in type-info!
	tmpHdr, err := writeTempHeader(aw.c, manifestChecksumStore, "header", args, false)
	if err != nil {
		return errors.Wrap(err, "writeArtifactV3: writing header")
	}
	defer os.Remove(tmpHdr.Name())

	var tmpAugHdr *os.File
	if augmentedDataPresent {
		tmpAugHdr, err = writeTempHeader(aw.c, augManifestChecksumStore, "header-augment", args, true)
		if err != nil {
			return errors.Wrap(err, "writeArtifactV3: writing augmented header")
		}
		defer os.Remove(tmpAugHdr.Name())
	}

	////////////////////////
	// write version file //
	////////////////////////
	inf, err := artifact.ToStream(&artifact.Info{Version: args.Version, Format: args.Format})
	if err != nil {
		return err
	}
	sa := artifact.NewTarWriterStream(tw)
	if err := sa.Write(inf, "version"); err != nil {
		return errors.Wrapf(err, "writer: can not write version tar header")
	}

	////////////////////////////
	// Write manifest         //
	// Write manifest.sig     //
	// Write manifest-augment //
	////////////////////////////
	if err = writeManifestVersion(args.Version, aw.signer, tw, manifestChecksumStore, augManifestChecksumStore, inf); err != nil {
		return errors.Wrap(err, "WriteArtifact")
	}

	////////////////////
	// Write header   //
	////////////////////
	if _, err := tmpHdr.Seek(0, 0); err != nil {
		return errors.Wrapf(err, "writer: error preparing tmp header for writing")
	}
	fw := artifact.NewTarWriterFile(tw)
	if err := fw.Write(tmpHdr, "header.tar"+aw.c.GetFileExtension()); err != nil {
		return errors.Wrapf(err, "writer: can not tar header")
	}

	/////////////////////////////
	// Write augmented-header  //
	/////////////////////////////
	if augmentedDataPresent {
		if _, err := tmpAugHdr.Seek(0, 0); err != nil {
			return errors.Wrapf(err, "writer: error preparing tmp augment-header for writing")
		}
		fw = artifact.NewTarWriterFile(tw)
		if err := fw.Write(tmpAugHdr, "header-augment.tar"+aw.c.GetFileExtension()); err != nil {
			return errors.Wrapf(err, "writer: can not tar augmented-header")
		}
	}

	//////////////////////////
	// Write the datafiles  //
	//////////////////////////
	return writeData(tw, aw.c, args.Updates)
}

// writeArtifactVersion writes version specific artifact records.
func writeManifestVersion(version int, signer artifact.Signer, tw *tar.Writer, manifestChecksumStore, augmanChecksumStore *artifact.ChecksumStore, artifactInfoStream []byte) error {
	switch version {
	case 2:
		// add checksum of `version`
		ch := artifact.NewWriterChecksum(ioutil.Discard)
		ch.Write(artifactInfoStream)
		manifestChecksumStore.Add("version", ch.Checksum())
		// write `manifest` file
		sw := artifact.NewTarWriterStream(tw)
		if err := sw.Write(manifestChecksumStore.GetRaw(), "manifest"); err != nil {
			return errors.Wrapf(err, "writer: can not write manifest stream")
		}
		// write signature
		if err := WriteSignature(tw, manifestChecksumStore.GetRaw(), signer); err != nil {
			return err
		}
	case 3:
		// Add checksum of `version`.
		ch := artifact.NewWriterChecksum(ioutil.Discard)
		ch.Write(artifactInfoStream)
		manifestChecksumStore.Add("version", ch.Checksum())
		// Write `manifest` file.
		sw := artifact.NewTarWriterStream(tw)
		if err := sw.Write(manifestChecksumStore.GetRaw(), "manifest"); err != nil {
			return errors.Wrapf(err, "writer: can not write manifest stream")
		}
		// Write signature.
		if err := WriteSignature(tw, manifestChecksumStore.GetRaw(), signer); err != nil {
			return err
		}
		// Write the augmented manifest, if any.
		if len(augmanChecksumStore.GetRaw()) > 0 {
			sw = artifact.NewTarWriterStream(tw)
			if err := sw.Write(augmanChecksumStore.GetRaw(), "manifest-augment"); err != nil {
				return errors.Wrapf(err, "writer: can not write manifest stream")
			}
		}
	default:
		return fmt.Errorf("writer: unsupported artifact version: %d", version)
	}
	return nil
}

func writeScripts(tw *tar.Writer, scr *artifact.Scripts) error {
	sw := artifact.NewTarWriterFile(tw)
	for _, script := range scr.Get() {
		f, err := os.Open(script)
		if err != nil {
			return errors.Wrapf(err, "writer: can not open script file: %s", script)
		}
		defer f.Close()

		if err :=
			sw.Write(f, filepath.Join("scripts", filepath.Base(script))); err != nil {
			return errors.Wrapf(err, "writer: can not store script: %s", script)
		}
	}
	return nil
}

func extractUpdateTypes(updates []handlers.Composer) []artifact.UpdateType {
	u := []artifact.UpdateType{}
	for _, upd := range updates {
		u = append(u, artifact.UpdateType{upd.GetUpdateType()})
	}
	return u
}

func writeHeader(tarWriter *tar.Writer, args *WriteArtifactArgs, augmented bool) error {
	var composers []handlers.Composer
	if augmented {
		composers = args.Updates.Augments
	} else {
		composers = args.Updates.Updates
	}
	if len(composers) == 0 {
		return errors.New("writeHeader: No Payloads added")
	}

	// store header info
	var hInfo artifact.WriteValidator
	upds := extractUpdateTypes(composers)
	switch args.Version {
	case 1, 2:
		hInfo = artifact.NewHeaderInfo(args.Name, upds, args.Devices)
	case 3:
		hInfo = artifact.NewHeaderInfoV3(upds, args.Provides, args.Depends)
	}

	sa := artifact.NewTarWriterStream(tarWriter)
	stream, err := artifact.ToStream(hInfo)
	if err != nil {
		return errors.Wrap(err, "writeHeader")
	}
	if err := sa.Write(stream, "header-info"); err != nil {
		return errors.New("writer: can not store header-info")
	}

	// write scripts
	if !augmented && args.Scripts != nil {
		if err := writeScripts(tarWriter, args.Scripts); err != nil {
			return err
		}
	}

	for i, upd := range composers {
		// TODO: We only have one `args` variable here, so making more
		// than one update is kind of useless. Should probably be made
		// into a list as well.
		composeHeaderArgs := handlers.ComposeHeaderArgs{
			TarWriter: tarWriter,
			No:        i,
			Version:   args.Version,
			Augmented: augmented,
		}
		if augmented {
			composeHeaderArgs.TypeInfoV3 = args.AugmentTypeInfoV3
			composeHeaderArgs.MetaData = args.AugmentMetaData
		} else {
			composeHeaderArgs.TypeInfoV3 = args.TypeInfoV3
			composeHeaderArgs.MetaData = args.MetaData
		}
		if err := upd.ComposeHeader(&composeHeaderArgs); err != nil {
			return errors.Wrapf(err, "writer: error composing header")
		}
	}
	return nil
}

func writeData(tw *tar.Writer, comp artifact.Compressor, updates *Updates) error {
	for i, upd := range updates.Updates {
		var augment handlers.Composer = nil
		if i < len(updates.Augments) {
			augment = updates.Augments[i]
		}
		if err := writeOneDataTar(tw, comp, i, upd, augment); err != nil {
			return errors.Wrapf(err, "writer: error writing data files")
		}
	}
	return nil
}

func writeOneDataTar(tw *tar.Writer, comp artifact.Compressor, no int,
	baseUpdate, augmentUpdate handlers.Composer) error {

	f, ferr := ioutil.TempFile("", "data")
	if ferr != nil {
		return errors.New("Payload: can not create temporary data file")
	}
	defer os.Remove(f.Name())

	err := func() error {
		gz, err := comp.NewWriter(f)
		if err != nil {
			return errors.Wrap(err, "Could not open compressor")
		}
		defer gz.Close()

		tarw := tar.NewWriter(gz)
		defer tarw.Close()

		for _, file := range baseUpdate.GetUpdateFiles() {
			err = writeOneDataFile(tarw, file)
			if err != nil {
				return err
			}
		}
		if augmentUpdate == nil {
			return nil
		}

		for _, file := range augmentUpdate.GetUpdateAugmentFiles() {
			err = writeOneDataFile(tarw, file)
			if err != nil {
				return err
			}
		}
		return nil
	}()

	if err != nil {
		return err
	}

	if _, err = f.Seek(0, 0); err != nil {
		return errors.Wrap(err, "Payload: can not reset file position")
	}

	dfw := artifact.NewTarWriterFile(tw)
	if err = dfw.Write(f, artifact.UpdateDataPath(no)+comp.GetFileExtension()); err != nil {
		return errors.Wrap(err, "Payload: can not write tar data header")
	}
	return nil
}

func writeOneDataFile(tarw *tar.Writer, file *handlers.DataFile) error {
	matched, err := regexp.MatchString(`^[\w\-.,]+$`, filepath.Base(file.Name))

	if err != nil {
		return errors.Wrapf(err, "Payload: invalid regular expression pattern")
	}

	if !matched {
		message := "Payload: data file " + file.Name + " contains forbidden characters"
		info := "Only letters, digits and characters in the set \".,_-\" are allowed"
		return fmt.Errorf("%s. %s", message, info)
	}

	df, err := os.Open(file.Name)
	if err != nil {
		return errors.Wrapf(err, "Payload: can not open data file: %s", file.Name)
	}
	fw := artifact.NewTarWriterFile(tarw)
	if err := fw.Write(df, filepath.Base(file.Name)); err != nil {
		df.Close()
		return errors.Wrapf(err,
			"Payload: can not write tar temp data header: %v", file)
	}
	df.Close()
	return nil
}
