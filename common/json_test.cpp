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

#include <common/json.hpp>

#include <cerrno>
#include <gtest/gtest.h>
#include <gmock/gmock.h>
#include <fstream>

#include <common/io.hpp>

namespace json = mender::common::json;
namespace io = mender::common::io;

using namespace std;
using testing::MatchesRegex;
using testing::StartsWith;

const string json_example_str = R"({
  "string": "string value",
  "integer": 42,
  "boolean": true,
  "null": null,
  "array": ["val1", 2, false, null],
  "child": {
    "child_key": "child_val"
  },
  "obj_array": [
    { "key1": "val1" },
    { "key2": "val2" }
  ]
})";

TEST(JsonStringTests, LoadFromValidString) {
	json::ExpectedJson ej = json::LoadFromString("{}");
	EXPECT_TRUE(ej);

	ej = json::LoadFromString(R"("just_string")");
	EXPECT_TRUE(ej);

	ej = json::LoadFromString("140");
	EXPECT_TRUE(ej);

	ej = json::LoadFromString("141.14");
	EXPECT_TRUE(ej);

	ej = json::LoadFromString("true");
	EXPECT_TRUE(ej);

	ej = json::LoadFromString("false");
	EXPECT_TRUE(ej);

	ej = json::LoadFromString("null");
	EXPECT_TRUE(ej);

	ej = json::LoadFromString("[]");
	EXPECT_TRUE(ej);

	ej = json::LoadFromString(json_example_str);
	ASSERT_TRUE(ej);
	json::Json j = ej.value();
	EXPECT_FALSE(j.IsNull());
}

TEST(JsonStringTests, LoadFromInvalidString) {
	auto expected_error = json::MakeError(json::JsonErrorCode::ParseError, "");
	json::ExpectedJson ej = json::LoadFromString("{ invalid: json }");
	EXPECT_FALSE(ej);
	EXPECT_EQ(ej.error().code, expected_error.code);
	EXPECT_THAT(ej.error().message, StartsWith("Failed to parse"));

	ej = json::LoadFromString(R"({"invalid": "json")");
	EXPECT_FALSE(ej);
	EXPECT_EQ(ej.error().code, expected_error.code);
	EXPECT_THAT(ej.error().message, StartsWith("Failed to parse"));

	ej = json::LoadFromString("");
	EXPECT_FALSE(ej);
	EXPECT_EQ(ej.error().code, expected_error.code);
	EXPECT_THAT(ej.error().message, StartsWith("Failed to parse"));
}

class JsonFileTests : public testing::Test {
protected:
	const char *test_json_fname = "test.json";
	void TearDown() override {
		remove(test_json_fname);
	}
};

TEST_F(JsonFileTests, LoadFromValidFile) {
	ofstream os(test_json_fname);
	os << json_example_str;
	os.close();

	json::ExpectedJson ej = json::LoadFromFile(test_json_fname);
	ASSERT_TRUE(ej);
	EXPECT_FALSE(ej.value().IsNull());
}

TEST_F(JsonFileTests, LoadFromInvalidFile) {
	ofstream os(test_json_fname);
	os << "{ invalid: json";
	os.close();

	json::ExpectedJson ej = json::LoadFromFile(test_json_fname);
	ASSERT_FALSE(ej);
	EXPECT_EQ(ej.error().code, json::MakeError(json::JsonErrorCode::ParseError, "").code);
	EXPECT_THAT(
		ej.error().message, MatchesRegex(string(".*Failed to parse.*") + test_json_fname + ".*"));
}

TEST_F(JsonFileTests, LoadFromNonexistingFile) {
	json::ExpectedJson ej = json::LoadFromFile("non-existing-file");
	ASSERT_FALSE(ej);
	EXPECT_TRUE(ej.error().IsErrno(ENOENT));
	EXPECT_THAT(
		ej.error().message,
		MatchesRegex(string(".*Failed to open.*non-existing-file.*No such file.*")));
}

TEST_F(JsonFileTests, LoadFromValidStream) {
	ofstream os(test_json_fname);
	os << json_example_str;
	os.close();

	ifstream i_str(test_json_fname);
	json::ExpectedJson ej = json::LoadFromStream(i_str);
	ASSERT_TRUE(ej);
	EXPECT_FALSE(ej.value().IsNull());
}

TEST_F(JsonFileTests, LoadFromInvalidStream) {
	ofstream os(test_json_fname);
	os << "{ invalid: json";
	os.close();

	ifstream i_str(test_json_fname);
	json::ExpectedJson ej = json::LoadFromStream(i_str);
	ASSERT_FALSE(ej);
	EXPECT_EQ(ej.error().code, json::MakeError(json::JsonErrorCode::ParseError, "").code);
	EXPECT_THAT(ej.error().message, MatchesRegex(".*Failed to parse.*"));
}

TEST_F(JsonFileTests, LoadFromValidReader) {
	ofstream os(test_json_fname);
	os << json_example_str;
	os.close();

	ifstream i_str(test_json_fname);
	io::StreamReader reader(i_str);
	json::ExpectedJson ej = json::LoadFromReader(reader);
	ASSERT_TRUE(ej);
	EXPECT_FALSE(ej.value().IsNull());
}

TEST_F(JsonFileTests, LoadFromInvalidReader) {
	ofstream os(test_json_fname);
	os << "{ invalid: json";
	os.close();

	ifstream i_str(test_json_fname);
	io::StreamReader reader(i_str);
	json::ExpectedJson ej = json::LoadFromReader(reader);
	ASSERT_FALSE(ej);
	EXPECT_EQ(ej.error().code, json::MakeError(json::JsonErrorCode::ParseError, "").code);
	EXPECT_THAT(ej.error().message, MatchesRegex(".*Failed to parse.*"));
}

TEST(JsonDataTests, GetJsonData) {
	json::ExpectedJson ej = json::LoadFromString(json_example_str);
	ASSERT_TRUE(ej);

	const json::Json j = ej.value();
	EXPECT_TRUE(j.IsObject());

	json::ExpectedJson echild = j.Get("nosuch");
	ASSERT_FALSE(echild);
	EXPECT_EQ(echild.error().code, json::MakeError(json::JsonErrorCode::KeyError, "").code);
	EXPECT_EQ(echild.error().message, "Key 'nosuch' doesn't exist");

	// Try the same again, because we have seen j.Get("nosuch") to have a
	// side-effect of adding "nosuch" to the object.
	echild = j.Get("nosuch");
	ASSERT_FALSE(echild);
	EXPECT_EQ(echild.error().code, json::MakeError(json::JsonErrorCode::KeyError, "").code);
	EXPECT_EQ(echild.error().message, "Key 'nosuch' doesn't exist");

	echild = j.Get("string");
	ASSERT_TRUE(echild);
	EXPECT_TRUE(echild.value().IsString());

	echild = j.Get(string("integer"));
	ASSERT_TRUE(echild);
	EXPECT_TRUE(echild.value().IsInt());

	echild = j["boolean"];
	ASSERT_TRUE(echild);
	EXPECT_TRUE(echild.value().IsBool());

	echild = j.Get("null");
	ASSERT_TRUE(echild);
	EXPECT_TRUE(echild.value().IsNull());

	echild = j.Get("array");
	ASSERT_TRUE(echild);
	EXPECT_TRUE(echild.value().IsArray());

	echild = j.Get("child");
	ASSERT_TRUE(echild);
	EXPECT_TRUE(echild.value().IsObject());

	echild = j.Get("array");
	ASSERT_TRUE(echild);

	json::Json j_arr = echild.value();
	echild = j_arr.Get(5);
	ASSERT_FALSE(echild);
	EXPECT_EQ(echild.error().code, json::MakeError(json::JsonErrorCode::IndexError, "").code);
	EXPECT_EQ(echild.error().message, "Index 5 out of range");

	echild = j_arr.Get(static_cast<int>(0));
	ASSERT_TRUE(echild);
	EXPECT_TRUE(echild.value().IsString());

	echild = j_arr.Get(1);
	ASSERT_TRUE(echild);
	EXPECT_TRUE(echild.value().IsInt());

	echild = j_arr.Get(2);
	ASSERT_TRUE(echild);
	EXPECT_TRUE(echild.value().IsBool());

	echild = j_arr.Get(3);
	ASSERT_TRUE(echild);
	EXPECT_TRUE(echild.value().IsNull());

	echild = j.Get("child");
	ASSERT_TRUE(echild);
	echild = echild.value().Get("child_key");
	ASSERT_TRUE(echild);
	EXPECT_TRUE(echild.value().IsString());

	echild = j.Get("obj_array");
	ASSERT_TRUE(echild);
	echild = echild.value().Get(1);
	ASSERT_TRUE(echild);
	echild = echild.value().Get("key2");
	ASSERT_TRUE(echild);
	EXPECT_TRUE(echild.value().IsString());
}

TEST(JsonDataTests, GetDataValues) {
	json::ExpectedJson ej = json::LoadFromString(json_example_str);
	ASSERT_TRUE(ej);

	const json::Json j = ej.value();
	ASSERT_TRUE(j.IsObject());

	auto echild = j.Get("string");
	ASSERT_TRUE(echild);
	json::ExpectedString estr = echild.value().GetString();
	ASSERT_TRUE(estr);
	EXPECT_EQ(estr.value(), "string value");

	json::ExpectedInt eint = echild.value().GetInt();
	ASSERT_FALSE(eint);
	EXPECT_EQ(eint.error().code, json::MakeError(json::JsonErrorCode::TypeError, "").code);
	EXPECT_EQ(eint.error().message, "Type mismatch when getting int");

	json::ExpectedBool ebool = echild.value().GetBool();
	ASSERT_FALSE(ebool);
	EXPECT_EQ(ebool.error().code, json::MakeError(json::JsonErrorCode::TypeError, "").code);
	EXPECT_EQ(ebool.error().message, "Type mismatch when getting bool");

	echild = j.Get("integer");
	ASSERT_TRUE(echild);
	eint = echild.value().GetInt();
	ASSERT_TRUE(eint);
	EXPECT_EQ(eint.value(), 42);

	ebool = echild.value().GetBool();
	ASSERT_FALSE(ebool);
	EXPECT_EQ(ebool.error().code, json::MakeError(json::JsonErrorCode::TypeError, "").code);
	EXPECT_EQ(ebool.error().message, "Type mismatch when getting bool");

	echild = j.Get("boolean");
	ASSERT_TRUE(echild);
	ebool = echild.value().GetBool();
	EXPECT_EQ(ebool.value(), true);

	eint = echild.value().GetInt();
	ASSERT_TRUE(eint);
	EXPECT_EQ(eint.value(), 1);

	echild = j.Get("array");
	ASSERT_TRUE(echild);
	json::ExpectedSize esize = echild.value().GetArraySize();
	EXPECT_EQ(esize.value(), 4);

	echild = j.Get("obj_array");
	ASSERT_TRUE(echild);
	esize = echild.value().GetArraySize();
	EXPECT_EQ(esize.value(), 2);

	echild = j.Get("string");
	ASSERT_TRUE(echild);
	esize = echild.value().GetArraySize();
	EXPECT_EQ(esize.error().code, json::MakeError(json::JsonErrorCode::TypeError, "").code);
	EXPECT_EQ(esize.error().message, "Not a JSON array");

	echild = j.Get("child");
	ASSERT_TRUE(echild);
	esize = echild.value().GetArraySize();
	EXPECT_EQ(esize.error().code, json::MakeError(json::JsonErrorCode::TypeError, "").code);
	EXPECT_EQ(esize.error().message, "Not a JSON array");
}
