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

#include <artifact/artifact.hpp>

#include <common/error.hpp>
#include <common/expected.hpp>

#include <artifact/error.hpp>


namespace mender {
namespace artifact {

namespace error = mender::common::error;
namespace expected = mender::common::expected;


ExpectedArtifact Parse(io::Reader &reader, config::ParserConfig conf) {
	return parser::Parse(reader, conf);
}

ExpectedPayloadHeaderView View(parser::Artifact &artifact, size_t index) {
	// Check if the index is available
	if (index >= artifact.header.info.payloads.size()) {
		return expected::unexpected(
			parser_error::MakeError(parser_error::Code::ParseError, "Payload index out of range"));
	}
	mender::common::json::Json meta_data;
	if (artifact.header.subHeaders.at(index).metadata) {
		meta_data = artifact.header.subHeaders.at(index).metadata.value();
	}
	return PayloadHeaderView {
		.version = artifact.version.version,
		.header =
			HeaderView {
				.artifact_group = artifact.header.info.provides.artifact_group.value_or(""),
				.artifact_name = artifact.header.info.provides.artifact_name,
				.payload_type = artifact.header.info.payloads.at(index).name,
				.header_info = artifact.header.info,
				.type_info = artifact.header.subHeaders.at(index).type_info,
				.meta_data = meta_data,
			},
	};
};
} // namespace artifact
} // namespace mender
