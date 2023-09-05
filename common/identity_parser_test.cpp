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

TEST_F(IdentityParserTests, GetIdentityDataTest) {
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
