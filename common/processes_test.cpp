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

#include <common/conf.hpp>
#include <common/io.hpp>
#include <common/path.hpp>
#include <common/testing.hpp>

namespace conf = mender::common::conf;
namespace error = mender::common::error;
namespace io = mender::common::io;
namespace path = mender::common::path;
namespace procs = mender::common::processes;
namespace mtesting = mender::common::testing;

namespace tpl = TinyProcessLib;

using namespace std;

class ProcessesTests : public testing::Test {
protected:
	void SetUp() override {
		tmpdir_ = make_unique<mtesting::TemporaryDirectory>();
	}

	string TestScriptPath() {
		return path::Join(tmpdir_->Path(), "test_script.sh");
	}

	bool PrepareTestScript(const string script) {
		string path = TestScriptPath();
		ofstream os(path);
		os << script;
		os.close();

		int ret = chmod(path.c_str(), S_IRUSR | S_IWUSR | S_IXUSR);
		return ret == 0;
	}

	void TearDown() override {
		tmpdir_.reset();
	}

	unique_ptr<mtesting::TemporaryDirectory> tmpdir_;
};

class ProcessesTestsHelper {
public:
	static unique_ptr<tpl::Process> &GetNativeProc(procs::Process &proc) {
		return proc.proc_;
	}
	static chrono::seconds &GetMaxTerminationTime(procs::Process &proc) {
		return proc.max_termination_time_;
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

	procs::Process proc({TestScriptPath()});
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

	procs::Process proc({TestScriptPath()});
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

	procs::Process proc({TestScriptPath()});
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

	procs::Process proc({TestScriptPath()});
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

	procs::Process proc({TestScriptPath()});
	auto ex_line_data = proc.GenerateLineData();
	ASSERT_FALSE(ex_line_data);
	EXPECT_EQ(proc.GetExitStatus(), 1);
}

TEST_F(ProcessesTests, StartInBackground) {
	mtesting::TemporaryDirectory tmpdir;

	string testfile = path::Join(tmpdir.Path(), "testfile");

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

	procs::Process proc({TestScriptPath()});
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

	err = proc.Wait();
	EXPECT_EQ(err, error::NoError);
	auto exit_status = proc.GetExitStatus();
	EXPECT_EQ(exit_status, 0);
}

TEST_F(ProcessesTests, Terminate) {
	auto ld_preload = conf::GetEnv("LD_PRELOAD", "");
	if (ld_preload.find("/valgrind/") != string::npos) {
		// Exact reason is unknown, but killing sub processes seems to be unreliable under
		// Valgrind.
		GTEST_SKIP() << "This test does not work under Valgrind";
	}

	string script = R"(#!/bin/sh
sleep 10
exit 0
)";
	auto ret = PrepareTestScript(script);
	ASSERT_TRUE(ret);

	procs::Process proc({TestScriptPath()});
	auto err = proc.Start();
	ASSERT_EQ(err, error::NoError);

	proc.Terminate();

	err = proc.Wait();
	EXPECT_NE(err, error::NoError);
	EXPECT_EQ(err.code, procs::MakeError(procs::NonZeroExitStatusError, "").code);
	EXPECT_THAT(err.String(), testing::HasSubstr("status 15"));
	auto exit_status = proc.GetExitStatus();
	EXPECT_EQ(exit_status, 15);
}

TEST_F(ProcessesTests, KillAndAutomaticKill) {
	auto ld_preload = conf::GetEnv("LD_PRELOAD", "");
	if (ld_preload.find("/valgrind/") != string::npos) {
		// Exact reason is unknown, but killing sub processes seems to be unreliable under
		// Valgrind.
		GTEST_SKIP() << "This test does not work under Valgrind";
	}

	string script = R"delim(#!/bin/bash
# Make us unkillable by common signals.
no_kill() {
    echo "Dodged attempted kill"
}
trap no_kill SIGTERM
trap no_kill SIGINT
trap no_kill SIGQUIT

# Create file to signal we are not unkillable.
touch "$(dirname "$0")/test_script-ready"

hard_sleep() {
    # Need to sleep via unconventional means because we cannot prevent the sub commands from
    # respecting signals.
    local target="$(date -d "now + $1 seconds" +%s)"
    # Because the parent is constantly trying to kill our process group, the variable above may be
    # empty.
    while [ -z "$target" ]; do
        target="$(date -d "now + $1 seconds" +%s)"
    done

    local now
    while true; do
        sleep 1
        now=
        while [ -z "$now" ]; do
            now="$(date -d now +%s)"
        done
        test "$now" -ge "$target" && break
    done
}
hard_sleep 10
exit 0
)delim";

	auto ret = PrepareTestScript(script);
	ASSERT_TRUE(ret);

	string ready_file = path::Join(path::DirName(TestScriptPath()), "test_script-ready");

	auto proc = make_shared<procs::Process>(vector<string> {TestScriptPath()});
	auto err = proc->Start();
	ASSERT_EQ(err, error::NoError);

	while (true) {
		ifstream is(ready_file);
		if (is.good()) {
			break;
		}
	}

	proc->Kill();

	err = proc->Wait();
	EXPECT_NE(err, error::NoError);
	EXPECT_EQ(err.code, procs::MakeError(procs::NonZeroExitStatusError, "").code);
	EXPECT_THAT(err.String(), testing::HasSubstr("status 9"));
	auto exit_status = proc->GetExitStatus();
	EXPECT_EQ(exit_status, 9);

	remove(ready_file.c_str());

	auto start_time = chrono::steady_clock::now();

	proc = make_shared<procs::Process>(vector<string> {TestScriptPath()});
	err = proc->Start();
	ASSERT_EQ(err, error::NoError);

	while (true) {
		ifstream is(ready_file);
		if (is.good()) {
			break;
		}
	}

	auto &max_termination_time = ProcessesTestsHelper::GetMaxTerminationTime(*proc);
	// Cut down a bit on the kill time in tests.
	max_termination_time = chrono::seconds {1};

	auto pid = ProcessesTestsHelper::GetNativeProc(*proc)->get_id();

	// Kill by destruction.
	proc.reset();

	auto result = ::kill(pid, 0);
	ASSERT_EQ(result, -1);
	int errnum = errno;
	// No such process.
	ASSERT_EQ(errnum, ESRCH);

	auto finish_time = chrono::steady_clock::now();
	auto diff = finish_time - start_time;
	EXPECT_LT(diff, chrono::seconds {10});
}

TEST_F(ProcessesTests, ProcessReaders) {
	mtesting::TestEventLoop loop;

	string script = R"(#!/bin/bash
sleep 0.2
echo stdout 1

sleep 0.2
echo stderr 1 1>&2

sleep 0.2
echo stdout 2

sleep 0.2
echo stderr 2 1>&2

sleep 0.2
echo stdout 3

sleep 0.2
echo stderr 3 1>&2

exit 0
)";
	auto ret = PrepareTestScript(script);
	ASSERT_TRUE(ret);

	procs::Process proc({TestScriptPath()});

	auto maybe_reader = proc.GetAsyncStdoutReader(loop);
	ASSERT_TRUE(maybe_reader);
	auto stdout = maybe_reader.value();

	maybe_reader = proc.GetAsyncStderrReader(loop);
	ASSERT_TRUE(maybe_reader);
	auto stderr = maybe_reader.value();

	int stdout_count = 0, stderr_count = 0;
	bool stdout_eof = false, stderr_eof = false, wait_finished = false;

	auto err = proc.Start();
	ASSERT_EQ(err, error::NoError);

	auto maybe_stop = [&]() {
		if (stdout_eof && stderr_eof && wait_finished) {
			loop.Stop();
		}
	};

	vector<uint8_t> recv_stdout;
	recv_stdout.resize(100);
	function<void(io::ExpectedSize result)> stdout_handler = [&](io::ExpectedSize result) {
		ASSERT_TRUE(result);
		if (result.value() > 0) {
			stdout_count++;

			string expected = "stdout " + to_string(stdout_count) + "\n";

			ASSERT_EQ(result.value(), expected.size());

			EXPECT_EQ(string(recv_stdout.begin(), recv_stdout.begin() + result.value()), expected);
			auto err = stdout->AsyncRead(recv_stdout.begin(), recv_stdout.end(), stdout_handler);
			ASSERT_EQ(err, error::NoError);
		} else {
			stdout_eof = true;
			maybe_stop();
		}
	};
	err = stdout->AsyncRead(recv_stdout.begin(), recv_stdout.end(), stdout_handler);
	ASSERT_EQ(err, error::NoError);

	vector<uint8_t> recv_stderr;
	recv_stderr.resize(100);
	function<void(io::ExpectedSize result)> stderr_handler = [&](io::ExpectedSize result) {
		ASSERT_EQ(err, error::NoError);
		if (result.value() > 0) {
			stderr_count++;

			string expected = "stderr " + to_string(stderr_count) + "\n";

			ASSERT_EQ(result.value(), expected.size());

			EXPECT_EQ(string(recv_stderr.begin(), recv_stderr.begin() + result.value()), expected);
			auto err = stderr->AsyncRead(recv_stderr.begin(), recv_stderr.end(), stderr_handler);
			ASSERT_EQ(err, error::NoError);
		} else {
			stderr_eof = true;
			maybe_stop();
		}
	};
	err = stderr->AsyncRead(recv_stderr.begin(), recv_stderr.end(), stderr_handler);
	ASSERT_EQ(err, error::NoError);

	err = proc.AsyncWait(loop, [&](error::Error err) {
		EXPECT_EQ(err, error::NoError);
		EXPECT_EQ(proc.GetExitStatus(), 0);
		wait_finished = true;
		maybe_stop();
	});
	ASSERT_EQ(err, error::NoError);

	loop.Run();

	EXPECT_EQ(stdout_count, 3);
	EXPECT_EQ(stderr_count, 3);
	EXPECT_TRUE(stdout_eof);
	EXPECT_TRUE(stderr_eof);
	EXPECT_TRUE(wait_finished);
}

TEST_F(ProcessesTests, AutomaticTermination) {
	mtesting::TestEventLoop loop;

	string script = R"(#!/bin/bash
sleep 10
exit 0
)";
	auto ret = PrepareTestScript(script);
	ASSERT_TRUE(ret);

	int pid;
	int result;
	{
		procs::Process proc({TestScriptPath()});

		auto err = proc.Start();
		ASSERT_EQ(err, error::NoError);

		pid = ProcessesTestsHelper::GetNativeProc(proc)->get_id();
		result = ::kill(pid, 0);
		ASSERT_EQ(result, 0);

		// Destroy proc.
	}

	result = ::kill(pid, 0);
	ASSERT_EQ(result, -1);
	int err = errno;
	// No such process.
	ASSERT_EQ(err, ESRCH);
}

TEST_F(ProcessesTests, DestroyProcessReaders) {
	mtesting::TestEventLoop loop;

	string script = R"(#!/bin/bash
# Output tons of garbage, just to occupy the pipe.
dd if=/dev/urandom bs=1M count=1
dd if=/dev/urandom bs=1M count=1 1>&2
exit 0
)";
	auto ret = PrepareTestScript(script);
	ASSERT_TRUE(ret);

	procs::Process proc({TestScriptPath()});

	{
		auto maybe_reader = proc.GetAsyncStdoutReader(loop);
		ASSERT_TRUE(maybe_reader);

		maybe_reader = proc.GetAsyncStderrReader(loop);
		ASSERT_TRUE(maybe_reader);

		// Get rid of both instead of using them. Make sure this doesn't block the process.
	}

	auto err = proc.AsyncWait(loop, [&](error::Error err) {
		EXPECT_EQ(err, error::NoError);
		EXPECT_EQ(proc.GetExitStatus(), 0);
		loop.Stop();
	});
	ASSERT_EQ(err, error::NoError);

	proc.Start();

	loop.Run();
}

TEST_F(ProcessesTests, CancelProducesOperationCanceledError) {
	mtesting::TestEventLoop loop;
	mtesting::TemporaryDirectory tmpdir;

	string script = R"(#!/bin/sh
sleep 10
exit 1
)";
	auto ret = PrepareTestScript(script);
	ASSERT_TRUE(ret);

	procs::Process proc({TestScriptPath()});
	auto err = proc.Start();
	ASSERT_EQ(err, error::NoError);

	bool hit_handler {false};
	err = proc.AsyncWait(loop, [&loop, &hit_handler](error::Error err) {
		EXPECT_EQ(err.code, make_error_condition(errc::operation_canceled));
		hit_handler = true;
		loop.Stop();
	});
	EXPECT_EQ(err, error::NoError);

	proc.Cancel();

	loop.Run();

	EXPECT_TRUE(hit_handler);
}
