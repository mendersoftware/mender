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

#include <mender-update/update_module/v3/update_module.hpp>

#include <sys/stat.h>

#include <algorithm>
#include <string>
#include <sstream>

#include <gtest/gtest.h>
#include <gmock/gmock.h>

#include <common/common.hpp>
#include <common/events.hpp>
#include <common/path.hpp>
#include <common/key_value_database_lmdb.hpp>
#include <common/testing.hpp>
#include <common/processes.hpp>
#include <common/error.hpp>
#include <common/path.hpp>

#include <mender-update/context.hpp>

namespace io = mender::common::io;
namespace error = mender::common::error;
namespace events = mender::common::events;
namespace expected = mender::common::expected;
namespace common = mender::common;
namespace conf = mender::common::conf;
namespace context = mender::update::context;
namespace update_module = mender::update::update_module::v3;
namespace path = mender::common::path;
namespace json = mender::common::json;
namespace conf = mender::common::conf;
namespace error = mender::common::error;
namespace path = mender::common::path;

namespace processes = mender::common::processes;


using namespace std;
using namespace mender::common::testing;

class UpdateModuleTests : public testing::Test {
public:
	TemporaryDirectory temp_dir_;
	string test_scripts_dir_;
	string work_dir_;

	void SetUp() override {
		// mender::common::log::SetLevel(mender::common::log::LogLevel::Debug);

		test_scripts_dir_ = path::Join(temp_dir_.Path(), "modules");
		ASSERT_EQ(mkdir(test_scripts_dir_.c_str(), 0700), 0);
		test_scripts_dir_ = path::Join(test_scripts_dir_, "v3");
		ASSERT_EQ(mkdir(test_scripts_dir_.c_str(), 0700), 0);

		work_dir_ = path::Join(temp_dir_.Path(), "work");
		ASSERT_EQ(mkdir(work_dir_.c_str(), 0700), 0);
	}

	bool PrepareTestFile(const string &name, bool executable, string script = "") {
		string test_file_path {path::Join(test_scripts_dir_, name)};
		ofstream os(test_file_path);
		os << script;
		os.close();

		if (executable) {
			int ret = chmod(test_file_path.c_str(), 0700);
			return ret == 0;
		}
		return true;
	}

	expected::ExpectedString PrepareUpdateModuleScript(
		update_module::UpdateModule &update_module, string content = "") {
		string name = "update-module";
		if (!PrepareTestFile(name, true, content)) {
			int err = errno;
			return expected::unexpected(error::Error(
				generic_category().default_error_condition(err),
				"Cannot create update module script"));
		}

		update_module.SetUpdateModulePath(GetUpdateModulePath());
		update_module.SetUpdateModuleWorkDir(GetUpdateModuleWorkDir());

		return GetUpdateModulePath();
	}

	expected::ExpectedString PrepareArtifact(size_t mb = 1, size_t number_of_files = 1) {
		auto rootfs = path::Join(temp_dir_.Path(), "rootfs");
		{
			processes::Process proc(
				{"dd", "if=/dev/urandom", "of=" + rootfs, "bs=1M", "count=" + to_string(mb)});
			auto err = proc.Run();
			if (err != error::NoError) {
				return expected::unexpected(err);
			}
		}

		auto file = path::Join(temp_dir_.Path(), "artifact.mender");
		vector<string> args {
			"mender-artifact",
			"write",
			"module-image",
			"-T",
			"rootfs-image-v2",
			"-o",
			file,
			"-n",
			"test",
			"-t",
			"test",
			"-f",
			rootfs};
		for (size_t index = 1; index < number_of_files; index++) {
			auto extra_rootfs = rootfs + to_string(index + 1);
			processes::Process proc({"cp", rootfs, extra_rootfs});
			auto err = proc.Run();
			if (err != error::NoError) {
				return expected::unexpected(err);
			}

			args.push_back("-f");
			args.push_back(extra_rootfs);
		}
		{
			processes::Process proc(args);
			auto err = proc.Run();
			if (err != error::NoError) {
				return expected::unexpected(err);
			}
		}

		{
			processes::Process proc({"mender-artifact", "read", file});
			auto err = proc.Run();
			if (err != error::NoError) {
				return expected::unexpected(err);
			}
		}
		return file;
	}

	string GetTestScriptDir() {
		return test_scripts_dir_;
	}

	string GetUpdateModulePath() {
		return path::Join(test_scripts_dir_, "update-module");
	}

	string GetUpdateModuleWorkDir() {
		return path::Join(temp_dir_.Path(), "work");
	}
};

class UpdateModuleTestWithDefaultArtifact {
public:
	UpdateModuleTestWithDefaultArtifact(
		UpdateModuleTests &tests, size_t mb = 1, size_t number_of_files = 1) {
		// ASSERT doesn't work well inside constructors because of some peculiar return
		// semantics, so wrap it in a lambda.
		[&]() {
			auto maybe_artifact = tests.PrepareArtifact(mb, number_of_files);
			ASSERT_TRUE(maybe_artifact) << maybe_artifact.error();
			auto artifact_file = maybe_artifact.value();

			is = make_unique<ifstream>(artifact_file);
			ASSERT_TRUE(is->good());
			artifact_reader = make_unique<io::StreamReader>(*is);

			ctx = make_unique<context::MenderContext>(config);

			auto maybe_parsed = mender::artifact::parser::Parse(*artifact_reader);
			ASSERT_TRUE(maybe_parsed) << maybe_parsed.error();
			artifact = make_unique<mender::artifact::Artifact>(maybe_parsed.value());

			auto maybe_payload = artifact->Next();
			ASSERT_TRUE(maybe_payload) << maybe_payload.error();
			payload = make_unique<mender::artifact::Payload>(maybe_payload.value());

			auto maybe_payload_meta_data = mender::artifact::View(*artifact, 0);
			ASSERT_TRUE(maybe_payload_meta_data) << maybe_payload_meta_data.error();
			payload_meta_data =
				make_unique<mender::artifact::PayloadHeaderView>(maybe_payload_meta_data.value());

			update_module = make_unique<update_module::UpdateModule>(
				*ctx, payload_meta_data->header.payload_type);
		}();
	}

	unique_ptr<ifstream> is;
	unique_ptr<io::StreamReader> artifact_reader;
	conf::MenderConfig config;
	unique_ptr<context::MenderContext> ctx;
	unique_ptr<mender::artifact::Artifact> artifact;
	unique_ptr<mender::artifact::Payload> payload;
	unique_ptr<mender::artifact::PayloadHeaderView> payload_meta_data;
	unique_ptr<update_module::UpdateModule> update_module;
};

TEST_F(UpdateModuleTests, DiscoverUpdateModulesTest) {
	auto ok = PrepareTestFile("file1", false);
	ASSERT_TRUE(ok);

	ok = PrepareTestFile("script1", true);
	ASSERT_TRUE(ok);

	ok = PrepareTestFile("file2", false);
	ASSERT_TRUE(ok);

	ok = PrepareTestFile("script2", true);
	ASSERT_TRUE(ok);

	auto cfg = conf::MenderConfig();
	cfg.paths.SetDataStore(temp_dir_.Path());

	auto ex_modules = update_module::DiscoverUpdateModules(cfg);
	ASSERT_TRUE(ex_modules);
	auto modules = ex_modules.value();
	EXPECT_EQ(modules.size(), 2);
	EXPECT_EQ(count(modules.cbegin(), modules.cend(), path::Join(test_scripts_dir_, "script1")), 1);
	EXPECT_EQ(count(modules.cbegin(), modules.cend(), path::Join(test_scripts_dir_, "script2")), 1);
}

TEST_F(UpdateModuleTests, DiscoverUpdateModulesNoExistTest) {
	auto cfg = conf::MenderConfig();
	cfg.paths.SetDataStore(temp_dir_.Path());

	auto ex_modules = update_module::DiscoverUpdateModules(cfg);
	ASSERT_TRUE(ex_modules);

	auto modules = ex_modules.value();
	EXPECT_EQ(modules.size(), 0);
}

TEST_F(UpdateModuleTests, DiscoverUpdateModulesEmptyDirTest) {
	auto cfg = conf::MenderConfig();
	cfg.paths.SetDataStore(temp_dir_.Path());

	auto ex_modules = update_module::DiscoverUpdateModules(cfg);
	ASSERT_TRUE(ex_modules);

	auto modules = ex_modules.value();
	EXPECT_EQ(modules.size(), 0);
}

TEST_F(UpdateModuleTests, DiscoverUpdateModulesNoExecutablesTest) {
	auto ok = PrepareTestFile("file1", false);
	ASSERT_TRUE(ok);

	ok = PrepareTestFile("file2", false);
	ASSERT_TRUE(ok);

	auto cfg = conf::MenderConfig();
	cfg.paths.SetDataStore(temp_dir_.Path());

	auto ex_modules = update_module::DiscoverUpdateModules(cfg);
	ASSERT_TRUE(ex_modules);
	auto modules = ex_modules.value();
	EXPECT_EQ(modules.size(), 0);
}

class UpdateModuleFileTreeTests : public testing::Test {
public:
	void SetUp() override {
		this->cfg.paths.SetDataStore(test_state_dir.Path());

		this->ctx = make_shared<context::MenderContext>(cfg);
		auto err = ctx->Initialize();
		ASSERT_EQ(err, error::NoError);

		auto &db = ctx->GetMenderStoreDB();
		err = db.Write(
			"artifact-name", common::ByteVectorFromString("artifact-name existing-artifact-name"));
		ASSERT_EQ(err, error::NoError);
		err = db.Write(
			"artifact-group",
			common::ByteVectorFromString("artifact-group existing-artifact-group"));
		ASSERT_EQ(err, error::NoError);

		ofstream os(path::Join(cfg.paths.GetDataStore(), "device_type"));
		ASSERT_TRUE(os);
		os << "device_type=Some device type" << endl;
		os.close();

		ASSERT_TRUE(CreateArtifact());
		std::fstream fs {path::Join(temp_dir.Path(), "artifact.mender")};
		io::StreamReader sr {fs};
		auto expected_artifact = mender::artifact::Parse(sr);
		ASSERT_TRUE(expected_artifact);
		auto artifact = make_shared<mender::artifact::Artifact>(expected_artifact.value());

		auto expected_payload_header = mender::artifact::View(*artifact, 0);
		ASSERT_TRUE(expected_payload_header) << expected_payload_header.error().message;

		this->update_payload_header = make_shared<mender::artifact::PayloadHeaderView>(
			mender::artifact::View(*artifact, 0).value());
	}

	bool CreateArtifact() {
		string script = R"(#! /bin/sh

DIRNAME=$(dirname $0)

# Create small tar file
echo foobar > ${DIRNAME}/testdata
mender-artifact \
    --compression none \
    write rootfs-image \
    --no-progress \
    -t test-device \
    -n test-artifact \
    -f ${DIRNAME}/testdata \
    -o ${DIRNAME}/artifact.mender || exit 1

exit 0
		)";

		const string script_fname = path::Join(temp_dir.Path(), "test-script.sh");

		std::ofstream os(script_fname.c_str(), std::ios::out);
		os << script;
		os.close();

		int ret = chmod(script_fname.c_str(), S_IRUSR | S_IWUSR | S_IXUSR);
		if (ret != 0) {
			return ret;
		}

		processes::Process proc({script_fname});
		auto ex_line_data = proc.GenerateLineData();
		if (!ex_line_data) {
			return false;
		}
		EXPECT_EQ(proc.GetExitStatus(), 0) << "error message: " + ex_line_data.error().message;
		return true;
	}

protected:
	TemporaryDirectory test_state_dir;
	TemporaryDirectory test_tree_dir;
	TemporaryDirectory temp_dir;

	conf::MenderConfig cfg {};
	shared_ptr<context::MenderContext> ctx;
	shared_ptr<mender::artifact::PayloadHeaderView> update_payload_header;
};

TEST_F(UpdateModuleFileTreeTests, FileTreeTestHeader) {
	update_module::UpdateModule up_mod(*ctx, update_payload_header->header.payload_type);
	const string tree_path = test_tree_dir.Path();
	auto err = up_mod.CleanAndPrepareFileTree(tree_path, *update_payload_header);
	ASSERT_EQ(err, error::NoError);

	//
	// Current device contents
	//

	EXPECT_TRUE(FileContainsExactly(path::Join(tree_path, "version"), "3\n"));

	EXPECT_TRUE(FileContainsExactly(
		path::Join(tree_path, "current_artifact_name"), "artifact-name existing-artifact-name\n"));

	EXPECT_TRUE(FileContainsExactly(
		path::Join(tree_path, "current_artifact_group"),
		"artifact-group existing-artifact-group\n"));

	EXPECT_TRUE(
		FileContainsExactly(path::Join(tree_path, "current_device_type"), "Some device type\n"));

	//
	// Header contents (From the Artifact)
	//

	EXPECT_TRUE(FileContainsExactly(path::Join(tree_path, "header", "artifact_group"), ""));

	EXPECT_TRUE(
		FileContainsExactly(path::Join(tree_path, "header", "artifact_name"), "test-artifact"));

	EXPECT_TRUE(
		FileContainsExactly(path::Join(tree_path, "header", "payload_type"), "rootfs-image"));

	string expected_header_info = R"(
	{
	  "artifact_depends": {
	    "device_type": [
	      "test-device"
	    ]
	  },
	  "artifact_provides": {
	    "artifact_name": "test-artifact"
	  },
	  "payloads": [
	    {
	      "type": "rootfs-image"
	    }
	  ]
	}
	)";
	EXPECT_TRUE(
		FileJsonEquals(path::Join(tree_path, "header", "header-info"), expected_header_info));


	string expected_type_info = R"(
	{
	  "artifact_provides": {
	    "rootfs-image.checksum":
	    "aec070645fe53ee3b3763059376134f058cc337247c978add178b6ccdfb0019f",
	    "rootfs-image.version": "test-artifact"
	  },
	  "clears_artifact_provides": [
	    "artifact_group",
	    "rootfs_image_checksum",
	    "rootfs-image.*"
	  ],
	  "type": ""
	})";
	EXPECT_TRUE(FileJsonEquals(path::Join(tree_path, "header", "type-info"), expected_type_info));

	EXPECT_TRUE(FileContainsExactly(path::Join(tree_path, "header", "meta-data"), "null"));

	err = up_mod.DeleteFileTree(tree_path);
	ASSERT_EQ(err, error::NoError);
}

TEST_F(UpdateModuleTests, CallProvidePayloadFileSizes) {
	UpdateModuleTestWithDefaultArtifact update_module_test(*this);
	ASSERT_FALSE(HasFailure());

	// State: ProvidePayloadFileSizes: Yes
	string script = R"(#!/bin/sh
if [ $1 = "ProvidePayloadFileSizes" ]; then
	echo "Yes"
	exit 0
fi
exit 1
)";

	auto ok = PrepareUpdateModuleScript(*update_module_test.update_module, script);
	ASSERT_TRUE(ok);

	auto ret = update_module_test.update_module->ProvidePayloadFileSizes();
	ASSERT_TRUE(ret.has_value()) << ret.error();
	ASSERT_TRUE(ret.value());

	// State: ProvidePayloadFileSizes: No
	script = R"(#!/bin/sh
if [ $1 = "ProvidePayloadFileSizes" ]; then
	echo "No"
	exit 0
fi
exit 1
)";

	ok = PrepareUpdateModuleScript(*update_module_test.update_module, script);
	ASSERT_TRUE(ok);

	ret = update_module_test.update_module->ProvidePayloadFileSizes();
	ASSERT_TRUE(ret.has_value()) << ret.error();
	ASSERT_FALSE(ret.value());

	// State: ProvidePayloadFileSizes: no reply
	script = R"(#!/bin/sh
exit 0
)";

	ok = PrepareUpdateModuleScript(*update_module_test.update_module, script);
	ASSERT_TRUE(ok);

	ret = update_module_test.update_module->ProvidePayloadFileSizes();
	ASSERT_TRUE(ret.has_value()) << ret.error();
	ASSERT_FALSE(ret.value());

	// State: ProvidePayloadFileSizes: Bogus
	script = R"(#!/bin/sh
if [ $1 = "ProvidePayloadFileSizes" ]; then
	echo "I don't know how to use Update Modules"
	exit 0
fi
exit 1
)";

	ok = PrepareUpdateModuleScript(*update_module_test.update_module, script);
	ASSERT_TRUE(ok);

	ret = update_module_test.update_module->ProvidePayloadFileSizes();
	ASSERT_FALSE(ret.has_value()) << ret.error();
	ASSERT_EQ(ret.error().code, make_error_condition(errc::protocol_error));

	// State: ProvidePayloadFileSizes: Valid, but with trailing garbage
	script = R"(#!/bin/sh
if [ $1 = "ProvidePayloadFileSizes" ]; then
	echo "Yes"
	echo "Should not be here"
	exit 0
fi
exit 1
)";

	ok = PrepareUpdateModuleScript(*update_module_test.update_module, script);
	ASSERT_TRUE(ok);

	ret = update_module_test.update_module->ProvidePayloadFileSizes();
	ASSERT_FALSE(ret.has_value()) << ret.error();
	ASSERT_EQ(ret.error().code, make_error_condition(errc::protocol_error));
}

TEST_F(UpdateModuleTests, DownloadProcessFailsImmediately) {
	UpdateModuleTestWithDefaultArtifact art(*this);

	auto maybe_script = PrepareUpdateModuleScript(*art.update_module);
	ASSERT_TRUE(maybe_script) << maybe_script.error();
	auto script_path = maybe_script.value();
	{
		ofstream um_script(script_path);
		um_script << R"(#!/bin/bash
exit 2
)";
	}

	auto err = art.update_module->Download(*art.payload);
	EXPECT_NE(err, error::NoError) << err.String();
	EXPECT_EQ(err.code, processes::MakeError(processes::NonZeroExitStatusError, "").code);
	EXPECT_THAT(err.String(), testing::HasSubstr(" 2"));
}

TEST_F(UpdateModuleTests, DownloadProcess) {
	UpdateModuleTestWithDefaultArtifact art(*this);

	auto maybe_script = PrepareUpdateModuleScript(*art.update_module);
	ASSERT_TRUE(maybe_script) << maybe_script.error();
	auto script_path = maybe_script.value();
	{
		ofstream um_script(script_path);
		um_script << R"delim(#!/bin/bash
set -e
echo "Update Module called"
test "$1" = "Download"
file="$(cat stream-next)"
echo "Got file $file"
test "$file" = "streams/rootfs"
cat "$file" > payload
file="$(cat stream-next)"
test "$file" = ""
)delim";
	}

	auto err = art.update_module->Download(*art.payload);
	EXPECT_EQ(err, error::NoError) << err.String();
	EXPECT_TRUE(
		FilesEqual(path::Join(work_dir_, "payload"), path::Join(temp_dir_.Path(), "rootfs")));
}

TEST_F(UpdateModuleTests, DownloadProcessDiesMidway) {
	UpdateModuleTestWithDefaultArtifact art(*this);

	auto maybe_script = PrepareUpdateModuleScript(*art.update_module);
	ASSERT_TRUE(maybe_script) << maybe_script.error();
	auto script_path = maybe_script.value();
	{
		ofstream um_script(script_path);
		um_script << R"delim(#!/bin/bash
set -e
echo "Update Module called"
test "$1" = "Download"
file="$(cat stream-next)"
echo "Got file $file"
test "$file" = "streams/rootfs"
dd if="$file" of=payload bs=1048576 bs=123456 count=1
)delim";
	}

	auto err = art.update_module->Download(*art.payload);
	EXPECT_NE(err, error::NoError) << err.String();
	EXPECT_EQ(err.code, make_error_condition(errc::broken_pipe)) << err.String();
}

TEST_F(UpdateModuleTests, DownloadProcessDoesntOpenStream) {
	UpdateModuleTestWithDefaultArtifact art(*this);

	auto maybe_script = PrepareUpdateModuleScript(*art.update_module);
	ASSERT_TRUE(maybe_script) << maybe_script.error();
	auto script_path = maybe_script.value();
	{
		ofstream um_script(script_path);
		um_script << R"delim(#!/bin/bash
set -e
echo "Update Module called"
test "$1" = "Download"
file="$(cat stream-next)"
echo "Got file $file"
test "$file" = "streams/rootfs"
)delim";
	}

	auto err = art.update_module->Download(*art.payload);
	EXPECT_NE(err, error::NoError) << err.String();
	EXPECT_EQ(err.code, make_error_condition(errc::broken_pipe)) << err.String();
}

TEST_F(UpdateModuleTests, DownloadProcessOpensStreamNextButDoesntRead) {
	UpdateModuleTestWithDefaultArtifact art(*this);

	auto maybe_script = PrepareUpdateModuleScript(*art.update_module);
	ASSERT_TRUE(maybe_script) << maybe_script.error();
	auto script_path = maybe_script.value();
	{
		ofstream um_script(script_path);
		um_script << R"delim(#!/bin/bash
set -e
echo "Update Module called"
test "$1" = "Download"
dd if=stream-next count=0
)delim";
	}

	auto err = art.update_module->Download(*art.payload);
	EXPECT_NE(err, error::NoError) << err.String();
	EXPECT_EQ(err.code, make_error_condition(errc::broken_pipe)) << err.String();
}

TEST_F(UpdateModuleTests, DownloadProcessCrashesAfterStreamNext) {
	UpdateModuleTestWithDefaultArtifact art(*this);

	auto maybe_script = PrepareUpdateModuleScript(*art.update_module);
	ASSERT_TRUE(maybe_script) << maybe_script.error();
	auto script_path = maybe_script.value();
	{
		ofstream um_script(script_path);
		um_script << R"delim(#!/bin/bash
set -e
echo "Update Module called"
test "$1" = "Download"
file="$(cat stream-next)"
echo "Got file $file"
test "$file" = "streams/rootfs"
exit 2
)delim";
	}

	auto err = art.update_module->Download(*art.payload);
	EXPECT_NE(err, error::NoError) << err.String();
	EXPECT_EQ(err.code, processes::MakeError(processes::NonZeroExitStatusError, "").code)
		<< err.String();
}

TEST_F(UpdateModuleTests, DownloadProcessReadsEverythingExceptLastEntry) {
	UpdateModuleTestWithDefaultArtifact art(*this);

	auto maybe_script = PrepareUpdateModuleScript(*art.update_module);
	ASSERT_TRUE(maybe_script) << maybe_script.error();
	auto script_path = maybe_script.value();
	{
		ofstream um_script(script_path);
		um_script << R"delim(#!/bin/bash
set -e
echo "Update Module called"
test "$1" = "Download"
file="$(cat stream-next)"
echo "Got file $file"
test "$file" = "streams/rootfs"
cat "$file" > payload
)delim";
	}

	auto err = art.update_module->Download(*art.payload);
	EXPECT_NE(err, error::NoError) << err.String();
	EXPECT_EQ(err.code, make_error_condition(errc::broken_pipe)) << err.String();
}

TEST_F(UpdateModuleTests, DownloadProcessTwoFiles) {
	UpdateModuleTestWithDefaultArtifact art(*this, 1, 2);
	ASSERT_FALSE(HasFailure());

	auto maybe_script = PrepareUpdateModuleScript(*art.update_module);
	ASSERT_TRUE(maybe_script) << maybe_script.error();
	auto script_path = maybe_script.value();
	{
		ofstream um_script(script_path);
		um_script << R"delim(#!/bin/bash
set -e
echo "Update Module called"
test "$1" = "Download"

file="$(cat stream-next)"
echo "Got file $file"
test "$file" = "streams/rootfs"
cat "$file" > payload1

file="$(cat stream-next)"
echo "Got file $file"
test "$file" = "streams/rootfs2"
cat "$file" > payload2

file="$(cat stream-next)"
test "$file" = ""
)delim";
	}

	auto err = art.update_module->Download(*art.payload);
	EXPECT_EQ(err, error::NoError) << err.String();
	EXPECT_TRUE(
		FilesEqual(path::Join(work_dir_, "payload1"), path::Join(temp_dir_.Path(), "rootfs")));
	EXPECT_TRUE(
		FilesEqual(path::Join(work_dir_, "payload2"), path::Join(temp_dir_.Path(), "rootfs2")));
}

TEST_F(UpdateModuleTests, DownloadProcessStoreFiles) {
	UpdateModuleTestWithDefaultArtifact art(*this);

	auto maybe_script = PrepareUpdateModuleScript(*art.update_module);
	ASSERT_TRUE(maybe_script) << maybe_script.error();
	auto script_path = maybe_script.value();
	{
		ofstream um_script(script_path);
		um_script << R"delim(#!/bin/bash
exit 0
)delim";
	}

	auto err = art.update_module->Download(*art.payload);
	EXPECT_EQ(err, error::NoError) << err.String();
	EXPECT_TRUE(
		FilesEqual(path::Join(temp_dir_.Path(), "rootfs"), path::Join(work_dir_, "files/rootfs")));
}

TEST_F(UpdateModuleTests, DownloadProcessStoreTwoFiles) {
	UpdateModuleTestWithDefaultArtifact art(*this, 1, 2);

	auto maybe_script = PrepareUpdateModuleScript(*art.update_module);
	ASSERT_TRUE(maybe_script) << maybe_script.error();
	auto script_path = maybe_script.value();
	{
		ofstream um_script(script_path);
		um_script << R"delim(#!/bin/bash
exit 0
)delim";
	}

	auto err = art.update_module->Download(*art.payload);
	EXPECT_EQ(err, error::NoError) << err.String();
	EXPECT_TRUE(
		FilesEqual(path::Join(temp_dir_.Path(), "rootfs"), path::Join(work_dir_, "files/rootfs")));
	EXPECT_TRUE(
		FilesEqual(path::Join(temp_dir_.Path(), "rootfs"), path::Join(work_dir_, "files/rootfs2")));
}

TEST_F(UpdateModuleTests, DownloadProcessStoreFilesFailure) {
	// Make sure we get a sensible failure if storing a file failed. Running out of space is
	// more likely than the error we make here (directory blocks the path), but we still test
	// the error path.

	UpdateModuleTestWithDefaultArtifact art(*this);

	auto maybe_script = PrepareUpdateModuleScript(*art.update_module);
	ASSERT_TRUE(maybe_script) << maybe_script.error();
	auto script_path = maybe_script.value();
	{
		ofstream um_script(script_path);
		um_script << R"delim(#!/bin/bash
set -e
mkdir -p files/rootfs
exit 0
)delim";
	}

	auto err = art.update_module->Download(*art.payload);
	EXPECT_NE(err, error::NoError) << err.String();
	EXPECT_EQ(err.code, make_error_condition(errc::is_a_directory)) << err.String();
}

TEST_F(UpdateModuleTests, DownloadProcessTimesOut) {
	UpdateModuleTestWithDefaultArtifact art(*this);

	auto maybe_script = PrepareUpdateModuleScript(*art.update_module);
	ASSERT_TRUE(maybe_script) << maybe_script.error();
	auto script_path = maybe_script.value();
	{
		ofstream um_script(script_path);
		um_script << R"delim(#!/bin/bash
set -e
echo "Update Module called"
test "$1" = "Download"
file="$(cat stream-next)"
echo "Got file $file"
test "$file" = "streams/rootfs"
sleep 2
)delim";
	}

	// Set only 1 second timeout.
	art.config.module_timeout_seconds = 1;

	auto err = art.update_module->Download(*art.payload);
	EXPECT_NE(err, error::NoError) << err.String();
	EXPECT_EQ(err.code, make_error_condition(errc::timed_out)) << err.String();
}

TEST_F(UpdateModuleTests, CallArtifactInstall) {
	UpdateModuleTestWithDefaultArtifact update_module_test(*this);
	ASSERT_FALSE(HasFailure());

	// State: ArtifactInstall
	string installScript = R"(#!/bin/sh
echo "Called Update Module with" "$@"
if [ $1 = "ArtifactInstall" ]; then
	exit 0
fi
exit 1
)";

	auto ok = PrepareUpdateModuleScript(*update_module_test.update_module, installScript);
	ASSERT_TRUE(ok);

	auto ret = update_module_test.update_module->ArtifactInstall();
	ASSERT_EQ(error::NoError, ret);
}

TEST_F(UpdateModuleTests, DownloadWithFileSizesProcess) {
	UpdateModuleTestWithDefaultArtifact art(*this);

	auto maybe_script = PrepareUpdateModuleScript(*art.update_module);
	ASSERT_TRUE(maybe_script) << maybe_script.error();
	auto script_path = maybe_script.value();
	{
		ofstream um_script(script_path);
		um_script << R"delim(#!/bin/bash
set -e
echo "Update Module called"
test "$1" = "DownloadWithFileSizes"
line="$(cat stream-next)"
echo "Got line $line"
test "$line" = "streams/rootfs 1048576"
file="$(echo $line | cut -d' ' -f1)"
size="$(echo $line | cut -d' ' -f2)"
echo "Parsed: file $file, size $size"
test "$file" = "streams/rootfs"
test "$size" = "1048576"
cat "$file" > payload
line="$(cat stream-next)"
test "$line" = ""
)delim";
	}

	auto err = art.update_module->DownloadWithFileSizes(*art.payload);
	EXPECT_EQ(err, error::NoError) << err.String();
	EXPECT_TRUE(
		FilesEqual(path::Join(work_dir_, "payload"), path::Join(temp_dir_.Path(), "rootfs")));
}

TEST_F(UpdateModuleTests, CallArtifactReboot) {
	UpdateModuleTestWithDefaultArtifact update_module_test(*this);
	ASSERT_FALSE(HasFailure());

	string rebootScript = R"(#!/bin/sh
echo "Called Update Module with" "$@"
if [ $1 = "ArtifactReboot" ]; then
	exit 0
fi
exit 1
)";

	auto ok = PrepareUpdateModuleScript(*update_module_test.update_module, rebootScript);
	ASSERT_TRUE(ok);

	auto ret = update_module_test.update_module->ArtifactReboot();
	ASSERT_EQ(error::NoError, ret);
}

TEST_F(UpdateModuleTests, CallArtifactCommit) {
	UpdateModuleTestWithDefaultArtifact update_module_test(*this);
	ASSERT_FALSE(HasFailure());

	string commitScript = R"(#!/bin/sh
echo "Called Update Module with" "$@"
if [ $1 = "ArtifactCommit" ]; then
	exit 0
fi
exit 1
)";

	auto ok = PrepareUpdateModuleScript(*update_module_test.update_module, commitScript);
	ASSERT_TRUE(ok);

	auto ret = update_module_test.update_module->ArtifactCommit();
	ASSERT_EQ(error::NoError, ret);
}

TEST_F(UpdateModuleTests, CallArtifactRollback) {
	UpdateModuleTestWithDefaultArtifact update_module_test(*this);
	ASSERT_FALSE(HasFailure());

	string rollbackScript = R"(#!/bin/sh
echo "Called Update Module with" "$@"
if [ $1 = "ArtifactRollback" ]; then
	exit 0
fi
exit 1
)";

	auto ok = PrepareUpdateModuleScript(*update_module_test.update_module, rollbackScript);
	ASSERT_TRUE(ok);

	auto ret = update_module_test.update_module->ArtifactRollback();
	ASSERT_EQ(error::NoError, ret);
}

TEST_F(UpdateModuleTests, CallArtifactVerifyReboot) {
	UpdateModuleTestWithDefaultArtifact update_module_test(*this);
	ASSERT_FALSE(HasFailure());

	string verifyReboot = R"(#!/bin/sh
echo "Called Update Module with" "$@"
if [ $1 = "ArtifactVerifyReboot" ]; then
	exit 0
fi
exit 1
)";

	auto ok = PrepareUpdateModuleScript(*update_module_test.update_module, verifyReboot);
	ASSERT_TRUE(ok);

	auto ret = update_module_test.update_module->ArtifactVerifyReboot();
	ASSERT_EQ(error::NoError, ret);
}

TEST_F(UpdateModuleTests, CallArtifactRollbackReboot) {
	UpdateModuleTestWithDefaultArtifact update_module_test(*this);
	ASSERT_FALSE(HasFailure());

	string rollbackReboot = R"(#!/bin/sh
echo "Called Update Module with" "$@"
if [ $1 = "ArtifactRollbackReboot" ]; then
	exit 0
fi
exit 1
)";

	auto ok = PrepareUpdateModuleScript(*update_module_test.update_module, rollbackReboot);
	ASSERT_TRUE(ok);

	auto ret = update_module_test.update_module->ArtifactRollbackReboot();
	ASSERT_EQ(error::NoError, ret);
}

TEST_F(UpdateModuleTests, CallArtifactVerifyRollbackReboot) {
	UpdateModuleTestWithDefaultArtifact update_module_test(*this);
	ASSERT_FALSE(HasFailure());

	string verifyRollbackReboot = R"(#!/bin/sh
echo "Called Update Module with" "$@"
if [ $1 = "ArtifactVerifyRollbackReboot" ]; then
	exit 0
fi
exit 1
)";

	auto ok = PrepareUpdateModuleScript(*update_module_test.update_module, verifyRollbackReboot);
	ASSERT_TRUE(ok);

	auto ret = update_module_test.update_module->ArtifactVerifyRollbackReboot();
	ASSERT_EQ(error::NoError, ret);
}

TEST_F(UpdateModuleTests, CallArtifactFailure) {
	UpdateModuleTestWithDefaultArtifact update_module_test(*this);
	ASSERT_FALSE(HasFailure());

	string artifactFailure = R"(#!/bin/sh
echo "Called Update Module with" "$@"
if [ $1 = "ArtifactFailure" ]; then
	exit 0
fi
exit 1
)";

	auto ok = PrepareUpdateModuleScript(*update_module_test.update_module, artifactFailure);
	ASSERT_TRUE(ok);

	auto ret = update_module_test.update_module->ArtifactFailure();
	ASSERT_EQ(error::NoError, ret);
}

TEST_F(UpdateModuleTests, CallCleanup) {
	UpdateModuleTestWithDefaultArtifact update_module_test(*this);
	ASSERT_FALSE(HasFailure());

	string script = R"(#!/bin/sh
echo "Called Update Module with" "$@"
if [ $1 = "Cleanup" ]; then
	exit 0
fi
exit 1
)";

	auto ok = PrepareUpdateModuleScript(*update_module_test.update_module, script);
	ASSERT_TRUE(ok);

	auto ret = update_module_test.update_module->Cleanup();
	ASSERT_EQ(error::NoError, ret);
}

// TODO Check if all states are called.

TEST_F(UpdateModuleTests, CallNeedsArtifactReboot) {
	UpdateModuleTestWithDefaultArtifact update_module_test(*this);
	ASSERT_FALSE(HasFailure());

	// State: NeedsReboot: Yes
	string needsReboot = R"(#!/bin/sh
if [ $1 = "NeedsArtifactReboot" ]; then
	echo "Yes"
	exit 0
fi
exit 1
)";

	auto ok = PrepareUpdateModuleScript(*update_module_test.update_module, needsReboot);
	ASSERT_TRUE(ok);

	auto ret = update_module_test.update_module->NeedsReboot();
	ASSERT_TRUE(ret.has_value()) << ret.error();
	ASSERT_EQ(ret, mender::update::update_module::v3::RebootAction::Yes);

	// State: NeedsReboot: No
	needsReboot = R"(#!/bin/sh
if [ $1 = "NeedsArtifactReboot" ]; then
	echo "No"
	exit 0
fi
exit 1
)";

	ok = PrepareUpdateModuleScript(*update_module_test.update_module, needsReboot);
	ASSERT_TRUE(ok);

	ret = update_module_test.update_module->NeedsReboot();
	ASSERT_TRUE(ret.has_value()) << ret.error();
	ASSERT_EQ(ret, mender::update::update_module::v3::RebootAction::No);

	// State: NeedsReboot: Automatic
	needsReboot = R"(#!/bin/sh
if [ $1 = "NeedsArtifactReboot" ]; then
	echo "Automatic"
	exit 0
fi
exit 1
)";

	ok = PrepareUpdateModuleScript(*update_module_test.update_module, needsReboot);
	ASSERT_TRUE(ok);

	ret = update_module_test.update_module->NeedsReboot();
	ASSERT_TRUE(ret.has_value()) << ret.error();
	ASSERT_EQ(ret, mender::update::update_module::v3::RebootAction::Automatic);

	// State: NeedsReboot: Bogus
	needsReboot = R"(#!/bin/sh
if [ $1 = "NeedsArtifactReboot" ]; then
	echo "I don't know how to use Update Modules"
	exit 0
fi
exit 1
)";

	ok = PrepareUpdateModuleScript(*update_module_test.update_module, needsReboot);
	ASSERT_TRUE(ok);

	ret = update_module_test.update_module->NeedsReboot();
	ASSERT_FALSE(ret.has_value()) << ret.error();
	ASSERT_EQ(ret.error().code, make_error_condition(errc::protocol_error));

	// State: NeedsReboot: Valid, but with trailing garbage
	needsReboot = R"(#!/bin/sh
if [ $1 = "NeedsArtifactReboot" ]; then
	echo "Automatic"
	echo "Should not be here"
	exit 0
fi
exit 1
)";

	ok = PrepareUpdateModuleScript(*update_module_test.update_module, needsReboot);
	ASSERT_TRUE(ok);

	ret = update_module_test.update_module->NeedsReboot();
	ASSERT_FALSE(ret.has_value()) << ret.error();
	ASSERT_EQ(ret.error().code, make_error_condition(errc::protocol_error));
}


TEST_F(UpdateModuleTests, CallStatesWithOutputSupportsRollback) {
	UpdateModuleTestWithDefaultArtifact update_module_test(*this);
	ASSERT_FALSE(HasFailure());

	// State: SupportsRollback: Yes
	string supportsRollback = R"(#!/bin/sh
if [ $1 = "SupportsRollback" ]; then
	echo "Yes"
	exit 0
fi
exit 1
)";
	auto ok = PrepareUpdateModuleScript(*update_module_test.update_module, supportsRollback);
	ASSERT_TRUE(ok);

	auto ret = update_module_test.update_module->SupportsRollback();
	ASSERT_TRUE(ret.has_value()) << ret.error();
	ASSERT_EQ(ret, true);

	// State: SupportsRollback: No
	supportsRollback = R"(#!/bin/sh
if [ $1 = "SupportsRollback" ]; then
	echo "No"
	exit 0
fi
exit 1
)";
	ok = PrepareUpdateModuleScript(*update_module_test.update_module, supportsRollback);
	ASSERT_TRUE(ok);

	ret = update_module_test.update_module->SupportsRollback();
	ASSERT_TRUE(ret.has_value()) << ret.error();
	ASSERT_EQ(ret, false);

	// State: SupportsRollback: Invalid
	supportsRollback = R"(#!/bin/sh
if [ $1 = "SupportsRollback" ]; then
	echo "Nothing to see here"
	exit 0
fi
exit 1
)";
	ok = PrepareUpdateModuleScript(*update_module_test.update_module, supportsRollback);
	ASSERT_TRUE(ok);

	ret = update_module_test.update_module->SupportsRollback();
	ASSERT_FALSE(ret.has_value()) << ret.error();
	ASSERT_EQ(ret.error().code, make_error_condition(errc::protocol_error));

	// State: SupportsRollback: Valid, but with garbage at the end
	supportsRollback = R"(#!/bin/sh
if [ $1 = "SupportsRollback" ]; then
	echo "No"
	# Use sleep to try to split into two separate reads.
	sleep 0.1
	echo "Bogus stuff"
	exit 0
fi
exit 1
)";
	ok = PrepareUpdateModuleScript(*update_module_test.update_module, supportsRollback);
	ASSERT_TRUE(ok);

	ret = update_module_test.update_module->SupportsRollback();
	ASSERT_FALSE(ret.has_value()) << ret.error();
	ASSERT_EQ(ret.error().code, make_error_condition(errc::protocol_error));
}

TEST_F(UpdateModuleTests, CallStatesNegativeTests) {
	UpdateModuleTestWithDefaultArtifact update_module_test(*this);
	ASSERT_FALSE(HasFailure());

	// State: SupportsRollback: Yes
	string testScript = R"(#!/bin/sh
exit 2
)";

	auto ok = PrepareUpdateModuleScript(*update_module_test.update_module, testScript);
	ASSERT_TRUE(ok);

	// No work Path
	auto old = update_module_test.update_module->GetUpdateModuleWorkDir();
	update_module_test.update_module->SetUpdateModuleWorkDir("non-existing-dir");
	auto ret = update_module_test.update_module->ArtifactCommit();
	ASSERT_NE(ret, error::NoError);
	EXPECT_EQ(ret.message, "ArtifactCommit: Error while checking file tree: non-existing-dir");
	update_module_test.update_module->SetUpdateModuleWorkDir(old);

	// Non-existing executable
	old = update_module_test.update_module->GetUpdateModulePath();
	update_module_test.update_module->SetUpdateModulePath("non-existing-binary");
	ret = update_module_test.update_module->ArtifactCommit();
	ASSERT_NE(ret, error::NoError);
	EXPECT_EQ(ret.message, "ArtifactCommit: Process exited with status 1");
	update_module_test.update_module->SetUpdateModulePath(old);

	// Process returning an error
	ret = update_module_test.update_module->ArtifactCommit();
	ASSERT_NE(ret, error::NoError);
	EXPECT_EQ(ret.message, "ArtifactCommit: Process exited with status 2");
}

TEST_F(UpdateModuleTests, RegularStateTimeout) {
	UpdateModuleTestWithDefaultArtifact update_module_test(*this);
	ASSERT_FALSE(HasFailure());

	string commitScript = R"(#!/bin/sh
sleep 10
)";

	auto ok = PrepareUpdateModuleScript(*update_module_test.update_module, commitScript);
	ASSERT_TRUE(ok);

	update_module_test.config.module_timeout_seconds = 1;

	auto ret = update_module_test.update_module->ArtifactCommit();
	ASSERT_NE(ret, error::NoError) << ret.String();
	EXPECT_EQ(ret.code, make_error_condition(errc::timed_out));
}

TEST_F(UpdateModuleTests, SystemReboot) {
	TestEventLoop loop;
	UpdateModuleTestWithDefaultArtifact update_module_test(*this);
	auto &update_module = *update_module_test.update_module;

	unique_ptr<update_module::SystemRebootRunner> system_reboot;

	// Both successful and unsuccessful reboot commands should produce the same result, since
	// the reboot command itself is not predictable in what it returns.
	for (auto &cmd : vector<string> {"true", "false"}) {
		system_reboot.reset(new update_module::SystemRebootRunner {vector<string> {cmd}, loop});
		// We pass ownership, but keep a pointer to it so we can cancel it.
		auto system_reboot_ptr = system_reboot.get();
		update_module.SetSystemRebootRunner(std::move(system_reboot));

		bool reboot_returned {false};

		auto err =
			update_module.AsyncSystemReboot(loop, [&reboot_returned, &loop](error::Error err) {
				EXPECT_EQ(err.code, make_error_condition(errc::operation_canceled)) << err.String();
				reboot_returned = true;
				loop.Stop();
			});
		ASSERT_EQ(err, error::NoError);

		events::Timer cancel_timer(loop);
		cancel_timer.AsyncWait(
			chrono::milliseconds {200}, [&reboot_returned, &system_reboot_ptr](error::Error err) {
				ASSERT_EQ(err, error::NoError);
				// Reboot should be waiting for the system to reboot.
				EXPECT_FALSE(reboot_returned);
				// Cancel the long wait. This won't normally happen in production.
				system_reboot_ptr->timeout.Cancel();
			});

		loop.Run();

		EXPECT_TRUE(reboot_returned);
	}

	system_reboot.reset(
		new update_module::SystemRebootRunner {vector<string> {"/bogus-command"}, loop});
	update_module.SetSystemRebootRunner(std::move(system_reboot));

	auto err = update_module.AsyncSystemReboot(loop, [](error::Error err) {});
	EXPECT_NE(err, error::NoError);
	EXPECT_THAT(err.String(), testing::HasSubstr("Unable to call system reboot command"));
}

TEST(AsyncFifoOpener, Open) {
	TestEventLoop loop;
	TemporaryDirectory tmpdir;

	string fifo = path::Join(tmpdir.Path(), "fifo");
	ASSERT_EQ(0, mkfifo(fifo.c_str(), 0644));

	update_module::AsyncFifoOpener opener(loop);
	bool hit_handler {false};
	auto err = opener.AsyncOpen(fifo, [&loop, &hit_handler](io::ExpectedAsyncWriterPtr exp_writer) {
		ASSERT_TRUE(exp_writer) << exp_writer.error().String();
		hit_handler = true;
		loop.Stop();
	});
	ASSERT_EQ(err, error::NoError);

	loop.Post([&fifo]() {
		ifstream fd(fifo);
		ASSERT_TRUE(fd.good());
	});

	loop.Run();

	EXPECT_TRUE(hit_handler);
}

TEST(AsyncFifoOpener, Error) {
	TestEventLoop loop;
	TemporaryDirectory tmpdir;

	string fifo = path::Join(tmpdir.Path(), "non-existing/fifo");
	// Don't create it.

	update_module::AsyncFifoOpener opener(loop);
	bool hit_handler {false};
	auto err = opener.AsyncOpen(fifo, [&loop, &hit_handler](io::ExpectedAsyncWriterPtr exp_writer) {
		ASSERT_FALSE(exp_writer);
		hit_handler = true;
		loop.Stop();
	});
	ASSERT_EQ(err, error::NoError);

	loop.Run();

	EXPECT_TRUE(hit_handler);
}

TEST(AsyncFifoOpener, Cancel) {
	TestEventLoop loop;
	TemporaryDirectory tmpdir;

	string fifo = path::Join(tmpdir.Path(), "fifo");
	ASSERT_EQ(0, mkfifo(fifo.c_str(), 0644));

	update_module::AsyncFifoOpener opener(loop);
	bool hit_handler {false};
	auto err = opener.AsyncOpen(fifo, [&loop, &hit_handler](io::ExpectedAsyncWriterPtr exp_writer) {
		ASSERT_FALSE(exp_writer);
		EXPECT_EQ(exp_writer.error().code, make_error_condition(errc::operation_canceled));
		hit_handler = true;
		loop.Stop();
	});
	ASSERT_EQ(err, error::NoError);
	opener.Cancel();

	loop.Run();

	EXPECT_TRUE(hit_handler);
}
