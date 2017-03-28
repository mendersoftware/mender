// Copyright 2016 Mender Software AS
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
	"archive/tar"
	"encoding/json"

	"github.com/pkg/errors"
)

func ToStream(m WriteValidator) []byte {
	if err := m.Validate(); err != nil {
		return nil
	}
	data, err := json.Marshal(m)
	if err != nil {
		return nil
	}
	return data
}

type StreamArchiver struct {
	*tar.Writer
}

func NewTarWriterStream(w *tar.Writer) *StreamArchiver {
	return &StreamArchiver{
		Writer: w,
	}
}

func (str *StreamArchiver) Write(data []byte, archivePath string) error {
	hdr := &tar.Header{
		Name: archivePath,
		Mode: 0600,
		Size: int64(len(data)),
	}
	if err := str.Writer.WriteHeader(hdr); err != nil {
		return errors.Wrapf(err, "arch: can not write stream header")
	}
	_, err := str.Writer.Write(data)
	if err != nil {
		return errors.Wrapf(err, "arch: can not write stream data")
	}
	return nil
}
