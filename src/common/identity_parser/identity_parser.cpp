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

#include <common/common.hpp>
#include <common/json.hpp>
#include <common/key_value_parser.hpp>
#include <common/processes.hpp>

namespace mender {
namespace common {
namespace identity_parser {

using namespace std;
namespace kvp = mender::common::key_value_parser;
namespace procs = mender::common::processes;

kvp::ExpectedKeyValuesMap GetIdentityData(const string &identity_data_generator) {
	procs::Process proc({identity_data_generator});
	auto ex_line_data = proc.GenerateLineData();
	if (!ex_line_data) {
		return expected::unexpected(
			ex_line_data.error().WithContext("While getting identity data"));
	}

	auto ex_key_values = kvp::ParseKeyValues(ex_line_data.value());
	return ex_key_values;
}

string DumpIdentityData(kvp::KeyValuesMap identity_data) {
	stringstream top_ss;
	top_ss << "{";
	auto key_vector = common::GetMapKeyVector(identity_data);
	std::sort(key_vector.begin(), key_vector.end());
	for (const auto &key : key_vector) {
		top_ss << json::EscapeString(key);
		top_ss << R"(:)";
		if (identity_data[key].size() == 1) {
			top_ss << "\"" + json::EscapeString(identity_data[key][0]) + "\"";
		} else {
			stringstream items_ss;
			items_ss << "[";
			for (const auto &str : identity_data[key]) {
				items_ss << "\"" + json::EscapeString(str) + "\",";
			}
			auto items_str = items_ss.str();
			// replace the trailing comma with the closing square bracket
			items_str[items_str.size() - 1] = ']';
			top_ss << items_str;
		}
		top_ss << R"(,)";
	}
	auto str = top_ss.str();
	if (str[str.size() - 1] == ',') {
		// replace the trailing comma with the closing square bracket
		str.pop_back();
	}
	str.push_back('}');

	return str;
}

} // namespace identity_parser
} // namespace common
} // namespace mender
