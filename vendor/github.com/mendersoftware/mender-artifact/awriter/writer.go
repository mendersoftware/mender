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

package awriter

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/mendersoftware/mender-artifact/artifact"
	"github.com/mendersoftware/mender-artifact/handlers"
	"github.com/pkg/errors"
)

// Writer provides on the fly writing of artifacts metadata file used by
// the Mender client and the server.
type Writer struct {
	w      io.Writer // underlying writer
	signer artifact.Signer
}

func NewWriter(w io.Writer) *Writer {
	return &Writer{
		w: w,
	}
}

func NewWriterSigned(w io.Writer, manifestChecksumStore artifact.Signer) *Writer {
	return &Writer{
		w:      w,
		signer: manifestChecksumStore,
	}
}

type Updates struct {
	U []handlers.Composer
}

// Iterate through all data files inside `upd` and calculate checksums.
func calcDataHash(manifestChecksumStore *artifact.ChecksumStore, upd *Updates) error {
	for i, u := range upd.U {
		for _, f := range u.GetUpdateFiles() {
			ch := artifact.NewWriterChecksum(ioutil.Discard)
			df, err := os.Open(f.Name)
			if err != nil {
				return errors.Wrapf(err, "writer: can not open data file: %v", f)
			}
			defer df.Close()
			if _, err := io.Copy(ch, df); err != nil {
				return errors.Wrapf(err, "writer: can not calculate checksum: %v", f)
			}
			sum := ch.Checksum()
			f.Checksum = sum
			manifestChecksumStore.Add(filepath.Join(artifact.UpdatePath(i), filepath.Base(f.Name)), sum)
		}
	}
	return nil
}

// writeTempHeader can write both the standard and the augmented header by passing in the appropriate `writeHeaderVersion`
// function. (writeHeader/writeAugmentedHeader)
func writeTempHeader(manifestChecksumStore *artifact.ChecksumStore, name string, writeHeaderVersion func(tarWriter *tar.Writer, args *WriteArtifactArgs) error, args *WriteArtifactArgs) (*os.File, error) {
	// create temporary header file
	f, err := ioutil.TempFile("", name)
	if err != nil {
		return nil, errors.New("writer: can not create temporary header file")
	}

	ch := artifact.NewWriterChecksum(f)
	// use function to make sure to close gz and tar before
	// calculating checksum
	err = func() error {
		gz := gzip.NewWriter(ch)
		defer gz.Close()

		htw := tar.NewWriter(gz)
		defer htw.Close()

		// Header differs in version 3 from version 1 and 2.
		if err = writeHeaderVersion(htw, args); err != nil {
			return errors.Wrapf(err, "writer: error writing header")
		}
		return nil
	}()

	if err != nil {
		return nil, err
	}
	manifestChecksumStore.Add(name+".tar.gz", ch.Checksum())

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
	Format     string
	Version    int
	Devices    []string
	Name       string
	Updates    *Updates
	Scripts    *artifact.Scripts
	Depends    *artifact.ArtifactDepends
	Provides   *artifact.ArtifactProvides
	TypeInfoV3 *artifact.TypeInfoV3
}

func (aw *Writer) WriteArtifact(args *WriteArtifactArgs) (err error) {
	if !(args.Version == 1 || args.Version == 2 || args.Version == 3) {
		return errors.New("Unsupported artifact version")
	}

	if args.Version == 1 && aw.signer != nil {
		return errors.New("writer: can not create version 1 signed artifact")
	}

	if args.Version == 3 {
		return aw.writeArtifactV3(args)
	}

	return aw.writeArtifactV1V2(args)
}

func (aw *Writer) writeArtifactV1V2(args *WriteArtifactArgs) error {

	manifestChecksumStore := artifact.NewChecksumStore()
	// calculate checksums of all data files
	// we need this regardless of which artifact version we are writing
	if err := calcDataHash(manifestChecksumStore, args.Updates); err != nil {
		return err
	}
	// mender archive writer
	tw := tar.NewWriter(aw.w)
	defer tw.Close()

	tmpHdr, err := writeTempHeader(manifestChecksumStore, "header", writeHeader, args)

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
	if err := fw.Write(tmpHdr, "header.tar.gz"); err != nil {
		return errors.Wrapf(err, "writer: can not tar header")
	}

	// write data files
	return writeData(tw, args.Updates)
}

func (aw *Writer) writeArtifactV3(args *WriteArtifactArgs) (err error) {

	// Holds the checksum for the update, and 'header.tar.gz', and the 'version' file.
	manifestChecksumStore := artifact.NewChecksumStore()

	// Holds the checksum for 'header-augment.tar.gz'.
	augManifestChecksumStore := artifact.NewChecksumStore()
	if err := calcDataHash(manifestChecksumStore, args.Updates); err != nil {
		return err
	}
	tw := tar.NewWriter(aw.w)
	defer tw.Close()

	// The header in version 3 will have the original rootfs-checksum in type-info!
	tmpHdr, err := writeTempHeader(manifestChecksumStore, "header", writeHeader, args)
	if err != nil {
		return errors.Wrap(err, "writeArtifactV3: writeHeader")
	}
	defer os.Remove(tmpHdr.Name())
	tmpAugHdr, err := writeTempHeader(augManifestChecksumStore, "header-augment", writeAugmentedHeader, args)
	if err != nil {
		return errors.Wrap(err, "writeArtifactV3: writeAugmentedHeader")
	}
	defer os.Remove(tmpAugHdr.Name())

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
	if err := fw.Write(tmpHdr, "header.tar.gz"); err != nil {
		return errors.Wrapf(err, "writer: can not tar header")
	}

	/////////////////////////////
	// Write augmented-header  //
	/////////////////////////////
	if _, err := tmpAugHdr.Seek(0, 0); err != nil {
		return errors.Wrapf(err, "writer: error preparing tmp augment-header for writing")
	}
	fw = artifact.NewTarWriterFile(tw)
	if err := fw.Write(tmpAugHdr, "header-augment.tar.gz"); err != nil {
		return errors.Wrapf(err, "writer: can not tar augmented-header")
	}

	//////////////////////////
	// Write the datafiles  //
	//////////////////////////
	return writeData(tw, args.Updates)
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
		// Write the augmented manifest.
		sw = artifact.NewTarWriterStream(tw)
		if err := sw.Write(augmanChecksumStore.GetRaw(), "manifest-augment"); err != nil {
			return errors.Wrapf(err, "writer: can not write manifest stream")
		}
	case 1:

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

func extractUpdateTypes(updates *Updates) []artifact.UpdateType {
	u := []artifact.UpdateType{}
	for _, upd := range updates.U {
		u = append(u, artifact.UpdateType{upd.GetType()})
	}
	return u
}

func writeHeader(tarWriter *tar.Writer, args *WriteArtifactArgs) error {
	// store header info
	var hInfo artifact.WriteValidator
	upds := extractUpdateTypes(args.Updates)
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
	if args.Scripts != nil {
		if err := writeScripts(tarWriter, args.Scripts); err != nil {
			return err
		}
	}

	for i, upd := range args.Updates.U {
		if err := upd.ComposeHeader(&handlers.ComposeHeaderArgs{TarWriter: tarWriter, No: i, Version: args.Version, Augmented: false, TypeInfoV3: args.TypeInfoV3}); err != nil {
			return errors.Wrapf(err, "writer: error composing header")
		}
	}
	return nil
}

// writeAugmentedHeader writes the augmented header with the restrictions:
// header-info: Can only contain the `updates` field.
// type-info: Can only contain artifact-depends which has the `type` and  `rootfs_image_checksum` fields.
func writeAugmentedHeader(tarWriter *tar.Writer, args *WriteArtifactArgs) error {
	hInfo := new(artifact.AugmentedHeaderInfoV3)
	for _, upd := range args.Updates.U {
		hInfo.Updates =
			append(hInfo.Updates, artifact.UpdateType{Type: upd.GetType()})
	}
	sa := artifact.NewTarWriterStream(tarWriter)
	stream, err := artifact.ToStream(hInfo)
	if err != nil {
		return err
	}
	if err := sa.Write(stream, "header-info"); err != nil {
		return errors.New("writer: can not store header-info")
	}

	for i, upd := range args.Updates.U {
		if err := upd.ComposeHeader(&handlers.ComposeHeaderArgs{TarWriter: tarWriter, No: i, Augmented: true, TypeInfoV3: args.TypeInfoV3}); err != nil {
			return errors.Wrapf(err, "writer: error processing update directory")
		}
	}
	return nil
}

func writeData(tw *tar.Writer, updates *Updates) error {
	for i, upd := range updates.U {
		if err := upd.ComposeData(tw, i); err != nil {
			return errors.Wrapf(err, "writer: error writing data files")
		}
	}
	return nil
}
