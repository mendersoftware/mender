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


#include <fstream>
#include <string>

#include <gtest/gtest.h>

#include <common/testing.hpp>
#include <common/path.hpp>

using namespace std;

namespace mtesting = mender::common::testing;
namespace path = mender::common::path;


class TestFile : public testing::Test {
protected:
	mtesting::TemporaryDirectory tmpdir;

	void CreateTestFile(const string &test_fname, const string &content) {
		string manifest = content;
		ofstream os {tmpdir.Path() + "/" + test_fname};
		os << manifest;
		os.close();
	}
};

TEST_F(TestFile, TestAreFilesIdentical) {
	string file_one = R"(
        api_version: mender/v1
        kind: update_manifest
        version: system-core-v1
        )";

	string file_one_identical = R"(
        api_version: mender/v1
        kind: update_manifest
        version: system-core-v1
        )";

	string file_two = R"(
        api_version: mender/v2
        kind: update_manifest
        version: system-core-v1
        )";

	const string &file_one_path = tmpdir.Path() + "/file_one.yaml";
	const string &file_one_identical_path = tmpdir.Path() + "/file_one_identical.yaml";
	const string &file_two_path = tmpdir.Path() + "/file_two.yaml";

	CreateTestFile("file_one.yaml", file_one);
	EXPECT_TRUE(path::FileExists(file_one_path));

	CreateTestFile("file_one_identical.yaml", file_one);
	EXPECT_TRUE(path::FileExists(file_one_identical_path));

	CreateTestFile("file_two.yaml", file_two);
	EXPECT_TRUE(path::FileExists(file_two_path));

	auto different_files = path::AreFilesIdentical(file_one_path, file_two_path);
	EXPECT_FALSE(different_files.value());

	auto same_file = path::AreFilesIdentical(file_one_path, file_one_path);
	EXPECT_TRUE(same_file.value());

	auto identical_new_file = path::AreFilesIdentical(file_one_path, file_one_identical_path);
	EXPECT_TRUE(identical_new_file.value());
}
