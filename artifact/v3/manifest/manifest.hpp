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

#ifndef MENDER_ARTIFACT_V3_MANIFEST_PARSER_HPP
#define MENDER_ARTIFACT_V3_MANIFEST_PARSER_HPP

#include <string>
#include <unordered_map>

#include <common/expected.hpp>
#include <common/error.hpp>
#include <common/io.hpp>

namespace mender {
namespace artifact {
namespace v3 {
namespace manifest {

using namespace std;

namespace expected = mender::common::expected;
namespace io = mender::common::io;
namespace error = mender::common::error;

class Manifest {
public:
	string Get(const string &key);

	unordered_map<string, string> map_;
};

using ExpectedManifest = expected::expected<Manifest, error::Error>;

ExpectedManifest Parse(io::Reader &reader);

} // namespace manifest
} // namespace v3
} // namespace artifact
} // namespace mender
#endif // MENDER_ARTIFACT_V3_MANIFEST_PARSER_HPP
