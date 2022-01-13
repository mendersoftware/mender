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

package awriter

import (
	"archive/tar"
	"io"

	"github.com/pkg/errors"

	"github.com/mendersoftware/mender-artifact/artifact"
)

var ErrAlreadyExistingSignature = errors.New(
	"The Artifact is already signed, will not overwrite existing signature",
)
var ErrManifestNotFound = errors.New("`manifest` not found. Corrupt Artifact?")

// Special fast-track to just sign, nothing else. This skips all the expensive
// and complicated repacking, and simply adds the manifest.sig file.
func SignExisting(src io.Reader, dst io.Writer, key artifact.Signer, overwrite bool) error {
	var foundManifest bool
	rTar := tar.NewReader(src)
	wTar := tar.NewWriter(dst)
	for {
		header, err := rTar.Next()
		if err == io.EOF {
			break
		} else if err != nil {
			return errors.Wrap(err, "Could not read tar header")
		}

		switch header.Name {
		case "manifest":
			err = signManifestAndOutputSignature(header, rTar, wTar, key)
			if err != nil {
				return err
			}
			foundManifest = true
			continue
		case "manifest.sig":
			if overwrite {
				continue
			} else {
				return ErrAlreadyExistingSignature
			}
		}

		err = wTar.WriteHeader(header)
		if err != nil {
			return errors.Wrap(err, "Could not write tar header")
		}

		_, err = io.Copy(wTar, rTar)
		if err != nil {
			return errors.Wrap(err, "Failed to copy tar body")
		}
	}

	err := wTar.Close()
	if err != nil {
		return errors.Wrap(err, "Could not finalize tar archive")
	}

	if !foundManifest {
		return ErrManifestNotFound
	}

	return nil
}

func signManifestAndOutputSignature(
	header *tar.Header,
	src *tar.Reader,
	dst *tar.Writer,
	key artifact.Signer,
) error {
	buf := make([]byte, header.Size)
	read, err := src.Read(buf)
	if err != nil && err != io.EOF {
		return errors.Wrap(err, "Could not read manifest")
	} else if int64(read) != header.Size {
		return errors.New("Unexpected mismatch between header size and read size")
	}

	err = dst.WriteHeader(header)
	if err != nil {
		return errors.Wrap(err, "Could not write manifest header")
	}
	written, err := dst.Write(buf)
	if err != nil {
		return errors.Wrap(err, "Could not write manifest")
	} else if written != read {
		return errors.New("Could not write entire manifest")
	}

	signedBuf, err := key.Sign(buf)
	if err != nil {
		return errors.Wrap(err, "Could not sign manifest")
	}

	signedHeader := &tar.Header{
		Name: "manifest.sig",
		Size: int64(len(signedBuf)),
		Mode: 0644,
	}

	err = dst.WriteHeader(signedHeader)
	if err != nil {
		return errors.Wrap(err, "Could not write signature header")
	}
	written, err = dst.Write(signedBuf)
	if err != nil {
		return errors.Wrap(err, "Could not write signature")
	} else if written != len(signedBuf) {
		return errors.New("Could not write entire manifest.sig")
	}

	read, err = src.Read(buf)
	if err != io.EOF || read != 0 {
		return errors.New("File bigger than its header size")
	}

	return nil
}
