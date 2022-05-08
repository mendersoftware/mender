// Copyright 2022 Northern.tech AS
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

//go:build !local
// +build !local

package conf

import (
	"os"
	"path"
)

const (
	BrokenArtifactSuffix = "_INCONSISTENT"
)

func getenv(key, fallback string) string {
	value := os.Getenv(key)
	if len(value) == 0 {
		return fallback
	}
	return value
}

var (
	// needed so that we can override it when testing or deploying on partially read-only systems
	DefaultPathConfDir = getenv("MENDER_CONF_DIR", "/etc/mender")
	DefaultPathDataDir = getenv("MENDER_DATA_DIR", "/usr/share/mender")
	DefaultDataStore   = getenv("MENDER_DATASTORE_DIR", "/var/lib/mender")
	DefaultKeyFile     = "mender-agent.pem"

	DefaultConfFile         = path.Join(GetConfDirPath(), "mender.conf")
	DefaultFallbackConfFile = path.Join(GetStateDirPath(), "mender.conf")
)

var (
	// device specific paths
	DefaultArtifactInfoFile  = path.Join(GetConfDirPath(), "artifact_info")
	DefaultArtScriptsPath    = path.Join(GetStateDirPath(), "scripts")
	DefaultRootfsScriptsPath = path.Join(GetConfDirPath(), "scripts")
	DefaultModulesPath       = path.Join(GetDataDirPath(), "modules", "v3")
	DefaultModulesWorkPath   = path.Join(GetStateDirPath(), "modules", "v3")
)

func GetDataDirPath() string {
	return DefaultPathDataDir
}

func GetStateDirPath() string {
	return DefaultDataStore
}

func GetConfDirPath() string {
	return DefaultPathConfDir
}
