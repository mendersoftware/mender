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

#ifndef MENDER_ARTIFACT_PAYLOAD_PARSER_HPP
#define MENDER_ARTIFACT_PAYLOAD_PARSER_HPP

#include <vector>

#include <common/io.hpp>
#include <common/expected.hpp>

#include <artifact/sha/sha.hpp>

namespace mender {
namespace artifact {
namespace v3 {
namespace payload {

using namespace std;

namespace io = mender::common::io;
namespace error = mender::common::error;
namespace sha = mender::sha;

using mender::common::expected::ExpectedSize;

typedef sha::Reader Reader;

Reader Verify(io::Reader &reader, const string &expected_shasum);

} // namespace payload
} // namespace v3
} // namespace artifact
} // namespace mender

#endif // MENDER_ARTIFACT_PAYLOAD_PARSER_HPP
