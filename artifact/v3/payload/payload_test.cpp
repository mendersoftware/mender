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

#include <artifact/v3/payload/payload.hpp>

#include <string>
#include <fstream>

#include <gtest/gtest.h>
#include <gmock/gmock.h>

#include <artifact/tar/tar.hpp>
#include <artifact/v3/manifest/manifest.hpp>

#include <common/processes.hpp>
#include <common/testing.hpp>

using namespace std;

namespace io = mender::common::io;
namespace error = mender::common::error;
namespace tar = mender::tar;
namespace processes = mender::common::processes;
namespace mendertesting = mender::common::testing;
namespace payload = mender::artifact::v3::payload;
namespace manifest = mender::artifact::v3::manifest;


class PayloadTestEnv : public testing::Test {
public:
protected:
	static void SetUpTestSuite() {
		string script = R"(#! /bin/sh

    DIRNAME=$(dirname $0)

    # Create small tar payload file
    echo foobar > testdata
    tar cvf ${DIRNAME}/test.tar testdata

    # Create a tar with multiple files
    echo barbaz > testdata2
    tar cvf ${DIRNAME}/multiple-files-payload.tar testdata testdata2

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

unique_ptr<mendertesting::TemporaryDirectory> PayloadTestEnv::tmpdir =
	unique_ptr<mendertesting::TemporaryDirectory>(new mendertesting::TemporaryDirectory());

TEST_F(PayloadTestEnv, TestPayloadSuccess) {
	std::fstream fs {tmpdir->Path() + "/test.tar"};

	mender::common::io::StreamReader sr {fs};

	manifest::Manifest manifest {
		{{"data/0000/testdata",
		  "aec070645fe53ee3b3763059376134f058cc337247c978add178b6ccdfb0019f"}}};

	auto payload = payload::Payload(sr, manifest);

	auto expected_payload = payload.Next();
	ASSERT_TRUE(expected_payload);

	auto discard_writer = io::Discard {};
	auto err = io::Copy(discard_writer, expected_payload.value());
	EXPECT_EQ(error::NoError, err);
}

TEST_F(PayloadTestEnv, TestPayloadFailure) {
	std::fstream fs {tmpdir->Path() + "/test.tar"};

	mender::common::io::StreamReader sr {fs};

	manifest::Manifest manifest {
		{{"data/0000/testdata",
		  // Ends with (e) not (f)
		  "aec070645fe53ee3b3763059376134f058cc337247c978add178b6ccdfb0019e"}}};

	auto payload = payload::Payload(sr, manifest);

	auto expected_payload = payload.Next();
	ASSERT_TRUE(expected_payload);

	auto discard_writer = io::Discard {};
	auto err = io::Copy(discard_writer, expected_payload.value());
	EXPECT_NE(error::NoError, err);
}

TEST_F(PayloadTestEnv, TestPayloadMultipleFiles) {
	std::fstream fs {tmpdir->Path() + "/multiple-files-payload.tar"};

	mender::common::io::StreamReader reader {fs};

	manifest::Manifest manifest {
		{{"data/0000/testdata", "aec070645fe53ee3b3763059376134f058cc337247c978add178b6ccdfb0019f"},
		 {"data/0000/testdata2",
		  "73a2c64f9545172c1195efb6616ca5f7afd1df6f245407cafb90de3998a1c97f"}}};

	auto p = payload::Payload(reader, manifest);

	auto expected_payload = p.Next();
	ASSERT_TRUE(expected_payload);

	auto payload_reader {expected_payload.value()};

	EXPECT_EQ(payload_reader.Name(), "testdata");
	EXPECT_EQ(payload_reader.Size(), 7);

	auto discard_writer = io::Discard {};
	auto err = io::Copy(discard_writer, payload_reader);
	EXPECT_EQ(error::NoError, err);

	expected_payload = p.Next();
	EXPECT_TRUE(expected_payload);

	payload_reader = expected_payload.value();

	EXPECT_EQ(payload_reader.Name(), "testdata2");
	EXPECT_EQ(payload_reader.Size(), 7);

	discard_writer = io::Discard {};
	err = io::Copy(discard_writer, payload_reader);
	EXPECT_EQ(error::NoError, err);

	expected_payload = p.Next();
	EXPECT_FALSE(expected_payload);
	EXPECT_EQ(expected_payload.error().message, "Reached the end of the archive");
}
