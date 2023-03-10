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

#include <common/conf/paths.hpp>

#include <string>

#include <common/conf.hpp>
#include <boost/filesystem.hpp>

namespace mender {
namespace common {
namespace conf {
namespace paths {

using namespace std;
namespace conf = mender::common::conf;
namespace fs = boost::filesystem;

string Join(const string &prefix, const string &suffix) {
	return (fs::path(prefix) / suffix).string();
}

const string DefaultPathConfDir = conf::GetEnv("MENDER_CONF_DIR", Join("/etc", "mender"));
const string DefaultPathDataDir = conf::GetEnv("MENDER_DATA_DIR", Join("/usr/share", "mender"));
const string DefaultDataStore = conf::GetEnv("MENDER_DATASTORE_DIR", Join("/var/lib", "mender"));
const string DefaultKeyFile = "mender-agent.pem";

const string DefaultConfFile = Join(DefaultPathConfDir, "mender.conf");
const string DefaultFallbackConfFile = Join(DefaultDataStore, "mender.conf");

// device specific paths
const string DefaultArtScriptsPath = Join(DefaultDataStore, "scripts");
const string DefaultRootfsScriptsPath = Join(DefaultPathConfDir, "scripts");
const string DefaultModulesPath = Join(DefaultPathDataDir, "modules/v3");
const string DefaultModulesWorkPath = Join(DefaultDataStore, "modules/v3");
const string DefaultBootstrapArtifactFile = Join(DefaultDataStore, "bootstrap.mender");

} // namespace paths
} // namespace conf
} // namespace common
} // namespace mender
