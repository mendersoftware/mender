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

package artifact

import (
	"io"
)

type compressor_none_reader struct {
	r io.Reader
}

func (c *compressor_none_reader) Read(p []byte) (n int, err error) {
	return c.r.Read(p)
}

func (c *compressor_none_reader) Close() error {
	return nil
}

type compressor_none_writer struct {
	w io.Writer
}

func (c *compressor_none_writer) Write(p []byte) (n int, err error) {
	return c.w.Write(p)
}

func (c *compressor_none_writer) Close() error {
	return nil
}

type CompressorNone struct {
}

func NewCompressorNone() Compressor {
	return &CompressorNone{}
}

func (c *CompressorNone) GetFileExtension() string {
	return ""
}

func (c *CompressorNone) NewReader(r io.Reader) (io.ReadCloser, error) {
	return &compressor_none_reader{
		r: r,
	}, nil
}

func (c *CompressorNone) NewWriter(w io.Writer) (io.WriteCloser, error) {
	return &compressor_none_writer{
		w: w,
	}, nil
}

func init() {
	RegisterCompressor("none", &CompressorNone{})
}
