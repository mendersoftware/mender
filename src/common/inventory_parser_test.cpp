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

#include <common/inventory_parser.hpp>

#include <sys/stat.h>
#include <gtest/gtest.h>
#include <fstream>

#include <common/key_value_parser.hpp>
#include <common/log.hpp>
#include <common/testing.hpp>

namespace ivp = mender::common::inventory_parser;
namespace kvp = mender::common::key_value_parser;

using namespace std;
using namespace mender::common::testing;

class InventoryParserTests : public testing::Test {
protected:
	TemporaryDirectory test_scripts_dir;

	bool PrepareTestScript(const string &script_name, const string &script) {
		string test_script_path = test_scripts_dir.Path() + "/" + script_name;
		ofstream os(test_script_path);
		os << script;
		os.close();

		int ret = chmod(test_script_path.c_str(), S_IRUSR | S_IWUSR | S_IXUSR);
		return ret == 0;
	}
};

TEST_F(InventoryParserTests, GetInventoryDataOneScriptTest) {
	string script = R"(#!/bin/sh
echo "key1=value1"
echo "key2=value2"
echo "key3=value3"
echo "key1=value11"
exit 0
)";
	auto ret = PrepareTestScript("mender-inventory-script1", script);
	ASSERT_TRUE(ret);

	kvp::ExpectedKeyValuesMap ex_data = ivp::GetInventoryData(test_scripts_dir.Path());
	ASSERT_TRUE(ex_data);

	kvp::KeyValuesMap key_values_map = ex_data.value();
	EXPECT_EQ(key_values_map.size(), 3);
	EXPECT_EQ(key_values_map["key1"].size(), 2);
	EXPECT_EQ(key_values_map["key2"].size(), 1);
	EXPECT_EQ(key_values_map["key3"].size(), 1);
}

TEST_F(InventoryParserTests, GetInventoryDataMultiScriptTest) {
	string script = R"(#!/bin/sh
echo "key1=value1"
echo "key2=value2"
echo "key3=value3"
echo "key1=value11"
exit 0
)";
	auto ret = PrepareTestScript("mender-inventory-script1", script);
	ASSERT_TRUE(ret);

	script = R"(#!/bin/sh
echo "key1=value12"
echo "key4=value4"
echo "key1=value13"
exit 0
)";

	ret = PrepareTestScript("mender-inventory-script2", script);
	ASSERT_TRUE(ret);

	kvp::ExpectedKeyValuesMap ex_data = ivp::GetInventoryData(test_scripts_dir.Path());
	ASSERT_TRUE(ex_data);

	kvp::KeyValuesMap key_values_map = ex_data.value();
	EXPECT_EQ(key_values_map.size(), 4);
	EXPECT_EQ(key_values_map["key1"].size(), 4);
	EXPECT_EQ(key_values_map["key2"].size(), 1);
	EXPECT_EQ(key_values_map["key3"].size(), 1);
}

TEST_F(InventoryParserTests, GetInventoryDataMultiScriptOneFailTest) {
	string script = R"(#!/bin/sh
echo "key1=value1"
echo "key2=value2"
echo "key3=value3"
echo "key1=value11"
exit 0
)";
	auto ret = PrepareTestScript("mender-inventory-script1", script);
	ASSERT_TRUE(ret);

	script = R"(#!/bin/sh
echo "key1=value12"
echo "key4=value4"
echo "key1=value13"
exit 0
)";

	ret = PrepareTestScript("mender-inventory-script2", script);
	ASSERT_TRUE(ret);

	script = R"(#!/bin/sh
echo "keyval"
)";
	ret = PrepareTestScript("mender-inventory-script3", script);
	ASSERT_TRUE(ret);

	kvp::ExpectedKeyValuesMap ex_data = ivp::GetInventoryData(test_scripts_dir.Path());
	ASSERT_TRUE(ex_data);

	kvp::KeyValuesMap key_values_map = ex_data.value();
	EXPECT_EQ(key_values_map.size(), 4);
	EXPECT_EQ(key_values_map["key1"].size(), 4);
	EXPECT_EQ(key_values_map["key2"].size(), 1);
	EXPECT_EQ(key_values_map["key3"].size(), 1);
}

TEST_F(InventoryParserTests, GetInventoryDataNoScriptTest) {
	kvp::ExpectedKeyValuesMap ex_data = ivp::GetInventoryData(test_scripts_dir.Path());
	ASSERT_TRUE(ex_data);

	kvp::KeyValuesMap key_values_map = ex_data.value();
	EXPECT_EQ(key_values_map.size(), 0);
}

TEST_F(InventoryParserTests, GetInventoryDataNoWorkingScriptButNotEmptyTest) {
	string script = R"(#!/bin/sh
echo "key1=value1"
echo "key2=value2"
echo "key3=value3"
echo "key1=value11"
exit 0
)";
	auto ret = PrepareTestScript("some-other-script", script);
	ASSERT_TRUE(ret);

	string test_script_path = test_scripts_dir.Path() + "/" + "mender-inventory-script";
	ofstream os(test_script_path);
	os << script;
	os.close();
	// not making it executable

	kvp::ExpectedKeyValuesMap ex_data = ivp::GetInventoryData(test_scripts_dir.Path());
	ASSERT_TRUE(ex_data);

	kvp::KeyValuesMap key_values_map = ex_data.value();
	EXPECT_EQ(key_values_map.size(), 0);
}

TEST_F(InventoryParserTests, GetInventoryDataMultiScriptAllFailTest) {
	string script = R"(#!/bin/sh
echo "keyval"
)";

	auto ret = PrepareTestScript("mender-inventory-script1", script);
	ASSERT_TRUE(ret);

	ret = PrepareTestScript("mender-inventory-script2", script);
	ASSERT_TRUE(ret);

	ret = PrepareTestScript("mender-inventory-script3", script);
	ASSERT_TRUE(ret);

	kvp::ExpectedKeyValuesMap ex_data = ivp::GetInventoryData(test_scripts_dir.Path());
	ASSERT_FALSE(ex_data);
}
