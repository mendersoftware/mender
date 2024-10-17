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

#include <string>
#include <unordered_map>
#include <vector>

namespace mender {
namespace common {
namespace yaml {

const YamlErrorCategoryClass YamlErrorCategory;

const char *YamlErrorCategoryClass::name() const noexcept {
	return "YamlErrorCategory";
}

string YamlErrorCategoryClass::message(int code) const {
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

error::Error MakeError(YamlErrorCode code, const string &msg) {
	return error::Error(error_condition(code, YamlErrorCategory), msg);
}

std::ostream &operator<<(std::ostream &out, const YamlErrorCode value) {
	return out << YamlErrorCategoryClass().message(value);
}

ExpectedString ToString(const yaml::Yaml &y) {
	return y.Get<string>();
}

ExpectedStringVector ToStringVector(const yaml::Yaml &y) {
	if (not y.IsArray()) {
		return expected::unexpected(
			MakeError(YamlErrorCode::ParseError, "The YAML object is not an array"));
	}
	vector<string> vector_elements {};
	size_t vector_size {y.GetArraySize().value()};
	for (size_t i = 0; i < vector_size; ++i) {
		auto element = y.Get(i).and_then(ToString);
		if (not element) {
			return expected::unexpected(element.error());
		}
		vector_elements.push_back(element.value());
	}
	return vector_elements;
}

ExpectedKeyValueMap ToKeyValueMap(const yaml::Yaml &y) {
	if (not y.IsObject()) {
		return expected::unexpected(
			MakeError(YamlErrorCode::ParseError, "The YAML is not an object"));
	}

	auto expected_children = y.GetChildren();
	if (not expected_children) {
		return expected::unexpected(expected_children.error());
	}

	unordered_map<string, string> kv_map {};

	for (const auto &kv : expected_children.value()) {
		auto expected_value = kv.second.Get<string>();
		if (not expected_value) {
			return expected::unexpected(expected_value.error());
		}
		const string key = kv.first;
		kv_map[key] = expected_value.value();
	}

	return kv_map;
}

ExpectedInt64 ToInt64(const yaml::Yaml &y) {
	return y.Get<int64_t>();
}

ExpectedBool ToBool(const yaml::Yaml &y) {
	return y.Get<bool>();
}

template <>
expected::expected<KeyValueMap, error::Error> Yaml::Get<KeyValueMap>() const {
	return ToKeyValueMap(*this);
}

template <>
expected::expected<vector<string>, error::Error> Yaml::Get<vector<string>>() const {
	return ToStringVector(*this);
}

} // namespace yaml
} // namespace common
} // namespace mender
