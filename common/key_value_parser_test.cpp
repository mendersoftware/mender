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

#include <common/key_value_parser.hpp>

#include <vector>
#include <string>
#include <common/error.hpp>

#include <gtest/gtest.h>

namespace kvp = mender::common::key_value_parser;
namespace error = mender::common::error;

using namespace std;

TEST(KeyValueParserTests, ValidDistinctItems) {
	vector<string> items = {"key1=value1", "key2=value2", "key3=value3", "key4="};

	kvp::ExpectedKeyValuesMap ret = kvp::ParseKeyValues(items);
	ASSERT_TRUE(ret);

	kvp::KeyValuesMap ret_map = ret.value();
	EXPECT_EQ(ret_map.size(), 4);
	EXPECT_EQ(ret_map.count("key1"), 1);
	EXPECT_EQ(ret_map.count("key2"), 1);
	EXPECT_EQ(ret_map.count("key3"), 1);
	EXPECT_EQ(ret_map.count("key4"), 1);
	EXPECT_EQ(ret_map.count("key5"), 0);
	EXPECT_EQ(ret_map["key1"], vector<string> {"value1"});
	EXPECT_EQ(ret_map["key2"], vector<string> {"value2"});
	EXPECT_EQ(ret_map["key3"], vector<string> {"value3"});
	EXPECT_EQ(ret_map["key4"], vector<string> {""});

	items = {"key1~value1", "key2~value2", "key3~value3", "key4~"};

	ret = kvp::ParseKeyValues(items, '~');
	ASSERT_TRUE(ret);

	ret_map = ret.value();
	EXPECT_EQ(ret_map.size(), 4);
	EXPECT_EQ(ret_map.count("key1"), 1);
	EXPECT_EQ(ret_map.count("key2"), 1);
	EXPECT_EQ(ret_map.count("key3"), 1);
	EXPECT_EQ(ret_map.count("key4"), 1);
	EXPECT_EQ(ret_map.count("key5"), 0);
	EXPECT_EQ(ret_map["key1"], vector<string> {"value1"});
	EXPECT_EQ(ret_map["key2"], vector<string> {"value2"});
	EXPECT_EQ(ret_map["key3"], vector<string> {"value3"});
	EXPECT_EQ(ret_map["key4"], vector<string> {""});
}

TEST(KeyValueParserTests, ValidMultiItems) {
	vector<string> items = {
		"key1=value1",
		"key2=value2",
		"key3=value3",
		"key1=value11",
		"key1=value12",
		"key3=value31"};

	kvp::ExpectedKeyValuesMap ret = kvp::ParseKeyValues(items);
	ASSERT_TRUE(ret);

	kvp::KeyValuesMap ret_map = ret.value();
	EXPECT_EQ(ret_map.size(), 3);
	EXPECT_EQ(ret_map.count("key1"), 1);
	EXPECT_EQ(ret_map.count("key2"), 1);
	EXPECT_EQ(ret_map.count("key3"), 1);
	EXPECT_EQ(ret_map.count("key4"), 0);
	EXPECT_EQ(ret_map["key1"], (vector<string> {"value1", "value11", "value12"}));
	EXPECT_EQ(ret_map["key2"], (vector<string> {"value2"}));
	EXPECT_EQ(ret_map["key3"], (vector<string> {"value3", "value31"}));
}

TEST(KeyValueParserTests, ValidMultiAddItems) {
	vector<string> items = {
		"key1=value1",
		"key2=value2",
		"key3=value3",
		"key1=value11",
		"key1=value12",
		"key3=value31"};

	kvp::ExpectedKeyValuesMap ex_base = kvp::ParseKeyValues(items);
	ASSERT_TRUE(ex_base);

	items = {"key1=value13", "key3=value32", "key4=value4"};

	kvp::KeyValuesMap ret_map = ex_base.value();
	error::Error err = kvp::AddParseKeyValues(ret_map, items);
	ASSERT_EQ(error::NoError, err);

	EXPECT_EQ(ret_map.size(), 4);
	EXPECT_EQ(ret_map.count("key1"), 1);
	EXPECT_EQ(ret_map.count("key2"), 1);
	EXPECT_EQ(ret_map.count("key3"), 1);
	EXPECT_EQ(ret_map.count("key4"), 1);
	EXPECT_EQ(ret_map.count("key5"), 0);
	EXPECT_EQ(ret_map["key1"], (vector<string> {"value1", "value11", "value12", "value13"}));
	EXPECT_EQ(ret_map["key2"], (vector<string> {"value2"}));
	EXPECT_EQ(ret_map["key3"], (vector<string> {"value3", "value31", "value32"}));
	EXPECT_EQ(ret_map["key4"], (vector<string> {"value4"}));
}

TEST(KeyValueParserTests, InvalidItem) {
	vector<string> items = {"key1=value1", "key2=value2", "key3value3"};

	kvp::ExpectedKeyValuesMap ret = kvp::ParseKeyValues(items);
	ASSERT_FALSE(ret);
	EXPECT_EQ(
		ret.error().code, kvp::MakeError(kvp::KeyValueParserErrorCode::InvalidDataError, "").code);
	EXPECT_EQ(ret.error().message, "Invalid data given: 'key3value3'");
}
