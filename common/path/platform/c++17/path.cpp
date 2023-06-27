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

#include <common/path.hpp>

#include <string>

#include <filesystem>

namespace mender {
namespace common {
namespace path {

using namespace std;
namespace fs = std::filesystem;

string JoinOne(const string &prefix, const string &suffix) {
	return (fs::path(prefix) / suffix).string();
}

string BaseName(const string &path) {
	return fs::path(path).filename().string();
}

string DirName(const string &path) {
	return fs::path(path).parent_path().string();
}

bool IsAbsolute(const string &path) {
	return fs::path(path).is_absolute();
}

} // namespace path
} // namespace common
} // namespace mender
