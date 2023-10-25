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

#include <artifact/v3/version/version.hpp>

#include <gtest/gtest.h>
#include <gmock/gmock.h>

#include <fstream>


using namespace std;

using namespace mender;


TEST(ParserTest, TestParseVersion) {
	std::string json_data = R"(
  {
    "version": 3,
		"format" : "mender"
  }
)";

	std::stringstream ss {json_data};

	mender::common::io::StreamReader sr {ss};

	auto version = artifact::v3::version::Parse(sr);

	ASSERT_TRUE(version) << version.error().message << std::endl;

	auto version_unwrapped = version.value();

	EXPECT_EQ(version_unwrapped.version, 3);
	EXPECT_EQ(version_unwrapped.format, "mender");
}

TEST(ParserTest, TestParseWrongVersion) {
	// We don't support version 2
	std::string json_data = R"(
  {
    "version": 2,
		"format" : "mender"
  }
)";

	std::stringstream ss {json_data};

	mender::common::io::StreamReader sr {ss};

	auto version = artifact::v3::version::Parse(sr);

	ASSERT_FALSE(version) << version.error().message << std::endl;

	auto expected_error_message = "Only version 3 is supported, received version 2";

	EXPECT_EQ(version.error().message, expected_error_message);
}


TEST(ParserTest, TestParseWrongFormat) {
	// We don't support other formats than mender atm
	std::string json_data = R"(
  {
    "version": 3,
		"format" : "foobar"
  }
)";

	std::stringstream ss {json_data};

	mender::common::io::StreamReader sr {ss};

	auto version = artifact::v3::version::Parse(sr);

	ASSERT_FALSE(version) << version.error().message << std::endl;

	auto expected_error_message =
		"The client only understands the 'mender' Artifact type. Got format: foobar";

	EXPECT_EQ(version.error().message, expected_error_message);
}

TEST(ParserTest, TestParseMumboJumbo) {
	std::string json_data = R"(
foobarbaz
)";

	std::stringstream ss {json_data};

	mender::common::io::StreamReader sr {ss};

	auto version = artifact::v3::version::Parse(sr);

	ASSERT_FALSE(version) << version.error().message << std::endl;

	auto expected_error_message = "Failed to parse the version header JSON";

	EXPECT_THAT(version.error().message, testing::StartsWith(expected_error_message));
}


TEST(ParserTest, TestParseMalformedInput) {
	// Missing ending {
	std::string json_data = R"(
  {
    "version": 3,
		"format" : "mender"
)";

	std::stringstream ss {json_data};

	mender::common::io::StreamReader sr {ss};

	auto version = artifact::v3::version::Parse(sr);

	ASSERT_FALSE(version) << version.error().message << std::endl;

	auto expected_error_message = "Failed to parse the version header JSON";

	EXPECT_THAT(version.error().message, testing::StartsWith(expected_error_message));
}
