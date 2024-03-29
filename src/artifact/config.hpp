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

#ifndef MENDER_ARTIFACT_CONFIG_HPP
#define MENDER_ARTIFACT_CONFIG_HPP

#include <string>
#include <vector>

namespace mender {
namespace artifact {
namespace config {

using namespace std;

enum class Signature {
	Verify,
	Skip,
};

struct ParserConfig {
	string artifact_scripts_filesystem_path;
	int artifact_scripts_version;
	vector<string> artifact_verify_keys;
	Signature verify_signature;
};

} // namespace config
} // namespace artifact
} // namespace mender

#endif // MENDER_ARTIFACT_CONFIG_HPP
