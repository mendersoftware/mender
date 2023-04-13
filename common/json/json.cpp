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

#include <common/json.hpp>

#include <string>
#include <unordered_map>
#include <vector>

namespace mender {
namespace common {
namespace json {

const JsonErrorCategoryClass JsonErrorCategory;

const char *JsonErrorCategoryClass::name() const noexcept {
	return "JsonErrorCategory";
}

string JsonErrorCategoryClass::message(int code) const {
	switch (code) {
	case NoError:
		return "Success";
	case ParseError:
		return "Parse error";
	case KeyError:
		return "Key error";
	case IndexError:
		return "Index error";
	case TypeError:
		return "Type error";
	default:
		return "Unknown";
	}
}

error::Error MakeError(JsonErrorCode code, const string &msg) {
	return error::Error(error_condition(code, JsonErrorCategory), msg);
}

inline void StringReplaceAll(string &str, const string &what, const string &with) {
	for (string::size_type pos {}; str.npos != (pos = str.find(what.data(), pos, what.length()));
		 pos += with.length()) {
		str.replace(pos, what.length(), with);
	}
}

string EscapeString(const string &str) {
	string ret {str};

	// see https://www.json.org/json-en.html
	StringReplaceAll(ret, "\\", "\\\\");
	StringReplaceAll(ret, "\"", "\\\"");
	StringReplaceAll(ret, "\n", "\\n");
	StringReplaceAll(ret, "\t", "\\t");
	StringReplaceAll(ret, "\r", "\\r");
	StringReplaceAll(ret, "\f", "\\f");
	StringReplaceAll(ret, "\b", "\\b");

	return ret;
}

ExpectedString ToString(const json::Json &j) {
	return j.GetString();
}

ExpectedStringVector ToStringVector(const json::Json &j) {
	if (!j.IsArray()) {
		return expected::unexpected(
			MakeError(JsonErrorCode::ParseError, "The JSON object is not an array"));
	}
	vector<string> vector_elements {};
	size_t vector_size {j.GetArraySize().value()};
	for (size_t i = 0; i < vector_size; ++i) {
		auto element = j.Get(i).and_then(ToString);
		if (!element) {
			return expected::unexpected(element.error());
		}
		vector_elements.push_back(element.value());
	}
	return vector_elements;
}

ExpectedKeyValueMap ToKeyValuesMap(const json::Json &j) {
	if (!j.IsObject()) {
		return expected::unexpected(
			MakeError(JsonErrorCode::ParseError, "The JSON is not an object"));
	}

	auto expected_children = j.GetChildren();
	if (!expected_children) {
		return expected::unexpected(expected_children.error());
	}

	unordered_map<string, string> kv_map {};

	for (const auto &kv : expected_children.value()) {
		string key = kv.first;
		auto expected_value = kv.second.GetString();
		if (!expected_value) {
			return expected::unexpected(expected_value.error());
		}
		kv_map[key] = expected_value.value();
	}

	return kv_map;
}

} // namespace json
} // namespace common
} // namespace mender
