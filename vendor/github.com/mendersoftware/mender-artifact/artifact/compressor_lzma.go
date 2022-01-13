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
//go:build !nolzma && cgo
// +build !nolzma,cgo

package artifact

import (
	"io"

	xz "github.com/remyoudompheng/go-liblzma"
)

type CompressorLzma struct {
}

func NewCompressorLzma() Compressor {
	return &CompressorLzma{}
}

func (c *CompressorLzma) GetFileExtension() string {
	return ".xz"
}

func (c *CompressorLzma) NewReader(r io.Reader) (io.ReadCloser, error) {
	return xz.NewReader(r)
}

func (c *CompressorLzma) NewWriter(w io.Writer) (io.WriteCloser, error) {
	return xz.NewWriter(w, xz.Level9)
}

func init() {
	RegisterCompressor("lzma", &CompressorLzma{})
}
