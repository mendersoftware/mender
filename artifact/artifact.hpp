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

#ifndef MENDER_ARTIFACT_HPP_
#define MENDER_ARTIFACT_HPP_

#include <string>

#include <common/json.hpp>
#include <common/io.hpp>

#include <artifact/parser.hpp>
#include <artifact/config.hpp>

namespace mender {
namespace artifact {

using namespace std;

namespace error = mender::common::error;
namespace expected = mender::common::expected;
namespace json = mender::common::json;
namespace io = mender::common::io;

using error::Error;

using artifact::parser::Artifact;

using ExpectedArtifact = expected::expected<Artifact, error::Error>;

ExpectedArtifact Parse(io::Reader &reader, config::ParserConfig conf = {});

using Payload = mender::artifact::v3::payload::Payload;

struct HeaderView {
	string artifact_group;
	string artifact_name;
	string payload_type;
	json::Json header_info;
	json::Json type_info;
	json::Json meta_data;
};

struct PayloadHeaderView {
	int version;
	HeaderView header;
};

using ExpectedPayloadHeaderView = expected::expected<PayloadHeaderView, error::Error>;

// View is giving the meta-data view of a given payload index.
//
// This means that a PayloadHeaderView is the union of the global header-info,
// and the type-info for the given payload. A view will never leak information
// which is dedicated to another payload (given by it's index).
ExpectedPayloadHeaderView View(Artifact &artifact, size_t index);

} // namespace artifact
} // namespace mender


#endif // MENDER_ARTIFACT_HPP_
