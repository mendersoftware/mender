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

#ifndef MENDER_COMMON_HPP
#define MENDER_COMMON_HPP

#include <common/expected.hpp>

#include <cstdint>
#include <cstring>

#include <algorithm>
#include <string>
#include <unordered_map>
#include <vector>

// GCC and Clang method, respectively.
#if defined(__has_feature)
#if __has_feature(cxx_rtti)
#define __MENDER_RTTI_AVAILABLE
#include <typeinfo>
#endif
#elif defined(__GXX_RTTI)
#define __MENDER_RTTI_AVAILABLE
#include <typeinfo>
#endif

namespace mender {
namespace common {

using namespace std;

struct def_bool {
	bool value;

	def_bool() :
		value {false} {};
	def_bool(bool init_value) :
		value {init_value} {
	}

	operator bool() const {
		return value;
	}
};

using StringPair = std::pair<string, string>;
using ExpectedStringPair = expected::expected<StringPair, error::Error>;

inline static vector<uint8_t> ByteVectorFromString(const char *str) {
	return vector<uint8_t>(
		reinterpret_cast<const uint8_t *>(str),
		reinterpret_cast<const uint8_t *>(str + strlen(str)));
}

// Using a template here allows use of `string_view`, which is C++17. The
// includer can decide which standard to use.
template <typename STR>
vector<uint8_t> ByteVectorFromString(const STR &str) {
	return vector<uint8_t>(str.begin(), str.end());
}

inline static string StringFromByteVector(const vector<uint8_t> &vec) {
	return string(vec.begin(), vec.end());
}

mender::common::expected::ExpectedLongLong StringToLongLong(const string &str, int base = 10);

string StringToLower(const string &str);

vector<string> SplitString(const string &str, const string &delim);
string JoinStrings(const vector<string> &str, const string &delim);
vector<string> JoinStringsMaxWidth(
	const vector<string> &str, const string &delim, const size_t max_width);

template <typename T>
bool StartsWith(const T &str, const T &sub) {
	return (sub.size() <= str.size()) && equal(str.begin(), str.begin() + sub.size(), sub.begin());
}

template <typename T>
bool EndsWith(const T &str, const T &sub) {
	return (sub.size() <= str.size()) && equal(str.end() - sub.size(), str.end(), sub.begin());
}

template <typename T>
string BestAvailableTypeName(const T &object) {
#ifdef __MENDER_RTTI_AVAILABLE
	return typeid(object).name();
#else
	return "<Type name not available>";
#endif
}

static inline bool VectorContainsString(const vector<string> &vec, const string &str) {
	return std::find(vec.begin(), vec.end(), str) != vec.end();
}

static inline string StringVectorToString(const vector<string> &vec, const string delim = ",") {
	string ret = "{";
	auto sz = vec.size();
	if (sz > 0) {
		for (decltype(sz) i = 0; i < (sz - 1); i++) {
			ret += "\"" + vec[i] + "\"" + delim;
		}
		ret += "\"" + vec[sz - 1] + "\"";
	}
	ret += "}";
	return ret;
}

template <typename ValueType>
static inline bool MapContainsStringKey(
	const unordered_map<string, ValueType> &map, const string &str) {
	return map.find(str) != map.end();
}

template <typename KeyType, typename ValueType>
static inline vector<KeyType> GetMapKeyVector(const unordered_map<KeyType, ValueType> &map) {
	vector<KeyType> ret;
	ret.reserve(map.size());
	for (const auto &kv : map) {
		ret.push_back(kv.first);
	}
	return ret;
}

} // namespace common
} // namespace mender

#endif // MENDER_COMMON_HPP
