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
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"

	"github.com/pkg/errors"

	"github.com/mendersoftware/mender-artifact/artifact"
)

type ModuleImage struct {
	version    int
	updateType string
	files      [](*DataFile)
	typeInfoV3 *artifact.TypeInfoV3
	metaData   map[string]interface{}

	// If this is an augmented ModuleImage instance, pointer to the
	// original. This is nil, if this instance is the original.
	original ArtifactUpdate

	installerBase
}

func NewModuleImage(updateType string) *ModuleImage {
	mi := ModuleImage{
		version:    3,
		updateType: updateType,
	}
	return &mi
}

func NewAugmentedModuleImage(orig ArtifactUpdate, updateType string) *ModuleImage {
	mi := NewModuleImage(updateType)
	mi.original = orig
	return mi
}

func (img *ModuleImage) NewAugmentedInstance(orig ArtifactUpdate) (Installer, error) {
	newImg := img.NewInstance().(*ModuleImage)
	newImg.original = orig
	return newImg, nil
}

func (img *ModuleImage) NewInstance() Installer {
	newImg := ModuleImage{
		version:       img.version,
		installerBase: img.installerBase,
		updateType:    img.updateType,
	}

	return &newImg
}

func (img *ModuleImage) GetVersion() int {
	return img.version
}

func (img *ModuleImage) GetUpdateType() string {
	return img.updateType
}

func (img *ModuleImage) GetUpdateOriginalType() string {
	if img.original != nil {
		return img.original.GetUpdateType()
	} else {
		return ""
	}
}

func (img *ModuleImage) GetUpdateFiles() [](*DataFile) {
	if img.original == nil {
		return img.files
	} else {
		return img.original.GetUpdateFiles()
	}
}

func (img *ModuleImage) GetUpdateAugmentFiles() [](*DataFile) {
	if img.original == nil {
		// Not an augmented updater.
		return []*DataFile{}
	} else {
		return img.files
	}
}

func (img *ModuleImage) SetUpdateFiles(files [](*DataFile)) error {
	if img.original == nil {
		img.files = files
		return nil
	} else {
		return img.original.SetUpdateFiles(files)
	}
}

func (img *ModuleImage) SetUpdateAugmentFiles(files [](*DataFile)) error {
	if img.original == nil {
		if len(files) > 0 {
			return errors.New("Cannot add augmented files to non-augmented Payload")
		}
	} else {
		img.files = files
	}
	return nil
}

func (img *ModuleImage) GetUpdateAllFiles() [](*DataFile) {
	files := img.GetUpdateFiles()
	augmentFiles := img.GetUpdateAugmentFiles()
	allFiles := make([](*DataFile), 0, len(files)+len(augmentFiles))
	for n := range files {
		allFiles = append(allFiles, files[n])
	}
	for n := range augmentFiles {
		allFiles = append(allFiles, augmentFiles[n])
	}
	return allFiles
}

func (img *ModuleImage) GetUpdateOriginalDepends() artifact.TypeInfoDepends {
	if img.original == nil {
		return img.typeInfoV3.ArtifactDepends
	} else {
		return img.original.GetUpdateOriginalDepends()
	}
}

func (img *ModuleImage) GetUpdateOriginalProvides() artifact.TypeInfoProvides {
	if img.original == nil {
		return img.typeInfoV3.ArtifactProvides
	} else {
		return img.original.GetUpdateOriginalProvides()
	}
}

func (img *ModuleImage) GetUpdateOriginalMetaData() map[string]interface{} {
	if img.original == nil {
		return img.metaData
	} else {
		return img.original.GetUpdateOriginalMetaData()
	}
}

func (img *ModuleImage) GetUpdateOriginalClearsProvides() []string {
	if img.original == nil {
		if img.typeInfoV3 == nil {
			return nil
		}
		return img.typeInfoV3.ClearsArtifactProvides
	} else {
		return img.original.GetUpdateOriginalClearsProvides()
	}
}

func (img *ModuleImage) setUpdateOriginalMetaData(metaData map[string]interface{}) error {
	if img.original == nil {
		img.metaData = metaData
		return nil
	} else {
		return errors.New("Cannot set original meta-data after augmented instance has been created")
	}
}

func (img *ModuleImage) GetUpdateAugmentDepends() artifact.TypeInfoDepends {
	if img.original == nil {
		ret := make(artifact.TypeInfoDepends)
		return ret
	} else {
		return img.typeInfoV3.ArtifactDepends
	}
}

func (img *ModuleImage) GetUpdateAugmentProvides() artifact.TypeInfoProvides {
	if img.original == nil {
		ret := make(artifact.TypeInfoProvides)
		return ret
	} else {
		return img.typeInfoV3.ArtifactProvides
	}
}

func (img *ModuleImage) GetUpdateAugmentMetaData() map[string]interface{} {
	if img.original == nil {
		return make(map[string]interface{})
	} else {
		return img.metaData
	}
}

func (img *ModuleImage) GetUpdateAugmentClearsProvides() []string {
	if img.original == nil {
		return nil
	} else {
		if img.typeInfoV3 == nil {
			return nil
		}
		return img.typeInfoV3.ClearsArtifactProvides
	}
}

func (img *ModuleImage) setUpdateAugmentMetaData(metaData map[string]interface{}) error {
	if img.original == nil {
		if len(metaData) > 0 {
			return errors.New("Tried to set augmented meta-data on a non-augmented Payload")
		}
	} else {
		img.metaData = metaData

		// Check that we can merge original and augmented meta data.
		_, err := mergeJsonStructures(
			img.GetUpdateOriginalMetaData(),
			img.GetUpdateAugmentMetaData(),
		)
		if err != nil {
			return err
		}
	}
	return nil
}

// Copies JSON structures, with the additional restriction that lists cannot
// contain any object or list.
func jsonDeepCopy(src interface{}) (interface{}, error) {
	switch item := src.(type) {
	case map[string]interface{}:
		dst := make(map[string]interface{})
		for key := range item {
			obj, err := jsonDeepCopy(item[key])
			if err != nil {
				return nil, errors.Wrap(err, key)
			}
			dst[key] = obj
		}
		return dst, nil
	case []interface{}:
		dst := make([]interface{}, 0, len(item))
		for n := range item {
			switch item[n].(type) {
			case map[string]interface{}, []interface{}:
				return nil, errors.New("List cannot contain JSON object or list")
			}
			dst = append(dst, item[n])
		}
		return dst, nil
	default:
		return src, nil
	}
}

func mergeJsonStructures(orig, override map[string]interface{}) (map[string]interface{}, error) {
	// TODO: this function needs to take security into account. See the
	// 'PermittedAugmentedHeaders' section of the update modules spec.

	copy, err := jsonDeepCopy(orig)
	if err != nil {
		return nil, err
	}
	merged := copy.(map[string]interface{})
	for key := range override {
		if _, ok := merged[key]; !ok {
			merged[key], err = jsonDeepCopy(override[key])
			if err != nil {
				return nil, errors.Wrap(err, key)
			}
			continue
		}

		switch overrideValue := override[key].(type) {
		case map[string]interface{}:
			var origValue map[string]interface{}
			var ok bool
			if origValue, ok = orig[key].(map[string]interface{}); !ok {
				return nil, fmt.Errorf("%s: Cannot combine JSON object with non-object.", key)
			}
			var err error
			merged[key], err = mergeJsonStructures(origValue, overrideValue)
			if err != nil {
				return nil, errors.Wrap(err, key)
			}
			continue

		case []interface{}:
			if _, ok := orig[key].([]interface{}); !ok {
				return nil, fmt.Errorf("%s: Type conflict: list/non-list", key)
			}
			// fall through to bottom

		default:
			if _, ok := orig[key].(map[string]interface{}); ok {
				return nil, fmt.Errorf("%s: Type conflict: object/non-object", key)
			}
			if _, ok := orig[key].([]interface{}); ok {
				return nil, fmt.Errorf("%s: Type conflict: list/non-list", key)
			}
			// fall through to bottom
		}

		obj, err := jsonDeepCopy(override[key])
		if err != nil {
			return nil, errors.Wrap(err, key)
		}
		merged[key] = obj
	}
	return merged, nil
}

// Transforms JSON from structs into a generic map[string]interface{}
// structure. It's a bit heavy handed, but allows us to use mergeJsonStructures,
// since this function would be complicated to reimplement for specific
// structures.
func transformStructToGenericMap(data interface{}) (map[string]interface{}, error) {
	origJson, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	var generic map[string]interface{}
	err = json.Unmarshal(origJson, &generic)
	if err != nil {
		return nil, err
	}
	return generic, nil
}

// Inverse of the above function.
func transformGenericMapToStruct(src map[string]interface{}, dst interface{}) error {
	origJson, err := json.Marshal(src)
	if err != nil {
		return err
	}
	err = json.Unmarshal(origJson, dst)
	if err != nil {
		return err
	}
	return nil
}

func (img *ModuleImage) GetUpdateDepends() (artifact.TypeInfoDepends, error) {
	orig, err := transformStructToGenericMap(img.GetUpdateOriginalDepends())
	if err != nil {
		return nil, err
	}
	augm, err := transformStructToGenericMap(img.GetUpdateAugmentDepends())
	if err != nil {
		return nil, err
	}

	merged, err := mergeJsonStructures(orig, augm)
	if err != nil {
		return nil, err
	}

	var depends artifact.TypeInfoDepends
	err = transformGenericMapToStruct(merged, &depends)
	if err != nil {
		return nil, err
	}

	return merged, nil
}

func (img *ModuleImage) GetUpdateProvides() (artifact.TypeInfoProvides, error) {
	orig, err := transformStructToGenericMap(img.GetUpdateOriginalProvides())
	if err != nil {
		return nil, err
	}
	augm, err := transformStructToGenericMap(img.GetUpdateAugmentProvides())
	if err != nil {
		return nil, err
	}

	merged, err := mergeJsonStructures(orig, augm)
	if err != nil {
		return nil, err
	}

	var provides artifact.TypeInfoProvides
	err = transformGenericMapToStruct(merged, &provides)
	if err != nil {
		return nil, err
	}

	return provides, nil
}

func (img *ModuleImage) GetUpdateMetaData() (map[string]interface{}, error) {
	merged, err := mergeJsonStructures(
		img.GetUpdateOriginalMetaData(),
		img.GetUpdateAugmentMetaData(),
	)
	if err != nil {
		return nil, err
	}
	return merged, nil
}

func (img *ModuleImage) GetUpdateClearsProvides() []string {
	if img.typeInfoV3 == nil || img.typeInfoV3.ClearsArtifactProvides == nil {
		if img.original != nil {
			return img.original.GetUpdateOriginalClearsProvides()
		} else {
			return nil
		}
	}
	return img.typeInfoV3.ClearsArtifactProvides
}

func (img *ModuleImage) ComposeHeader(args *ComposeHeaderArgs) error {
	if img.version < 3 {
		return errors.New(
			"artifact version < 3 in ModuleImage.ComposeHeader. This is a bug in the application",
		)
	}

	img.typeInfoV3 = args.TypeInfoV3

	path := artifact.UpdateHeaderPath(args.No)

	if err := writeTypeInfoV3(&WriteInfoArgs{
		tarWriter:  args.TarWriter,
		dir:        path,
		typeinfov3: args.TypeInfoV3,
	}); err != nil {
		return errors.Wrap(err, "ComposeHeader: ")
	}

	if len(args.MetaData) > 0 {
		sw := artifact.NewTarWriterStream(args.TarWriter)
		data, err := json.Marshal(args.MetaData)
		if err != nil {
			return errors.Wrap(
				err,
				"MetaData field unmarshalable. This is a bug in the application",
			)
		}
		if err = sw.Write(data, filepath.Join(path, "meta-data")); err != nil {
			return errors.Wrap(err, "Payload: can not store meta-data")
		}
	}
	return nil
}

func (img *ModuleImage) ReadHeader(r io.Reader, path string, version int, augmented bool) error {
	// Check that augmented flag and our image original instance match.
	if augmented != (img.original != nil) {
		return errors.New("ModuleImage.ReadHeader called with unexpected augmented parameter")
	}

	img.version = version
	switch {
	case filepath.Base(path) == "type-info":
		dec := json.NewDecoder(r)
		err := dec.Decode(&img.typeInfoV3)
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
			err = img.setUpdateAugmentMetaData(jsonObj)
		} else {
			err = img.setUpdateOriginalMetaData(jsonObj)
		}
		if err != nil {
			return err
		}
	default:
		return errors.Errorf("Payload: unsupported file: %v", path)
	}
	return nil
}

func (img *ModuleImage) GetUpdateOriginalTypeInfoWriter() io.Writer {
	if img.original != nil {
		return img.original.GetUpdateOriginalTypeInfoWriter()
	} else {
		return img.typeInfoV3
	}
}

func (img *ModuleImage) GetUpdateAugmentTypeInfoWriter() io.Writer {
	if img.original != nil {
		return img.typeInfoV3
	} else {
		return nil
	}
}
