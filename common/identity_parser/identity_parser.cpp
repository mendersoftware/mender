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

#include <common/identity_parser.hpp>

#include <common/key_value_parser.hpp>
#include <common/processes.hpp>

namespace mender::common::identity_parser {

using namespace std;
namespace kvp = mender::common::key_value_parser;
namespace procs = mender::common::processes;

ExpectedKeyValuesMap GetIdentityData(const string identity_data_generator) {
	procs::Process proc({identity_data_generator});
	auto ex_line_data = proc.GenerateLineData();
	if (!ex_line_data) {
		switch (ex_line_data.error().error_code) {
		case procs::ProcessErrorCode::SpawnError:
			return ExpectedKeyValuesMap(IdentityParserError(
				IdentityParserErrorCode::SpawnError, ex_line_data.error().message));
			break;
		default:
			// all cases should be covered above
			assert(false);
		}
	}

	auto ex_key_values = kvp::ParseKeyValues(ex_line_data.value());
	if (!ex_key_values) {
		switch (ex_key_values.error().error_code) {
		case kvp::KeyValueParserErrorCode::InvalidData:
			return ExpectedKeyValuesMap(IdentityParserError(
				IdentityParserErrorCode::InvalidData, ex_key_values.error().message));
			break;
		default:
			// all cases should be covered above
			assert(false);
		}
	}

	return ExpectedKeyValuesMap(ex_key_values.value());
}

} // namespace mender::common::identity_parser
