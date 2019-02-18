// Copyright 2019 Northern.tech AS
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
	"io"
	"path/filepath"
	"strings"

	"github.com/mendersoftware/mender-artifact/artifact"
	"github.com/pkg/errors"
)

type GenericV1V2 struct {
	updateType        string
	version           int
	regularHeaderRead bool
	files             map[string](*DataFile)

	installerBase
}

func NewGenericV1V2(t string) *GenericV1V2 {
	return &GenericV1V2{
		updateType: t,
		files:      make(map[string](*DataFile)),
	}
}

func (g *GenericV1V2) GetUpdateFiles() [](*DataFile) {
	list := make([](*DataFile), len(g.files))
	i := 0
	for _, f := range g.files {
		list[i] = f
		i++
	}
	return list
}

func (g *GenericV1V2) SetUpdateFiles(files [](*DataFile)) error {
	// In version 1 and 2, the files list is fetched from the "files"
	// header, so just make sure they match with the manifest.
	check := make(map[string]bool)
	for file := range g.files {
		check[file] = true
	}
	retErr := errors.New("SetUpdateFiles: manifest does not match \"files\" list")
	if len(check) != len(files) {
		return retErr
	}
	for _, file := range files {
		if _, ok := check[file.Name]; !ok {
			return retErr
		}
	}
	return nil
}

func (g *GenericV1V2) GetUpdateAugmentFiles() [](*DataFile) {
	// No such thing for V1 and V2
	return [](*DataFile){}
}

func (g *GenericV1V2) SetUpdateAugmentFiles(files [](*DataFile)) error {
	// No such thing for V1 and V2
	if len(files) != 0 {
		return errors.New("Augmented files not allowed for GenericV1V2 handler")
	}
	return nil
}

func (g *GenericV1V2) GetUpdateAllFiles() [](*DataFile) {
	return g.GetUpdateFiles()
}

func (g *GenericV1V2) GetVersion() int {
	return g.version
}

func (g *GenericV1V2) GetUpdateType() string {
	return g.updateType
}

func (g *GenericV1V2) GetUpdateOriginalType() string {
	return ""
}

func (g *GenericV1V2) GetUpdateDepends() (*artifact.TypeInfoDepends, error) {
	return nil, nil
}

func (g *GenericV1V2) GetUpdateProvides() (*artifact.TypeInfoProvides, error) {
	return nil, nil
}

func (g *GenericV1V2) GetUpdateMetaData() (map[string]interface{}, error) {
	return nil, nil
}

func (g *GenericV1V2) GetUpdateOriginalDepends() *artifact.TypeInfoDepends {
	return nil
}

func (g *GenericV1V2) GetUpdateOriginalProvides() *artifact.TypeInfoProvides {
	return nil
}

func (g *GenericV1V2) GetUpdateOriginalMetaData() map[string]interface{} {
	return nil
}

func (g *GenericV1V2) GetUpdateAugmentDepends() *artifact.TypeInfoDepends {
	return nil
}

func (g *GenericV1V2) GetUpdateAugmentProvides() *artifact.TypeInfoProvides {
	return nil
}

func (g *GenericV1V2) GetUpdateAugmentMetaData() map[string]interface{} {
	return nil
}

func (g *GenericV1V2) NewInstance() Installer {
	newGeneric := NewGenericV1V2(g.updateType)
	newGeneric.installerBase = g.installerBase
	return newGeneric
}

func (g *GenericV1V2) NewAugmentedInstance(orig ArtifactUpdate) (Installer, error) {
	return nil, errors.New("Generic Payload type does not support augment sections")
}

func stripSum(path string) string {
	bName := filepath.Base(path)
	return strings.TrimSuffix(bName, filepath.Ext(bName))
}

func (g *GenericV1V2) ReadHeader(r io.Reader, path string, version int, augmented bool) error {
	g.version = version
	switch {
	case filepath.Base(path) == "files":
		files, err := parseFiles(r)
		if err != nil {
			return err
		}
		for _, f := range files.FileList {
			g.files[filepath.Base(f)] = &DataFile{
				Name: f,
			}
		}

	case match(artifact.HeaderDirectory+"/*/checksums/*", path):
		buf := bytes.NewBuffer(nil)
		if _, err := io.Copy(buf, r); err != nil {
			return errors.Wrapf(err, "update: error reading checksum")
		}
		key := stripSum(path)
		if _, ok := g.files[key]; !ok {
			return errors.Errorf("generic handler: can not find data file: %v", key)
		}
		g.files[key].Checksum = buf.Bytes()

	case filepath.Base(path) == "type-info",
		filepath.Base(path) == "meta-data",
		match(artifact.HeaderDirectory+"/*/signatures/*", path),
		match(artifact.HeaderDirectory+"/*/scripts/pre/*", path),
		match(artifact.HeaderDirectory+"/*/scripts/post/*", path),
		match(artifact.HeaderDirectory+"/*/scripts/check/*", path):
		// TODO: implement when needed
	default:
		return errors.Errorf("update: unsupported file: %v", path)
	}
	return nil
}

func (g *GenericV1V2) GetUpdateOriginalTypeInfoWriter() io.Writer {
	return nil
}

func (g *GenericV1V2) GetUpdateAugmentTypeInfoWriter() io.Writer {
	return nil
}
