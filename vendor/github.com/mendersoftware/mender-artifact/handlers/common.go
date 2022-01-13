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
	"archive/tar"
	"encoding/json"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"github.com/pkg/errors"

	"github.com/mendersoftware/mender-artifact/artifact"
)

// DataFile represents the minimum set of attributes each update file
// must contain. Some of those might be empty though for specific update types.
type DataFile struct {
	// name of the update file
	Name string
	// size of the update file
	Size int64
	// last modification time
	Date time.Time
	// checksum of the update file
	Checksum []byte
}

type ComposeHeaderArgs struct {
	TarWriter  *tar.Writer
	No         int
	Version    int
	Augmented  bool
	TypeInfoV3 *artifact.TypeInfoV3
	MetaData   map[string]interface{} // Generic JSON
	Files      []string
}

type ArtifactUpdateHeaders interface {
	GetVersion() int

	// Return type of this update, which could be augmented.
	GetUpdateType() string

	// Return type of original (non-augmented) update, if any.
	GetUpdateOriginalType() string

	// Returns merged data of non-augmented and augmented data, where the
	// latter overrides the former. Returns error if they cannot be merged.
	GetUpdateDepends() (artifact.TypeInfoDepends, error)
	GetUpdateProvides() (artifact.TypeInfoProvides, error)
	GetUpdateMetaData() (map[string]interface{}, error) // Generic JSON
	GetUpdateClearsProvides() []string

	// Returns non-augmented (original) data.
	GetUpdateOriginalDepends() artifact.TypeInfoDepends
	GetUpdateOriginalProvides() artifact.TypeInfoProvides
	GetUpdateOriginalMetaData() map[string]interface{} // Generic JSON
	GetUpdateOriginalClearsProvides() []string

	// Returns augmented data.
	GetUpdateAugmentDepends() artifact.TypeInfoDepends
	GetUpdateAugmentProvides() artifact.TypeInfoProvides
	GetUpdateAugmentMetaData() map[string]interface{} // Generic JSON
	GetUpdateAugmentClearsProvides() []string

	GetUpdateOriginalTypeInfoWriter() io.Writer
	GetUpdateAugmentTypeInfoWriter() io.Writer
}

type ArtifactUpdate interface {
	ArtifactUpdateHeaders

	// Operates on non-augmented files.
	GetUpdateFiles() [](*DataFile)
	SetUpdateFiles(files [](*DataFile)) error

	// Operates on augmented files.
	GetUpdateAugmentFiles() [](*DataFile)
	SetUpdateAugmentFiles(files [](*DataFile)) error

	// Gets both augmented and non-augmented files.
	GetUpdateAllFiles() [](*DataFile)
}

type Composer interface {
	ArtifactUpdate
	ComposeHeader(args *ComposeHeaderArgs) error
}

type UpdateStorer interface {
	// Called as soon as all headers all collected.
	Initialize(artifactHeaders,
		artifactAugmentedHeaders artifact.HeaderInfoer,
		payloadHeaders ArtifactUpdateHeaders) error
	// Called before storing any file for this UpdateStorer
	PrepareStoreUpdate() error
	// Called once for each file to store
	StoreUpdate(r io.Reader, info os.FileInfo) error
	// Called after all files have been stored, even if there was an error
	FinishStoreUpdate() error
}

type UpdateStorerProducer interface {
	NewUpdateStorer(updateType string, payloadNum int) (UpdateStorer, error)
}

type Installer interface {
	ArtifactUpdate
	UpdateStorerProducer
	ReadHeader(r io.Reader, path string, version int, augmented bool) error
	SetUpdateStorerProducer(producer UpdateStorerProducer)
	NewInstance() Installer
	NewAugmentedInstance(orig ArtifactUpdate) (Installer, error)
}

type installerBase struct {
	updateStorerProducer UpdateStorerProducer
}

type devNullUpdateStorer struct {
}

func (i *installerBase) SetUpdateStorerProducer(producer UpdateStorerProducer) {
	i.updateStorerProducer = producer
}

func (i *installerBase) NewUpdateStorer(updateType string, payloadNum int) (UpdateStorer, error) {
	if i.updateStorerProducer != nil {
		return i.updateStorerProducer.NewUpdateStorer(updateType, payloadNum)
	} else {
		return &devNullUpdateStorer{}, nil
	}
}

func (s *devNullUpdateStorer) Initialize(artifactHeaders,
	artifactAugmentedHeaders artifact.HeaderInfoer,
	payloadHeaders ArtifactUpdateHeaders) error {

	return nil
}

func (s *devNullUpdateStorer) PrepareStoreUpdate() error {
	return nil
}

func (s *devNullUpdateStorer) StoreUpdate(r io.Reader, info os.FileInfo) error {
	_, err := io.Copy(ioutil.Discard, r)
	return err
}

func (s *devNullUpdateStorer) FinishStoreUpdate() error {
	return nil
}

func parseFiles(r io.Reader) (*artifact.Files, error) {
	files := new(artifact.Files)
	if _, err := io.Copy(files, r); err != nil {
		return nil, errors.Wrap(err, "Payload: error reading files")
	}
	if err := files.Validate(); err != nil {
		return nil, err
	}
	return files, nil
}

func match(pattern, name string) bool {
	match, err := filepath.Match(pattern, name)
	if err != nil {
		return false
	}
	return match
}

func writeFiles(tw *tar.Writer, updFiles []string, dir string) error {
	if tw == nil {
		return errors.New("writer: tar-writer is nil")
	}
	files := new(artifact.Files)
	files.FileList = append(files.FileList, updFiles...)

	sa := artifact.NewTarWriterStream(tw)
	stream, err := artifact.ToStream(files)
	if err != nil {
		return errors.Wrap(err, "writeFiles: ")
	}
	if err := sa.Write(stream,
		filepath.Join(dir, "files")); err != nil {
		return errors.Wrapf(err, "writer: can not tar files")
	}
	return nil
}

func writeTypeInfo(tw *tar.Writer, updateType string, dir string) error {
	tInfo := artifact.TypeInfo{Type: updateType}
	info, err := json.Marshal(&tInfo)
	if err != nil {
		return errors.Wrapf(err, "Payload: can not create type-info")
	}

	w := artifact.NewTarWriterStream(tw)
	if err := w.Write(info, filepath.Join(dir, "type-info")); err != nil {
		return errors.Wrapf(err, "Payload: can not tar type-info")
	}
	return nil
}

type WriteInfoArgs struct {
	tarWriter  *tar.Writer
	dir        string
	typeinfov3 *artifact.TypeInfoV3
}

func writeTypeInfoV3(args *WriteInfoArgs) error {
	info, err := json.Marshal(args.typeinfov3)
	if err != nil {
		return errors.Wrapf(err, "Payload: can not create type-info")
	}

	w := artifact.NewTarWriterStream(args.tarWriter)
	if err := w.Write(info, filepath.Join(args.dir, "type-info")); err != nil {
		return errors.Wrapf(err, "Payload: can not tar type-info")
	}
	return nil
}
