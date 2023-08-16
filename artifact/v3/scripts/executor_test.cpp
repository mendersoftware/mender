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

#include <gtest/gtest.h>
#include <gmock/gmock.h>

#include <common/expected.hpp>
#include <common/error.hpp>
#include <common/path.hpp>
#include <common/testing.hpp>
#include <common/processes.hpp>
#include <common/optional.hpp>

#include <artifact/v3/scripts/executor.hpp>

using namespace std;

namespace error = mender::common::error;
namespace executor = mender::artifact::scripts::executor;
namespace expected = mender::common::expected;
namespace mtesting = mender::common::testing;
namespace optional = mender::common::optional;
namespace path = mender::common::path;
namespace processes = mender::common::processes;


class ArtifactScriptTestEnv : public testing::Test {
public:
	mtesting::TemporaryDirectory tmpdir;
	vector<char> mender_data_env_str;
	vector<char> mender_conf_env_str;

	void SetUp() override {
		tmpdir.CreateSubDirectory("scripts");
	}

	static void CreateScript(const string &script_path, const string &contents) {
		std::ofstream os(script_path, std::ios::out);
		os << contents;
		ASSERT_TRUE(os);
		ASSERT_EQ(chmod(script_path.c_str(), S_IRUSR | S_IWUSR | S_IXUSR), 0);
	}
};

TEST_F(ArtifactScriptTestEnv, VersionFileDoesNotExist_Success) {
	mtesting::TestEventLoop loop;
	executor::ScriptRunner runner {
		loop,
		executor::State::ArtifactInstall,
		executor::Action::Enter,
		chrono::seconds {10},
		path::Join(tmpdir.Path(), "scripts"),
		path::Join(tmpdir.Path(), "scripts")};
	auto handler_func = [](error::Error err) { EXPECT_EQ(err, error::NoError) << err.String(); };
	auto err = runner.AsyncRunScripts(handler_func);
	ASSERT_EQ(err, error::NoError) << err.String();
}

TEST_F(ArtifactScriptTestEnv, VersionFileHasWrongFormat_Error) {
	const string path {path::Join(tmpdir.Path(), "scripts", "version")};
	{
		ofstream version_file {path};
		ASSERT_TRUE(version_file);
		version_file << "foobar";
		ASSERT_TRUE(version_file);
	}
	mtesting::TestEventLoop loop;
	executor::ScriptRunner runner {
		loop,
		executor::State::ArtifactInstall,
		executor::Action::Enter,
		chrono::seconds {10},
		path::Join(tmpdir.Path(), "scripts"),
		path::Join(tmpdir.Path(), "scripts")};
	auto handler_func = [](error::Error err) { EXPECT_NE(err, error::NoError) << err.String(); };
	auto err = runner.AsyncRunScripts(handler_func);
	EXPECT_NE(err, error::NoError);
	EXPECT_EQ(err.code, executor::MakeError(executor::VersionFileError, "").code);
}

TEST_F(ArtifactScriptTestEnv, VersionFileIsCorrect_Success) {
	const string path {path::Join(tmpdir.Path(), "scripts", "version")};
	{
		ofstream version_file {path};
		ASSERT_TRUE(version_file);
		version_file << "3";
		ASSERT_TRUE(version_file);
	}
	mtesting::TestEventLoop loop;
	executor::ScriptRunner runner {
		loop,
		executor::State::ArtifactInstall,
		executor::Action::Enter,
		chrono::seconds {10},
		path::Join(tmpdir.Path(), "scripts"),
		path::Join(tmpdir.Path(), "scripts")};
	auto handler_func = [](error::Error err) {
		EXPECT_EQ(err, error::NoError) << "Received unexpected error: " + err.String();
	};
	auto err = runner.AsyncRunScripts(handler_func);
	ASSERT_EQ(err, error::NoError) << err.String();
}

TEST_F(ArtifactScriptTestEnv, TestRunArtifactInstall_Success) {
	CreateScript(
		path::Join(tmpdir.Path(), "scripts", "ArtifactInstall_Enter_01_test"),
		R"(#! /bin/sh
echo Executed ArtifactInstall_Enter_01-test
exit 0
)");
	CreateScript(
		path::Join(tmpdir.Path(), "scripts", "ArtifactInstall_Enter_02_test"),
		R"(#! /bin/sh
echo Executed ArtifactInstall_Enter_02-test
exit 0
)");

	mtesting::TestEventLoop loop;
	executor::ScriptRunner runner {
		loop,
		executor::State::ArtifactInstall,
		executor::Action::Enter,
		chrono::seconds {10},
		path::Join(tmpdir.Path(), "scripts"),
		path::Join(tmpdir.Path(), "scripts")};
	auto handler_func = [&loop](error::Error err) {
		EXPECT_EQ(err, error::NoError) << err.String();
		loop.Stop();
	};
	auto err = runner.AsyncRunScripts(handler_func);
	ASSERT_EQ(err, error::NoError) << err.String();

	loop.Run();
}

TEST_F(ArtifactScriptTestEnv, TestRunArtifactInstallExit1_Success) {
	CreateScript(
		path::Join(tmpdir.Path(), "scripts", "ArtifactInstall_Enter_01_test"),
		R"(#! /bin/sh
echo Executed ArtifactInstall_Enter_01-test
exit 0
)");
	CreateScript(
		path::Join(tmpdir.Path(), "scripts", "ArtifactInstall_Enter_02_test"),
		R"(#! /bin/sh
echo Executed ArtifactInstall_Enter_02-test
exit 1
)");

	mtesting::TestEventLoop loop;
	executor::ScriptRunner runner {
		loop,
		executor::State::ArtifactInstall,
		executor::Action::Enter,
		chrono::seconds {10},
		path::Join(tmpdir.Path(), "scripts"),
		path::Join(tmpdir.Path(), "scripts")};
	auto handler_func = [&loop](error::Error err) {
		EXPECT_NE(err, error::NoError) << err.String();
		EXPECT_EQ(err.code, executor::MakeError(executor::NonZeroExitStatusError, "").code)
			<< err.String();
		loop.Stop();
	};
	auto err = runner.AsyncRunScripts(handler_func);
	ASSERT_EQ(err, error::NoError) << err.String();

	loop.Run();
}

TEST_F(ArtifactScriptTestEnv, TestRunArtifactInstallVerifySorted_Success) {
	CreateScript(
		path::Join(tmpdir.Path(), "scripts", "ArtifactInstall_Enter_02_test"),
		R"(#! /bin/sh
echo Executed ArtifactInstall_Enter_02-test
exit 0
)");
	CreateScript(
		path::Join(tmpdir.Path(), "scripts", "ArtifactInstall_Enter_01_test"),
		R"(#! /bin/sh
echo Executed ArtifactInstall_Enter_01-test
exit 0
)");

	vector<string> stdout_collected {};
	auto stdout_script_collector = [&stdout_collected](const char *data, size_t size) {
		if (size == 0) {
			return;
		}
		string content(data, size);
		stdout_collected.push_back(data);
		return;
	};

	mtesting::TestEventLoop loop;
	executor::ScriptRunner runner {
		loop,
		executor::State::ArtifactInstall,
		executor::Action::Enter,
		chrono::seconds {10},
		path::Join(tmpdir.Path(), "scripts"),
		path::Join(tmpdir.Path(), "scripts"),
		stdout_script_collector};
	auto handler_func = [&loop, &stdout_collected](error::Error err) {
		EXPECT_EQ(err, error::NoError) << err.String();
		loop.Stop();
		// Verify the script order
		EXPECT_THAT(
			stdout_collected.at(0), testing::HasSubstr("Executed ArtifactInstall_Enter_01-test"));
		EXPECT_THAT(
			stdout_collected.at(1), testing::HasSubstr("Executed ArtifactInstall_Enter_02-test"));
	};
	auto err = runner.AsyncRunScripts(handler_func);
	ASSERT_EQ(err, error::NoError) << err.String();

	loop.Run();
}


TEST_F(ArtifactScriptTestEnv, TestRunRootfsScripts_Success) {
	CreateScript(
		path::Join(tmpdir.Path(), "scripts", "Download_Enter_02_test"),
		R"(#! /bin/sh
echo Executed Download_Enter_02-test
exit 0
)");
	CreateScript(
		path::Join(tmpdir.Path(), "scripts", "Download_Enter_01_test"),
		R"(#! /bin/sh
echo Executed Download_Enter_01-test
exit 0
)");

	vector<string> stdout_collected {};
	auto stdout_script_collector = [&stdout_collected](const char *data, size_t size) {
		if (size == 0) {
			return;
		}
		string content(data, size);
		stdout_collected.push_back(data);
		return;
	};

	mtesting::TestEventLoop loop;
	executor::ScriptRunner runner {
		loop,
		executor::State::Download,
		executor::Action::Enter,
		chrono::seconds {10},
		path::Join(tmpdir.Path(), "scripts"),
		path::Join(tmpdir.Path(), "scripts"),
		stdout_script_collector};
	auto handler_func = [&loop, &stdout_collected](error::Error err) {
		EXPECT_EQ(err, error::NoError) << err.String();
		loop.Stop();
		// Verify the script order
		EXPECT_THAT(stdout_collected.at(0), testing::HasSubstr("Executed Download_Enter_01-test"));
		EXPECT_THAT(stdout_collected.at(1), testing::HasSubstr("Executed Download_Enter_02-test"));
	};
	auto err = runner.AsyncRunScripts(handler_func);
	ASSERT_EQ(err, error::NoError) << err.String();

	loop.Run();
}

TEST_F(ArtifactScriptTestEnv, TestRunErrorScripts_Success) {
	CreateScript(
		path::Join(tmpdir.Path(), "scripts", "Download_Error_01_test"),
		R"(#! /bin/sh
echo Executed Download_Error_01-test
exit 1
)");
	CreateScript(
		path::Join(tmpdir.Path(), "scripts", "Download_Error_02_test"),
		R"(#! /bin/sh
echo Executed Download_Error_02-test
exit 2
)");

	vector<string> stdout_collected {};
	auto stdout_script_collector = [&stdout_collected](const char *data, size_t size) {
		if (size == 0) {
			return;
		}
		string content(data, size);
		stdout_collected.push_back(data);
		return;
	};

	mtesting::TestEventLoop loop;
	executor::ScriptRunner runner {
		loop,
		executor::State::Download,
		executor::Action::Error,
		chrono::seconds {10},
		path::Join(tmpdir.Path(), "scripts"),
		path::Join(tmpdir.Path(), "scripts"),
		stdout_script_collector};
	auto handler_func = [&loop, &stdout_collected](error::Error err) {
		EXPECT_NE(err, error::NoError);
		EXPECT_EQ(err.code, executor::MakeError(executor::NonZeroExitStatusError, "").code)
			<< err.String();
		loop.Stop();
		// Verify the script order
		EXPECT_THAT(stdout_collected.at(0), testing::HasSubstr("Executed Download_Error_01-test"));
		EXPECT_THAT(stdout_collected.at(1), testing::HasSubstr("Executed Download_Error_02-test"));
	};
	auto err = runner.AsyncRunScripts(handler_func);
	ASSERT_EQ(err, error::NoError) << err.String();

	loop.Run();
}
