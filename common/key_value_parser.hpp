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

#ifndef MENDER_COMMON_KEY_VALUE_PARSER_HPP
#define MENDER_COMMON_KEY_VALUE_PARSER_HPP

#include <string>
#include <vector>
#include <unordered_map>
#include <common/error.hpp>
#include <common/expected.hpp>

namespace mender::common::key_value_parser {

using namespace std;

enum KeyValueParserErrorCode {
	InvalidData,
};

using KeyValueParserError = mender::common::error::Error<KeyValueParserErrorCode>;

using KeyValuesMap = unordered_map<string, vector<string>>;
using ExpectedKeyValuesMap = mender::common::expected::Expected<KeyValuesMap, KeyValueParserError>;

ExpectedKeyValuesMap ParseKeyValues(const vector<string> &items, char delimiter = '=');

} // namespace mender::common::key_value_parser

#endif // MENDER_COMMON_KEY_VALUE_PARSER_HPP
