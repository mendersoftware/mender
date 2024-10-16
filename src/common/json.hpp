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

#include <common/config.h>

#include <string>
#include <map>
#include <unordered_map>

#include <common/common.hpp>
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
namespace common = mender::common;

enum JsonErrorCode {
	NoError = 0,
	ParseError,
	KeyError,
	IndexError,
	TypeError,
	EmptyError,
};

class CaseInsensitiveLess {
public:
	bool operator()(const string &lhs, const string &rhs) const {
		return common::StringToLower(lhs) < common::StringToLower(rhs);
	}
};

template <class Key, class T, class IgnoredLess, class Allocator = allocator<pair<const Key, T>>>
class CaseInsensitiveMap : public map<const Key, T, CaseInsensitiveLess, Allocator> {
public:
	using CaseInsensitiveMapType = map<const Key, T, CaseInsensitiveLess, Allocator>;

	CaseInsensitiveMap() :
		CaseInsensitiveMapType() {
	}

	template <class InputIterator>
	CaseInsensitiveMap(
		InputIterator first,
		InputIterator last,
		const CaseInsensitiveLess &comp = CaseInsensitiveLess(),
		const Allocator &alloc = Allocator()) :
		CaseInsensitiveMapType(first, last, comp, alloc) {
	}
};

#ifdef MENDER_USE_NLOHMANN_JSON
using insensitive_json = nlohmann::basic_json<CaseInsensitiveMap>;
#endif

class JsonErrorCategoryClass : public error_category {
public:
	const char *name() const noexcept override;
	string message(int code) const override;
};
extern const JsonErrorCategoryClass JsonErrorCategory;

error::Error MakeError(JsonErrorCode code, const string &msg);

using ExpectedString = mender::common::expected::ExpectedString;
using ExpectedInt64 = mender::common::expected::ExpectedInt64;
using ExpectedDouble = mender::common::expected::ExpectedDouble;
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
	bool IsInt64() const;
	bool IsNumber() const;
	bool IsDouble() const;
	bool IsBool() const;
	bool IsNull() const;

	ExpectedString GetString() const;
	ExpectedInt64 GetInt64() const;
	ExpectedDouble GetDouble() const;
	ExpectedBool GetBool() const;

	// Defined in cpp file as specialized templates.
	template <typename T>
	typename enable_if<
		not is_integral<T>::value or is_same<T, int64_t>::value,
		expected::expected<T, error::Error>>::type
	Get() const;

	// Use this as a catch-all for all integral types besides int64_t. It then automates the
	// process of checking whether it fits in the requested data type.
	template <typename T>
	typename enable_if<
		is_integral<T>::value and not is_same<T, int64_t>::value,
		expected::expected<T, error::Error>>::type
	Get() const {
		auto num = Get<int64_t>();
		if (!num) {
			return expected::unexpected(num.error());
		}
		if (num.value() < numeric_limits<T>::min() or num.value() > numeric_limits<T>::max()) {
			return expected::unexpected(error::Error(
				make_error_condition(errc::result_out_of_range),
				"Json::Get(): Number " + to_string(num.value())
					+ " does not fit in requested data type"));
		}
		return static_cast<T>(num.value());
	}

	ExpectedSize GetArraySize() const;

	friend ExpectedJson LoadFromFile(string file_path);
	friend ExpectedJson Load(string json_str);
	friend ExpectedJson Load(istream &str);
	friend ExpectedJson Load(io::Reader &reader);

private:
#ifdef MENDER_USE_NLOHMANN_JSON
	insensitive_json n_json;
	Json(insensitive_json n_json) :
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
ExpectedKeyValueMap ToKeyValueMap(const json::Json &j);
ExpectedString ToString(const json::Json &j);
ExpectedInt64 ToInt64(const json::Json &j);
ExpectedBool ToBool(const json::Json &j);

template <typename T>
expected::expected<T, error::Error> To(const json::Json &j) {
	return j.Get<T>();
}

// Template which we specialize for the given type in the platform dependent implementation
template <typename DataType>
ExpectedString Dump(DataType);

enum class MissingOk {
	No,
	Yes,
};

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

} // namespace json
} // namespace common
} // namespace mender

#endif // MENDER_COMMON_JSON_HPP
