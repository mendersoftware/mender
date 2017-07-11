// Copyright 2017 Northern.tech AS
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

// +build local

package main

import (
	"os"
	"path"
	"path/filepath"
)

func getRunningBinaryPath() string {
	return filepath.Dir(os.Args[0])
}

func getDataDirPath() string {
	return path.Join(getRunningBinaryPath(), "support")
}

func getStateDirPath() string {
	return getRunningBinaryPath()
}

func getConfDirPath() string {
	return getRunningBinaryPath()
}
