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

#include <common/yaml.hpp>

#include <cerrno>
#include <gtest/gtest.h>
#include <gmock/gmock.h>

#include <common/io.hpp>

using namespace std;

using testing::MatchesRegex;
using testing::StartsWith;

namespace io = mender::common::io;
namespace yaml = mender::common::yaml;

const string yaml_example_str = R"(
# Valid example testdata
---
string: "string value"
integer: 42
boolean: true
null: null
array:
  - val1
  - 2
  - false
  -
child:
  child_key: child_val
obj_array:
  - key1: val1
  - key2: val2
)";


TEST(YamlStringTests, LoadFromValidString) {
	yaml::ExpectedYaml expected_yaml = yaml::Load("{}");
	EXPECT_TRUE(expected_yaml);

	expected_yaml = yaml::Load(R"("just_string")");
	EXPECT_TRUE(expected_yaml);

	expected_yaml = yaml::Load("140");
	EXPECT_TRUE(expected_yaml);

	expected_yaml = yaml::Load("141.14");
	EXPECT_TRUE(expected_yaml);

	expected_yaml = yaml::Load("true");
	EXPECT_TRUE(expected_yaml);

	expected_yaml = yaml::Load("false");
	EXPECT_TRUE(expected_yaml);

	expected_yaml = yaml::Load("null");
	EXPECT_TRUE(expected_yaml);

	expected_yaml = yaml::Load("[]");
	EXPECT_TRUE(expected_yaml);

	expected_yaml = yaml::Load(yaml_example_str);
	ASSERT_TRUE(expected_yaml);
	yaml::Yaml _yaml = expected_yaml.value();
	EXPECT_FALSE(_yaml.IsNull());
}


TEST(YamlStringTests, LoadFromInvalidString) {
	string invalid_yaml = R"("foo: bar)";
	auto expected_error = yaml::MakeError(yaml::YamlErrorCode::ParseError, "");
	yaml::ExpectedYaml expected_yaml = yaml::Load(invalid_yaml);
	EXPECT_FALSE(expected_yaml) << "Node is: ";
	EXPECT_EQ(expected_yaml.error().code, expected_error.code)
		<< "Got unexpected error code: " + expected_yaml.error().String();
	EXPECT_THAT(expected_yaml.error().message, StartsWith("Failed to parse"));

	expected_yaml = yaml::Load(invalid_yaml);
	EXPECT_FALSE(expected_yaml);
	EXPECT_EQ(expected_yaml.error().code, expected_error.code);
	EXPECT_THAT(expected_yaml.error().message, StartsWith("Failed to parse"));

	/* NOTE - This is not an error in the YAML parser - but is in the JSON parser */
	expected_yaml = yaml::Load("");
	ASSERT_TRUE(expected_yaml);
}


class YamlFileTests : public testing::Test {
protected:
	const char *test_yaml_fname = "test.yaml";
	void TearDown() override {
		remove(test_yaml_fname);
	}
};

TEST_F(YamlFileTests, LoadFromValidFile) {
	ofstream os(test_yaml_fname);
	os << yaml_example_str;
	os.close();

	yaml::ExpectedYaml expected_yaml = yaml::LoadFromFile(test_yaml_fname);
	ASSERT_TRUE(expected_yaml);
	EXPECT_FALSE(expected_yaml.value().IsNull());
}


TEST_F(YamlFileTests, LoadFromInvalidFile) {
	ofstream os(test_yaml_fname);
	os << "{ invalid: yaml";
	os.close();

	yaml::ExpectedYaml expected_yaml = yaml::LoadFromFile(test_yaml_fname);
	ASSERT_FALSE(expected_yaml);
	EXPECT_EQ(
		expected_yaml.error().code, yaml::MakeError(yaml::YamlErrorCode::ParseError, "").code);
	EXPECT_THAT(
		expected_yaml.error().message,
		MatchesRegex(string(".*Failed to parse.*") + test_yaml_fname + ".*"));
}

TEST_F(YamlFileTests, LoadFromNonexistingFile) {
	yaml::ExpectedYaml expected_yaml = yaml::LoadFromFile("non-existing-file");
	ASSERT_FALSE(expected_yaml);
	EXPECT_TRUE(expected_yaml.error().IsErrno(ENOENT));
	EXPECT_THAT(
		expected_yaml.error().message,
		MatchesRegex(string(".*Failed to open.*non-existing-file.*No such file.*")));
}


TEST_F(YamlFileTests, LoadFromValidStream) {
	ofstream os(test_yaml_fname);
	os << yaml_example_str;
	os.close();

	ifstream i_str(test_yaml_fname);
	yaml::ExpectedYaml expected_yaml = yaml::Load(i_str);
	ASSERT_TRUE(expected_yaml);
	EXPECT_FALSE(expected_yaml.value().IsNull());
}

TEST_F(YamlFileTests, LoadFromInvalidStream) {
	ofstream os(test_yaml_fname);
	os << "{ invalid: yaml";
	os.close();

	ifstream i_str(test_yaml_fname);
	yaml::ExpectedYaml expected_yaml = yaml::Load(i_str);
	ASSERT_FALSE(expected_yaml);
	EXPECT_EQ(
		expected_yaml.error().code, yaml::MakeError(yaml::YamlErrorCode::ParseError, "").code);
	EXPECT_THAT(expected_yaml.error().message, MatchesRegex(".*Failed to parse.*"));
}


TEST_F(YamlFileTests, LoadFromValidReader) {
	ofstream os(test_yaml_fname);
	os << yaml_example_str;
	os.close();

	ifstream i_str(test_yaml_fname);
	io::StreamReader reader(i_str);
	yaml::ExpectedYaml expected_yaml = yaml::Load(reader);
	ASSERT_TRUE(expected_yaml);
	EXPECT_FALSE(expected_yaml.value().IsNull());
}

TEST_F(YamlFileTests, LoadFromInvalidReader) {
	ofstream os(test_yaml_fname);
	os << "{ invalid: yaml";
	os.close();

	ifstream i_str(test_yaml_fname);
	io::StreamReader reader(i_str);
	yaml::ExpectedYaml expected_yaml = yaml::Load(reader);
	ASSERT_FALSE(expected_yaml);
	EXPECT_EQ(
		expected_yaml.error().code, yaml::MakeError(yaml::YamlErrorCode::ParseError, "").code);
	EXPECT_THAT(expected_yaml.error().message, MatchesRegex(".*Failed to parse.*"));
}

TEST(YamlDataTests, GetYamlData) {
	yaml::ExpectedYaml expected_yaml = yaml::Load(yaml_example_str);
	ASSERT_TRUE(expected_yaml);

	const yaml::Yaml _yaml = expected_yaml.value();
	EXPECT_TRUE(_yaml.IsObject());

	yaml::ExpectedYaml echild = _yaml.Get("nosuch");
	ASSERT_FALSE(echild);
	EXPECT_EQ(echild.error().code, yaml::MakeError(yaml::YamlErrorCode::KeyError, "").code);
	EXPECT_EQ(echild.error().message, "Key 'nosuch' doesn't exist");

	// Try the same again, because we have seen _yaml.Get("nosuch") to have a
	// side-effect of adding "nosuch" to the object.
	echild = _yaml.Get("nosuch");
	ASSERT_FALSE(echild);
	EXPECT_EQ(echild.error().code, yaml::MakeError(yaml::YamlErrorCode::KeyError, "").code);
	EXPECT_EQ(echild.error().message, "Key 'nosuch' doesn't exist");

	echild = _yaml.Get("string");
	ASSERT_TRUE(echild);
	EXPECT_TRUE(echild.value().IsString());
	EXPECT_EQ(echild.value().Get<string>(), "string value");

	echild = _yaml.Get("integer");
	ASSERT_TRUE(echild);
	EXPECT_TRUE(echild.value().IsInt64());
	EXPECT_EQ(echild.value().Get<int64_t>(), 42);

	echild = _yaml.Get("string");
	ASSERT_TRUE(echild);
	EXPECT_TRUE(echild.value().IsString());
	echild = _yaml["boolean"];
	ASSERT_TRUE(echild);
	EXPECT_TRUE(echild.value().IsBool());

	// TODO - https://github.com/jbeder/yaml-cpp/issues/1269
	// echild = _yaml.Get("null");
	// ASSERT_TRUE(echild) << echild.error().message;
	// EXPECT_TRUE(echild.value().IsNull());

	echild = _yaml.Get("array");
	ASSERT_TRUE(echild);
	ASSERT_TRUE(echild.value().IsArray()) << echild.value().GetType();

	echild = _yaml.Get("child");
	ASSERT_TRUE(echild);
	EXPECT_TRUE(echild.value().IsObject());

	echild = _yaml.Get("array");
	ASSERT_TRUE(echild);
	ASSERT_TRUE(echild.value().IsArray()) << "Got unexpected type: " << echild.value().GetType();

	yaml::Yaml j_arr = echild.value();
	echild = j_arr.Get(5);
	ASSERT_FALSE(echild);
	EXPECT_EQ(echild.error().code, yaml::MakeError(yaml::YamlErrorCode::IndexError, "").code);
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

	echild = _yaml.Get("child");
	ASSERT_TRUE(echild);
	echild = echild.value().Get("child_key");
	ASSERT_TRUE(echild);
	EXPECT_TRUE(echild.value().IsString());

	echild = _yaml.Get("obj_array");
	ASSERT_TRUE(echild);
	echild = echild.value().Get(1);
	ASSERT_TRUE(echild);
	echild = echild.value().Get("key2");
	ASSERT_TRUE(echild);
	EXPECT_TRUE(echild.value().IsString());
}

TEST(YamlDataTests, GetDataValues) {
	yaml::ExpectedYaml expected_yaml = yaml::Load(yaml_example_str);
	ASSERT_TRUE(expected_yaml);

	const yaml::Yaml j = expected_yaml.value();
	ASSERT_TRUE(j.IsObject());

	auto echild = j.Get("string");
	ASSERT_TRUE(echild);
	yaml::ExpectedString estr = echild.value().Get<string>();
	ASSERT_TRUE(estr);
	EXPECT_EQ(estr.value(), "string value");

	yaml::ExpectedInt64 eint = echild.value().Get<int64_t>();
	ASSERT_FALSE(eint);
	EXPECT_EQ(eint.error().code, yaml::MakeError(yaml::YamlErrorCode::TypeError, "").code);
	EXPECT_THAT(eint.error().message, ::testing::HasSubstr("is not a integer"));

	yaml::ExpectedBool ebool = echild.value().Get<bool>();
	ASSERT_FALSE(ebool);
	EXPECT_EQ(ebool.error().code, yaml::MakeError(yaml::YamlErrorCode::TypeError, "").code);
	EXPECT_THAT(ebool.error().message, ::testing::HasSubstr("is not a bool"));

	echild = j.Get("integer");
	ASSERT_TRUE(echild);
	eint = echild.value().Get<int64_t>();
	ASSERT_TRUE(eint);
	EXPECT_EQ(eint.value(), 42);

	ebool = echild.value().Get<bool>();
	ASSERT_FALSE(ebool);
	EXPECT_EQ(ebool.error().code, yaml::MakeError(yaml::YamlErrorCode::TypeError, "").code);
	EXPECT_THAT(ebool.error().message, ::testing::HasSubstr("is not a bool"));

	echild = j.Get("boolean");
	ASSERT_TRUE(echild);
	ebool = echild.value().Get<bool>();
	EXPECT_EQ(ebool.value(), true);

	echild = j.Get("array");
	ASSERT_TRUE(echild);
	yaml::ExpectedSize esize = echild.value().GetArraySize();
	EXPECT_EQ(esize.value(), 4);

	echild = j.Get("obj_array");
	ASSERT_TRUE(echild);
	esize = echild.value().GetArraySize();
	EXPECT_EQ(esize.value(), 2);

	echild = j.Get("string");
	ASSERT_TRUE(echild);
	esize = echild.value().GetArraySize();
	EXPECT_EQ(esize.error().code, yaml::MakeError(yaml::YamlErrorCode::TypeError, "").code);
	EXPECT_EQ(esize.error().message, "The YAML node is a 'Scalar', not a Sequence");

	echild = j.Get("child");
	ASSERT_TRUE(echild);
	esize = echild.value().GetArraySize();
	EXPECT_EQ(esize.error().code, yaml::MakeError(yaml::YamlErrorCode::TypeError, "").code);
	EXPECT_EQ(esize.error().message, "The YAML node is a 'Map', not a Sequence");
}

TEST(YamlDataTests, GetChildren) {
	yaml::ExpectedYaml expected_yaml = yaml::Load(yaml_example_str);
	ASSERT_TRUE(expected_yaml);

	yaml::Yaml j = expected_yaml.value();
	ASSERT_TRUE(j.IsObject());

	yaml::ExpectedChildrenMap e_map = j.GetChildren();
	ASSERT_TRUE(e_map);

	yaml::ChildrenMap ch_map = e_map.value();
	EXPECT_EQ(ch_map.size(), 7);
	EXPECT_EQ(ch_map["string"].Get<string>().value_or(""), "string value");

	j = ch_map["child"];
	EXPECT_TRUE(j.IsObject());

	e_map = j.GetChildren();
	ASSERT_TRUE(e_map);

	ch_map = e_map.value();
	EXPECT_EQ(ch_map.size(), 1);
}

TEST(Yaml, GetDouble) {
	auto expected_yaml = yaml::Load(R"(141.14)");
	ASSERT_TRUE(expected_yaml);
	auto ed = expected_yaml.value().Get<double>();
	ASSERT_TRUE(ed) << ed.error().message;
	EXPECT_THAT(ed.value(), testing::DoubleEq(141.14));
}

TEST(Yaml, TemplateGet) {
	auto data = yaml::Load(R"(
  "string": "abc"
  "int": 9223372036854775807
  "double": 9007199254740992
  "bool": true
  "stringlist":
    - "a"
    - "b"
  "map": {
    "a": "b"
  }
)");

	ASSERT_TRUE(data) << data.error();

	EXPECT_EQ(data.value().Get("string").value().Get<string>(), "abc");
	EXPECT_EQ(data.value().Get("int").value().Get<int64_t>(), 9223372036854775807);
	EXPECT_EQ(data.value().Get("double").value().Get<double>(), 9007199254740992);
	EXPECT_EQ(data.value().Get("bool").value().Get<bool>(), true);
	EXPECT_EQ(
		data.value().Get("stringlist").value().Get<vector<string>>(), (vector<string> {"a", "b"}));
	EXPECT_EQ(
		data.value().Get("map").value().Get<yaml::KeyValueMap>(), (yaml::KeyValueMap {{"a", "b"}}));
}

TEST(Yaml, Dump) {
	const string yaml_expected_str = R"(string: string value
integer: 42
boolean: true
~: ~
array:
  - val1
  - 2
  - false
  - ~
child:
  child_key: child_val
obj_array:
  - key1: val1
  - key2: val2)";


	auto data = yaml::Load(yaml_example_str);

	ASSERT_TRUE(data) << data.error();

	EXPECT_EQ(data.value().Dump(), yaml_expected_str);
}
