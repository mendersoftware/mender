// Copyright 2024 Northern.tech AS
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

#ifndef MENDER_COMMON_YAML_HPP
#define MENDER_COMMON_YAML_HPP

#include <common/config.h>

#include <map>
#include <string>
#include <unordered_map>

#include <common/common.hpp>
#include <common/error.hpp>
#include <common/expected.hpp>
#include <common/io.hpp>

#ifdef MENDER_USE_YAML_CPP
#include <yaml-cpp/yaml.h>
#endif

namespace mender {
namespace common {
namespace yaml {

using namespace std;

namespace error = mender::common::error;
namespace io = mender::common::io;
namespace common = mender::common;

enum YamlErrorCode {
	NoError = 0,
	ParseError,
	KeyError,
	IndexError,
	TypeError,
};

class YamlErrorCategoryClass : public error_category {
public:
	const char *name() const noexcept override;
	string message(int code) const override;
};
extern const YamlErrorCategoryClass YamlErrorCategory;

error::Error MakeError(YamlErrorCode code, const string &msg);

using ExpectedString = mender::common::expected::ExpectedString;
using ExpectedInt64 = mender::common::expected::ExpectedInt64;
using ExpectedDouble = mender::common::expected::ExpectedDouble;
using ExpectedBool = mender::common::expected::ExpectedBool;
using ExpectedSize = mender::common::expected::ExpectedSize;

class Yaml;
using ExpectedYaml = expected::expected<Yaml, error::Error>;
using ChildrenMap = map<string, Yaml>;
using ExpectedChildrenMap = expected::expected<ChildrenMap, error::Error>;

class Yaml {
public:
	Yaml() = default;

	Yaml(const Yaml &) = default;
	Yaml &operator=(const Yaml &) = default;

	Yaml(Yaml &&) = default;
	Yaml &operator=(Yaml &&) = default;

	string Dump(const int indent = 2) const;

	ExpectedYaml Get(const char *child_key) const;
	ExpectedYaml operator[](const char *child_key) const {
		return this->Get(child_key);
	}
	ExpectedYaml Get(const string &child_key) const {
		return this->Get(child_key.data());
	}
	ExpectedYaml operator[](const string &child_key) const {
		return this->Get(child_key.data());
	}
	ExpectedYaml Get(const size_t idx) const;
	ExpectedYaml operator[](const size_t idx) const {
		return this->Get(idx);
	}

	ExpectedChildrenMap GetChildren() const;

	bool IsObject() const;
	bool IsArray() const;
	bool IsString() const;
	bool IsInt() const;
	bool IsNumber() const;
	bool IsDouble() const;
	bool IsBool() const;
	bool IsNull() const;

	template <typename T>
	expected::expected<T, error::Error> Get() const;

	ExpectedSize GetArraySize() const;

	string GetType() const;
	friend std::ostream &operator<<(std::ostream &os, const Yaml &y) {
#ifdef MENDER_USE_YAML_CPP
		os << y.n_yaml;
#endif
		return os;
	}

	friend ExpectedYaml LoadFromFile(string file_path);
	friend ExpectedYaml Load(string yaml_str);
	friend ExpectedYaml Load(istream &str);
	friend ExpectedYaml Load(io::Reader &reader);

public:
#ifdef MENDER_USE_YAML_CPP
	YAML::Node n_yaml;
	Yaml(YAML::Node n_yaml) :
		n_yaml(n_yaml) {};
#endif
};

ExpectedYaml LoadFromFile(string file_path);
ExpectedYaml Load(string yaml_str);
ExpectedYaml Load(istream &str);
ExpectedYaml Load(io::Reader &reader);

using ExpectedStringVector = expected::ExpectedStringVector;
using KeyValueMap = unordered_map<string, string>;
using ExpectedKeyValueMap = expected::expected<KeyValueMap, error::Error>;

ExpectedStringVector ToStringVector(const yaml::Yaml &j);
ExpectedKeyValueMap ToKeyValueMap(const yaml::Yaml &j);
ExpectedString ToString(const yaml::Yaml &j);
ExpectedInt64 ToInt(const yaml::Yaml &j);
ExpectedBool ToBool(const yaml::Yaml &j);

enum class MissingOk {
	No,
	Yes,
};

template <typename T>
expected::expected<T, error::Error> Get(
	const yaml::Yaml &yaml, const string &key, MissingOk missing_ok);

} // namespace yaml
} // namespace common
} // namespace mender

#endif // MENDER_COMMON_YAML_HPP
