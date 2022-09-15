// Copyright 2022 Northern.tech AS
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

type BootstrapArtifact struct {
	version    int
	files      [](*DataFile)
	typeInfoV3 *artifact.TypeInfoV3
}

func NewBootstrapArtifact() *BootstrapArtifact {
	return &BootstrapArtifact{
		version: 3,
	}
}

func (b *BootstrapArtifact) GetVersion() int {
	return b.version
}

// Return type of this update, which could be augmented.
func (b *BootstrapArtifact) GetUpdateType() *string {
	return nil
}

// Return type of original (non-augmented) update, if any.
func (b *BootstrapArtifact) GetUpdateOriginalType() *string {
	return nil
}

func (b *BootstrapArtifact) GetUpdateDepends() (artifact.TypeInfoDepends, error) {
	return b.typeInfoV3.ArtifactDepends, nil
}

func (b *BootstrapArtifact) GetUpdateProvides() (artifact.TypeInfoProvides, error) {
	return b.typeInfoV3.ArtifactProvides, nil
}

func (b *BootstrapArtifact) GetUpdateMetaData() (map[string]interface{}, error) {
	return nil, nil
}

func (b *BootstrapArtifact) GetUpdateClearsProvides() []string {
	return b.typeInfoV3.ClearsArtifactProvides
}

// Returns non-augmented (original) data.
func (b *BootstrapArtifact) GetUpdateOriginalDepends() artifact.TypeInfoDepends {
	return nil
}

func (b *BootstrapArtifact) GetUpdateOriginalProvides() artifact.TypeInfoProvides {
	return nil
}

func (b *BootstrapArtifact) GetUpdateOriginalMetaData() map[string]interface{} {
	return nil
}

func (b *BootstrapArtifact) GetUpdateOriginalClearsProvides() []string {
	return nil
}

// Returns augmented data.
func (b *BootstrapArtifact) GetUpdateAugmentDepends() artifact.TypeInfoDepends {
	return nil
}

func (b *BootstrapArtifact) GetUpdateAugmentProvides() artifact.TypeInfoProvides {
	return nil
}

func (b *BootstrapArtifact) GetUpdateAugmentMetaData() map[string]interface{} {
	return nil
}

func (b *BootstrapArtifact) GetUpdateAugmentClearsProvides() []string {
	return nil
}

func (b *BootstrapArtifact) GetUpdateOriginalTypeInfoWriter() io.Writer {
	return nil
}

func (b *BootstrapArtifact) GetUpdateAugmentTypeInfoWriter() io.Writer {
	return nil
}

func (b *BootstrapArtifact) SetUpdateFiles(files [](*DataFile)) error {
	b.files = files
	return nil
}

func (b *BootstrapArtifact) ComposeHeader(args *ComposeHeaderArgs) error {
	b.typeInfoV3 = args.TypeInfoV3

	path := artifact.UpdateHeaderPath(args.No)

	if err := writeTypeInfoV3(&WriteInfoArgs{
		tarWriter:  args.TarWriter,
		dir:        path,
		typeinfov3: args.TypeInfoV3,
	}); err != nil {
		return errors.Wrap(err, "ComposeHeader")
	}
	return nil
}

func (b *BootstrapArtifact) ReadHeader(
	r io.Reader,
	path string,
	version int,
	augmented bool,
) error {
	if version != 3 {
		return errors.New(fmt.Sprintf("version %d not supported for bootstrap artifacts", version))
	}
	b.version = version
	switch {
	case filepath.Base(path) == "type-info":
		dec := json.NewDecoder(r)
		err := dec.Decode(&b.typeInfoV3)
		if err != nil {
			return errors.Wrap(err, "error reading type-info")
		}
	default:
		return errors.Errorf("Payload: unsupported file: %v", path)
	}
	return nil
}

func (b *BootstrapArtifact) NewInstance() Installer {
	newBootstrap := BootstrapArtifact{
		version: b.version,
	}

	return &newBootstrap
}

func (b *BootstrapArtifact) NewAugmentedInstance(orig ArtifactUpdate) (Installer, error) {
	return nil, nil
}

func (b *BootstrapArtifact) NewUpdateStorer(
	updateType *string,
	payloadNum int,
) (UpdateStorer, error) {
	return &devNullUpdateStorer{}, nil
}

func (b *BootstrapArtifact) SetUpdateStorerProducer(producer UpdateStorerProducer) {}

func (b *BootstrapArtifact) GetUpdateAllFiles() [](*DataFile) {
	return nil
}

func (b *BootstrapArtifact) GetUpdateAugmentFiles() [](*DataFile) {
	return nil
}

func (b *BootstrapArtifact) GetUpdateFiles() [](*DataFile) {
	return nil
}

func (b *BootstrapArtifact) SetUpdateAugmentFiles(files [](*DataFile)) error {
	return nil
}
