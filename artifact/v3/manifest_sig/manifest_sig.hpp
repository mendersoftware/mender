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

#ifndef MENDER_ARTIFACT_V3_MANIFEST_SIG_PARSER_HPP
#define MENDER_ARTIFACT_V3_MANIFEST_SIG_PARSER_HPP

#include <string>
#include <vector>

#include <common/expected.hpp>
#include <common/error.hpp>
#include <common/io.hpp>
#include <artifact/sha/sha.hpp>

namespace mender {
namespace artifact {
namespace v3 {
namespace manifest_sig {

using namespace std;

namespace expected = mender::common::expected;
namespace io = mender::common::io;
namespace error = mender::common::error;

using ManifestSignature = string;

using ExpectedManifestSignature = expected::expected<ManifestSignature, error::Error>;

ExpectedManifestSignature Parse(io::Reader &reader);

expected::ExpectedBool VerifySignature(
	const ManifestSignature &signature,
	const mender::sha::SHA &shasum,
	const vector<string> &artifact_verify_keys);

} // namespace manifest_sig
} // namespace v3
} // namespace artifact
} // namespace mender
#endif // MENDER_ARTIFACT_V3_MANIFEST_SIG_PARSER_HPP
