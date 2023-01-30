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

#include <fstream>
#include <string>
#include <nlohmann/json.hpp>

using njson = nlohmann::json;
using namespace std;

namespace mender::common::json {

ExpectedJson LoadFromFile(string file_path) {
	ifstream f(file_path);
	try {
		njson parsed = njson::parse(f);
		Json j = Json(parsed);
		return ExpectedJson(j);
	} catch (njson::parse_error &e) {
		auto err = MakeError(
			JsonErrorCode::ParseError, "Failed to parse '" + file_path + "': " + e.what());
		return ExpectedJson(err);
	}
}

ExpectedJson LoadFromString(string json_str) {
	try {
		njson parsed = njson::parse(json_str);
		Json j = Json(parsed);
		return ExpectedJson(j);
	} catch (njson::parse_error &e) {
		auto err = MakeError(
			JsonErrorCode::ParseError, "Failed to parse '''" + json_str + "''': " + e.what());
		return ExpectedJson(err);
	}
}

string Json::Dump(const int indent) const {
	return this->n_json.dump(indent);
}

ExpectedJson Json::Get(const char *child_key) const {
	if (!this->n_json.is_object()) {
		auto err = MakeError(
			JsonErrorCode::TypeError, "Invalid JSON type to get '" + string(child_key) + "' from");
		return ExpectedJson(err);
	}

	bool contains = this->n_json.contains(child_key);
	if (!contains) {
		auto err =
			MakeError(JsonErrorCode::KeyError, "Key '" + string(child_key) + "' doesn't exist");
		return ExpectedJson(err);
	}

	njson n_json = this->n_json[child_key];
	Json j = Json(n_json);
	return ExpectedJson(j);
}

ExpectedJson Json::Get(const size_t idx) const {
	if (!this->n_json.is_array()) {
		auto err = MakeError(
			JsonErrorCode::TypeError,
			"Invalid JSON type to get item at index " + to_string(idx) + " from");
		return ExpectedJson(err);
	}

	if (this->n_json.size() <= idx) {
		auto err =
			MakeError(JsonErrorCode::IndexError, "Index " + to_string(idx) + " out of range");
		return ExpectedJson(err);
	}

	njson n_json = this->n_json[idx];
	Json j = Json(n_json);
	return ExpectedJson(j);
}

bool Json::IsObject() const {
	return this->n_json.is_object();
}

bool Json::IsArray() const {
	return this->n_json.is_array();
}

bool Json::IsString() const {
	return this->n_json.is_string();
}

bool Json::IsInt() const {
	return this->n_json.is_number_integer();
}

bool Json::IsBool() const {
	return this->n_json.is_boolean();
}

bool Json::IsNull() const {
	return this->n_json.is_null();
}

ExpectedString Json::GetString() const {
	try {
		string s = this->n_json.get<string>();
		return ExpectedString(s);
	} catch (njson::type_error &e) {
		auto err = MakeError(JsonErrorCode::TypeError, "Type mismatch when getting string");
		return ExpectedString(err);
	}
}

ExpectedInt Json::GetInt() const {
	try {
		int s = this->n_json.get<int>();
		return ExpectedInt(s);
	} catch (njson::type_error &e) {
		auto err = MakeError(JsonErrorCode::TypeError, "Type mismatch when getting int");
		return ExpectedInt(err);
	}
}

ExpectedBool Json::GetBool() const {
	try {
		bool s = this->n_json.get<bool>();
		return ExpectedBool(s);
	} catch (njson::type_error &e) {
		auto err = MakeError(JsonErrorCode::TypeError, "Type mismatch when getting bool");
		return ExpectedBool(err);
	}
}

ExpectedSize Json::GetArraySize() const {
	if (!this->n_json.is_array()) {
		auto err = MakeError(JsonErrorCode::TypeError, "Not a JSON array");
		return ExpectedSize(err);
	} else {
		return ExpectedSize(this->n_json.size());
	}
}

} // namespace mender::common::json
