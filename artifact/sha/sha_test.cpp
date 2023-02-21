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

#include <artifact/sha/sha.hpp>
#include <common/io.hpp>

#include <gtest/gtest.h>
#include <gmock/gmock.h>
#include <string>
#include <vector>

using namespace std;

namespace io = mender::common::io;
namespace sha = mender::sha;

TEST(ShasummerTest, TestShaSum) {
	string input = "foobarbaz";

	io::StringReader is {input};

	sha::Reader r {is};

	vector<uint8_t> actual(4096);

	auto bytes_read = r.Read(actual);

	EXPECT_TRUE(bytes_read);

	// EOF read and get the proper shasum
	ASSERT_GT(bytes_read.value(), 0);

	auto expected_shasum = r.ShaSum();
	ASSERT_TRUE(expected_shasum);
	auto shasum = expected_shasum.value();

	EXPECT_EQ(shasum, "97df3588b5a3f24babc3851b372f0ba71a9dcdded43b14b9d06961bfc1707d9d");
}

TEST(ShasummerTest, TestShaSumReadVerifySuccess) {
	namespace io = mender::common::io;

	string input = "foobarbaz";

	io::StringReader is {input};

	sha::Reader r {is, "97df3588b5a3f24babc3851b372f0ba71a9dcdded43b14b9d06961bfc1707d9d"};

	auto discard_writer = io::Discard {};

	auto err = io::Copy(discard_writer, r);

	EXPECT_FALSE(err);
}


TEST(ShasummerTest, TestShaSumReadVerifyWrongChecksum) {
	string input = "foobarbaz";

	io::StringReader is {input};

	sha::Reader r {
		is, "97df3588b5a3f24babc3851b372f0ba71a9dcdded43b14b9d06961bfc1707d9e"}; // Ends with (e)
																				 // not (d)

	auto discard_writer = io::Discard {};

	auto err = io::Copy(discard_writer, r);

	EXPECT_TRUE(err);

	auto expected_message =
		"The checksum of the read byte-stream does not match the expected checksum, (expected): 97df3588b5a3f24babc3851b372f0ba71a9dcdded43b14b9d06961bfc1707d9e (calculated): 97df3588b5a3f24babc3851b372f0ba71a9dcdded43b14b9d06961bfc1707d9d";
	auto expected_error = sha::MakeError(sha::ShasumMismatchError, expected_message);
	EXPECT_EQ(err.message, expected_message);
	EXPECT_EQ(err, expected_error);
}
