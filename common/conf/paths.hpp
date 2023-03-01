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

#ifndef MENDER_COMMON_CONF_PATHS_HPP
#define MENDER_COMMON_CONF_PATHS_HPP

#include <string>

namespace mender {
namespace common {
namespace conf {
namespace paths {

using namespace std;

extern const string DefaultPathConfDir;
extern const string DefaultPathDataDir;
extern const string DefaultDataStore;
extern const string DefaultKeyFile;

extern const string DefaultConfFile;
extern const string DefaultFallbackConfFile;

// device specific paths
extern const string DefaultArtScriptsPath;
extern const string DefaultRootfsScriptsPath;
extern const string DefaultModulesPath;
extern const string DefaultModulesWorkPath;
extern const string DefaultBootstrapArtifactFile;

} // namespace paths
} // namespace conf
} // namespace common
} // namespace mender

#endif // MENDER_COMMON_CONF_PATHS_HPP
