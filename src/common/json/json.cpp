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
	case EmptyError:
		return "Empty input error";
	default:
		return "Unknown";
	}
}

error::Error MakeError(JsonErrorCode code, const string &msg) {
	return error::Error(error_condition(code, JsonErrorCategory), msg);
}

template <>
expected::expected<KeyValueMap, error::Error> Json::Get<KeyValueMap>() const {
	return ToKeyValueMap(*this);
}

template <>
expected::expected<vector<string>, error::Error> Json::Get<vector<string>>() const {
	return ToStringVector(*this);
}

template <>
expected::expected<string, error::Error> Json::Get<string>() const {
	return GetString();
}

template <>
expected::expected<int64_t, error::Error> Json::Get<int64_t>() const {
	return GetInt64();
}

template <>
expected::expected<double, error::Error> Json::Get<double>() const {
	return GetDouble();
}

template <>
expected::expected<bool, error::Error> Json::Get<bool>() const {
	return GetBool();
}

string EscapeString(const string &str) {
	// Reserve space to reduce reallocations. Assume +10% size after escaping
	string ret;
	ret.reserve(str.length() + str.length() / 10);

	// Escape all control characters (U+0000 through U+001F)
	// see https://www.json.org/json-en.html and https://datatracker.ietf.org/doc/html/rfc8259
	for (size_t i = 0; i < str.length(); i++) {
		unsigned char c = static_cast<unsigned char>(str[i]);

		switch (c) {
		case '\\':
			ret += "\\\\";
			break;
		case '\"':
			ret += "\\\"";
			break;
		case '\n':
			ret += "\\n";
			break;
		case '\t':
			ret += "\\t";
			break;
		case '\r':
			ret += "\\r";
			break;
		case '\f':
			ret += "\\f";
			break;
		case '\b':
			ret += "\\b";
			break;
		default:
			// Control characters (0x00-0x1F) and DEL (0x7F) must be escaped
			// using \uXXXX format per RFC 8259
			if (c < 0x20 || c == 0x7F) {
				char buf[7];
				snprintf(buf, sizeof(buf), "\\u%04x", c);
				ret += buf;
			} else {
				ret += static_cast<char>(c);
			}
			break;
		}
	}

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

ExpectedKeyValueMap ToKeyValueMap(const json::Json &j) {
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

ExpectedInt64 ToInt64(const json::Json &j) {
	return j.GetInt64();
}

ExpectedBool ToBool(const json::Json &j) {
	return j.GetBool();
}

} // namespace json
} // namespace common
} // namespace mender
