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

template <>
expected::expected<KeyValueMap, error::Error> Json::Get<KeyValueMap>() const {
	return ToKeyValuesMap(*this);
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
	return GetInt();
}

template <>
expected::expected<double, error::Error> Json::Get<double>() const {
	return GetDouble();
}

template <>
expected::expected<bool, error::Error> Json::Get<bool>() const {
	return GetBool();
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

template <typename T>
expected::expected<T, error::Error> Get(
	const json::Json &json, const string &key, MissingOk missing_ok) {
	auto exp_value = json.Get(key);
	if (!exp_value) {
		if (missing_ok == MissingOk::Yes
			&& exp_value.error().code != json::MakeError(json::KeyError, "").code) {
			return T();
		} else {
			auto err = exp_value.error();
			err.message += ": Could not get `" + key + "` from state data";
			return expected::unexpected(err);
		}
	}
	return exp_value.value().Get<T>();
}
// The number of instantiations is pretty much set in stone since it depends on the number of JSON
// types, which isn't going to change. So use explicit instantiation for compile time efficiency.
template expected::expected<KeyValueMap, error::Error> Get(
	const json::Json &json, const string &key, MissingOk missing_ok);
template expected::expected<vector<string>, error::Error> Get(
	const json::Json &json, const string &key, MissingOk missing_ok);
template expected::expected<string, error::Error> Get(
	const json::Json &json, const string &key, MissingOk missing_ok);
template expected::expected<int64_t, error::Error> Get(
	const json::Json &json, const string &key, MissingOk missing_ok);
template expected::expected<double, error::Error> Get(
	const json::Json &json, const string &key, MissingOk missing_ok);
template expected::expected<bool, error::Error> Get(
	const json::Json &json, const string &key, MissingOk missing_ok);

} // namespace json
} // namespace common
} // namespace mender
