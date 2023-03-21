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

#include <mender-update/update_module/v3/update_module.hpp>

#include <sys/stat.h>

#include <algorithm>
#include <fstream>

#include <gtest/gtest.h>
#include <gmock/gmock.h>

#include <common/conf.hpp>
#include <common/testing.hpp>

namespace update_module = mender::update::update_module::v3;
namespace conf = mender::common::conf;

using namespace std;
using namespace mender::common::testing;

class UpdateModuleTests : public testing::Test {
protected:
	TemporaryDirectory temp_dir;
	string test_scripts_dir;

	bool PrepareTestScriptsDir() {
		test_scripts_dir = temp_dir.Path() + "/modules";
		if (mkdir(test_scripts_dir.c_str(), 0700) != 0) {
			return false;
		}
		test_scripts_dir += "/v3";
		if (mkdir(test_scripts_dir.c_str(), 0700) != 0) {
			return false;
		}

		return true;
	}

	bool PrepareTestFile(const string &name, bool executable) {
		string test_file_path = test_scripts_dir + "/" + name;
		ofstream os(test_file_path);
		os.close();

		if (executable) {
			int ret = chmod(test_file_path.c_str(), 0700);
			return ret == 0;
		}
		return true;
	}
};

TEST_F(UpdateModuleTests, DiscoverUpdateModulesTest) {
	auto ok = PrepareTestScriptsDir();
	ASSERT_TRUE(ok);

	ok = PrepareTestFile("file1", false);
	ASSERT_TRUE(ok);

	ok = PrepareTestFile("script1", true);
	ASSERT_TRUE(ok);

	ok = PrepareTestFile("file2", false);
	ASSERT_TRUE(ok);

	ok = PrepareTestFile("script2", true);
	ASSERT_TRUE(ok);

	auto cfg = conf::MenderConfig();
	cfg.data_store_dir = temp_dir.Path();

	auto ex_modules = update_module::DiscoverUpdateModules(cfg);
	ASSERT_TRUE(ex_modules);
	auto modules = ex_modules.value();
	EXPECT_EQ(modules.size(), 2);
	EXPECT_EQ(count(modules.cbegin(), modules.cend(), test_scripts_dir + "/script1"), 1);
	EXPECT_EQ(count(modules.cbegin(), modules.cend(), test_scripts_dir + "/script2"), 1);
}

TEST_F(UpdateModuleTests, DiscoverUpdateModulesNoExistTest) {
	auto ok = PrepareTestScriptsDir();
	ASSERT_TRUE(ok);

	auto cfg = conf::MenderConfig();
	cfg.data_store_dir = temp_dir.Path();

	auto ex_modules = update_module::DiscoverUpdateModules(cfg);
	ASSERT_TRUE(ex_modules);

	auto modules = ex_modules.value();
	EXPECT_EQ(modules.size(), 0);
}

TEST_F(UpdateModuleTests, DiscoverUpdateModulesEmptyDirTest) {
	auto cfg = conf::MenderConfig();
	cfg.data_store_dir = temp_dir.Path();

	auto ex_modules = update_module::DiscoverUpdateModules(cfg);
	ASSERT_TRUE(ex_modules);

	auto modules = ex_modules.value();
	EXPECT_EQ(modules.size(), 0);
}

TEST_F(UpdateModuleTests, DiscoverUpdateModulesNoExecutablesTest) {
	auto ok = PrepareTestScriptsDir();
	ASSERT_TRUE(ok);

	ok = PrepareTestFile("file1", false);
	ASSERT_TRUE(ok);

	ok = PrepareTestFile("file2", false);
	ASSERT_TRUE(ok);

	auto cfg = conf::MenderConfig();
	cfg.data_store_dir = temp_dir.Path();

	auto ex_modules = update_module::DiscoverUpdateModules(cfg);
	ASSERT_TRUE(ex_modules);
	auto modules = ex_modules.value();
	EXPECT_EQ(modules.size(), 0);
}
