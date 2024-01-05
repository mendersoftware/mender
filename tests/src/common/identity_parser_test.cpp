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

#include <common/identity_parser.hpp>

#include <sys/stat.h>
#include <gtest/gtest.h>
#include <fstream>

#include <common/key_value_parser.hpp>

namespace id_p = mender::common::identity_parser;
namespace kv_p = mender::common::key_value_parser;

using namespace std;

class IdentityParserTests : public testing::Test {
protected:
	const char *test_script_fname = "./test_script.sh";

	bool PrepareTestScript(const string script) {
		ofstream os(test_script_fname);
		os << script;
		os.close();

		int ret = chmod(test_script_fname, S_IRUSR | S_IWUSR | S_IXUSR);
		return ret == 0;
	}

	void TearDown() override {
		remove(test_script_fname);
	}
};

TEST_F(IdentityParserTests, GetIdentityData) {
	string script = R"(#!/bin/sh
echo "key1=value1"
echo "key2=value2"
echo "key3=value3"
echo "key1=value11"
exit 0
)";
	auto ret = PrepareTestScript(script);
	ASSERT_TRUE(ret);

	// Not much to test here, this function only unwraps and combines results of
	// processes::Process::GenerateLineData() and
	// key_value_parser::ParserKeyValues() to wrap them again in the proper
	// return type.
	kv_p::ExpectedKeyValuesMap ex_data = id_p::GetIdentityData(test_script_fname);
	ASSERT_TRUE(ex_data);

	kv_p::KeyValuesMap key_values_map = ex_data.value();
	EXPECT_EQ(key_values_map.size(), 3);
	EXPECT_EQ(key_values_map["key1"].size(), 2);
	EXPECT_EQ(key_values_map["key2"].size(), 1);
	EXPECT_EQ(key_values_map["key3"].size(), 1);
}

TEST_F(IdentityParserTests, GetIdentityDataBlank) {
	string script = R"(#!/bin/sh
echo "key-empty="
echo "key-non-empty=something"
exit 0
)";
	auto ret = PrepareTestScript(script);
	ASSERT_TRUE(ret);

	kv_p::ExpectedKeyValuesMap ex_data = id_p::GetIdentityData(test_script_fname);
	ASSERT_TRUE(ex_data);

	kv_p::KeyValuesMap key_values_map = ex_data.value();
	EXPECT_EQ(key_values_map.size(), 2);
	EXPECT_EQ(key_values_map["key-empty"].size(), 1);
	EXPECT_EQ(key_values_map["key-non-empty"].size(), 1);
}

TEST_F(IdentityParserTests, DumpIdentityData) {
	kv_p::KeyValuesMap key_values_map;
	key_values_map.insert({"key1", vector<string> {"value1", "value11"}});
	key_values_map.insert({"key2", vector<string> {"value2"}});
	key_values_map.insert({"key3", vector<string> {"value3"}});

	auto json_str = id_p::DumpIdentityData(key_values_map);

	ASSERT_EQ(R"({"key1":["value1","value11"],"key2":"value2","key3":"value3"})", json_str);
}

TEST_F(IdentityParserTests, DumpIdentityDataBlackField) {
	kv_p::KeyValuesMap key_values_map;
	key_values_map.insert({"key-empty-string", vector<string> {""}});
	key_values_map.insert({"key-empty-vector", vector<string> {"", ""}});
	key_values_map.insert({"key-non-empty", vector<string> {"something"}});

	auto json_str = id_p::DumpIdentityData(key_values_map);

	ASSERT_EQ(
		R"({"key-empty-string":"","key-empty-vector":["",""],"key-non-empty":"something"})",
		json_str);
}

TEST_F(IdentityParserTests, DumpIdentityEmptyIdentity) {
	kv_p::KeyValuesMap key_values_map;

	auto json_str = id_p::DumpIdentityData(key_values_map);

	ASSERT_EQ(R"({})", json_str);
}


TEST_F(IdentityParserTests, VerifyIdentityKeyOrder) {
	string script = R"(#!/bin/sh
echo "foo=bar"
echo "key=value=23"
echo "some value=bar"
echo "mac=de:ad:be:ef:00:01"
exit 0
)";
	auto ret = PrepareTestScript(script);
	ASSERT_TRUE(ret);

	kv_p::ExpectedKeyValuesMap ex_data = id_p::GetIdentityData(test_script_fname);
	ASSERT_TRUE(ex_data);

	kv_p::KeyValuesMap key_values_map = ex_data.value();
	auto json_str = id_p::DumpIdentityData(key_values_map);

	ASSERT_EQ(
		R"({"foo":"bar","key":"value=23","mac":"de:ad:be:ef:00:01","some value":"bar"})", json_str);
}

TEST_F(IdentityParserTests, VerifyIdentityKeyOrderJumbledValues) {
	string script = R"(#!/bin/sh
echo "mac=de:ad:be:ef:00:01"
echo "key=value=23"
echo "some value=bar"
echo "foo=bar"
exit 0
)";
	auto ret = PrepareTestScript(script);
	ASSERT_TRUE(ret);

	kv_p::ExpectedKeyValuesMap ex_data = id_p::GetIdentityData(test_script_fname);
	ASSERT_TRUE(ex_data);

	kv_p::KeyValuesMap key_values_map = ex_data.value();
	auto json_str = id_p::DumpIdentityData(key_values_map);

	ASSERT_EQ(
		R"({"foo":"bar","key":"value=23","mac":"de:ad:be:ef:00:01","some value":"bar"})", json_str);
}



TEST_F(IdentityParserTests, VerifyIdentityKeyOrderMultipleValues) {
	string script = R"(#!/bin/sh
echo "foo=bar"
echo "foo=baz"
echo "key=value=23"
echo "some value=bar"
echo "mac=de:ad:be:ef:00:01"
exit 0
)";
	auto ret = PrepareTestScript(script);
	ASSERT_TRUE(ret);

	kv_p::ExpectedKeyValuesMap ex_data = id_p::GetIdentityData(test_script_fname);
	ASSERT_TRUE(ex_data);

	kv_p::KeyValuesMap key_values_map = ex_data.value();
	auto json_str = id_p::DumpIdentityData(key_values_map);

	ASSERT_EQ(
		R"({"foo":["bar","baz"],"key":"value=23","mac":"de:ad:be:ef:00:01","some value":"bar"})",
		json_str);
}

TEST_F(IdentityParserTests, VerifyIdentityKeyOrderMultipleValuesReversedArray) {
	string script = R"(#!/bin/sh
echo "foo=baz"
echo "foo=bar"
echo "key=value=23"
echo "some value=bar"
echo "mac=de:ad:be:ef:00:01"
exit 0
)";
	auto ret = PrepareTestScript(script);
	ASSERT_TRUE(ret);

	kv_p::ExpectedKeyValuesMap ex_data = id_p::GetIdentityData(test_script_fname);
	ASSERT_TRUE(ex_data);

	kv_p::KeyValuesMap key_values_map = ex_data.value();
	auto json_str = id_p::DumpIdentityData(key_values_map);

	ASSERT_EQ(
		R"({"foo":["baz","bar"],"key":"value=23","mac":"de:ad:be:ef:00:01","some value":"bar"})",
		json_str);
}
