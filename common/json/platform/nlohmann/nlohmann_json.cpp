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
#include <map>
#include <string>

#include <nlohmann/json.hpp>

#include <common/io.hpp>

using njson = nlohmann::json;
using namespace std;
namespace expected = mender::common::expected;
namespace error = mender::common::error;
namespace io = mender::common::io;

namespace mender::common::json {

static error::Error GetErrorFromException(exception &e, const string &context_message) {
	try {
		// Trick to delegate the exception into the handlers below: Rethrow the exception
		// (Lippincott Function). The `e` argument is not actually needed, but included for
		// clarity.
		throw;
	} catch (njson::parse_error &e) {
		return MakeError(JsonErrorCode::ParseError, context_message + ": " + e.what());
	} catch (njson::type_error &e) {
		return MakeError(JsonErrorCode::TypeError, context_message + ": " + e.what());
	} catch (system_error &e) {
		return error::Error(e.code().default_error_condition(), context_message + ": " + e.what());
	} catch (exception &e) {
		return error::MakeError(error::GenericError, context_message + ": " + e.what());
	}
}

ExpectedJson LoadFromFile(string file_path) {
	ifstream f;
	errno = 0;
	f.open(file_path);
	if (!f) {
		int io_errno = errno;
		auto err = error::Error(
			std::generic_category().default_error_condition(io_errno),
			"Failed to open '" + file_path + "': " + strerror(io_errno));
		return expected::unexpected(err);
	}

	try {
		njson parsed = njson::parse(f);
		Json j = Json(parsed);
		return ExpectedJson(j);
	} catch (exception &e) {
		return expected::unexpected(
			GetErrorFromException(e, "Failed to parse '" + file_path + "'"));
	}
}

ExpectedJson Load(string json_str) {
	try {
		njson parsed = njson::parse(json_str);
		Json j = Json(parsed);
		return ExpectedJson(j);
	} catch (exception &e) {
		return expected::unexpected(GetErrorFromException(e, "Failed to parse '" + json_str + "'"));
	}
}

ExpectedJson Load(istream &str) {
	try {
		njson parsed = njson::parse(str);
		Json j = Json(parsed);
		return ExpectedJson(j);
	} catch (exception &e) {
		return expected::unexpected(GetErrorFromException(e, "Failed to parse JSON from stream"));
	}
}

ExpectedJson Load(io::Reader &reader) {
	auto str_ptr = reader.GetStream();
	try {
		return Load(*str_ptr);
	} catch (exception &e) {
		return expected::unexpected(GetErrorFromException(e, "Failed to parse JSON from stream"));
	}
}

string Json::Dump(const int indent) const {
	return this->n_json.dump(indent);
}

ExpectedJson Json::Get(const char *child_key) const {
	if (!this->n_json.is_object()) {
		auto err = MakeError(
			JsonErrorCode::TypeError, "Invalid JSON type to get '" + string(child_key) + "' from");
		return expected::unexpected(err);
	}

	bool contains = this->n_json.contains(child_key);
	if (!contains) {
		auto err =
			MakeError(JsonErrorCode::KeyError, "Key '" + string(child_key) + "' doesn't exist");
		return expected::unexpected(err);
	}

	njson n_json = this->n_json[child_key];
	Json j = Json(n_json);
	return j;
}

ExpectedJson Json::Get(const size_t idx) const {
	if (!this->n_json.is_array()) {
		auto err = MakeError(
			JsonErrorCode::TypeError,
			"Invalid JSON type to get item at index " + to_string(idx) + " from");
		return expected::unexpected(err);
	}

	if (this->n_json.size() <= idx) {
		auto err =
			MakeError(JsonErrorCode::IndexError, "Index " + to_string(idx) + " out of range");
		return expected::unexpected(err);
	}

	njson n_json = this->n_json[idx];
	return Json(n_json);
}

ExpectedChildrenMap Json::GetChildren() const {
	if (!this->IsObject()) {
		auto err = MakeError(JsonErrorCode::TypeError, "Invalid JSON type to get children from");
		return expected::unexpected(err);
	}

	ChildrenMap ret {};
	for (const auto &item : this->n_json.items()) {
		ret[item.key()] = Json(item.value());
	}
	return ExpectedChildrenMap(ret);
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

bool Json::IsNumber() const {
	return this->n_json.is_number();
}

bool Json::IsDouble() const {
	return this->n_json.is_number_float();
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
		return s;
	} catch (exception &e) {
		return expected::unexpected(GetErrorFromException(e, "Type mismatch when getting string"));
	}
}

ExpectedInt64 Json::GetInt() const {
	try {
		int64_t s {this->n_json.get<int64_t>()};
		return s;
	} catch (exception &e) {
		return expected::unexpected(GetErrorFromException(e, "Type mismatch when getting int"));
	}
}

ExpectedDouble Json::GetDouble() const {
	try {
		return this->n_json.get<double>();
	} catch (exception &e) {
		return expected::unexpected(GetErrorFromException(e, "Type mismatch when getting double"));
	}
}

ExpectedBool Json::GetBool() const {
	try {
		bool s = this->n_json.get<bool>();
		return s;
	} catch (exception &e) {
		return expected::unexpected(GetErrorFromException(e, "Type mismatch when getting bool"));
	}
}

ExpectedSize Json::GetArraySize() const {
	if (!this->n_json.is_array()) {
		auto err = MakeError(JsonErrorCode::TypeError, "Not a JSON array");
		return expected::unexpected(err);
	} else {
		return this->n_json.size();
	}
}

} // namespace mender::common::json
