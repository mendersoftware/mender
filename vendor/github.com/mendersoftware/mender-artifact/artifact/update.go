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
	"fmt"
	"path/filepath"
)

const (
	HeaderDirectory = "headers"
	DataDirectory   = "data"
)

func UpdatePath(no int) string {
	return filepath.Join(DataDirectory, fmt.Sprintf("%04d", no))
}

func UpdateHeaderPath(no int) string {
	return filepath.Join(HeaderDirectory, fmt.Sprintf("%04d", no))
}

func UpdateDataPath(no int) string {
	return filepath.Join(DataDirectory, fmt.Sprintf("%04d.tar", no))
}
