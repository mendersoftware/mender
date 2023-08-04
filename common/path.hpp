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

#ifndef MENDER_COMMON_PATH_HPP
#define MENDER_COMMON_PATH_HPP

#include <functional>
#include <string>

#include <common/expected.hpp>
#include <common/log.hpp>

namespace mender {
namespace common {
namespace path {

using namespace std;

namespace expected = mender::common::expected;

string JoinOne(const string &prefix, const string &path);

template <typename... Paths>
string Join(const string &prefix, const Paths &...paths) {
	string final_path {prefix};
	for (const auto &path : {paths...}) {
		final_path = JoinOne(final_path, path);
	}
	return final_path;
}

string BaseName(const string &path);
string DirName(const string &path);

bool IsAbsolute(const string &path);

expected::ExpectedUnorderedSet<string> ListFiles(const string &in, function<bool(string)> matcher);

} // namespace path
} // namespace common
} // namespace mender

#endif // MENDER_COMMON_PATH_HPP
