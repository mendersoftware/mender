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

package artifact

import (
	"io"
	"strings"

	"github.com/pkg/errors"
)

type Compressor interface {
	GetFileExtension() string
	NewReader(r io.Reader) (io.ReadCloser, error)
	NewWriter(w io.Writer) io.WriteCloser
}

func NewCompressorFromFileName(name string) (Compressor, error) {
	if strings.HasSuffix(name, ".gz") {
		return NewCompressorGzip(), nil
	} else {
		return NewCompressorNone(), nil
	}
}

func NewCompressorFromId(id string) (Compressor, error) {
	switch id {
	case "gzip":
		return NewCompressorGzip(), nil
	case "none":
		return NewCompressorNone(), nil
	default:
		return nil, errors.Errorf("invalid compressor id: %v", id)
	}
}
