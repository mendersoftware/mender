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

package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"

	"github.com/pkg/errors"

	"github.com/mendersoftware/mender-artifact/artifact"
)

// Rootfs handles updates of type 'rootfs-image'.
type Rootfs struct {
	version           int
	update            *DataFile
	regularHeaderRead bool

	typeInfoV3 *artifact.TypeInfoV3
	metaData   map[string]interface{}

	// If this is augmented instance: The original instance.
	original ArtifactUpdate

	installerBase
}

func NewRootfsV2(updFile string) *Rootfs {
	uf := &DataFile{
		Name: updFile,
	}
	return &Rootfs{
		update:  uf,
		version: 2,
	}
}

func NewRootfsV3(updFile string) *Rootfs {
	var uf *DataFile
	if updFile != "" {
		uf = &DataFile{
			Name: updFile,
		}
	} else {
		uf = nil
	}
	return &Rootfs{
		update:  uf,
		version: 3,
	}
}

func NewAugmentedRootfs(orig ArtifactUpdate, updFile string) *Rootfs {
	rootfs := NewRootfsV3(updFile)
	rootfs.original = orig
	return rootfs
}

// NewRootfsInstaller is used by the artifact reader to read and install
// rootfs-image update type.
func NewRootfsInstaller() *Rootfs {
	return &Rootfs{}
}

// Copy creates a new instance of Rootfs handler from the existing one.
func (rp *Rootfs) NewInstance() Installer {
	return &Rootfs{
		version:           rp.version,
		installerBase:     rp.installerBase,
		regularHeaderRead: rp.regularHeaderRead,
	}
}

func (rp *Rootfs) NewAugmentedInstance(orig ArtifactUpdate) (Installer, error) {
	if orig.GetVersion() < 3 {
		return nil, errors.New(
			"Rootfs Payload type version < 3 does not support augmented sections.",
		)
	}
	if orig.GetUpdateType() != "rootfs-image" {
		return nil, fmt.Errorf("rootfs-image type cannot be an augmented instance of %s type.",
			orig.GetUpdateType())
	}

	newRootfs := rp.NewInstance().(*Rootfs)
	newRootfs.original = orig
	return newRootfs, nil
}

func (rp *Rootfs) GetVersion() int {
	return rp.version
}

func (rp *Rootfs) ReadHeader(r io.Reader, path string, version int, augmented bool) error {
	rp.version = version
	switch {
	case filepath.Base(path) == "files":
		if version >= 3 {
			return errors.New("\"files\" entry found in version 3 artifact")
		}
		files, err := parseFiles(r)
		if err != nil {
			return err
		} else if len(files.FileList) != 1 {
			return errors.New("Rootfs image does not contain exactly one file")
		}
		err = rp.SetUpdateFiles([]*DataFile{{Name: files.FileList[0]}})
		if err != nil {
			return err
		}
	case filepath.Base(path) == "type-info":
		if rp.version < 3 {
			// This was ignored in pre-v3 versions, so keep ignoring it.
			break
		}
		dec := json.NewDecoder(r)
		err := dec.Decode(&rp.typeInfoV3)
		if err != nil {
			return errors.Wrap(err, "error reading type-info")
		}

	case filepath.Base(path) == "meta-data":
		dec := json.NewDecoder(r)
		var data interface{}
		err := dec.Decode(&data)
		if err == io.EOF {
			break
		} else if err != nil {
			return errors.Wrap(err, "error reading meta-data")
		}
		jsonObj, ok := data.(map[string]interface{})
		if !ok {
			return errors.New("Top level object in meta-data must be a JSON object")
		}
		if augmented {
			err = rp.setUpdateAugmentMetaData(jsonObj)
		} else {
			err = rp.setUpdateOriginalMetaData(jsonObj)
		}
		if err != nil {
			return err
		}
	case match(artifact.HeaderDirectory+"/*/signatures/*", path),
		match(artifact.HeaderDirectory+"/*/scripts/*/*", path):
		if augmented {
			return errors.New("signatures and scripts not allowed in augmented header")
		}
		// TODO: implement when needed
	case match(artifact.HeaderDirectory+"/*/checksums/*", path):
		buf := bytes.NewBuffer(nil)
		if _, err := io.Copy(buf, r); err != nil {
			return errors.Wrap(err, "update: error reading checksum")
		}
		rp.update.Checksum = buf.Bytes()
	default:
		return errors.Errorf("update: unsupported file: %v", path)
	}
	return nil
}

func (rfs *Rootfs) GetUpdateFiles() [](*DataFile) {
	if rfs.original != nil {
		return rfs.original.GetUpdateFiles()
	} else if rfs.update != nil {
		return [](*DataFile){rfs.update}
	} else {
		return [](*DataFile){}
	}
}

func (rfs *Rootfs) SetUpdateFiles(files [](*DataFile)) error {
	if rfs.original != nil {
		if len(files) > 0 && len(rfs.GetUpdateAugmentFiles()) > 0 {
			return errors.New("Rootfs: Cannot handle both augmented and non-augmented update file")
		}
		return rfs.original.SetUpdateFiles(files)
	}

	if len(files) == 0 {
		rfs.update = nil
		return nil
	} else if len(files) != 1 {
		return errors.New("Rootfs: Must provide exactly one update file")
	}

	rfs.update = files[0]
	return nil
}

func (rfs *Rootfs) GetUpdateAugmentFiles() [](*DataFile) {
	if rfs.original != nil && rfs.update != nil {
		return [](*DataFile){rfs.update}
	} else {
		return [](*DataFile){}
	}
}

func (rfs *Rootfs) SetUpdateAugmentFiles(files [](*DataFile)) error {
	if rfs.original == nil {
		if len(files) > 0 {
			return errors.New("Rootfs: Cannot set augmented data file on non-augmented instance.")
		} else {
			return nil
		}
	}

	if len(files) == 0 {
		rfs.update = nil
		return nil
	} else if len(files) != 1 {
		return errors.New("Rootfs: Must provide exactly one update file")
	}

	if len(rfs.GetUpdateFiles()) > 0 {
		return errors.New("Rootfs: Cannot handle both augmented and non-augmented update file")
	}

	rfs.update = files[0]
	return nil
}

func (rfs *Rootfs) GetUpdateAllFiles() [](*DataFile) {
	allFiles := make([]*DataFile, 0, len(rfs.GetUpdateAugmentFiles())+len(rfs.GetUpdateFiles()))
	allFiles = append(allFiles, rfs.GetUpdateFiles()...)
	allFiles = append(allFiles, rfs.GetUpdateAugmentFiles()...)
	return allFiles
}

func (rfs *Rootfs) GetUpdateType() string {
	return "rootfs-image"
}

func (rfs *Rootfs) GetUpdateOriginalType() string {
	return ""
}

func (rfs *Rootfs) GetUpdateDepends() (artifact.TypeInfoDepends, error) {
	return rfs.GetUpdateOriginalDepends(), nil
}

func (rfs *Rootfs) GetUpdateProvides() (artifact.TypeInfoProvides, error) {
	return rfs.GetUpdateOriginalProvides(), nil
}

func (rfs *Rootfs) GetUpdateMetaData() (map[string]interface{}, error) {
	// No metadata for rootfs update type.
	return rfs.GetUpdateOriginalMetaData(), nil
}

func (rfs *Rootfs) GetUpdateClearsProvides() []string {
	if rfs.typeInfoV3 == nil {
		return nil
	}
	if rfs.typeInfoV3.ClearsArtifactProvides == nil && rfs.original != nil {
		return rfs.original.GetUpdateOriginalClearsProvides()
	}
	return rfs.typeInfoV3.ClearsArtifactProvides
}

func (rfs *Rootfs) setUpdateOriginalMetaData(jsonObj map[string]interface{}) error {
	if rfs.original != nil {
		return errors.New("setUpdateOriginalMetaData() called on non-original instance.")
	} else {
		rfs.metaData = jsonObj
	}
	return nil
}

func (rfs *Rootfs) setUpdateAugmentMetaData(jsonObj map[string]interface{}) error {
	if rfs.original == nil {
		return errors.New("Called setUpdateAugmentMetaData() on non-augment instance")
	}
	rfs.metaData = jsonObj
	return nil
}

func (rfs *Rootfs) GetUpdateOriginalDepends() artifact.TypeInfoDepends {
	if rfs.typeInfoV3 == nil {
		return nil
	}
	return rfs.typeInfoV3.ArtifactDepends
}

func (rfs *Rootfs) GetUpdateOriginalProvides() artifact.TypeInfoProvides {
	if rfs.typeInfoV3 == nil {
		return nil
	}
	return rfs.typeInfoV3.ArtifactProvides
}

func (rfs *Rootfs) GetUpdateOriginalMetaData() map[string]interface{} {
	if rfs.original != nil {
		return rfs.original.GetUpdateOriginalMetaData()
	} else {
		return rfs.metaData
	}
}

func (rfs *Rootfs) GetUpdateOriginalClearsProvides() []string {
	if rfs.original == nil {
		if rfs.typeInfoV3 == nil {
			return nil
		}
		return rfs.typeInfoV3.ClearsArtifactProvides
	} else {
		return rfs.original.GetUpdateOriginalClearsProvides()
	}
}

func (rfs *Rootfs) GetUpdateAugmentDepends() artifact.TypeInfoDepends {
	return nil
}

func (rfs *Rootfs) GetUpdateAugmentProvides() artifact.TypeInfoProvides {
	return nil
}

func (rfs *Rootfs) GetUpdateAugmentMetaData() map[string]interface{} {
	if rfs.original == nil {
		return nil
	} else {
		return rfs.metaData
	}
}

func (rfs *Rootfs) GetUpdateAugmentClearsProvides() []string {
	if rfs.original == nil {
		return nil
	} else {
		if rfs.typeInfoV3 == nil {
			return nil
		}
		return rfs.typeInfoV3.ClearsArtifactProvides
	}
}

func (rfs *Rootfs) ComposeHeader(args *ComposeHeaderArgs) error {

	path := artifact.UpdateHeaderPath(args.No)

	switch rfs.version {
	case 1, 2:
		// first store files
		if err := writeFiles(args.TarWriter, []string{filepath.Base(rfs.update.Name)},
			path); err != nil {
			return err
		}

		if err := writeTypeInfo(args.TarWriter, "rootfs-image", path); err != nil {
			return err
		}

	case 3:
		if args.Augmented {
			// Remove the typeinfov3.provides, as this should not be written in the
			// augmented-header.
			if args.TypeInfoV3 != nil {
				args.TypeInfoV3.ArtifactProvides = nil
			}
		}

		if err := writeTypeInfoV3(&WriteInfoArgs{
			tarWriter:  args.TarWriter,
			dir:        path,
			typeinfov3: args.TypeInfoV3,
		}); err != nil {
			return errors.Wrap(err, "ComposeHeader")
		}
	default:
		return fmt.Errorf("ComposeHeader: rootfs-version %d not supported", rfs.version)

	}

	// store empty meta-data
	// the file needs to be a part of artifact even if this one is empty
	if len(args.MetaData) != 0 {
		return errors.New(
			"MetaData not empty in Rootfs.ComposeHeader. This is a bug in the application.",
		)
	}
	sw := artifact.NewTarWriterStream(args.TarWriter)
	if err := sw.Write(nil, filepath.Join(path, "meta-data")); err != nil {
		return errors.Wrap(err, "Payload: can not store meta-data")
	}

	return nil
}

func (rfs *Rootfs) GetUpdateOriginalTypeInfoWriter() io.Writer {
	// We don't use the type-info information with rootfs payloads.
	return nil
}

func (rfs *Rootfs) GetUpdateAugmentTypeInfoWriter() io.Writer {
	// We don't use the type-info information with rootfs payloads.
	return nil
}
