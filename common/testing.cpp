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

#include <common/testing.hpp>

#include <filesystem>
#include <random>
#include <iostream>

#include <common/json.hpp>
#include <common/processes.hpp>

namespace mender {
namespace common {
namespace testing {

namespace fs = std::filesystem;

TemporaryDirectory::TemporaryDirectory() {
	fs::path path = fs::temp_directory_path();
	path.append("mender-test-" + std::to_string(std::random_device()()));
	fs::create_directories(path);
	path_ = path;
}

TemporaryDirectory::~TemporaryDirectory() {
	fs::remove_all(path_);
}

std::string TemporaryDirectory::Path() {
	return path_;
}

::testing::AssertionResult FileContains(const string &filename, const string &expected_content) {
	ifstream is {filename};
	ostringstream contents_s;
	contents_s << is.rdbuf();
	string contents {contents_s.str()};
	if (contents == expected_content) {
		return ::testing::AssertionSuccess();
	}
	return ::testing::AssertionFailure()
		   << "Expected: '" << expected_content << "' Got: '" << contents << "'";
}


::testing::AssertionResult FileJsonEquals(const string &filename, const string &expected_content) {
	ifstream is {filename};
	json::Json contents = json::Load(is).value();
	json::Json expected_contents = json::Load(expected_content).value();
	if (contents.Dump() == expected_contents.Dump()) {
		return ::testing::AssertionSuccess();
	}
	return ::testing::AssertionFailure()
		   << "Expected: '" << contents.Dump() << "' Got: '" << expected_contents.Dump() << "'";
}

::testing::AssertionResult FilesEqual(const string &filename1, const string &filename2) {
	processes::Process proc({"diff", "-u", filename1, filename2});
	auto err = proc.Run();
	if (err == error::NoError) {
		return ::testing::AssertionSuccess();
	}
	// Some extra information in case of failure.
	cout << "ls -l " << filename1 << " " << filename2 << endl;
	processes::Process listdir({"ls", "-l", filename1, filename2});
	listdir.Run();
	return ::testing::AssertionFailure() << filename1 << " and " << filename2 << " differ";
}

} // namespace testing
} // namespace common
} // namespace mender
