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
#include <common/error.hpp>
#include <common/expected.hpp>

#ifdef MENDER_USE_NLOHMANN_JSON
#include <nlohmann/json.hpp>
#endif

namespace mender {
namespace common {
namespace json {

using namespace std;

namespace error = mender::common::error;

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
using ExpectedInt = mender::common::expected::ExpectedInt;
using ExpectedBool = mender::common::expected::ExpectedBool;
using ExpectedSize = mender::common::expected::ExpectedSize;

class Json {
public:
	using ExpectedJson = mender::common::expected::Expected<Json, error::Error>;

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

	bool IsObject() const;
	bool IsArray() const;
	bool IsString() const;
	bool IsInt() const;
	bool IsBool() const;
	bool IsNull() const;

	ExpectedString GetString() const;
	ExpectedInt GetInt() const;
	ExpectedBool GetBool() const;

	ExpectedSize GetArraySize() const;

	friend ExpectedJson LoadFromFile(string file_path);
	friend ExpectedJson LoadFromString(string json_str);

private:
#ifdef MENDER_USE_NLOHMANN_JSON
	nlohmann::json n_json;
	Json(nlohmann::json n_json) :
		n_json(n_json) {};
#endif
};

using ExpectedJson = mender::common::expected::Expected<Json, error::Error>;

ExpectedJson LoadFromFile(string file_path);
ExpectedJson LoadFromString(string json_str);

} // namespace json
} // namespace common
} // namespace mender

#endif // MENDER_COMMON_JSON_HPP
