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

#include <gtest/gtest.h>
#include <gmock/gmock.h>

#include <common/common.hpp>
#include <common/path.hpp>
#include <common/key_value_database_lmdb.hpp>
#include <common/testing.hpp>
#include <common/processes.hpp>
#include <common/error.hpp>
#include <common/path.hpp>

#include <mender-update/context.hpp>
#include <string>
#include <sstream>

namespace io = mender::common::io;
namespace error = mender::common::error;
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
protected:
	TemporaryDirectory temp_dir;
	string test_scripts_dir;

	bool PrepareTestScriptsDir() {
		test_scripts_dir = path::Join(temp_dir.Path(), "modules");
		if (mkdir(test_scripts_dir.c_str(), 0700) != 0) {
			return false;
		}
		test_scripts_dir = path::Join(test_scripts_dir, "v3");
		if (mkdir(test_scripts_dir.c_str(), 0700) != 0) {
			return false;
		}

		return true;
	}

	bool PrepareTestFile(const string &name, bool executable, string script = "") {
		string test_file_path {path::Join(test_scripts_dir, name)};
		ofstream os(test_file_path);
		os << script;
		os.close();

		if (executable) {
			int ret = chmod(test_file_path.c_str(), 0700);
			return ret == 0;
		}
		return true;
	}

	string GetTestScriptDir() {
		return test_scripts_dir;
	}
};

class UpdateModuleTest : public update_module::UpdateModule {
public:
	UpdateModuleTest(
		context::MenderContext &ctx,
		mender::artifact::PayloadHeader &update_meta_data,
		string name,
		string path,
		string workPath) :
		UpdateModule(ctx, update_meta_data) {
		modulePath_ = path;
		workPath_ = workPath;
		name_ = name;
	}

protected:
	std::string GetModulePath() const override {
		return path::Join(modulePath_, name_);
	}
	std::string GetModulesWorkPath() override {
		return workPath_;
	}

	string modulePath_;
	string workPath_;
	string name_;
};

class UpdateModuleTestContiner {
private:
	conf::MenderConfig config_;
	context::MenderContext ctx_;
	mender::artifact::PayloadHeader update_meta_data_;

public:
	UpdateModuleTestContiner(string name, string path, string workPath) :
		ctx_(config_),
		update_module(ctx_, update_meta_data_, name, path, workPath) {
	}
	UpdateModuleTest update_module;
};

TEST_F(UpdateModuleTests, DiscoverUpdateModulesTest) {
	auto ok = PrepareTestScriptsDir();
	ASSERT_TRUE(ok);

	ok = PrepareTestFile("file1", false);
	ASSERT_TRUE(ok);

	ok = PrepareTestFile("script1", true);
	ASSERT_TRUE(ok);

	ok = PrepareTestFile("file2", false);
	ASSERT_TRUE(ok);

	ok = PrepareTestFile("script2", true);
	ASSERT_TRUE(ok);

	auto cfg = conf::MenderConfig();
	cfg.data_store_dir = temp_dir.Path();

	auto ex_modules = update_module::DiscoverUpdateModules(cfg);
	ASSERT_TRUE(ex_modules);
	auto modules = ex_modules.value();
	EXPECT_EQ(modules.size(), 2);
	EXPECT_EQ(count(modules.cbegin(), modules.cend(), path::Join(test_scripts_dir, "script1")), 1);
	EXPECT_EQ(count(modules.cbegin(), modules.cend(), path::Join(test_scripts_dir, "script2")), 1);
}

TEST_F(UpdateModuleTests, DiscoverUpdateModulesNoExistTest) {
	auto ok = PrepareTestScriptsDir();
	ASSERT_TRUE(ok);

	auto cfg = conf::MenderConfig();
	cfg.data_store_dir = temp_dir.Path();

	auto ex_modules = update_module::DiscoverUpdateModules(cfg);
	ASSERT_TRUE(ex_modules);

	auto modules = ex_modules.value();
	EXPECT_EQ(modules.size(), 0);
}

TEST_F(UpdateModuleTests, DiscoverUpdateModulesEmptyDirTest) {
	auto cfg = conf::MenderConfig();
	cfg.data_store_dir = temp_dir.Path();

	auto ex_modules = update_module::DiscoverUpdateModules(cfg);
	ASSERT_TRUE(ex_modules);

	auto modules = ex_modules.value();
	EXPECT_EQ(modules.size(), 0);
}

TEST_F(UpdateModuleTests, DiscoverUpdateModulesNoExecutablesTest) {
	auto ok = PrepareTestScriptsDir();
	ASSERT_TRUE(ok);

	ok = PrepareTestFile("file1", false);
	ASSERT_TRUE(ok);

	ok = PrepareTestFile("file2", false);
	ASSERT_TRUE(ok);

	auto cfg = conf::MenderConfig();
	cfg.data_store_dir = temp_dir.Path();

	auto ex_modules = update_module::DiscoverUpdateModules(cfg);
	ASSERT_TRUE(ex_modules);
	auto modules = ex_modules.value();
	EXPECT_EQ(modules.size(), 0);
}

testing::AssertionResult FileContains(const string &filename, const string &expected_content) {
	ifstream is {filename};
	ostringstream contents_s;
	contents_s << is.rdbuf();
	string contents {contents_s.str()};
	if (contents == expected_content) {
		return testing::AssertionSuccess();
	}
	return testing::AssertionFailure()
		   << "Expected: '" << expected_content << "' Got: '" << contents << "'";
};


testing::AssertionResult FileJsonEquals(const string &filename, const string &expected_content) {
	ifstream is {filename};
	json::Json contents = json::Load(is).value();
	json::Json expected_contents = json::Load(expected_content).value();
	if (contents.Dump() == expected_contents.Dump()) {
		return testing::AssertionSuccess();
	}
	return testing::AssertionFailure()
		   << "Expected: '" << contents.Dump() << "' Got: '" << expected_contents.Dump() << "'";
};


class UpdateModuleFileTreeTests : public testing::Test {
public:
	void SetUp() override {
		this->cfg.data_store_dir = test_state_dir.Path();

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

		ofstream os(path::Join(cfg.data_store_dir, "device_type"));
		ASSERT_TRUE(os);
		os << "device_type=Some device type" << endl;
		os.close();

		ASSERT_TRUE(CreateArtifact());
		std::fstream fs {path::Join(temp_dir.Path(), "artifact.mender")};
		io::StreamReader sr {fs};
		auto expected_artifact = mender::artifact::parser::Parse(sr);
		ASSERT_TRUE(expected_artifact);
		auto artifact = expected_artifact.value();

		auto expected_payload_header = mender::artifact::View(artifact, 0);
		ASSERT_TRUE(expected_payload_header) << expected_payload_header.error().message;

		this->update_payload_header = mender::artifact::View(artifact, 0).value();
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

		const string script_fname = path::Join(temp_dir.Path(), "/test-script.sh");

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
	mender::artifact::PayloadHeader update_payload_header;
};

TEST_F(UpdateModuleFileTreeTests, FileTreeTestHeader) {
	update_module::UpdateModule up_mod(*ctx, update_payload_header);
	const string tree_path = test_tree_dir.Path();
	auto err = up_mod.PrepareFileTree(tree_path);
	ASSERT_EQ(err, error::NoError);

	//
	// Current device contents
	//

	EXPECT_TRUE(FileContains(path::Join(tree_path, "version"), "3\n"));

	EXPECT_TRUE(FileContains(
		path::Join(tree_path, "current_artifact_name"), "artifact-name existing-artifact-name\n"));

	EXPECT_TRUE(FileContains(
		path::Join(tree_path, "current_artifact_group"),
		"artifact-group existing-artifact-group\n"));

	EXPECT_TRUE(FileContains(path::Join(tree_path, "current_device_type"), "Some device type\n"));

	//
	// Header contents (From the Artifact)
	//

	EXPECT_TRUE(FileContains(path::Join(tree_path, "header", "artifact_group"), ""));

	EXPECT_TRUE(FileContains(path::Join(tree_path, "header", "artifact_name"), "test-artifact"));

	EXPECT_TRUE(FileContains(path::Join(tree_path, "header", "payload_type"), "rootfs-image"));

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
		FileJsonEquals(path::Join(tree_path, "header", "header_info"), expected_header_info));


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
	EXPECT_TRUE(FileJsonEquals(path::Join(tree_path, "header", "type_info"), expected_type_info));

	// TODO
	// EXPECT_TRUE(FileContains(path::Join(tree_path, "header", "meta_data"), "bar"));

	err = up_mod.DeleteFileTree(tree_path);
	ASSERT_EQ(err, error::NoError);
}

TEST_F(UpdateModuleTests, CallAllNoOutputStates) {
	auto ok = PrepareTestScriptsDir();
	ASSERT_TRUE(ok);

	// State: ArtifactInstall
	string installScript = R"(#!/bin/sh
if [ $1 = "ArtifactInstall" ]; then
	exit 0
fi
exit 1
)";

	ok = PrepareTestFile("installScript", true, installScript);
	ASSERT_TRUE(ok);

	UpdateModuleTestContiner update_module1(
		"installScript", GetTestScriptDir(), GetTestScriptDir());
	auto ret = update_module1.update_module.ArtifactInstall();
	ASSERT_EQ(error::NoError, ret);

	// State: ArtifactReboot
	string rebootScript = R"(#!/bin/sh
if [ $1 = "ArtifactReboot" ]; then
	exit 0
fi
exit 1
)";

	ok = PrepareTestFile("rebootScript", true, rebootScript);
	ASSERT_TRUE(ok);

	UpdateModuleTestContiner update_module2("rebootScript", GetTestScriptDir(), GetTestScriptDir());
	ret = update_module2.update_module.ArtifactReboot();
	ASSERT_EQ(error::NoError, ret);

	// State: ArtifactCommit
	string commitScript = R"(#!/bin/sh
if [ $1 = "ArtifactCommit" ]; then
	exit 0
fi
exit 1
)";

	ok = PrepareTestFile("commitScript", true, rebootScript);
	ASSERT_TRUE(ok);

	UpdateModuleTestContiner update_module3("commitScript", GetTestScriptDir(), GetTestScriptDir());
	ret = update_module3.update_module.ArtifactReboot();
	ASSERT_EQ(error::NoError, ret);

	// State: ArtifactRollback
	string rollbackScript = R"(#!/bin/sh
if [ $1 = "ArtifactRollback" ]; then
	exit 0
fi
exit 1
)";

	ok = PrepareTestFile("rollbackScript", true, rollbackScript);
	ASSERT_TRUE(ok);

	UpdateModuleTestContiner update_module4(
		"rollbackScript", GetTestScriptDir(), GetTestScriptDir());
	ret = update_module4.update_module.ArtifactRollback();
	ASSERT_EQ(error::NoError, ret);

	// State: ArtifactRollback
	string verifyReboot = R"(#!/bin/sh
if [ $1 = "ArtifactVerifyReboot" ]; then
	exit 0
fi
exit 1
)";

	ok = PrepareTestFile("verifyReboot", true, verifyReboot);
	ASSERT_TRUE(ok);

	UpdateModuleTestContiner update_module5("verifyReboot", GetTestScriptDir(), GetTestScriptDir());
	ret = update_module5.update_module.ArtifactVerifyReboot();
	ASSERT_EQ(error::NoError, ret);

	// State: ArtifactRollbackReboot
	string rollbackReboot = R"(#!/bin/sh
if [ $1 = "ArtifactRollbackReboot" ]; then
	exit 0
fi
exit 1
)";

	ok = PrepareTestFile("rollbackReboot", true, rollbackReboot);
	ASSERT_TRUE(ok);

	UpdateModuleTestContiner update_module6(
		"rollbackReboot", GetTestScriptDir(), GetTestScriptDir());
	ret = update_module6.update_module.ArtifactRollbackReboot();
	ASSERT_EQ(error::NoError, ret);

	// State: ArtifactVerifyRollbackReboot
	string verifyRollbackReboot = R"(#!/bin/sh
if [ $1 = "ArtifactVerifyRollbackReboot" ]; then
	exit 0
fi
exit 1
)";

	ok = PrepareTestFile("verifyRollbackReboot", true, verifyRollbackReboot);
	ASSERT_TRUE(ok);

	UpdateModuleTestContiner update_module7(
		"verifyRollbackReboot", GetTestScriptDir(), GetTestScriptDir());
	ret = update_module7.update_module.ArtifactVerifyRollbackReboot();
	ASSERT_EQ(error::NoError, ret);

	// State: ArtifactFailure
	string artifactFailure = R"(#!/bin/sh
if [ $1 = "ArtifactFailure" ]; then
	exit 0
fi
exit 1
)";

	ok = PrepareTestFile("artifactFailure", true, artifactFailure);
	ASSERT_TRUE(ok);

	UpdateModuleTestContiner update_module8(
		"artifactFailure", GetTestScriptDir(), GetTestScriptDir());
	ret = update_module8.update_module.ArtifactFailure();
	ASSERT_EQ(error::NoError, ret);
}

TEST_F(UpdateModuleTests, CallStatesWithOutputNeedsReboot) {
	auto ok = PrepareTestScriptsDir();
	ASSERT_TRUE(ok);

	// State: NeedsReboot: Yes
	string needsReboot = R"(#!/bin/sh
if [ $1 = "NeedsArtifactReboot" ]; then
	echo "Yes"
	exit 0
fi
exit 1
)";

	ok = PrepareTestFile("needsReboot", true, needsReboot);
	ASSERT_TRUE(ok);

	UpdateModuleTestContiner update_module("needsReboot", GetTestScriptDir(), GetTestScriptDir());
	auto ret = update_module.update_module.NeedsReboot();
	ASSERT_TRUE(ret.has_value());
	ASSERT_EQ(ret, mender::update::update_module::v3::RebootAction::Yes);

	// State: NeedsReboot: No
	string needsReboot2 = R"(#!/bin/sh
if [ $1 = "NeedsArtifactReboot" ]; then
	echo "No"
	exit 0
fi
exit 1
)";

	ok = PrepareTestFile("needsReboot2", true, needsReboot2);
	ASSERT_TRUE(ok);

	UpdateModuleTestContiner update_module2("needsReboot2", GetTestScriptDir(), GetTestScriptDir());
	auto ret2 = update_module2.update_module.NeedsReboot();
	ASSERT_TRUE(ret2.has_value());
	ASSERT_EQ(ret2, mender::update::update_module::v3::RebootAction::No);

	// State: NeedsReboot: Automatic
	string needsReboot3 = R"(#!/bin/sh
if [ $1 = "NeedsArtifactReboot" ]; then
	echo "Automatic"
	exit 0
fi
exit 1
)";

	ok = PrepareTestFile("needsReboot3", true, needsReboot3);
	ASSERT_TRUE(ok);

	UpdateModuleTestContiner update_module3("needsReboot3", GetTestScriptDir(), GetTestScriptDir());
	auto ret3 = update_module3.update_module.NeedsReboot();
	ASSERT_TRUE(ret3.has_value());
	ASSERT_EQ(ret3, mender::update::update_module::v3::RebootAction::Automatic);
}


TEST_F(UpdateModuleTests, CallStatesWithOutputSupportsRollback) {
	auto ok = PrepareTestScriptsDir();
	ASSERT_TRUE(ok);

	// State: SupportsRollback: Yes
	string supportsRollback = R"(#!/bin/sh
if [ $1 = "SupportsRollback" ]; then
	echo "Yes"
	exit 0
fi
exit 1
)";
	ok = PrepareTestFile("supportsRollback", true, supportsRollback);
	ASSERT_TRUE(ok);

	UpdateModuleTestContiner update_module(
		"supportsRollback", GetTestScriptDir(), GetTestScriptDir());
	auto ret = update_module.update_module.SupportsRollback();
	ASSERT_TRUE(ret.has_value());
	ASSERT_EQ(ret, true);


	// State: SupportsRollback: No
	string supportsRollback2 = R"(#!/bin/sh
if [ $1 = "SupportsRollback" ]; then
	echo "No"
	exit 0
fi
exit 1
)";
	ok = PrepareTestFile("supportsRollback2", true, supportsRollback2);
	ASSERT_TRUE(ok);

	UpdateModuleTestContiner update_module2(
		"supportsRollback2", GetTestScriptDir(), GetTestScriptDir());
	auto ret2 = update_module2.update_module.SupportsRollback();
	ASSERT_TRUE(ret2.has_value());
	ASSERT_EQ(ret2, false);
}

TEST_F(UpdateModuleTests, CallStatesNegativeTests) {
	auto ok = PrepareTestScriptsDir();
	ASSERT_TRUE(ok);

	// State: SupportsRollback: Yes
	string testScript = R"(#!/bin/sh
exit 2
)";

	ok = PrepareTestFile("testScript", true, testScript);
	ASSERT_TRUE(ok);

	// No work Path
	UpdateModuleTestContiner update_module("testScript", GetTestScriptDir(), "non-existing-dir");
	auto ret = update_module.update_module.ArtifactCommit();
	ASSERT_NE(ret, error::NoError);
	ASSERT_EQ(ret.message, "File tree does not exist: non-existing-dir");

	// Non-existing executable
	UpdateModuleTestContiner update_module2("testScript2", GetTestScriptDir(), GetTestScriptDir());
	auto ret2 = update_module2.update_module.ArtifactCommit();
	ASSERT_NE(ret2, error::NoError);
	ASSERT_EQ(ret2.message, "Process exited with error: 1");

	// Process returning an error
	UpdateModuleTestContiner update_module3("testScript", GetTestScriptDir(), GetTestScriptDir());
	auto ret3 = update_module3.update_module.ArtifactCommit();
	ASSERT_NE(ret3, error::NoError);
	ASSERT_EQ(ret3.message, "Process exited with error: 2");
}
