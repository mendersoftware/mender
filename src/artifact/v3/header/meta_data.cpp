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


#include <common/expected.hpp>
#include <common/error.hpp>
#include <common/io.hpp>
#include <common/json.hpp>
#include <common/log.hpp>
#include <common/common.hpp>

#include <artifact/error.hpp>
#include <string>
#include <iostream>

namespace mender {
namespace artifact {
namespace v3 {
namespace header {
namespace meta_data {

const string empty_json_error_message =
	"[json.exception.parse_error.101] parse error at line 1, column 1: syntax error while parsing value - unexpected end of input; expected '[', '{', or a literal";

namespace json = mender::common::json;
namespace log = mender::common::log;

ExpectedMetaData Parse(io::Reader &reader) {
	log::Trace("Parsing the header meta-data");
	auto expected_json = json::Load(reader);

	if (!expected_json) {
		log::Trace("Received json load error: " + expected_json.error().message);
		bool is_empty_payload_error =
			expected_json.error().message.find(empty_json_error_message) != string::npos;
		if (is_empty_payload_error) {
			log::Trace("Received an empty Json body. Not treating this as an error");
			return json::Json();
		}
		return expected::unexpected(parser_error::MakeError(
			parser_error::Code::ParseError,
			"Failed to parse the  meta-data JSON: " + expected_json.error().message));
	}

	const json::Json meta_data_json = expected_json.value();

	if (!meta_data_json.IsObject()) {
        return expected::unexpected(parser_error::MakeError(
            parser_error::Code::ParseError, "The meta-data needs to be valid JSON with a top-level JSON object"));
    }

	return meta_data_json;
}

} // namespace meta_data
} // namespace header
} // namespace v3
} // namespace artifact
} // namespace mender
