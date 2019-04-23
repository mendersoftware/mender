// Copyright 2019 Northern.tech AS
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

// +build !local

package main

import (
	"path"
)

var (
	// needed so that we can override it when testing
	defaultPathDataDir      = "/usr/share/mender"
	defaultDataStore        = "/var/lib/mender"
	defaultConfFile         = path.Join(getConfDirPath(), "mender.conf")
	defaultFallbackConfFile = path.Join(getStateDirPath(), "mender.conf")
)

func getDataDirPath() string {
	return defaultPathDataDir
}

func getStateDirPath() string {
	return defaultDataStore
}

func getConfDirPath() string {
	return "/etc/mender"
}
