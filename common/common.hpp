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

#include <cstdint>
#include <cstring>
#include <string>
#include <vector>

namespace mender::common {

using namespace std;

inline static vector<uint8_t> ByteVectorFromString(const char *str) {
	return vector<uint8_t>(reinterpret_cast<const uint8_t *>(str),
		reinterpret_cast<const uint8_t *>(str + strlen(str)));
}

inline static vector<uint8_t> ByteVectorFromString(const string &str) {
	return vector<uint8_t>(reinterpret_cast<const uint8_t *>(str.data()),
		reinterpret_cast<const uint8_t *>(str.data() + str.size()));
}

inline static string StringFromByteVector(const vector<uint8_t> &vec) {
	return string(vec.data(), vec.data() + vec.size());
}

} // namespace mender::common

#endif // MENDER_COMMON_HPP
