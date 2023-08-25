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
#include <unordered_set>

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

bool FileExists(const string &path) {
	return fs::exists(path);
}

expected::ExpectedBool IsExecutable(const string &file_path, const bool warn) {
	fs::perms perms = fs::status(file_path).permissions();
	if ((perms & (fs::perms::owner_exec | fs::perms::group_exec | fs::perms::others_exec))
		== fs::perms::none) {
		if (warn) {
			log::Warning("'" + file_path + "' is not executable");
		}
		return false;
	}
	return true;
}

expected::ExpectedUnorderedSet<string> ListFiles(
	const string &in_directory, function<bool(string)> matcher) {
	unordered_set<string> matching_files {};
	fs::path dir_path(in_directory);
	if (!fs::exists(dir_path)) {
		auto err {errno};
		return expected::unexpected(error::Error(
			generic_category().default_error_condition(err),
			"No such file or directory: " + in_directory));
	}

	for (const auto &entry : fs::directory_iterator {dir_path}) {
		fs::path file_path = entry.path();
		if (!fs::is_regular_file(file_path)) {
			log::Warning("'" + file_path.string() + "'" + " is not a regular file. Ignoring.");
			continue;
		}

		if (matcher(file_path)) {
			matching_files.insert(file_path);
		}
	}

	return matching_files;
}

} // namespace path
} // namespace common
} // namespace mender
