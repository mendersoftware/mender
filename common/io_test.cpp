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

#include <common/io.hpp>

#include <gtest/gtest.h>
#include <gmock/gmock.h>

using namespace std;

namespace io = mender::common::io;
namespace error = mender::common::error;

namespace expected = mender::common::expected;

TEST(IO, Copy) {
	class TestReader : public io::Reader {
	public:
		MOCK_METHOD(
			expected::ExpectedSize,
			Read,
			(vector<uint8_t>::iterator start, vector<uint8_t>::iterator end),
			(override));
	};
	class TestWriter : public io::Writer {
	public:
		MOCK_METHOD(
			expected::ExpectedSize,
			Write,
			(vector<uint8_t>::const_iterator start, vector<uint8_t>::const_iterator end),
			(override));
	};

	testing::StrictMock<TestReader> r;
	testing::StrictMock<TestWriter> w;

	vector<uint8_t> data;

	// Zero copy.
	EXPECT_CALL(r, Read).Times(testing::Exactly(1)).WillRepeatedly(testing::Return(0));
	EXPECT_CALL(w, Write).Times(testing::Exactly(0));
	auto error = Copy(w, r);
	ASSERT_FALSE(error);

	// Random data.
	EXPECT_CALL(r, Read)
		.Times(testing::Exactly(2))
		.WillOnce(testing::Invoke(
			[](vector<uint8_t>::iterator start,
			   vector<uint8_t>::iterator end) -> expected::ExpectedSize {
				*(start++) = uint8_t('a');
				*(start++) = uint8_t('b');
				*(start++) = uint8_t('c');
				return 3;
			}))
		.WillRepeatedly(testing::Return(expected::ExpectedSize(0)));
	vector<uint8_t> expected {uint8_t('a'), uint8_t('b'), uint8_t('c')};
	EXPECT_CALL(w, Write)
		.Times(testing::Exactly(1))
		.WillOnce(testing::DoAll(
			testing::Invoke(
				[&expected](
					vector<uint8_t>::const_iterator start, vector<uint8_t>::const_iterator end) {
					ASSERT_TRUE(equal(start, end, expected.cbegin()));
				}),
			testing::Return(expected::ExpectedSize(3))));
	error = Copy(w, r);
	ASSERT_FALSE(error);

	// Short read (should re-read and succeed).
	EXPECT_CALL(r, Read)
		.Times(testing::Exactly(3))
		.WillOnce(testing::Invoke(
			[](vector<uint8_t>::iterator start,
			   vector<uint8_t>::iterator end) -> expected::ExpectedSize {
				*(start++) = uint8_t('a');
				*(start++) = uint8_t('b');
				return 2;
			}))
		.WillOnce(testing::Invoke(
			[](vector<uint8_t>::iterator start,
			   vector<uint8_t>::iterator end) -> expected::ExpectedSize {
				*(start++) = uint8_t('c');
				return 1;
			}))
		.WillRepeatedly(testing::Return(expected::ExpectedSize(0)));
	expected = vector<uint8_t> {uint8_t('a'), uint8_t('b')};
	auto expected2 = vector<uint8_t> {uint8_t('c')};
	EXPECT_CALL(w, Write)
		.Times(testing::Exactly(2))
		.WillOnce(testing::DoAll(
			testing::Invoke(
				[&expected](
					vector<uint8_t>::const_iterator start, vector<uint8_t>::const_iterator end) {
					ASSERT_TRUE(equal(start, end, expected.begin()));
				}),
			testing::Return(expected::ExpectedSize(expected.cend() - expected.cbegin()))))
		.WillRepeatedly(testing::DoAll(
			testing::Invoke(
				[&expected2](
					vector<uint8_t>::const_iterator start, vector<uint8_t>::const_iterator end) {
					ASSERT_TRUE(equal(start, end, expected2.begin()));
				}),
			testing::Return(expected::ExpectedSize(expected2.cend() - expected2.cbegin()))));
	error = Copy(w, r);
	ASSERT_FALSE(error);

	// Error on second read.
	EXPECT_CALL(r, Read)
		.Times(testing::Exactly(2))
		.WillOnce(testing::Invoke(
			[](vector<uint8_t>::iterator start,
			   vector<uint8_t>::iterator end) -> expected::ExpectedSize {
				*(start++) = uint8_t('a');
				*(start++) = uint8_t('b');
				return 2;
			}))
		.WillRepeatedly(testing::Invoke(
			[](vector<uint8_t>::iterator start,
			   vector<uint8_t>::iterator end) -> expected::ExpectedSize {
				*(start++) = uint8_t('c');
				return expected::unexpected(error::Error(std::errc::io_error, "Error"));
			}));
	expected = vector<uint8_t> {uint8_t('a'), uint8_t('b')};
	EXPECT_CALL(w, Write)
		.Times(testing::Exactly(1))
		.WillOnce(testing::DoAll(
			testing::Invoke(
				[&expected](
					vector<uint8_t>::const_iterator start, vector<uint8_t>::const_iterator end) {
					ASSERT_TRUE(equal(start, end, expected.begin()));
				}),
			testing::Return(expected::ExpectedSize(expected.cend() - expected.cbegin()))));
	error = Copy(w, r);
	ASSERT_TRUE(error);
	ASSERT_EQ(error.code, std::errc::io_error);

	// Error on write.
	EXPECT_CALL(r, Read)
		.Times(testing::Exactly(2))
		.WillOnce(testing::Invoke(
			[](vector<uint8_t>::iterator start,
			   vector<uint8_t>::iterator end) -> expected::ExpectedSize {
				*(start++) = uint8_t('a');
				*(start++) = uint8_t('b');
				return 2;
			}))
		.WillRepeatedly(testing::Invoke(
			[](vector<uint8_t>::iterator start,
			   vector<uint8_t>::iterator end) -> expected::ExpectedSize {
				*(start++) = uint8_t('c');
				return 1;
			}));
	expected = vector<uint8_t> {uint8_t('a'), uint8_t('b')};
	expected2 = vector<uint8_t> {uint8_t('c')};
	EXPECT_CALL(w, Write)
		.Times(testing::Exactly(2))
		.WillOnce(testing::DoAll(
			testing::Invoke(
				[&expected](
					vector<uint8_t>::const_iterator start, vector<uint8_t>::const_iterator end) {
					ASSERT_TRUE(equal(start, end, expected.begin()));
				}),
			testing::Return(expected::ExpectedSize(expected.cend() - expected.cbegin()))))
		.WillRepeatedly(testing::DoAll(
			testing::Invoke(
				[&expected2](
					vector<uint8_t>::const_iterator start, vector<uint8_t>::const_iterator end) {
					ASSERT_TRUE(equal(start, end, expected2.begin()));
				}),
			testing::Return(
				expected::unexpected(error::Error(std::errc::invalid_argument, "Error")))));
	error = Copy(w, r);
	ASSERT_TRUE(error);
	ASSERT_EQ(error.code, std::errc::invalid_argument);

	// Short write.
	EXPECT_CALL(r, Read)
		.Times(testing::Exactly(1))
		.WillOnce(testing::Invoke(
			[](vector<uint8_t>::iterator start,
			   vector<uint8_t>::iterator end) -> expected::ExpectedSize {
				*(start++) = uint8_t('a');
				*(start++) = uint8_t('b');
				return 2;
			}));
	expected = vector<uint8_t> {uint8_t('a'), uint8_t('b')};
	EXPECT_CALL(w, Write)
		.Times(testing::Exactly(1))
		.WillRepeatedly(testing::DoAll(
			testing::Invoke(
				[&expected](
					vector<uint8_t>::const_iterator start, vector<uint8_t>::const_iterator end) {
					ASSERT_TRUE(equal(start, end, expected.begin()));
				}),
			testing::Return(expected::ExpectedSize(expected.cend() - expected.cbegin() - 1))));
	error = Copy(w, r);
	ASSERT_TRUE(error);
	ASSERT_EQ(error.code, std::errc::io_error);

	// No write.
	EXPECT_CALL(r, Read)
		.Times(testing::Exactly(1))
		.WillOnce(testing::Invoke(
			[](vector<uint8_t>::iterator start,
			   vector<uint8_t>::iterator end) -> expected::ExpectedSize {
				*(start++) = uint8_t('a');
				*(start++) = uint8_t('b');
				return 2;
			}));
	expected = vector<uint8_t> {uint8_t('a'), uint8_t('b')};
	EXPECT_CALL(w, Write)
		.Times(testing::Exactly(1))
		.WillRepeatedly(testing::DoAll(
			testing::Invoke(
				[&expected](
					vector<uint8_t>::const_iterator start, vector<uint8_t>::const_iterator end) {
					ASSERT_TRUE(equal(start, end, expected.begin()));
				}),
			testing::Return(expected::ExpectedSize(0))));
	error = Copy(w, r);
	ASSERT_TRUE(error);
	ASSERT_EQ(error.code, std::errc::io_error);
}


TEST(IO, TestStringReader) {
	auto string_reader = io::StringReader("foobar");

	auto discard_writer = io::Discard {};

	auto err = Copy(discard_writer, string_reader);

	EXPECT_FALSE(err);
}
