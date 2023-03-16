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

#include <artifact/v3/version/version.hpp>

#include <string>
#include <vector>

#include <common/common.hpp>
#include <common/expected.hpp>
#include <common/error.hpp>
#include <common/json.hpp>


namespace mender {
namespace artifact {
namespace v3 {
namespace version {

namespace io = mender::common::io;
namespace expected = mender::common::expected;
namespace error = mender::common::error;
namespace json = mender::common::json;

const int supported_parser_version {3};
const string supported_parser_format {"mender"};

const ErrorCategoryClass ErrorCategory {};

const char *ErrorCategoryClass::name() const noexcept {
	return "VersionParserErrorCategory";
}

string ErrorCategoryClass::message(int code) const {
	switch (code) {
	case NoError:
		return "Success";
	case ParseError:
		return "Parse error";
	case VersionError:
		return "Wrong Artifact version";
	case FormatError:
		return "Wrong Artifact format";
	default:
		return "Unknown";
	}
}

error::Error MakeError(ErrorCode code, const string &msg) {
	return error::Error(error_condition(code, ErrorCategory), msg);
}


ExpectedVersion Parse(io::Reader &reader) {
	// Read in all the bytes into a tmp buffer
	std::vector<uint8_t> buf(1024);
	io::ByteWriter bw {buf};

	auto err = io::Copy(bw, reader);
	if (err) {
		return expected::unexpected(err);
	}

	auto expected_json = json::LoadFromString(mender::common::StringFromByteVector(buf));

	if (!expected_json) {
		return expected::unexpected(MakeError(
			ParseError,
			"Failed to parse the version header JSON: " + expected_json.error().message));
	}

	const json::Json version_json = expected_json.value();

	auto version =
		version_json.Get("version").and_then([](const json::Json &j) { return j.GetInt(); });

	if (!version) {
		return expected::unexpected(MakeError(VersionError, version.error().message));
	}

	if (version.value() != supported_parser_version) {
		return expected::unexpected(MakeError(
			FormatError,
			"Only version " + std::to_string(supported_parser_version)
				+ " is supported, received version " + std::to_string(version.value())));
	}

	auto format =
		version_json.Get("format").and_then([](const json::Json &j) { return j.GetString(); });

	if (!format) {
		return expected::unexpected(MakeError(FormatError, format.error().message));
	}

	if (format != supported_parser_format) {
		return expected::unexpected(MakeError(
			FormatError,
			"The client only understands the 'mender' Artifact type. Got format: "
				+ format.value()));
	}

	return Version {.version = supported_parser_version, .format = supported_parser_format};
}

} // namespace version
} // namespace v3
} // namespace artifact
} // namespace mender
