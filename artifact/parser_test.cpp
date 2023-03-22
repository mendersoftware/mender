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

#include <artifact/parser.hpp>
#include <artifact/lexer.hpp>

#include <gtest/gtest.h>
#include <gmock/gmock.h>

#include <common/processes.hpp>

#include <common/testing.hpp>

#include <fstream>


using namespace std;

namespace io = mender::common::io;

namespace tar = mender::tar;

namespace processes = mender::common::processes;

namespace mendertesting = mender::common::testing;

class ParserTestEnv : public testing::Test {
public:
protected:
	static void SetUpTestSuite() {
		string script = R"(#! /bin/sh

    DIRNAME=$(dirname $0)

		# Create small tar file
		echo foobar > ${DIRNAME}/testdata
		mender-artifact --compression none write rootfs-image --no-progress -t test-device -n test-artifact -f ${DIRNAME}/testdata -o ${DIRNAME}/test-artifact-no-compression.mender || exit 1

		mender-artifact --compression gzip write rootfs-image --no-progress -t test-device -n test-artifact -f ${DIRNAME}/testdata -o ${DIRNAME}/test-artifact-gzip.mender || exit 1

		mender-artifact --compression lzma write rootfs-image --no-progress -t test-device -n test-artifact -f ${DIRNAME}/testdata -o ${DIRNAME}/test-artifact-lzma.mender || exit 1

		mender-artifact --compression zstd_better write rootfs-image --no-progress -t test-device -n test-artifact -f ${DIRNAME}/testdata -o ${DIRNAME}/test-artifact-zstd.mender || exit 1

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

unique_ptr<mendertesting::TemporaryDirectory> ParserTestEnv::tmpdir =
	unique_ptr<mendertesting::TemporaryDirectory>(new mendertesting::TemporaryDirectory());
;

TEST_F(ParserTestEnv, TestParseTopLevelNoCompression) {
	std::fstream fs {tmpdir->Path() + "/test-artifact-no-compression.mender"};

	io::StreamReader sr {fs};

	auto artifact = mender::artifact::parser::Parse(sr);

	ASSERT_TRUE(artifact) << artifact.error().message << std::endl;
}

TEST_F(ParserTestEnv, TestParseTopLevelGzip) {
	std::fstream fs {tmpdir->Path() + "/test-artifact-gzip.mender"};

	mender::common::io::StreamReader sr {fs};

	auto artifact = mender::artifact::parser::Parse(sr);

	ASSERT_TRUE(artifact) << artifact.error().message << std::endl;
}

TEST_F(ParserTestEnv, TestParseTopLevelLZMA) {
	std::fstream fs {tmpdir->Path() + "/test-artifact-lzma.mender"};

	mender::common::io::StreamReader sr {fs};

	auto artifact = mender::artifact::parser::Parse(sr);

	ASSERT_TRUE(artifact) << artifact.error().message << std::endl;
}

TEST_F(ParserTestEnv, TestParseTopLevelZstd) {
	std::fstream fs {tmpdir->Path() + "/test-artifact-zstd.mender"};

	mender::common::io::StreamReader sr {fs};

	auto artifact = mender::artifact::parser::Parse(sr);

	ASSERT_TRUE(artifact) << artifact.error().message << std::endl;
}

TEST(ParserTest, TestParseMumboJumbo) {
	std::stringstream ss {"foobar"};

	mender::common::io::StreamReader sr {ss};

	auto artifact = mender::artifact::parser::Parse(sr);

	ASSERT_FALSE(artifact) << artifact.error().message << std::endl;
	ASSERT_EQ(artifact.error().message, "Got unexpected token : 'Unrecognized' expected 'version'");
}
