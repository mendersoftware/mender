// Copyright 2023 Northern.tech AS
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
//go:build !nozstd
// +build !nozstd

package artifact

import (
	"io"

	"github.com/klauspost/compress/zstd"
)

type CompressorZstd struct {
	level zstd.EncoderLevel
}

func NewCompressorZstd(level zstd.EncoderLevel) Compressor {
	return &CompressorZstd{
		level: level,
	}
}

func (c *CompressorZstd) GetFileExtension() string {
	return ".zst"
}

func (c *CompressorZstd) NewReader(r io.Reader) (io.ReadCloser, error) {
	zstdReader, err := zstd.NewReader(r)
	if err != nil {
		return nil, err
	}
	return zstdReader.IOReadCloser(), nil
}

func (c *CompressorZstd) NewWriter(w io.Writer) (io.WriteCloser, error) {
	return zstd.NewWriter(w, zstd.WithEncoderLevel(c.level))
}

func init() {
	RegisterCompressor("zstd_fastest", NewCompressorZstd(zstd.SpeedFastest))
	RegisterCompressor("zstd_fast", NewCompressorZstd(zstd.SpeedDefault))
	RegisterCompressor("zstd_better", NewCompressorZstd(zstd.SpeedBetterCompression))
	RegisterCompressor("zstd_best", NewCompressorZstd(zstd.SpeedBestCompression))
}
