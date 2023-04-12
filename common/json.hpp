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

#ifndef MENDER_COMMON_JSON_HPP
#define MENDER_COMMON_JSON_HPP

#include <config.h>

#include <string>
#include <map>
#include <unordered_map>

#include <common/error.hpp>
#include <common/expected.hpp>
#include <common/io.hpp>

#ifdef MENDER_USE_NLOHMANN_JSON
#include <nlohmann/json.hpp>
#endif

namespace mender {
namespace common {
namespace json {

using namespace std;

namespace error = mender::common::error;
namespace io = mender::common::io;

enum JsonErrorCode {
	NoError = 0,
	ParseError,
	KeyError,
	IndexError,
	TypeError,
};

class JsonErrorCategoryClass : public std::error_category {
public:
	const char *name() const noexcept override;
	string message(int code) const override;
};
extern const JsonErrorCategoryClass JsonErrorCategory;

error::Error MakeError(JsonErrorCode code, const string &msg);

using ExpectedString = mender::common::expected::ExpectedString;
using ExpectedInt64 = mender::common::expected::ExpectedInt64;
using ExpectedBool = mender::common::expected::ExpectedBool;
using ExpectedSize = mender::common::expected::ExpectedSize;

class Json {
public:
	using ExpectedJson = expected::expected<Json, error::Error>;
	using ChildrenMap = map<string, Json>;
	using ExpectedChildrenMap = expected::expected<ChildrenMap, error::Error>;

	Json() = default;

	string Dump(const int indent = 2) const;

	ExpectedJson Get(const char *child_key) const;
	ExpectedJson operator[](const char *child_key) const {
		return this->Get(child_key);
	}
	ExpectedJson Get(const string &child_key) const {
		return this->Get(child_key.data());
	}
	ExpectedJson operator[](const string &child_key) const {
		return this->Get(child_key.data());
	}
	ExpectedJson Get(const size_t idx) const;
	ExpectedJson operator[](const size_t idx) const {
		return this->Get(idx);
	}

	ExpectedChildrenMap GetChildren() const;

	bool IsObject() const;
	bool IsArray() const;
	bool IsString() const;
	bool IsInt() const;
	bool IsBool() const;
	bool IsNull() const;

	ExpectedString GetString() const;
	ExpectedInt64 GetInt() const;
	ExpectedBool GetBool() const;

	ExpectedSize GetArraySize() const;

	friend ExpectedJson LoadFromFile(string file_path);
	friend ExpectedJson Load(string json_str);
	friend ExpectedJson Load(istream &str);
	friend ExpectedJson Load(io::Reader &reader);

private:
#ifdef MENDER_USE_NLOHMANN_JSON
	nlohmann::json n_json;
	Json(nlohmann::json n_json) :
		n_json(n_json) {};
#endif
};

using ExpectedJson = expected::expected<Json, error::Error>;
using ChildrenMap = map<string, Json>;
using ExpectedChildrenMap = expected::expected<ChildrenMap, error::Error>;

ExpectedJson LoadFromFile(string file_path);
ExpectedJson Load(string json_str);
ExpectedJson Load(istream &str);
ExpectedJson Load(io::Reader &reader);

string EscapeString(const string &str);

using ExpectedStringVector = expected::ExpectedStringVector;
using KeyValueMap = unordered_map<string, string>;
using ExpectedKeyValueMap = expected::expected<KeyValueMap, error::Error>;

ExpectedStringVector ToStringVector(const json::Json &j);
ExpectedKeyValueMap ToKeyValuesMap(const json::Json &j);
ExpectedString ToString(const json::Json &j);

} // namespace json
} // namespace common
} // namespace mender

#endif // MENDER_COMMON_JSON_HPP
