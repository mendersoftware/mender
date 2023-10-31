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


#include <artifact/tar/tar.hpp>

#include <cstdint>
#include <memory>
#include <string>
#include <vector>
#include <fstream>
#include <ostream>
#include <memory>


#include <gtest/gtest.h>
#include <gmock/gmock.h>

#include <common/processes.hpp>

#include <common/testing.hpp>

using namespace std;

namespace io = mender::common::io;
namespace error = mender::common::error;
namespace tar = mender::tar;
namespace processes = mender::common::processes;
namespace mendertesting = mender::common::testing;

class TarTestEnv : public testing::Test {
public:
protected:
	static void SetUpTestSuite() {
		string script = R"(#! /bin/sh

    DIRNAME=$(dirname $0)

		# Create small tar file
		echo foobar > ${DIRNAME}/testdata
		tar cvfz ${DIRNAME}/test.tar ${DIRNAME}/testdata

    # Create large tar file
    dd if=/dev/random of=${DIRNAME}/testinput.large bs=1M count=4
    tar cvf ${DIRNAME}/test-large.tar ${DIRNAME}/testinput.large

    # Create a corrupt tar file
    cp ${DIRNAME}/test.tar ${DIRNAME}/test-corrupt.tar
    dd if=/dev/random of=${DIRNAME}/test-corrupt.tar seek=10 count=5 bs=1 conv=notrunc

		exit 0
		)";

		const string script_fname = tmpdir->Path() + "/test-script.sh";

		std::ofstream os(script_fname.c_str(), std::ios::out);
		os << script;
		os.close();

		int ret = chmod(script_fname.c_str(), S_IRUSR | S_IWUSR | S_IXUSR);
		ASSERT_EQ(ret, 0);


		processes::Process proc({script_fname});
		auto ex_line_data = proc.GenerateLineData();
		ASSERT_TRUE(ex_line_data);
		EXPECT_EQ(proc.GetExitStatus(), 0) << "error message: " + ex_line_data.error().message;
	}

	static void TearDownTestSuite() {
		tmpdir.reset();
	}

	static unique_ptr<mendertesting::TemporaryDirectory> tmpdir;
};

unique_ptr<mendertesting::TemporaryDirectory> TarTestEnv::tmpdir =
	unique_ptr<mendertesting::TemporaryDirectory>(new mendertesting::TemporaryDirectory());
;

TEST_F(TarTestEnv, TestTarReaderInitialization) {
	std::fstream fs {tmpdir->Path() + "/test.tar"};

	mender::common::io::StreamReader sr {fs};

	mender::tar::Reader tar_reader {sr};

	mender::tar::Entry tar_entry = tar_reader.Next().value();

	ASSERT_THAT(tar_entry.Name(), testing::EndsWith("testdata"));

	vector<uint8_t> data(10);

	io::ByteWriter bw {data};

	auto err = io::Copy(bw, tar_entry);

	EXPECT_EQ(error::NoError, err);

	vector<uint8_t> expected {'f', 'o', 'o', 'b', 'a', 'r', '\n', '\0', '\0', '\0'};

	ASSERT_EQ(data, expected);
}

TEST_F(TarTestEnv, TestTarReaderMultipleReadCalls) {
	std::fstream fs {tmpdir->Path() + "/test.tar"};

	mender::common::io::StreamReader sr {fs};

	mender::tar::Reader tar_reader {sr};

	mender::tar::Entry tar_entry = tar_reader.Next().value();

	ASSERT_THAT(tar_entry.Name(), testing::EndsWith("testdata"));

	vector<uint8_t> data(10);

	auto bytes_read = tar_entry.Read(data.begin(), data.end());

	ASSERT_TRUE(bytes_read);

	ASSERT_GT(bytes_read.value(), 0);

	vector<uint8_t> expected {'f', 'o', 'o', 'b', 'a', 'r', '\n', '\0', '\0', '\0'};

	ASSERT_EQ(data, expected);

	// Second read should return 0 now

	auto second_bytes_read = tar_entry.Read(data.begin(), data.end());

	ASSERT_TRUE(second_bytes_read);

	ASSERT_EQ(second_bytes_read.value(), 0);
}

TEST_F(TarTestEnv, TestTarReaderLargeTarRead) {
	std::fstream fs {tmpdir->Path() + "/test-large.tar"};

	mender::common::io::StreamReader sr {fs};

	mender::tar::Reader tar_reader {sr};

	mender::tar::Entry tar_entry = tar_reader.Next().value();

	auto discard_writer = io::Discard {};

	auto err = io::Copy(discard_writer, tar_entry);

	EXPECT_EQ(error::NoError, err);
}

TEST_F(TarTestEnv, TestTarReaderEOF) {
	std::fstream fs {tmpdir->Path() + "/test-large.tar"};

	mender::common::io::StreamReader sr {fs};

	mender::tar::Reader tar_reader {sr};

	mender::tar::ExpectedEntry tar_entry = tar_reader.Next();

	ASSERT_TRUE(tar_entry);

	mender::tar::ExpectedEntry next_tar_entry = tar_reader.Next();

	EXPECT_FALSE(next_tar_entry);
	EXPECT_EQ(next_tar_entry.error().message, "Reached the end of the archive");
}


TEST_F(TarTestEnv, TestCorruptTar) {
	std::fstream fs {tmpdir->Path() + "/test-corrupt.tar"};

	mender::common::io::StreamReader sr {fs};

	mender::tar::Reader tar_reader {sr};

	auto tar_entry = tar_reader.Next();

	EXPECT_FALSE(tar_entry);
}
