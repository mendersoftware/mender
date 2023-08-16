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

#include <cerrno>
#include <functional>

#include <common/testing.hpp>

#include <gtest/gtest.h>
#include <gmock/gmock.h>

using namespace std;
using namespace mender::common::testing;

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
	ASSERT_EQ(error::NoError, error);

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
	ASSERT_EQ(error::NoError, error);

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
	ASSERT_EQ(error::NoError, error);

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
	ASSERT_NE(error::NoError, error);
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
	ASSERT_NE(error::NoError, error);
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
	ASSERT_NE(error::NoError, error);
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
	ASSERT_NE(error::NoError, error);
	ASSERT_EQ(error.code, std::errc::io_error);
}


TEST(IO, TestStringReader) {
	auto string_reader = io::StringReader("foobar");

	auto discard_writer = io::Discard {};

	auto err = Copy(discard_writer, string_reader);

	ASSERT_EQ(error::NoError, err);
}

TEST(IO, TestByteReader) {
	vector<uint8_t> buffer {1, 2, 3, 4, 5, 6, 5, 4, 3, 2, 1};
	auto byte_reader = io::ByteReader(buffer);

	auto discard_writer = io::Discard {};

	auto err = Copy(discard_writer, byte_reader);

	ASSERT_EQ(error::NoError, err);
}

TEST(IO, TestByteWriter) {
	auto string_reader = io::StringReader("foobar");

	vector<uint8_t> vec {};
	auto byte_writer = io::ByteWriter(vec);
	byte_writer.SetUnlimited(true);

	auto err = Copy(byte_writer, string_reader);
	ASSERT_EQ(error::NoError, err);

	EXPECT_EQ(vec, (vector<uint8_t> {'f', 'o', 'o', 'b', 'a', 'r'}));

	string_reader = io::StringReader("tadow!");
	unique_ptr<io::ByteWriter> byte_writer2;
	{
		auto vec2 = make_shared<vector<uint8_t>>();
		byte_writer2.reset(new io::ByteWriter(vec2));
		byte_writer2->SetUnlimited(true);
	}
	// vec2 out of scope, but it's a shared pointer and byte_writer2 should
	// still have access to it so there should be no errors
	err = Copy(*byte_writer2, string_reader);
	ASSERT_EQ(error::NoError, err);

	auto vec3 = make_shared<vector<uint8_t>>();
	function<bool()> some_fn = []() { return false; };
	{
		auto fn = [vec3]() {
			auto writer = make_shared<io::ByteWriter>(vec3);
			writer->SetUnlimited(true);
			return true;
		};
		some_fn = fn;
	}
	EXPECT_EQ(some_fn(), true);
}

class StreamIOTests : public testing::Test {
protected:
	TemporaryDirectory tmp_dir;
};

TEST_F(StreamIOTests, OpenIfstreamOfstreamOK) {
	string test_file_path = tmp_dir.Path() + "/test_file";

	auto ex_os = io::OpenOfstream(test_file_path);
	ASSERT_TRUE(ex_os);
	auto &os = ex_os.value();
	os << "test data" << endl;
	EXPECT_TRUE(os.good());
	os.close();

	auto ex_is = io::OpenIfstream(test_file_path);
	ASSERT_TRUE(ex_is);
	auto &is = ex_is.value();
	string data;
	getline(is, data);
	EXPECT_EQ(data, "test data");

	getline(is, data);
	EXPECT_TRUE(is.eof());
	EXPECT_EQ(data, "");
	is.close();
}

TEST_F(StreamIOTests, OpenIfstreamOfstreamNoexist) {
	string test_file_path = tmp_dir.Path() + "/test_file";
	auto ex_is = io::OpenIfstream(test_file_path);
	ASSERT_FALSE(ex_is);
	EXPECT_TRUE(ex_is.error().IsErrno(ENOENT));

	test_file_path = tmp_dir.Path() + "/noexist/test_file";
	auto ex_os = io::OpenOfstream(test_file_path);
	ASSERT_FALSE(ex_os);
	EXPECT_TRUE(ex_os.error().IsErrno(ENOENT));
}

TEST_F(StreamIOTests, WriteStringIntoOfstreamOK) {
	string test_file_path = tmp_dir.Path() + "/test_file";

	auto ex_os = io::OpenOfstream(test_file_path);
	ASSERT_TRUE(ex_os);

	auto &os = ex_os.value();
	auto err = io::WriteStringIntoOfstream(os, "some\nnon-trivial\n\tdata here\n");
	ASSERT_EQ(err, error::NoError);
	os.close();

	ifstream is(test_file_path);
	string data;
	getline(is, data);
	EXPECT_EQ(data, "some");
	getline(is, data);
	EXPECT_EQ(data, "non-trivial");
	getline(is, data);
	EXPECT_EQ(data, "\tdata here");

	getline(is, data);
	EXPECT_TRUE(is.eof());
	EXPECT_EQ(data, "");
	is.close();
}

TEST_F(StreamIOTests, WriteStringIntoClosedOfstream) {
	string test_file_path = tmp_dir.Path() + "/test_file";

	auto ex_os = io::OpenOfstream(test_file_path);
	ASSERT_TRUE(ex_os);

	auto &os = ex_os.value();
	os.close();

	auto err = io::WriteStringIntoOfstream(os, "some data");
	EXPECT_NE(err, error::NoError);
}
