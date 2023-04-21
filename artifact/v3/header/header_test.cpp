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

#include <artifact/v3/header/header.hpp>

#include <string>
#include <fstream>
#include <memory>

#include <gtest/gtest.h>
#include <gmock/gmock.h>

#include <common/processes.hpp>
#include <common/testing.hpp>
#include <common/log.hpp>
#include <common/path.hpp>

#include <artifact/tar/tar.hpp>


using namespace std;

namespace io = mender::common::io;
namespace json = mender::common::json;
namespace tar = mender::tar;
namespace processes = mender::common::processes;
namespace mendertesting = mender::common::testing;
namespace path = mender::common::path;

namespace header = mender::artifact::v3::header;

using ExpectedHeader = mender::artifact::v3::header::ExpectedHeader;

class HeaderTestEnv : public testing::Test {
public:
protected:
	static void CreateTestArtifact(
		mendertesting::TemporaryDirectory &tmpdir,
		string update_type,
		vector<string> extra_artifact_args) {
		string script = R"(#! /bin/sh

DIRNAME=$(dirname $0)

# Create two dummy Artifact scripts
echo foobar > ${DIRNAME}/ArtifactInstall_Enter_01_test-dummy
echo foobar > ${DIRNAME}/ArtifactInstall_Enter_02_test-dummy

# Create some dummy meta-data
echo '{"foo": "bar"}' > ${DIRNAME}/meta-data-file

# Create an Artifact
echo foobar > ${DIRNAME}/testdata
mender-artifact write)";

		script += " " + update_type + " ";

		script += R"(\
    --compression=none    \
    --device-type header-test-device \
    -artifact-name header-tester-name \
    --file ${DIRNAME}/testdata )";

		for (const auto &arg : extra_artifact_args) {
			script += +" " + arg + " ";
		}
		script += R"(--output-path ${DIRNAME}/artifact.mender || exit 1

#Extract the header
tar xOf ${DIRNAME}/artifact.mender header.tar > ${DIRNAME}/header.tar || exit 2

exit 0)";

		const string script_fname = tmpdir.Path() + "/test-script.sh";

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

	static void CreateWrongHeadersFromHeader(
		mendertesting::TemporaryDirectory &tmpdir, string tar_archive) {
		string script = R"(#! /bin/sh

set -e

			)";
		script += "cd " + tmpdir.Path();
		script += R"(
#Extract the archive
					  tar xvf )"
				  + path::Join(tmpdir.Path(), tar_archive);

		script += R"(

# Create an archive with files out of order
tar cvf wrong-file-order.tar headers/0000/type-info header-info

tar tvf wrong-file-order.tar >&2

#Change the indexes
mkdir headers/0001
mv headers/0000/type-info  headers/0001/type-info
mv headers/0000/meta-data  headers/0001/meta-data 2>/dev/null || true

# Recreate the archive
tar cvf wrong-index.tar header-info headers/0001/type-info $(stat headers/0001/meta-data 2>/dev/null && echo headers/0001/meta-data)


exit 0)";

		const string script_fname = tmpdir.Path() + "/create-wrong-script.sh";

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
};

TEST_F(HeaderTestEnv, TestHeaderRootfsAllFlagsSetSuccess) {
	mendertesting::TemporaryDirectory tmpdir {};
	CreateTestArtifact(
		tmpdir,
		"rootfs-image",
		{{R"(--script ${DIRNAME}/ArtifactInstall_Enter_01_test-dummy)"},
		 {R"(--script ${DIRNAME}/ArtifactInstall_Enter_02_test-dummy)"},
		 {"--provides-group test-artifact-group1"},
		 {"--artifact-name-depends header-test-artifact-name-depends"},
		 {"--depends-groups header-artifact-depends-group"},
		 {"--depends foo:bar"}});


	std::fstream fs {tmpdir.Path() + "/header.tar"};

	mender::common::io::StreamReader sr {fs};

	ExpectedHeader expected_header =
		header::Parse(sr, mender::artifact::parser::config::ParserConfig {tmpdir.Path()});

	ASSERT_TRUE(expected_header) << expected_header.error().message;

	auto header = expected_header.value();

	//
	// Header-info
	//

	EXPECT_EQ(header.info.payloads.size(), 1);
	EXPECT_EQ(header.info.payloads.at(0).type, header::Payload::RootfsImage);
	EXPECT_EQ(header.info.provides.artifact_name, "header-tester-name");
	EXPECT_EQ(header.info.depends.device_type.at(0), "header-test-device");

	// Optional provides (artifact_group)
	ASSERT_TRUE(header.info.provides.artifact_group);
	EXPECT_EQ(header.info.provides.artifact_group, "test-artifact-group1");

	// depends

	// device-type
	ASSERT_EQ(header.info.depends.device_type.size(), 1);
	EXPECT_EQ(header.info.depends.device_type.at(0), "header-test-device");

	// depends:artifact-name (optional)
	ASSERT_TRUE(header.info.depends.artifact_name);
	ASSERT_EQ(header.info.depends.artifact_name.value().size(), 1);
	EXPECT_EQ(header.info.depends.artifact_name.value().at(0), "header-test-artifact-name-depends");

	// depends:artifact-groups (optional)
	ASSERT_TRUE(header.info.depends.artifact_group);
	ASSERT_EQ(header.info.depends.artifact_group.value().size(), 1);
	EXPECT_EQ(header.info.depends.artifact_group.value().at(0), "header-artifact-depends-group");

	//
	// Artifact Scripts
	//
	ASSERT_TRUE(header.artifactScripts);
	EXPECT_EQ(header.artifactScripts.value().size(), 2);
	EXPECT_THAT(
		header.artifactScripts.value().at(0),
		testing::AnyOf(
			testing::EndsWith("ArtifactInstall_Enter_01_test-dummy"),
			testing::EndsWith("ArtifactInstall_Enter_02_test-dummy")));
	EXPECT_THAT(
		header.artifactScripts.value().at(1),
		testing::AnyOf(
			testing::EndsWith("ArtifactInstall_Enter_01_test-dummy"),
			testing::EndsWith("ArtifactInstall_Enter_02_test-dummy")));


	//
	// Sub-headers
	//
	EXPECT_EQ(header.subHeaders.size(), 1);

	//
	// type-info
	//

	EXPECT_EQ(header.subHeaders.at(0).type_info.type, "rootfs-image");

	// type_info/0000/artifact_provides
	EXPECT_TRUE(header.subHeaders.at(0).type_info.artifact_provides);
	EXPECT_EQ(
		header.subHeaders.at(0).type_info.artifact_provides.value()["rootfs-image.checksum"],
		"aec070645fe53ee3b3763059376134f058cc337247c978add178b6ccdfb0019f");

	// type_info/0000/artifact_depends
	EXPECT_TRUE(header.subHeaders.at(0).type_info.artifact_depends);
	EXPECT_EQ(header.subHeaders.at(0).type_info.artifact_depends.value()["foo"], "bar");

	// type_info/0000/clears_artifact_provides
	EXPECT_TRUE(header.subHeaders.at(0).type_info.clears_artifact_provides);
	EXPECT_EQ(
		header.subHeaders.at(0).type_info.clears_artifact_provides.value().at(0), "artifact_group");
	EXPECT_EQ(
		header.subHeaders.at(0).type_info.clears_artifact_provides.value().at(1),
		"rootfs_image_checksum");
	EXPECT_EQ(
		header.subHeaders.at(0).type_info.clears_artifact_provides.value().at(2), "rootfs-image.*");
}

TEST_F(HeaderTestEnv, TestHeaderModuleImageAllFlagsSetSuccess) {
	mendertesting::TemporaryDirectory tmpdir {};
	CreateTestArtifact(
		tmpdir,
		"module-image",
		{{"--type dummy-update-module"},
		 {R"(--script ${DIRNAME}/ArtifactInstall_Enter_01_test-dummy)"},
		 {R"(--script ${DIRNAME}/ArtifactInstall_Enter_02_test-dummy)"},
		 {"--provides-group test-artifact-group1"},
		 {"--artifact-name-depends header-test-artifact-name-depends"},
		 {R"(--meta-data ${DIRNAME}/meta-data-file)"},
		 {"--depends-groups header-artifact-depends-group"},
		 {"--depends foo:bar"}});


	std::fstream fs {tmpdir.Path() + "/header.tar"};

	mender::common::io::StreamReader sr {fs};

	ExpectedHeader expected_header =
		header::Parse(sr, mender::artifact::parser::config::ParserConfig {tmpdir.Path()});

	ASSERT_TRUE(expected_header) << expected_header.error().message;

	auto header = expected_header.value();

	//
	// Header-info
	//

	EXPECT_EQ(header.info.payloads.size(), 1);
	EXPECT_EQ(header.info.payloads.at(0).type, header::Payload::ModuleImage);
	EXPECT_EQ(header.info.payloads.at(0).name, "dummy-update-module");
	EXPECT_EQ(header.info.provides.artifact_name, "header-tester-name");
	EXPECT_EQ(header.info.depends.device_type.at(0), "header-test-device");

	// Optional provides (artifact_group)
	ASSERT_TRUE(header.info.provides.artifact_group);
	EXPECT_EQ(header.info.provides.artifact_group, "test-artifact-group1");

	// depends

	// device-type
	ASSERT_EQ(header.info.depends.device_type.size(), 1);
	EXPECT_EQ(header.info.depends.device_type.at(0), "header-test-device");

	// depends:artifact-name (optional)
	ASSERT_TRUE(header.info.depends.artifact_name);
	ASSERT_EQ(header.info.depends.artifact_name.value().size(), 1);
	EXPECT_EQ(header.info.depends.artifact_name.value().at(0), "header-test-artifact-name-depends");

	// depends:artifact-groups (optional)
	ASSERT_TRUE(header.info.depends.artifact_group);
	ASSERT_EQ(header.info.depends.artifact_group.value().size(), 1);
	EXPECT_EQ(header.info.depends.artifact_group.value().at(0), "header-artifact-depends-group");

	//
	// Artifact Scripts
	//
	ASSERT_TRUE(header.artifactScripts);
	EXPECT_EQ(header.artifactScripts.value().size(), 2);
	EXPECT_THAT(
		header.artifactScripts.value().at(0),
		testing::AnyOf(
			testing::EndsWith("ArtifactInstall_Enter_01_test-dummy"),
			testing::EndsWith("ArtifactInstall_Enter_02_test-dummy")));
	EXPECT_THAT(
		header.artifactScripts.value().at(1),
		testing::AnyOf(
			testing::EndsWith("ArtifactInstall_Enter_01_test-dummy"),
			testing::EndsWith("ArtifactInstall_Enter_02_test-dummy")));

	//
	// Sub-headers
	//
	EXPECT_EQ(header.subHeaders.size(), 1);

	//
	// type-info
	//

	EXPECT_EQ(header.subHeaders.at(0).type_info.type, "dummy-update-module");

	// type_info/0000/artifact_provides
	EXPECT_TRUE(header.subHeaders.at(0).type_info.artifact_provides);
	EXPECT_EQ(
		header.subHeaders.at(0)
			.type_info.artifact_provides.value()["rootfs-image.dummy-update-module.version"],
		"header-tester-name");

	// type_info/0000/artifact_depends
	EXPECT_TRUE(header.subHeaders.at(0).type_info.artifact_depends);
	EXPECT_EQ(header.subHeaders.at(0).type_info.artifact_depends.value()["foo"], "bar");

	// type_info/0000/clears_artifact_provides
	EXPECT_TRUE(header.subHeaders.at(0).type_info.clears_artifact_provides);
	EXPECT_EQ(
		header.subHeaders.at(0).type_info.clears_artifact_provides.value().at(0),
		"rootfs-image.dummy-update-module.*");

	// headers/0000/meta-data
	ASSERT_TRUE(header.subHeaders.at(0).metadata);
	// EXPECT_EQ(header.subHeaders.at(0).meta_data.value().Dump(), "rootfs-image.*");
}


TEST_F(HeaderTestEnv, TestTwoArtifactScriptsSuccess) {
	mendertesting::TemporaryDirectory tmpdir {};
	CreateTestArtifact(
		tmpdir,
		"rootfs-image",
		{
			{R"(--script ${DIRNAME}/ArtifactInstall_Enter_01_test-dummy)"},
			{R"(--script ${DIRNAME}/ArtifactInstall_Enter_02_test-dummy)"},
		});


	std::fstream fs {tmpdir.Path() + "/header.tar"};

	mender::common::io::StreamReader sr {fs};

	ExpectedHeader expected_header = header::Parse(sr);

	ASSERT_TRUE(expected_header) << expected_header.error().message;

	auto header = expected_header.value();

	ASSERT_TRUE(header.artifactScripts);
	EXPECT_EQ(header.artifactScripts.value().size(), 2);
}

TEST_F(HeaderTestEnv, TestOneArtifactScripts) {
	mendertesting::TemporaryDirectory tmpdir {};
	CreateTestArtifact(
		tmpdir,
		"rootfs-image",
		{
			{R"(--script ${DIRNAME}/ArtifactInstall_Enter_01_test-dummy)"},
		});

	std::fstream fs {tmpdir.Path() + "/header.tar"};

	mender::common::io::StreamReader sr {fs};

	ExpectedHeader expected_header = header::Parse(sr);

	ASSERT_TRUE(expected_header) << expected_header.error().message;

	auto header = expected_header.value();

	ASSERT_TRUE(header.artifactScripts);
	EXPECT_EQ(header.artifactScripts.value().size(), 1);
}

TEST_F(HeaderTestEnv, TestHeaderNoExtraData) {
	mendertesting::TemporaryDirectory tmpdir {};
	CreateTestArtifact(tmpdir, "module-image", {"--type test-module-image"});

	std::fstream fs {tmpdir.Path() + "/header.tar"};

	mender::common::io::StreamReader sr {fs};

	ExpectedHeader expected_header = header::Parse(sr);

	ASSERT_TRUE(expected_header) << expected_header.error().message;
}

TEST_F(HeaderTestEnv, TestHeaderIndexError) {
	mendertesting::TemporaryDirectory tmpdir {};
	CreateTestArtifact(tmpdir, "module-image", {"--type test-module-image"});

	CreateWrongHeadersFromHeader(tmpdir, "header.tar");

	std::fstream fs {tmpdir.Path() + "/wrong-index.tar"};

	mender::common::io::StreamReader sr {fs};

	ExpectedHeader expected_header = header::Parse(sr);

	ASSERT_FALSE(expected_header);
	EXPECT_EQ(
		expected_header.error().message,
		"Unexpected index order for the type-info: headers/0001/type-info expected: headers/0000/type-info");
}

TEST_F(HeaderTestEnv, TestHeaderFilesOutOfOrder) {
	mendertesting::TemporaryDirectory tmpdir {};
	CreateTestArtifact(tmpdir, "module-image", {"--type test-module-image"});

	CreateWrongHeadersFromHeader(tmpdir, "header.tar");

	std::fstream fs {tmpdir.Path() + "/wrong-file-order.tar"};

	mender::common::io::StreamReader sr {fs};

	ExpectedHeader expected_header = header::Parse(sr);

	ASSERT_FALSE(expected_header);
	EXPECT_EQ(
		expected_header.error().message,
		"Got unexpected token: 'type-info' expected 'header-info'");
}

TEST_F(HeaderTestEnv, TestHeaderMetaDataSuccess) {
	stringstream meta_data {
		R"(
{
  "foo": "bar",
  "bar": "100",
  "baz": 1,
  "bur": ["foo", 1000]
}
)"};

	mender::common::io::StreamReader sr {meta_data};

	auto expected_meta_data = header::meta_data::Parse(sr);

	ASSERT_TRUE(expected_meta_data) << expected_meta_data.error().message;
}

TEST_F(HeaderTestEnv, TestHeaderMetaDataParsingTopLevelKeys) {
	// Invalid, can only contain top-level keys
	stringstream meta_data {
		R"(
["foo", "bar" ]
)"};

	mender::common::io::StreamReader sr {meta_data};

	auto expected_meta_data = header::meta_data::Parse(sr);

	ASSERT_FALSE(expected_meta_data);
	EXPECT_EQ(expected_meta_data.error().message, "The meta-data needs to be a top-level object");
}

TEST_F(HeaderTestEnv, TestHeaderMetaDataParsingNumbersStringsAndLists) {
	// Invalid, can only contain strings, numbers and lists of numbers and strings
	stringstream meta_data {
		R"(
{
  "foo": { "bar": "baz" }
}
)"};

	mender::common::io::StreamReader sr {meta_data};

	auto expected_meta_data = header::meta_data::Parse(sr);

	ASSERT_FALSE(expected_meta_data);
	EXPECT_EQ(
		expected_meta_data.error().message,
		"The meta-data needs to only be strings, ints and arrays of ints and strings");
}

TEST_F(HeaderTestEnv, TestHeaderMetaDataParsingListOfObjectsNotAllowed) {
	// Invalid, can only contain strings, numbers and lists of numbers and strings
	// Not list of objects
	stringstream meta_data {
		R"(
{
  "foo": [ { "bar": "baz" } ]
}
)"};

	mender::common::io::StreamReader sr {meta_data};

	auto expected_meta_data = header::meta_data::Parse(sr);

	ASSERT_FALSE(expected_meta_data);
	EXPECT_EQ(
		expected_meta_data.error().message,
		"The meta-data needs to only be strings, ints and arrays of ints and strings");
}

TEST_F(HeaderTestEnv, TestHeaderMetaDataSingleBracketPayloadTest) {
	stringstream meta_data {R"({)"};

	mender::common::io::StreamReader sr {meta_data};

	auto expected_meta_data = header::meta_data::Parse(sr);

	ASSERT_FALSE(expected_meta_data) << expected_meta_data.error().message;
}

TEST_F(HeaderTestEnv, TestHeaderMetaDataSingleSpacePayloadTest) {
	stringstream meta_data {R"( )"};

	mender::common::io::StreamReader sr {meta_data};

	auto expected_meta_data = header::meta_data::Parse(sr);

	ASSERT_FALSE(expected_meta_data) << expected_meta_data.error().message;
}



// any integer less than -9007199254740991 or greater than 9007199254740991
// 	should be stored as a string, otherwise the value will be rounded to the
// 	nearest representable number.
TEST_F(HeaderTestEnv, TestHeaderMetaDataIs64BitFloatingPointRepresented) {
	stringstream meta_data {
		R"(
{
  "test": 10000000,
  "correct-max-int": 9007199254740991,
  "correct-min-int": -9007199254740991
}
)"};

	mender::common::io::StreamReader sr {meta_data};

	auto expected_meta_data = header::meta_data::Parse(sr);

	ASSERT_TRUE(expected_meta_data);

	{
		auto val = expected_meta_data.value().Get("test").and_then(
			[](const json::Json &json) { return json.GetInt(); });

		ASSERT_TRUE(val) << val.error().message;
		EXPECT_EQ(val.value(), 10000000);
	}

	{
		auto val =
			expected_meta_data.value().Get("correct-max-int").and_then([](const json::Json &json) {
				return json.GetInt();
			});

		ASSERT_TRUE(val) << val.error().message;
		ASSERT_EQ(val.value(), 9007199254740991);
	}

	{
		auto val =
			expected_meta_data.value().Get("correct-min-int").and_then([](const json::Json &json) {
				return json.GetInt();
			});

		ASSERT_TRUE(val) << val.error().message;
		ASSERT_EQ(val.value(), -9007199254740991);
	}
}

TEST_F(HeaderTestEnv, TestHeaderMetaDataIs53BitFloatingPointIsRounded) {
	stringstream meta_data {
		R"(
{
  "one-out-of-53-bit-range": 9007199254740992,
  "one-out-of-negative-53-bit-range": -9007199254740992
}
)"};

	mender::common::io::StreamReader sr {meta_data};

	auto expected_meta_data = header::meta_data::Parse(sr);

	ASSERT_TRUE(expected_meta_data);

	{
		auto expected_val = expected_meta_data.value()
								.Get("one-out-of-53-bit-range")
								.and_then([](const json::Json &json) { return json.GetDouble(); });

		ASSERT_TRUE(expected_val) << expected_val.error().message;
		EXPECT_THAT(expected_val.value(), testing::DoubleEq(9007199254740991));
	}

	{
		auto expected_val = expected_meta_data.value()
								.Get("one-out-of-negative-53-bit-range")
								.and_then([](const json::Json &json) { return json.GetDouble(); });

		ASSERT_TRUE(expected_val) << expected_val.error().message;
		EXPECT_THAT(expected_val.value(), testing::DoubleEq(-9007199254740991));
	}
}
