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

#include <common/common.hpp>
#include <common/conf.hpp>
#include <common/key_value_database_lmdb.hpp>
#include <common/testing.hpp>
#include <mender-update/context.hpp>

namespace error = mender::common::error;
namespace common = mender::common;
namespace conf = mender::common::conf;
namespace context = mender::update::context;
namespace update_module = mender::update::update_module::v3;

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


class UpdateModuleFileTreeTests : public testing::Test {
protected:
	TemporaryDirectory test_state_dir;
	TemporaryDirectory test_tree_dir;
};

TEST_F(UpdateModuleFileTreeTests, PrepareFileTreeTest) {
	conf::MenderConfig cfg;
	cfg.data_store_dir = test_state_dir.Path();

	context::MenderContext ctx(cfg);
	auto err = ctx.Initialize();
	ASSERT_EQ(err, error::NoError);

	auto &db = ctx.GetMenderStoreDB();
	err = db.Write("artifact-name", common::ByteVectorFromString("artifact-name value"));
	ASSERT_EQ(err, error::NoError);
	err = db.Write("artifact-group", common::ByteVectorFromString("artifact-group value"));
	ASSERT_EQ(err, error::NoError);

	ofstream os(cfg.data_store_dir + "/device_type");
	ASSERT_TRUE(os);
	os << "device_type=Some device type" << endl;
	os.close();

	const string tree_path = test_tree_dir.Path();
	update_module::UpdateModule up_mod(ctx);
	err = up_mod.PrepareFileTree(tree_path);
	ASSERT_EQ(err, error::NoError);

	struct stat st = {0};
	int ret = stat((tree_path + "/tmp").c_str(), &st);
	EXPECT_EQ(ret, 0);
	EXPECT_EQ(st.st_mode & S_IFMT, S_IFDIR);

	st = {0};
	ret = stat((tree_path + "/header").c_str(), &st);
	EXPECT_EQ(ret, 0);
	EXPECT_EQ(st.st_mode & S_IFMT, S_IFDIR);

	ifstream is;
	string line;
	is.open(tree_path + "/version");
	getline(is, line);
	EXPECT_EQ(line, "3");
	is.close();

	is.open(tree_path + "/current_artifact_name");
	getline(is, line);
	EXPECT_EQ(line, "artifact-name value");
	is.close();

	is.open(tree_path + "/current_artifact_group");
	getline(is, line);
	EXPECT_EQ(line, "artifact-group value");
	is.close();

	is.open(tree_path + "/current_device_type");
	getline(is, line);
	EXPECT_EQ(line, "Some device type");
	is.close();
}
