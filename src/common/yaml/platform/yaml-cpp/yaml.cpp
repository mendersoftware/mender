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

#include <common/yaml.hpp>

#include <fstream>
#include <map>
#include <string>

#include <yaml-cpp/yaml.h>

#include <common/io.hpp>

using namespace std;

namespace expected = mender::common::expected;
namespace error = mender::common::error;
namespace io = mender::common::io;

namespace mender {
namespace common {
namespace yaml {

ExpectedYaml LoadFromFile(string file_path) {
	ifstream f;
	errno = 0;
	f.open(file_path);
	if (not f) {
		int io_errno = errno;
		auto err = error::Error(
			std::generic_category().default_error_condition(io_errno),
			"Failed to open '" + file_path + "': " + strerror(io_errno));
		return expected::unexpected(err);
	}

	try {
		YAML::Node loaded_yaml = YAML::LoadFile(file_path);
		return loaded_yaml;
	} catch (YAML::Exception &e) {
		return expected::unexpected(
			MakeError(ParseError, "Failed to parse '" + file_path + "'" + e.what()));
	}
}

ExpectedYaml Load(string yaml_str) {
	try {
		YAML::Node loaded_yaml = YAML::Load(yaml_str);
		return loaded_yaml;
	} catch (YAML::Exception &e) {
		return expected::unexpected(
			MakeError(ParseError, "Failed to parse '" + yaml_str + "'" + e.what()));
	}
}

ExpectedYaml Load(istream &str) {
	try {
		YAML::Node loaded_yaml = YAML::Load(str);
		return loaded_yaml;
	} catch (YAML::Exception &e) {
		return expected::unexpected(
			MakeError(ParseError, "Failed to parse YAML from stream: " + string(e.what())));
	}
}

ExpectedYaml Load(io::Reader &reader) {
	auto str_ptr = reader.GetStream();
	return Load(*str_ptr);
}

bool Yaml::IsObject() const {
	return n_yaml.IsMap();
}
bool Yaml::IsArray() const {
	return n_yaml.IsSequence();
}

template <typename T>
bool Is(const YAML::Node &n) {
	try {
		n.as<T>();
		return true;
	} catch (YAML::Exception &e) {
		return false;
	}
}

bool Yaml::IsString() const {
	return Is<string>(this->n_yaml);
}
bool Yaml::IsInt64() const {
	return Is<int64_t>(this->n_yaml);
}
bool Yaml::IsDouble() const {
	return Is<double>(this->n_yaml);
}
bool Yaml::IsNumber() const {
	if (not n_yaml.IsScalar()) {
		return false;
	}
	return IsInt64() or IsDouble();
}
bool Yaml::IsBool() const {
	return Is<bool>(this->n_yaml);
}
bool Yaml::IsNull() const {
	return this->n_yaml.IsNull();
}

string Yaml::Dump(const int indent) const {
	stringstream ss {};
	YAML::Emitter out {ss};
	out.SetIndent(indent);
	out << n_yaml;
	return ss.str();
}

string GetYamlNodeType(YAML::Node n) {
	switch (n.Type()) {
	case YAML::NodeType::Map:
		return "Map";
	case YAML::NodeType::Undefined:
		return "Undefined";
	case YAML::NodeType::Null:
		return "Null";
	case YAML::NodeType::Sequence:
		return "Sequence";
	case YAML::NodeType::Scalar:
		return "Scalar";
	}
	assert(false); // Should never happen
	return "Unknown";
}

string Yaml::GetType() const {
	return GetYamlNodeType(this->n_yaml);
}

ExpectedYaml Yaml::Get(const char *child_key) const {
	if (not n_yaml[child_key]) {
		return expected::unexpected(
			MakeError(KeyError, "Key '" + string(child_key) + "' doesn't exist"));
	}
	return YAML::Clone(n_yaml[child_key]);
}

ExpectedYaml Yaml::Get(const size_t idx) const {
	if (not n_yaml.IsSequence()) {
		return expected::unexpected(MakeError(
			TypeError,
			"The YAML node is not a Sequence. Unable to index it. The node is a: "
				+ GetYamlNodeType(n_yaml)));
	}
	if (not n_yaml[idx]) {
		return expected::unexpected(
			MakeError(IndexError, "Index " + to_string(idx) + " out of range"));
	}
	return n_yaml[idx];
}

ExpectedSize Yaml::GetArraySize() const {
	if (not n_yaml.IsSequence()) {
		return expected::unexpected(MakeError(
			TypeError, "The YAML node is a '" + GetYamlNodeType(n_yaml) + "', not a Sequence"));
	}
	return n_yaml.size();
}

ExpectedChildrenMap Yaml::GetChildren() const {
	if (not this->IsObject()) {
		return expected::unexpected(MakeError(
			TypeError,
			"The YAML node is a '" + GetYamlNodeType(n_yaml) + "', not an Map (Object)"));
	}
	ChildrenMap map {};
	for (const auto &item : this->n_yaml) {
		map[item.first.Scalar()] = item.second;
	}
	return map;
}

template <typename T>
string ToString();

template <>
string ToString<string>() {
	return "string";
}
template <>
string ToString<int64_t>() {
	return "integer";
}
template <>
string ToString<bool>() {
	return "bool";
}
template <>
string ToString<double>() {
	return "double";
}

template <typename T>
typename enable_if<
	not is_integral<T>::value or is_same<T, int64_t>::value or is_same<T, bool>::value,
	expected::expected<T, error::Error>>::type
Yaml::Get() const {
	try {
		return n_yaml.as<T>();
	} catch (YAML::Exception &e) {
		return expected::unexpected(
			MakeError(TypeError, "The YAML node is not a " + ToString<T>()));
	}
}

template expected::expected<string, error::Error> Yaml::Get<string>() const;

template expected::expected<int64_t, error::Error> Yaml::Get<int64_t>() const;

template expected::expected<double, error::Error> Yaml::Get<double>() const;

template expected::expected<bool, error::Error> Yaml::Get<bool>() const;

} // namespace yaml
} // namespace common
} // namespace mender
