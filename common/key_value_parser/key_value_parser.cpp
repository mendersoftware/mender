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

#include <common/key_value_parser.hpp>

#include <string>
#include <vector>

#include <common/error.hpp>
#include <common/expected.hpp>

namespace mender::common::key_value_parser {

using namespace std;

ExpectedKeyValuesMap ParseKeyValues(const vector<string> &items, char delimiter) {
	KeyValuesMap ret;
	string invalid_data = "";

	for (auto str : items) {
		auto delim_pos = str.find(delimiter);
		if (delim_pos == string::npos) {
			invalid_data = str;
			break;
		}

		string key = str.substr(0, delim_pos);
		string value = str.substr(delim_pos + 1, str.size() - delim_pos - 1);
		if (ret.count(key) != 0) {
			ret[key].push_back(value);
		} else {
			ret[key] = {value};
		}
	}

	if (invalid_data != "") {
		return ExpectedKeyValuesMap(KeyValueParserError(
			KeyValueParserErrorCode::InvalidData, "Invalid data given: '" + invalid_data + "'"));
	}

	return ExpectedKeyValuesMap(ret);
}

} // namespace mender::common::key_value_parser
