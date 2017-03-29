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

func NewWriterSigned(w io.Writer, s artifact.Signer) *Writer {
	return &Writer{
		w:      w,
		signer: s,
	}
}

type Updates struct {
	U []handlers.Composer
}

// Iterate through all data files inside `upd` and calculate checksums.
func calcDataHash(s *artifact.ChecksumStore, upd *Updates) error {
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
			s.Add(filepath.Join(artifact.UpdatePath(i), filepath.Base(f.Name)), sum)
		}
	}
	return nil
}

func writeTempHeader(s *artifact.ChecksumStore, devices []string, name string,
	upd *Updates) (*os.File, error) {
	// create temporary header file
	f, err := ioutil.TempFile("", "header")
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

		if err = writeHeader(htw, devices, name, upd); err != nil {
			return errors.Wrapf(err, "writer: error writing header")
		}
		return nil
	}()

	if err != nil {
		return nil, err
	}
	s.Add("header.tar.gz", ch.Checksum())

	return f, nil
}

func writeSignature(tw *tar.Writer, message []byte,
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

func (aw *Writer) WriteArtifact(format string, version int,
	devices []string, name string, upd *Updates) error {

	if version == 1 && aw.signer != nil {
		return errors.New("writer: can not create version 1 signed artifact")
	}

	s := artifact.NewChecksumStore()
	// calculate checksums of all data files
	// we need this regardless of which artifact version we are writing
	if err := calcDataHash(s, upd); err != nil {
		return err
	}

	// write temporary header (we need to know the size before storing in tar)
	tmpHdr, err := writeTempHeader(s, devices, name, upd)
	if err != nil {
		return err
	}
	defer os.Remove(tmpHdr.Name())

	// mender archive writer
	tw := tar.NewWriter(aw.w)
	defer tw.Close()

	// write version file
	inf := artifact.ToStream(&artifact.Info{Version: version, Format: format})
	sa := artifact.NewTarWriterStream(tw)
	if err := sa.Write(inf, "version"); err != nil {
		return errors.Wrapf(err, "writer: can not write version tar header")
	}

	// add checksum of `version`
	if aw.signer != nil {
		ch := artifact.NewWriterChecksum(ioutil.Discard)
		ch.Write(inf)
		s.Add("version", ch.Checksum())
	}

	switch version {
	case 2:
		// write `manifest` file
		sw := artifact.NewTarWriterStream(tw)
		if err := sw.Write(s.GetRaw(), "manifest"); err != nil {
			return errors.Wrapf(err, "writer: can not write manifest stream")
		}

		// write signature
		if err := writeSignature(tw, s.GetRaw(), aw.signer); err != nil {
			return err
		}
		// header is written later on

	case 1:
		// header is written later on

	default:
		return errors.New("writer: unsupported artifact version")
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
	return writeData(tw, upd)
}

func writeHeader(tw *tar.Writer, devices []string, name string,
	updates *Updates) error {
	// store header info
	hInfo := new(artifact.HeaderInfo)

	for _, upd := range updates.U {
		hInfo.Updates =
			append(hInfo.Updates, artifact.UpdateType{Type: upd.GetType()})
	}
	hInfo.CompatibleDevices = devices
	hInfo.ArtifactName = name

	sa := artifact.NewTarWriterStream(tw)
	if err := sa.Write(artifact.ToStream(hInfo), "header-info"); err != nil {
		return errors.New("writer: can not store header-info")
	}

	for i, upd := range updates.U {
		if err := upd.ComposeHeader(tw, i); err != nil {
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
