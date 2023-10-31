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

#include <artifact/v3/header/header.hpp>

#include <vector>

#include <common/expected.hpp>
#include <common/error.hpp>
#include <common/io.hpp>
#include <common/log.hpp>
#include <common/json.hpp>
#include <common/common.hpp>

#include <artifact/error.hpp>
#include <artifact/lexer.hpp>
#include <artifact/tar/tar.hpp>


namespace mender {
namespace artifact {
namespace v3 {
namespace header {

namespace type_info {

namespace expected = mender::common::expected;
namespace io = mender::common::io;
namespace error = mender::common::error;
namespace log = mender::common::log;
namespace json = mender::common::json;

ExpectedTypeInfo Parse(io::Reader &reader) {
	TypeInfo type_info;

	log::Trace("Parse(type-info)...");

	auto expected_json = json::Load(reader);

	if (!expected_json) {
		return expected::unexpected(parser_error::MakeError(
			parser_error::Code::ParseError,
			"Failed to parse the  sub-header JSON: " + expected_json.error().message));
	}

	const json::Json type_info_json = expected_json.value();
	type_info.verbatim = type_info_json;


	//
	// Parse the single payload_type key:value (required)
	//

	log::Trace("type-info: Parsing the payload type");

	auto expected_payload = type_info_json.Get("type");
	if (!expected_payload) {
		return expected::unexpected(parser_error::MakeError(
			parser_error::Code::ParseError,
			"Failed to get the type-info payload type JSON: " + expected_payload.error().message));
	}
	auto payload_type = expected_payload.value();
	if (payload_type.IsNull()) {
		type_info.type = "null";
	} else if (payload_type.IsString()) {
		type_info.type = payload_type.GetString().value();
	} else {
		return expected::unexpected(parser_error::MakeError(
			parser_error::Code::ParseError,
			"Failed to parse the type-info payload type JSON: "
				+ expected_payload.error().message));
	}

	log::Trace("type-info: Parsing the artifact_provides");

	//
	// artifact_provides (Optional)
	//

	auto expected_artifact_provides =
		type_info_json.Get("artifact_provides").and_then(json::ToKeyValueMap);
	if (!expected_artifact_provides
		&& expected_artifact_provides.error().code.value() != json::KeyError) {
		return expected::unexpected(parser_error::MakeError(
			parser_error::Code::ParseError,
			"Failed to parse the type-info artifact_provides JSON: "
				+ expected_artifact_provides.error().message));
	}
	if (expected_artifact_provides) {
		type_info.artifact_provides = expected_artifact_provides.value();
	} else {
		log::Trace("No artifact_provides found in type-info");
	}


	//
	// artifact_depends (Optional)
	//
	log::Trace("type-info: Parsing the artifact_depends");

	auto expected_artifact_depends =
		type_info_json.Get("artifact_depends").and_then(json::ToKeyValueMap);
	if (!expected_artifact_depends
		&& expected_artifact_depends.error().code.value() != json::KeyError) {
		return expected::unexpected(parser_error::MakeError(
			parser_error::Code::ParseError,
			"Failed to parse the type-info artifact_depends JSON: "
				+ expected_artifact_provides.error().message));
	}
	if (expected_artifact_depends) {
		type_info.artifact_depends = expected_artifact_depends.value();
	} else {
		log::Trace("No artifact_depends found in type-info");
	}

	//
	// clears_artifact_provides (Optional)
	//

	log::Trace("type-info: Parsing the clears_artifact_provides");
	auto expected_clears_artifact_provides =
		type_info_json.Get("clears_artifact_provides").and_then(json::ToStringVector);
	if (!expected_clears_artifact_provides
		&& expected_clears_artifact_provides.error().code.value() != json::KeyError) {
		return expected::unexpected(parser_error::MakeError(
			parser_error::Code::ParseError,
			"Failed to parse the type-info clears_artifact_depends JSON: "
				+ expected_artifact_provides.error().message));
	}
	if (expected_clears_artifact_provides) {
		type_info.clears_artifact_provides = expected_clears_artifact_provides.value();
	} else {
		log::Trace("No artifact_clears_provides found in type-info");
	}

	log::Trace("Finished parsing the type-info..");

	return type_info;
}

} // namespace type_info
} // namespace header
} // namespace v3
} // namespace artifact
} // namespace mender
