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

package handlers

import (
	"bytes"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/mendersoftware/mender-artifact/artifact"
	"github.com/pkg/errors"
)

type Generic struct {
	updateType        string
	version           int
	regularHeaderRead bool
	files             map[string](*DataFile)
}

func NewGeneric(t string) *Generic {
	return &Generic{
		updateType: t,
		files:      make(map[string](*DataFile)),
	}
}

func (g *Generic) GetUpdateFiles() [](*DataFile) {
	list := make([](*DataFile), len(g.files))
	i := 0
	for _, f := range g.files {
		list[i] = f
		i++
	}
	return list
}

func (g *Generic) GetType() string {
	return g.updateType
}

// Copy is implemented only to satisfy Installer interface.
// Generic parser is not supposed to be copied.
func (g *Generic) Copy() Installer {
	return nil
}

func stripSum(path string) string {
	bName := filepath.Base(path)
	return strings.TrimSuffix(bName, filepath.Ext(bName))
}

func (g *Generic) ReadHeader(r io.Reader, path string, version int) error {
	switch {
	case filepath.Base(path) == "files":
		files, err := parseFiles(r)
		if version == 3 {
			if !g.regularHeaderRead {
				g.regularHeaderRead = true
				if err == nil {
					return errors.New("ReadHeader: files-list should be empty")
				}
				return nil
			}
		}
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

func (g *Generic) Install(r io.Reader, info *os.FileInfo) error {
	_, err := io.Copy(ioutil.Discard, r)
	return err
}
