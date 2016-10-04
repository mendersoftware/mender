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

package tutils

import (
	"os"
	"path"
)

type TestDirEntry struct {
	Path    string
	Content []byte
	IsDir   bool
}

func MakeFakeUpdateDir(updateDir string, elements []TestDirEntry) error {
	for _, elem := range elements {
		if elem.IsDir {
			if err := os.MkdirAll(path.Join(updateDir, elem.Path), os.ModeDir|os.ModePerm); err != nil {
				return err
			}
		} else {
			f, err := os.Create(path.Join(updateDir, elem.Path))
			if err != nil {
				return err
			}
			defer f.Close()
			if len(elem.Content) > 0 {
				if _, err = f.Write(elem.Content); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
