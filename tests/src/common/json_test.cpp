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
	json::ExpectedJson ej = json::Load("{}");
	EXPECT_TRUE(ej);

	ej = json::Load(R"("just_string")");
	EXPECT_TRUE(ej);

	ej = json::Load("140");
	EXPECT_TRUE(ej);

	ej = json::Load("141.14");
	EXPECT_TRUE(ej);

	ej = json::Load("true");
	EXPECT_TRUE(ej);

	ej = json::Load("false");
	EXPECT_TRUE(ej);

	ej = json::Load("null");
	EXPECT_TRUE(ej);

	ej = json::Load("[]");
	EXPECT_TRUE(ej);

	ej = json::Load(json_example_str);
	ASSERT_TRUE(ej);
	json::Json j = ej.value();
	EXPECT_FALSE(j.IsNull());
}

TEST(JsonStringTests, LoadFromInvalidString) {
	auto expected_error = json::MakeError(json::JsonErrorCode::ParseError, "");
	json::ExpectedJson ej = json::Load("{ invalid: json }");
	EXPECT_FALSE(ej);
	EXPECT_EQ(ej.error().code, expected_error.code);
	EXPECT_THAT(ej.error().message, StartsWith("Failed to parse"));

	ej = json::Load(R"({"invalid": "json")");
	EXPECT_FALSE(ej);
	EXPECT_EQ(ej.error().code, expected_error.code);
	EXPECT_THAT(ej.error().message, StartsWith("Failed to parse"));

	ej = json::Load("");
	EXPECT_FALSE(ej);
	EXPECT_EQ(ej.error().code, json::MakeError(json::JsonErrorCode::EmptyError, "").code);
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
	json::ExpectedJson ej = json::Load(i_str);
	ASSERT_TRUE(ej);
	EXPECT_FALSE(ej.value().IsNull());
}

TEST_F(JsonFileTests, LoadFromInvalidStream) {
	ofstream os(test_json_fname);
	os << "{ invalid: json";
	os.close();

	ifstream i_str(test_json_fname);
	json::ExpectedJson ej = json::Load(i_str);
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
	json::ExpectedJson ej = json::Load(reader);
	ASSERT_TRUE(ej);
	EXPECT_FALSE(ej.value().IsNull());
}

TEST_F(JsonFileTests, LoadFromInvalidReader) {
	ofstream os(test_json_fname);
	os << "{ invalid: json";
	os.close();

	ifstream i_str(test_json_fname);
	io::StreamReader reader(i_str);
	json::ExpectedJson ej = json::Load(reader);
	ASSERT_FALSE(ej);
	EXPECT_EQ(ej.error().code, json::MakeError(json::JsonErrorCode::ParseError, "").code);
	EXPECT_THAT(ej.error().message, MatchesRegex(".*Failed to parse.*"));
}

TEST(JsonDataTests, GetJsonData) {
	json::ExpectedJson ej = json::Load(json_example_str);
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
	EXPECT_TRUE(echild.value().IsInt64());

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
	EXPECT_TRUE(echild.value().IsInt64());

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
	json::ExpectedJson ej = json::Load(json_example_str);
	ASSERT_TRUE(ej);

	const json::Json j = ej.value();
	ASSERT_TRUE(j.IsObject());

	auto echild = j.Get("string");
	ASSERT_TRUE(echild);
	json::ExpectedString estr = echild.value().GetString();
	ASSERT_TRUE(estr);
	EXPECT_EQ(estr.value(), "string value");

	json::ExpectedInt64 eint = echild.value().GetInt64();
	ASSERT_FALSE(eint);
	EXPECT_EQ(eint.error().code, json::MakeError(json::JsonErrorCode::TypeError, "").code);
	EXPECT_THAT(eint.error().message, ::testing::HasSubstr("Type mismatch when getting int"));

	json::ExpectedBool ebool = echild.value().GetBool();
	ASSERT_FALSE(ebool);
	EXPECT_EQ(ebool.error().code, json::MakeError(json::JsonErrorCode::TypeError, "").code);
	EXPECT_THAT(ebool.error().message, ::testing::HasSubstr("Type mismatch when getting bool"));

	echild = j.Get("integer");
	ASSERT_TRUE(echild);
	eint = echild.value().GetInt64();
	ASSERT_TRUE(eint);
	EXPECT_EQ(eint.value(), 42);

	ebool = echild.value().GetBool();
	ASSERT_FALSE(ebool);
	EXPECT_EQ(ebool.error().code, json::MakeError(json::JsonErrorCode::TypeError, "").code);
	EXPECT_THAT(ebool.error().message, ::testing::HasSubstr("Type mismatch when getting bool"));

	echild = j.Get("boolean");
	ASSERT_TRUE(echild);
	ebool = echild.value().GetBool();
	EXPECT_EQ(ebool.value(), true);

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

TEST(JsonDataTests, GetChildren) {
	json::ExpectedJson ej = json::Load(json_example_str);
	ASSERT_TRUE(ej);

	json::Json j = ej.value();
	ASSERT_TRUE(j.IsObject());

	json::ExpectedChildrenMap e_map = j.GetChildren();
	ASSERT_TRUE(e_map);

	json::ChildrenMap ch_map = e_map.value();
	EXPECT_EQ(ch_map.size(), 7);
	EXPECT_EQ(ch_map["string"].GetString().value_or(""), "string value");

	j = ch_map["child"];
	EXPECT_TRUE(j.IsObject());

	e_map = j.GetChildren();
	ASSERT_TRUE(e_map);

	ch_map = e_map.value();
	EXPECT_EQ(ch_map.size(), 1);
}

TEST(JsonUtilTests, EscapeString) {
	string str = "nothing to change";
	EXPECT_EQ(json::EscapeString(str), str);

	str = "quoted \"string\"";
	EXPECT_EQ(json::EscapeString(str), R"(quoted \"string\")");

	str = "escape\ncharacters\n\teverywhere\r\n";
	EXPECT_EQ(json::EscapeString(str), R"(escape\ncharacters\n\teverywhere\r\n)");

	str = "A \"really\" bad\n\t combination";
	EXPECT_EQ(json::EscapeString(str), R"(A \"really\" bad\n\t combination)");
}

TEST(JsonUtilTests, EscapeStringControlCharacters) {
	string str = "\033[31mRed text\033[0m";
	EXPECT_EQ(json::EscapeString(str), "\\u001b[31mRed text\\u001b[0m");

	str = "before\033after";
	EXPECT_EQ(json::EscapeString(str), "before\\u001bafter");

	str = "text\177more";
	EXPECT_EQ(json::EscapeString(str), "text\\u007fmore");

	str = string("test\0more", 9);
	EXPECT_EQ(json::EscapeString(str), "test\\u0000more");

	str = "start\001end";
	EXPECT_EQ(json::EscapeString(str), "start\\u0001end");

	str = "start\002end";
	EXPECT_EQ(json::EscapeString(str), "start\\u0002end");

	str = "bell\007sound";
	EXPECT_EQ(json::EscapeString(str), "bell\\u0007sound");

	str = "vertical\013tab";
	EXPECT_EQ(json::EscapeString(str), "vertical\\u000btab");

	str = "shift\016out";
	EXPECT_EQ(json::EscapeString(str), "shift\\u000eout");

	str = "shift\017in";
	EXPECT_EQ(json::EscapeString(str), "shift\\u000fin");

	str = "unit\037separator";
	EXPECT_EQ(json::EscapeString(str), "unit\\u001fseparator");

	str = string("line1\nline2\033[31m\ttab\rcarriage\000null", 34);
	EXPECT_EQ(json::EscapeString(str), "line1\\nline2\\u001b[31m\\ttab\\rcarriage\\u0000null");

	str = string("\000\001\002\033\177", 5);
	EXPECT_EQ(json::EscapeString(str), "\\u0000\\u0001\\u0002\\u001b\\u007f");

	// Test range 0x00-0x1F
	str = string("\000\001\002\003\004\005\006\007", 8); // 0x00-0x07
	EXPECT_EQ(json::EscapeString(str), "\\u0000\\u0001\\u0002\\u0003\\u0004\\u0005\\u0006\\u0007");

	str = "\010\011\012\013\014\015\016\017"; // 0x08-0x0F (includes \b, \t, \n, \f, \r)
	EXPECT_EQ(json::EscapeString(str), "\\b\\t\\n\\u000b\\f\\r\\u000e\\u000f");

	str = "\020\021\022\023\024\025\026\027"; // 0x10-0x17
	EXPECT_EQ(json::EscapeString(str), "\\u0010\\u0011\\u0012\\u0013\\u0014\\u0015\\u0016\\u0017");

	str = "\030\031\032\033\034\035\036\037"; // 0x18-0x1F
	EXPECT_EQ(json::EscapeString(str), "\\u0018\\u0019\\u001a\\u001b\\u001c\\u001d\\u001e\\u001f");

	// Combine standard escapes and control characters
	str = "\\\033";
	EXPECT_EQ(json::EscapeString(str), "\\\\\\u001b");

	str = "\"\033";
	EXPECT_EQ(json::EscapeString(str), "\\\"\\u001b");

	// Customer reported cases
	str = "\xc3\xa4\xc3\xb6\xc3\xbc";
	EXPECT_EQ(json::EscapeString(str), "\xc3\xa4\xc3\xb6\xc3\xbc");

	str = string(
		"\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00", 20);
	EXPECT_EQ(
		json::EscapeString(str),
		"\\u0000\\u0000\\u0000\\u0000\\u0000\\u0000\\u0000\\u0000\\u0000\\u0000"
		"\\u0000\\u0000\\u0000\\u0000\\u0000\\u0000\\u0000\\u0000\\u0000\\u0000");
}

TEST(Json, GetDouble) {
	auto ej = json::Load(R"(141.14)");
	ASSERT_TRUE(ej);
	auto ed = ej.value().GetDouble();
	ASSERT_TRUE(ed) << ed.error().message;
	EXPECT_THAT(ed.value(), testing::DoubleEq(141.14));
}

TEST(Json, TemplateGet) {
	auto data = json::Load(R"({
  "string": "abc",
  "int": 9223372036854775807,
  "double": 9007199254740992,
  "bool": true,
  "stringlist": [
    "a",
    "b"
  ],
  "map": {
    "a": "b"
  }
})");

	ASSERT_TRUE(data) << data.error();

	EXPECT_EQ(data.value().Get("string").value().Get<string>(), "abc");
	EXPECT_EQ(data.value().Get("int").value().Get<int64_t>(), 9223372036854775807);
	EXPECT_EQ(data.value().Get("double").value().Get<double>(), 9007199254740992);
	EXPECT_EQ(data.value().Get("bool").value().Get<bool>(), true);
	EXPECT_EQ(
		data.value().Get("stringlist").value().Get<vector<string>>(), (vector<string> {"a", "b"}));
	EXPECT_EQ(
		data.value().Get("map").value().Get<json::KeyValueMap>(), (json::KeyValueMap {{"a", "b"}}));
}

TEST(Json, ParseEmpty) {
	auto data = json::Load("");

	ASSERT_FALSE(data);

	EXPECT_EQ(data.error().code, json::MakeError(json::JsonErrorCode::EmptyError, "").code);
}
