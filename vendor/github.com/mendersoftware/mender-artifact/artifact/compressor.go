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

package artifact

import (
	"io"
	"sort"
	"strings"

	"github.com/pkg/errors"
)

var compressors = make(map[string]Compressor)

type Compressor interface {
	GetFileExtension() string
	NewReader(r io.Reader) (io.ReadCloser, error)
	NewWriter(w io.Writer) (io.WriteCloser, error)
}

func RegisterCompressor(id string, compressor Compressor) {
	compressors[id] = compressor
}

func NewCompressorFromFileName(name string) (Compressor, error) {
	for _, compressor := range compressors {
		extension := compressor.GetFileExtension()
		if extension != "" && strings.HasSuffix(name, extension) {
			return compressor, nil
		}
	}
	return NewCompressorNone(), nil
}

func NewCompressorFromId(id string) (Compressor, error) {
	compressor, ok := compressors[id]
	if !ok {
		return nil, errors.Errorf("invalid compressor id: %v", id)
	}
	return compressor, nil
}

type compressorIdSort []string

func (s compressorIdSort) Len() int {
	return len(s)
}
func (s compressorIdSort) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}
func (s compressorIdSort) Less(i, j int) bool {
	if s[i] == "none" {
		return true
	} else if s[j] == "none" {
		return false
	} else {
		return s[i] < s[j]
	}
}

func GetRegisteredCompressorIds() []string {
	ids := make([]string, 0, len(compressors))
	for id := range compressors {
		ids = append(ids, id)
	}

	// Sort 'none' at the front and alphabetically thereafter
	sort.Sort(compressorIdSort(ids))

	return ids
}
