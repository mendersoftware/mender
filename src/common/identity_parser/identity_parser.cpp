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

string DumpIdentityData(const kvp::KeyValuesMap &identity_data) {
	stringstream top_ss;
	top_ss << "{";
	auto key_vector = common::GetMapKeyVector(identity_data);
	std::sort(key_vector.begin(), key_vector.end());
	for (const auto &key : key_vector) {
		top_ss << R"(")" << json::EscapeString(key) << R"(":)";
		if (identity_data.at(key).size() == 1) {
			top_ss << R"(")" + json::EscapeString(identity_data.at(key)[0]) + R"(")";
		} else {
			stringstream items_ss;
			items_ss << "[";
			for (const auto &str : identity_data.at(key)) {
				items_ss << R"(")" + json::EscapeString(str) + R"(",)";
			}
			auto items_str = items_ss.str();
			// replace the trailing comma with the closing square bracket
			items_str[items_str.size() - 1] = ']';
			top_ss << items_str;
		}
		top_ss << ",";
	}
	auto str = top_ss.str();
	if (str[str.size() - 1] == ',') {
		// replace the trailing comma with the closing curly bracket
		str.pop_back();
	}
	str.push_back('}');

	return str;
}

} // namespace identity_parser
} // namespace common
} // namespace mender
