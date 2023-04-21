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

	bool PrepareTestFile(const string &name, bool executable) {
		string test_file_path {path::Join(test_scripts_dir, name)};
		ofstream os(test_file_path);
		os.close();

		if (executable) {
			int ret = chmod(test_file_path.c_str(), 0700);
			return ret == 0;
		}
		return true;
	}
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
	mender::artifact::PayloadHeaderView update_payload_header;
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

	EXPECT_TRUE(FileContains(path::Join(tree_path, "header", "meta_data"), ""));

	err = up_mod.DeleteFileTree(tree_path);
	ASSERT_EQ(err, error::NoError);
}
