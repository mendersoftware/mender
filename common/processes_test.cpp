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

#include <common/processes.hpp>

#include <fstream>
#include <cstdio>
#include <sys/stat.h>

#include <gtest/gtest.h>
#include <gmock/gmock.h>

#include <common/testing.hpp>

namespace error = mender::common::error;
namespace procs = mender::common::processes;
namespace mtesting = mender::common::testing;

using namespace std;

class ProcessesTests : public testing::Test {
protected:
	const char *test_script_fname = "./test_script.sh";

	bool PrepareTestScript(const string script) {
		ofstream os(test_script_fname);
		os << script;
		os.close();

		int ret = chmod(test_script_fname, S_IRUSR | S_IWUSR | S_IXUSR);
		return ret == 0;
	}

	void TearDown() override {
		remove(test_script_fname);
	}
};

TEST_F(ProcessesTests, SimpleGenerateLineDataTest) {
	string script = R"(#!/bin/sh
echo "Hello, world!"
echo "Hi, there!"
exit 0
)";
	auto ret = PrepareTestScript(script);
	ASSERT_TRUE(ret);

	procs::Process proc({test_script_fname});
	auto ex_line_data = proc.GenerateLineData();
	ASSERT_TRUE(ex_line_data);
	EXPECT_EQ(proc.GetExitStatus(), 0);
	EXPECT_EQ(ex_line_data.value().size(), 2);
	EXPECT_EQ(ex_line_data.value()[0], "Hello, world!");
	EXPECT_EQ(ex_line_data.value()[1], "Hi, there!");
}

TEST_F(ProcessesTests, GenerateLineDataNoEOLTest) {
	string script = R"(#!/bin/sh
echo "Hello, world!"
echo -n "Hi, there!"
exit 0
)";
	auto ret = PrepareTestScript(script);
	ASSERT_TRUE(ret);

	procs::Process proc({test_script_fname});
	auto ex_line_data = proc.GenerateLineData();
	ASSERT_TRUE(ex_line_data);
	EXPECT_EQ(proc.GetExitStatus(), 0);
	EXPECT_EQ(ex_line_data.value().size(), 2);
	EXPECT_EQ(ex_line_data.value()[0], "Hello, world!");
	EXPECT_EQ(ex_line_data.value()[1], "Hi, there!");
}

TEST_F(ProcessesTests, GenerateOneLineDataNoEOLTest) {
	string script = R"(#!/bin/sh
echo -n "Hi, there!"
exit 0
)";
	auto ret = PrepareTestScript(script);
	ASSERT_TRUE(ret);

	procs::Process proc({test_script_fname});
	auto ex_line_data = proc.GenerateLineData();
	ASSERT_TRUE(ex_line_data);
	EXPECT_EQ(proc.GetExitStatus(), 0);
	EXPECT_EQ(ex_line_data.value().size(), 1);
	EXPECT_EQ(ex_line_data.value()[0], "Hi, there!");
}

TEST_F(ProcessesTests, GenerateEmptyLineDataTest) {
	string script = R"(#!/bin/sh
exit 0
)";
	auto ret = PrepareTestScript(script);
	ASSERT_TRUE(ret);

	procs::Process proc({test_script_fname});
	auto ex_line_data = proc.GenerateLineData();
	ASSERT_TRUE(ex_line_data);
	EXPECT_EQ(proc.GetExitStatus(), 0);
	EXPECT_EQ(ex_line_data.value().size(), 0);
}

TEST_F(ProcessesTests, FailGenerateLineDataTest) {
	string script = R"(#!/bin/sh
exit 1
)";
	auto ret = PrepareTestScript(script);
	ASSERT_TRUE(ret);

	procs::Process proc({test_script_fname});
	auto ex_line_data = proc.GenerateLineData();
	ASSERT_TRUE(ex_line_data);
	EXPECT_EQ(proc.GetExitStatus(), 1);
	EXPECT_EQ(ex_line_data.value().size(), 0);
}

TEST_F(ProcessesTests, GenerateLineDataAndFailTest) {
	string script = R"(#!/bin/sh
echo "Hello, world!"
echo "Hi, there!"
exit 1
)";
	auto ret = PrepareTestScript(script);
	ASSERT_TRUE(ret);

	procs::Process proc({test_script_fname});
	auto ex_line_data = proc.GenerateLineData();
	ASSERT_TRUE(ex_line_data);
	EXPECT_EQ(proc.GetExitStatus(), 1);
	EXPECT_EQ(ex_line_data.value().size(), 2);
	EXPECT_EQ(ex_line_data.value()[0], "Hello, world!");
	EXPECT_EQ(ex_line_data.value()[1], "Hi, there!");
}

TEST_F(ProcessesTests, SpawnFailGenerateLineDataTest) {
	// XXX: This should probably return an error, but for the line data
	//      generation use case we don't really care if there is no data or
	//      there was an error running the script
	procs::Process proc({test_script_fname + string("-noexist")});
	auto ex_line_data = proc.GenerateLineData();
	ASSERT_TRUE(ex_line_data);
	EXPECT_EQ(proc.GetExitStatus(), 1);
	EXPECT_EQ(ex_line_data.value().size(), 0);
}

TEST_F(ProcessesTests, StartInBackground) {
	mtesting::TemporaryDirectory tmpdir;

	string testfile = tmpdir.Path() + "/testfile";

	string script = R"(#!/bin/sh
touch )" + testfile + R"(
while [ -e )" + testfile
					+ R"( ]; do
    # Tight loop, but we expect the file to be removed fast.
    :
done
exit 0
)";
	auto ret = PrepareTestScript(script);
	ASSERT_TRUE(ret);

	procs::Process proc({test_script_fname});
	auto err = proc.Start();
	ASSERT_EQ(err, error::NoError);
	while (true) {
		ifstream f(testfile);
		if (f.good()) {
			break;
		}

		// Tight loop, but we expect the script to create the file quickly.
	}

	remove(testfile.c_str());

	auto exit_status = proc.Wait();
	EXPECT_EQ(exit_status, 0);
}

TEST_F(ProcessesTests, Terminate) {
	mtesting::TemporaryDirectory tmpdir;

	string testfile = tmpdir.Path() + "/testfile";

	string script = R"(#!/bin/sh
sleep 10
exit 0
)";
	auto ret = PrepareTestScript(script);
	ASSERT_TRUE(ret);

	procs::Process proc({test_script_fname});
	auto err = proc.Start();
	ASSERT_EQ(err, error::NoError);

	proc.Terminate();

	auto exit_status = proc.Wait();
	EXPECT_NE(exit_status, 0);
}

TEST_F(ProcessesTests, Kill) {
	mtesting::TemporaryDirectory tmpdir;

	string testfile = tmpdir.Path() + "/testfile";

	string script = R"(#!/bin/sh
sleep 10
exit 0
)";
	auto ret = PrepareTestScript(script);
	ASSERT_TRUE(ret);

	procs::Process proc({test_script_fname});
	auto err = proc.Start();
	ASSERT_EQ(err, error::NoError);

	proc.Kill();

	auto exit_status = proc.Wait();
	EXPECT_NE(exit_status, 0);
}
