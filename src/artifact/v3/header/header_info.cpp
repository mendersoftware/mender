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

#include <artifact/error.hpp>

namespace mender {
namespace artifact {
namespace v3 {
namespace header {
namespace info {

namespace expected = mender::common::expected;
namespace io = mender::common::io;
namespace error = mender::common::error;
namespace log = mender::common::log;
namespace json = mender::common::json;

using ExpectedPayloadType = expected::expected<vector<PayloadType>, error::Error>;

ExpectedPayloadType ToPayloadTypes(const json::Json &j) {
	if (!j.IsArray()) {
		return expected::unexpected(parser_error::MakeError(
			parser_error::Code::ParseError, "The JSON object is not an array"));
	}
	vector<PayloadType> vector_elements {};
	size_t vector_size {j.GetArraySize().value()};
	for (size_t i = 0; i < vector_size; ++i) {
		auto expected_element =
			j.Get(i).and_then([](const json::Json &j) { return j.Get("type"); });
		if (!expected_element) {
			return expected::unexpected(parser_error::MakeError(
				parser_error::Code::ParseError,
				"Failed to get the type from the payload: " + expected_element.error().message));
		}
		auto json_element = expected_element.value();
		if (json_element.IsString()) {
			string payload_type = json_element.GetString().value();
			if (payload_type == "") {
				return expected::unexpected(
					parser_error::MakeError(parser_error::Code::ParseError, "Empty Payload type"));
			}
			Payload type {Payload::RootfsImage};
			if (payload_type != "rootfs-image") {
				type = Payload::ModuleImage;
			}
			vector_elements.push_back({type, payload_type});
		} else if (json_element.IsNull()) {
			vector_elements.push_back({Payload::EmptyPayload, ""});
		} else {
			return expected::unexpected(
				parser_error::MakeError(parser_error::Code::ParseError, "Unexpected payload type"));
		}
	}
	return vector_elements;
}

ExpectedHeaderInfo Parse(io::Reader &reader) {
	log::Trace("Parse(header-info)");

	Info info {};

	auto expected_json = json::Load(reader);

	if (!expected_json) {
		return expected::unexpected(parser_error::MakeError(
			parser_error::Code::ParseError,
			"Failed to parse the header JSON: " + expected_json.error().message));
	}

	const auto header_info_json = expected_json.value();
	info.verbatim = header_info_json;

	//
	// Payloads (required)
	//
	log::Trace("Parsing the payloads");

	auto payloads = header_info_json.Get("payloads").and_then(ToPayloadTypes);
	if (!payloads) {
		return expected::unexpected(parser_error::MakeError(
			parser_error::Code::ParseError,
			"Failed to parse the header-info payloads JSON: " + payloads.error().message));
	}
	info.payloads = payloads.value();

	//
	// provides (required)
	//
	log::Trace("Parsing the header-info artifact_provides");

	auto provides = header_info_json.Get("artifact_provides");
	if (!provides) {
		return expected::unexpected(parser_error::MakeError(
			parser_error::Code::ParseError,
			"Failed to parse the header-info artifact_provides JSON: " + provides.error().message));
	}

	// provides:artifact_name (required)
	log::Trace("Parsing the header-info provides:artifact_name");
	auto artifact_name = provides.value().Get("artifact_name").and_then(json::ToString);

	if (!artifact_name) {
		return expected::unexpected(
			parser_error::MakeError(parser_error::Code::ParseError, artifact_name.error().message));
	}
	info.provides.artifact_name = artifact_name.value();

	// provides:artifact_group (optional)
	log::Trace("Parsing the header-info provides:artifact_group (if any)");
	auto artifact_group = provides.value().Get("artifact_group").and_then(json::ToString);
	if (!artifact_group && artifact_group.error().code.value() != json::KeyError) {
		return expected::unexpected(parser_error::MakeError(
			parser_error::Code::ParseError,
			"Failed to parse the header-info artifact_group provides JSON: "
				+ artifact_group.error().message));
	}
	if (artifact_group) {
		info.provides.artifact_group = artifact_group.value();
	}

	//
	// depends (required)
	//
	log::Trace("Parsing the header-info depends:artifact_depends (if any)");

	auto depends = header_info_json.Get("artifact_depends");
	if (!depends) {
		return expected::unexpected(parser_error::MakeError(
			parser_error::Code::ParseError,
			"Failed to parse the header-info artifact_depends JSON: " + depends.error().message));
	}

	// device_type[string] (required)
	auto device_type = depends.value().Get("device_type").and_then(json::ToStringVector);
	if (!device_type) {
		return expected::unexpected(
			parser_error::MakeError(parser_error::Code::ParseError, device_type.error().message));
	}
	info.depends.device_type = device_type.value();


	// depends::artifact_name (optional)
	auto artifact_name_depends =
		depends.value().Get("artifact_name").and_then(json::ToStringVector);
	if (!artifact_name_depends && artifact_name_depends.error().code.value() != json::KeyError) {
		return expected::unexpected(parser_error::MakeError(
			parser_error::Code::ParseError,
			"Failed to parse the header-info artifact_name depends JSON: "
				+ artifact_name_depends.error().message));
	}
	if (artifact_name_depends) {
		info.depends.artifact_name = artifact_name_depends.value();
	}

	// depends::artifact_group (optional)
	auto artifact_group_depends =
		depends.value().Get("artifact_group").and_then(json::ToStringVector);
	if (!artifact_group_depends && artifact_group_depends.error().code.value() != json::KeyError) {
		return expected::unexpected(parser_error::MakeError(
			parser_error::Code::ParseError,
			"Failed to parse the header-info artifact_group_depends JSON: "
				+ artifact_group_depends.error().message));
	}
	if (artifact_group_depends) {
		info.depends.artifact_group = artifact_group_depends.value();
	}

	return info;
}

} // namespace info
} // namespace header
} // namespace v3
} // namespace artifact
} // namespace mender
