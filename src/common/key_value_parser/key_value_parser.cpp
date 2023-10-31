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

namespace mender {
namespace common {
namespace key_value_parser {

using namespace std;

const KeyValueParserErrorCategoryClass KeyValueParserErrorCategory;

const char *KeyValueParserErrorCategoryClass::name() const noexcept {
	return "KeyValueParserErrorCategory";
}

string KeyValueParserErrorCategoryClass::message(int code) const {
	switch (code) {
	case NoError:
		return "Success";
	case InvalidDataError:
		return "Invalid data";
	case NoDataError:
		return "No data";
	default:
		return "Unknown";
	}
}

error::Error MakeError(KeyValueParserErrorCode code, const string &msg) {
	return error::Error(error_condition(code, KeyValueParserErrorCategory), msg);
}

ExpectedKeyValuesMap ParseKeyValues(const vector<string> &items, char delimiter) {
	KeyValuesMap ret;
	error::Error err = AddParseKeyValues(ret, items, delimiter);
	if (error::NoError != err) {
		return expected::unexpected(err);
	} else {
		return ExpectedKeyValuesMap(ret);
	}
}

error::Error AddParseKeyValues(KeyValuesMap &base, const vector<string> &items, char delimiter) {
	string invalid_data = "";

	for (auto str : items) {
		auto delim_pos = str.find(delimiter);
		if (delim_pos == string::npos) {
			invalid_data = str;
			break;
		}

		string key = str.substr(0, delim_pos);
		string value = str.substr(delim_pos + 1, str.size() - delim_pos - 1);
		if (base.count(key) != 0) {
			base[key].push_back(value);
		} else {
			base[key] = {value};
		}
	}

	if (invalid_data != "") {
		return MakeError(
			KeyValueParserErrorCode::InvalidDataError,
			"Invalid data given: '" + invalid_data + "'");
	}

	return error::NoError;
}


} // namespace key_value_parser
} // namespace common
} // namespace mender
