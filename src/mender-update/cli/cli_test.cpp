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

#include <mender-update/cli/cli.hpp>

#include <filesystem>
#include <fstream>

#include <gtest/gtest.h>
#include <gmock/gmock.h>

#include <common/common.hpp>
#include <common/conf.hpp>
#include <common/error.hpp>
#include <common/path.hpp>
#include <common/processes.hpp>
#include <common/testing.hpp>

#include <mender-update/cli/actions.hpp>
#include <mender-update/context.hpp>

namespace cli = mender::update::cli;
namespace common = mender::common;
namespace conf = mender::common::conf;
namespace context = mender::update::context;
namespace error = mender::common::error;
namespace mtesting = mender::common::testing;
namespace path = mender::common::path;
namespace processes = mender::common::processes;

using namespace std;

bool VerifyOnlyMessages(const string &output, const vector<string> &messages) {
	auto lines = common::SplitString(output, "\n");
	for (const auto &line : lines) {
		if (line == "") {
			continue;
		}

		EXPECT_TRUE(any_of(
			messages.begin(),
			messages.end(),
			[&line](const string &msg) { return line.find(msg) != string::npos; }))
			<< line << " is an unexpected message";
	}

	return !testing::Test::HasFailure();
}

TEST(CliTest, NoAction) {
	mtesting::TemporaryDirectory tmpdir;

	conf::MenderConfig conf;
	conf.paths.SetDataStore(tmpdir.Path());
	context::MenderContext context(conf);

	auto err = context.Initialize();
	ASSERT_EQ(err, error::NoError) << err.String();

	{
		mtesting::RedirectStreamOutputs redirect_output;
		vector<string> args {"--data", tmpdir.Path()};
		EXPECT_EQ(cli::Main(args), 1);
		EXPECT_EQ(
			redirect_output.GetCerr(),
			"Could not fulfill request: Invalid options given: Need an action\n");
	}
}

TEST(CliTest, ShowArtifact) {
	mtesting::TemporaryDirectory tmpdir;

	conf::MenderConfig conf;
	conf.paths.SetDataStore(tmpdir.Path());
	context::MenderContext context(conf);

	auto err = context.Initialize();
	ASSERT_EQ(err, error::NoError) << err.String();

	{
		mtesting::RedirectStreamOutputs redirect_output;
		vector<string> args {"--data", tmpdir.Path(), "show-artifact"};
		EXPECT_EQ(cli::Main(args), 0);
		EXPECT_EQ(redirect_output.GetCout(), "Unknown\n");
	}

	auto &db = context.GetMenderStoreDB();
	string data = "my-name";
	err = db.Write(context.artifact_name_key, vector<uint8_t>(data.begin(), data.end()));
	ASSERT_EQ(err, error::NoError) << err.String();

	{
		mtesting::RedirectStreamOutputs redirect_output;
		vector<string> args {"--data", tmpdir.Path(), "show-artifact"};
		EXPECT_EQ(cli::Main(args), 0);
		EXPECT_EQ(redirect_output.GetCout(), "my-name\n");
	}
}

TEST(CliTest, ShowArtifactErrors) {
	mtesting::TemporaryDirectory tmpdir;

	conf::MenderConfig conf;
	conf.paths.SetDataStore(tmpdir.Path());

	{
		mtesting::RedirectStreamOutputs redirect_output;
		vector<string> args {"--data", tmpdir.Path(), "show-artifact", "--bogus-option"};
		EXPECT_EQ(cli::Main(args), 1);
		EXPECT_EQ(
			redirect_output.GetCerr(),
			"Could not fulfill request: Invalid options given: Unrecognized option '--bogus-option'\n");
	}

	{
		mtesting::RedirectStreamOutputs redirect_output;
		vector<string> args {"--data", tmpdir.Path(), "show-artifact", "bogus-argument"};
		EXPECT_EQ(cli::Main(args), 1);
		EXPECT_EQ(
			redirect_output.GetCerr(),
			"Could not fulfill request: Invalid options given: Unexpected argument 'bogus-argument'\n");
	}
}

TEST(CliTest, ShowProvides) {
	mtesting::TemporaryDirectory tmpdir;

	conf::MenderConfig conf;
	conf.paths.SetDataStore(tmpdir.Path());
	context::MenderContext context(conf);

	auto err = context.Initialize();
	ASSERT_EQ(err, error::NoError) << err.String();

	{
		mtesting::RedirectStreamOutputs redirect_output;
		vector<string> args {"--data", tmpdir.Path(), "show-provides"};
		EXPECT_EQ(cli::Main(args), 0);
		EXPECT_EQ(redirect_output.GetCout(), "");
	}

	auto verify = [&](const string &content) {
		{
			mtesting::RedirectStreamOutputs redirect_output;
			vector<string> args {"--data", tmpdir.Path(), "show-provides"};
			EXPECT_EQ(cli::Main(args), 0);
			EXPECT_EQ(redirect_output.GetCout(), content);
		}
	};

	auto &db = context.GetMenderStoreDB();
	string data;

	{
		SCOPED_TRACE("Line number");
		verify("");
	}

	{
		SCOPED_TRACE("Line number");
		data = "my-name";
		err = db.Write(context.artifact_name_key, vector<uint8_t>(data.begin(), data.end()));
		ASSERT_EQ(err, error::NoError) << err.String();
		verify("artifact_name=my-name\n");
	}

	{
		SCOPED_TRACE("Line number");
		data = R"({"rootfs-image.checksum":"abc"})";
		err = db.Write(context.artifact_provides_key, vector<uint8_t>(data.begin(), data.end()));
		data = "my-name";
		err = db.Write(context.artifact_name_key, vector<uint8_t>(data.begin(), data.end()));
		ASSERT_EQ(err, error::NoError) << err.String();
		verify("rootfs-image.checksum=abc\nartifact_name=my-name\n");
	}

	{
		SCOPED_TRACE("Line number");
		data = R"({"artifact_name":"this-one", "rootfs-image.checksum":"abc"})";
		err = db.Write(context.artifact_provides_key, vector<uint8_t>(data.begin(), data.end()));
		data = "not-this-one";
		err = db.Write(context.artifact_name_key, vector<uint8_t>(data.begin(), data.end()));
		ASSERT_EQ(err, error::NoError) << err.String();
		verify("rootfs-image.checksum=abc\nartifact_name=this-one\n");
	}

	ASSERT_EQ(db.Remove(context.artifact_provides_key), error::NoError);
	ASSERT_EQ(db.Remove(context.artifact_name_key), error::NoError);

	{
		SCOPED_TRACE("Line number");
		data = "my-group";
		err = db.Write(context.artifact_group_key, vector<uint8_t>(data.begin(), data.end()));
		ASSERT_EQ(err, error::NoError) << err.String();
		verify("artifact_group=my-group\n");
	}

	{
		SCOPED_TRACE("Line number");
		data = R"({"rootfs-image.checksum":"abc"})";
		err = db.Write(context.artifact_provides_key, vector<uint8_t>(data.begin(), data.end()));
		data = "my-group";
		err = db.Write(context.artifact_group_key, vector<uint8_t>(data.begin(), data.end()));
		ASSERT_EQ(err, error::NoError) << err.String();
		verify("rootfs-image.checksum=abc\nartifact_group=my-group\n");
	}

	{
		SCOPED_TRACE("Line number");
		data = R"({"artifact_group":"this-one", "rootfs-image.checksum":"abc"})";
		err = db.Write(context.artifact_provides_key, vector<uint8_t>(data.begin(), data.end()));
		data = "not-this-one";
		err = db.Write(context.artifact_group_key, vector<uint8_t>(data.begin(), data.end()));
		ASSERT_EQ(err, error::NoError) << err.String();
		verify("rootfs-image.checksum=abc\nartifact_group=this-one\n");
	}
}

TEST(CliTest, ShowProvidesErrors) {
	mtesting::TemporaryDirectory tmpdir;

	conf::MenderConfig conf;
	conf.paths.SetDataStore(tmpdir.Path());

	{
		mtesting::RedirectStreamOutputs redirect_output;
		vector<string> args {"--data", tmpdir.Path(), "show-provides", "--bogus-option"};
		EXPECT_EQ(cli::Main(args), 1);
		EXPECT_EQ(
			redirect_output.GetCerr(),
			"Could not fulfill request: Invalid options given: Unrecognized option '--bogus-option'\n");
	}

	{
		mtesting::RedirectStreamOutputs redirect_output;
		vector<string> args {"--data", tmpdir.Path(), "show-provides", "bogus-argument"};
		EXPECT_EQ(cli::Main(args), 1);
		EXPECT_EQ(
			redirect_output.GetCerr(),
			"Could not fulfill request: Invalid options given: Unexpected argument 'bogus-argument'\n");
	}
}

void SetTestDir(const string &dir, context::MenderContext &ctx) {
	ctx.GetConfig().paths.SetModulesPath(dir);
	ctx.GetConfig().paths.SetModulesWorkPath(dir);

	string scripts_dir = path::Join(dir, "scripts");
	ASSERT_EQ(path::CreateDirectories(scripts_dir), error::NoError);
	ctx.GetConfig().paths.SetArtScriptsPath(scripts_dir);
	ctx.GetConfig().paths.SetRootfsScriptsPath(scripts_dir);
}

bool PrepareSimpleArtifact(
	const string &tmpdir,
	const string &artifact,
	const string artifact_name = "test",
	bool legacy = false) {
	string payload = path::Join(tmpdir, "payload");
	string device_type = path::Join(tmpdir, "device_type");
	string update_module = path::Join(tmpdir, "rootfs-image");

	{
		ofstream f(payload);
		f << artifact_name << "\n";
		EXPECT_TRUE(f.good());
	}
	{
		ofstream f(device_type);
		f << "device_type=test\n";
		EXPECT_TRUE(f.good());
	}

	vector<string> args {
		"mender-artifact",
		"write",
		"rootfs-image",
		"--file",
		payload,
		"--device-type",
		"test",
		"--artifact-name",
		artifact_name,
		"-o",
		artifact,
	};
	if (legacy) {
		args.push_back("--no-checksum-provide");
		args.push_back("--no-default-clears-provides");
		args.push_back("--no-default-software-version");
	}
	processes::Process proc(args);
	auto err = proc.Run();
	EXPECT_EQ(err, error::NoError) << err.String();

	return !::testing::Test::HasFailure();
}

bool PrepareBootstrapArtifact(
	const string &tmpdir, const string &artifact, const string artifact_name = "test") {
	string device_type = path::Join(tmpdir, "device_type");

	{
		ofstream f(device_type);
		f << "device_type=test\n";
		EXPECT_TRUE(f.good());
	}

	vector<string> args {
		"mender-artifact",
		"write",
		"bootstrap-artifact",
		"--device-type",
		"test",
		"--artifact-name",
		artifact_name,
		"-o",
		artifact,
	};
	processes::Process proc(args);
	auto err = proc.Run();
	EXPECT_EQ(err, error::NoError) << err.String();

	return !::testing::Test::HasFailure();
}

bool InitDefaultProvides(const string &tmpdir) {
	string artifact = path::Join(tmpdir, "artifact.mender");
	EXPECT_TRUE(PrepareSimpleArtifact(tmpdir, artifact, "previous"));

	string update_module = path::Join(tmpdir, "rootfs-image");

	{
		ofstream f(update_module);
		f << R"(#!/bin/bash
exit 0
)";
		EXPECT_TRUE(f.good());
	}
	EXPECT_EQ(chmod(update_module.c_str(), 0755), 0);

	{
		vector<string> args {
			"--data",
			tmpdir,
			"install",
			artifact,
		};

		int exit_status =
			cli::Main(args, [&tmpdir](context::MenderContext &ctx) { SetTestDir(tmpdir, ctx); });
		EXPECT_EQ(exit_status, 0) << exit_status;
	}

	return !::testing::Test::HasFailure();
}

bool VerifyProvides(const string &tmpdir, const string &expected) {
	vector<string> args {
		"--data",
		tmpdir,
		"show-provides",
	};

	mtesting::RedirectStreamOutputs output;
	int exit_status =
		cli::Main(args, [&tmpdir](context::MenderContext &ctx) { SetTestDir(tmpdir, ctx); });
	EXPECT_EQ(exit_status, 0) << exit_status;

	EXPECT_EQ(output.GetCout(), expected);

	return !::testing::Test::HasFailure();
}

bool PrepareUpdateModule(const string &update_module, const string &content) {
	ofstream f(update_module);
	f << content;
	EXPECT_TRUE(f.good());
	EXPECT_EQ(chmod(update_module.c_str(), 0755), 0);

	return !::testing::Test::HasFailure();
}

TEST(CliTest, InstallAndCommitArtifact) {
	mtesting::TemporaryDirectory tmpdir;
	string artifact = path::Join(tmpdir.Path(), "artifact.mender");
	ASSERT_TRUE(PrepareSimpleArtifact(tmpdir.Path(), artifact));

	string update_module = path::Join(tmpdir.Path(), "rootfs-image");

	ASSERT_TRUE(PrepareUpdateModule(update_module, R"(#!/bin/bash

TEST_DIR=")" + tmpdir.Path() + R"("

echo "$1" >> $TEST_DIR/call.log

exit 0
)"));

	{
		vector<string> args {
			"--data",
			tmpdir.Path(),
			"install",
			artifact,
		};

		mtesting::RedirectStreamOutputs output;
		int exit_status = cli::Main(
			args, [&tmpdir](context::MenderContext &ctx) { SetTestDir(tmpdir.Path(), ctx); });
		EXPECT_EQ(exit_status, 0) << exit_status;

		EXPECT_EQ(output.GetCout(), R"(Installing artifact...
Update Module doesn't support rollback. Committing immediately.
Installed and committed.
)");
		EXPECT_EQ(output.GetCerr(), "");
	}

	EXPECT_TRUE(mtesting::FileContainsExactly(
		path::Join(tmpdir.Path(), "call.log"), R"(ProvidePayloadFileSizes
Download
ArtifactInstall
NeedsArtifactReboot
SupportsRollback
ArtifactCommit
Cleanup
)"));

	EXPECT_TRUE(VerifyProvides(tmpdir.Path(), R"(rootfs-image.version=test
rootfs-image.checksum=f2ca1bb6c7e907d06dafe4687e579fce76b37e4e93b7605022da52e6ccc26fd2
artifact_name=test
)"));
}

TEST(CliTest, DownloadWithFileSizesInstallAndCommitArtifact) {
	mtesting::TemporaryDirectory tmpdir;

	ASSERT_TRUE(InitDefaultProvides(tmpdir.Path()));

	string artifact = path::Join(tmpdir.Path(), "artifact.mender");
	ASSERT_TRUE(PrepareSimpleArtifact(tmpdir.Path(), artifact));

	string update_module = path::Join(tmpdir.Path(), "rootfs-image");

	ASSERT_TRUE(PrepareUpdateModule(update_module, R"(#!/bin/bash

TEST_DIR=")" + tmpdir.Path() + R"("

echo "$1" >> $TEST_DIR/call.log

case "$1" in
    ProvidePayloadFileSizes)
        echo "Yes"
        ;;
    Download)
        exit 1
        ;;
    DownloadWithFileSizes)
        exit 0
        ;;
esac

exit 0
)"));

	{
		vector<string> args {
			"--data",
			tmpdir.Path(),
			"install",
			artifact,
		};

		mtesting::RedirectStreamOutputs output;
		int exit_status = cli::Main(
			args, [&tmpdir](context::MenderContext &ctx) { SetTestDir(tmpdir.Path(), ctx); });
		EXPECT_EQ(exit_status, 0) << exit_status;

		EXPECT_EQ(output.GetCout(), R"(Installing artifact...
Update Module doesn't support rollback. Committing immediately.
Installed and committed.
)");
		EXPECT_EQ(output.GetCerr(), "");
	}

	EXPECT_TRUE(mtesting::FileContainsExactly(
		path::Join(tmpdir.Path(), "call.log"), R"(ProvidePayloadFileSizes
DownloadWithFileSizes
ArtifactInstall
NeedsArtifactReboot
SupportsRollback
ArtifactCommit
Cleanup
)"));


	EXPECT_TRUE(VerifyProvides(tmpdir.Path(), R"(rootfs-image.version=test
rootfs-image.checksum=f2ca1bb6c7e907d06dafe4687e579fce76b37e4e93b7605022da52e6ccc26fd2
artifact_name=test
)"));
}

TEST(CliTest, InstallAndThenCommitArtifact) {
	mtesting::TemporaryDirectory tmpdir;

	ASSERT_TRUE(InitDefaultProvides(tmpdir.Path()));

	string artifact = path::Join(tmpdir.Path(), "artifact.mender");
	ASSERT_TRUE(PrepareSimpleArtifact(tmpdir.Path(), artifact));

	string update_module = path::Join(tmpdir.Path(), "rootfs-image");

	ASSERT_TRUE(PrepareUpdateModule(update_module, R"(#!/bin/bash

TEST_DIR=")" + tmpdir.Path() + R"("

echo "$1" >> $TEST_DIR/call.log

case "$1" in
    SupportsRollback)
        echo "Yes"
        ;;
esac

exit 0
)"));

	{
		vector<string> args {
			"--data",
			tmpdir.Path(),
			"install",
			artifact,
		};

		mtesting::RedirectStreamOutputs output;
		int exit_status = cli::Main(
			args, [&tmpdir](context::MenderContext &ctx) { SetTestDir(tmpdir.Path(), ctx); });
		EXPECT_EQ(exit_status, 0) << exit_status;

		EXPECT_EQ(output.GetCout(), R"(Installing artifact...
Installed, but not committed.
Use 'commit' to update, or 'rollback' to roll back the update.
)");
		EXPECT_EQ(output.GetCerr(), "");
	}

	EXPECT_TRUE(mtesting::FileContainsExactly(
		path::Join(tmpdir.Path(), "call.log"), R"(ProvidePayloadFileSizes
Download
ArtifactInstall
NeedsArtifactReboot
SupportsRollback
)"));

	{
		vector<string> args {
			"--data",
			tmpdir.Path(),
			"commit",
		};

		mtesting::RedirectStreamOutputs output;
		int exit_status = cli::Main(
			args, [&tmpdir](context::MenderContext &ctx) { SetTestDir(tmpdir.Path(), ctx); });
		EXPECT_EQ(exit_status, 0) << exit_status;

		EXPECT_EQ(output.GetCout(), R"(Committed.
)");
		EXPECT_EQ(output.GetCerr(), "");
	}

	EXPECT_TRUE(mtesting::FileContainsExactly(
		path::Join(tmpdir.Path(), "call.log"), R"(ProvidePayloadFileSizes
Download
ArtifactInstall
NeedsArtifactReboot
SupportsRollback
ArtifactCommit
Cleanup
)"));

	EXPECT_TRUE(VerifyProvides(tmpdir.Path(), R"(rootfs-image.version=test
rootfs-image.checksum=f2ca1bb6c7e907d06dafe4687e579fce76b37e4e93b7605022da52e6ccc26fd2
artifact_name=test
)"));
}

TEST(CliTest, InstallAndThenRollBackArtifact) {
	mtesting::TemporaryDirectory tmpdir;

	ASSERT_TRUE(InitDefaultProvides(tmpdir.Path()));

	string artifact = path::Join(tmpdir.Path(), "artifact.mender");
	ASSERT_TRUE(PrepareSimpleArtifact(tmpdir.Path(), artifact));

	string update_module = path::Join(tmpdir.Path(), "rootfs-image");

	ASSERT_TRUE(PrepareUpdateModule(update_module, R"(#!/bin/bash

TEST_DIR=")" + tmpdir.Path() + R"("

echo "$1" >> $TEST_DIR/call.log

case "$1" in
    SupportsRollback)
        echo "Yes"
        ;;
esac

exit 0
)"));

	{
		vector<string> args {
			"--data",
			tmpdir.Path(),
			"install",
			artifact,
		};

		mtesting::RedirectStreamOutputs output;
		int exit_status = cli::Main(
			args, [&tmpdir](context::MenderContext &ctx) { SetTestDir(tmpdir.Path(), ctx); });
		EXPECT_EQ(exit_status, 0) << exit_status;

		EXPECT_EQ(output.GetCout(), R"(Installing artifact...
Installed, but not committed.
Use 'commit' to update, or 'rollback' to roll back the update.
)");
		EXPECT_EQ(output.GetCerr(), "");
	}

	EXPECT_TRUE(mtesting::FileContainsExactly(
		path::Join(tmpdir.Path(), "call.log"), R"(ProvidePayloadFileSizes
Download
ArtifactInstall
NeedsArtifactReboot
SupportsRollback
)"));

	{
		vector<string> args {
			"--data",
			tmpdir.Path(),
			"rollback",
		};

		mtesting::RedirectStreamOutputs output;
		int exit_status = cli::Main(
			args, [&tmpdir](context::MenderContext &ctx) { SetTestDir(tmpdir.Path(), ctx); });
		EXPECT_EQ(exit_status, 0) << exit_status;

		EXPECT_EQ(output.GetCout(), R"(Rolled back.
)");
		EXPECT_EQ(output.GetCerr(), "");
	}

	EXPECT_TRUE(mtesting::FileContainsExactly(
		path::Join(tmpdir.Path(), "call.log"), R"(ProvidePayloadFileSizes
Download
ArtifactInstall
NeedsArtifactReboot
SupportsRollback
SupportsRollback
ArtifactRollback
Cleanup
)"));

	EXPECT_TRUE(VerifyProvides(tmpdir.Path(), R"(rootfs-image.version=previous
rootfs-image.checksum=46ca895be3a18fb50c1c6b5a3bd2e97fb637b35a22924c2f3dea3cf09e9e2e74
artifact_name=previous
)"));
}

TEST(CliTest, RollbackAfterFailure) {
	mtesting::TemporaryDirectory tmpdir;

	ASSERT_TRUE(InitDefaultProvides(tmpdir.Path()));

	string artifact = path::Join(tmpdir.Path(), "artifact.mender");
	ASSERT_TRUE(PrepareSimpleArtifact(tmpdir.Path(), artifact));

	string update_module = path::Join(tmpdir.Path(), "rootfs-image");

	ASSERT_TRUE(PrepareUpdateModule(update_module, R"(#!/bin/bash

TEST_DIR=")" + tmpdir.Path() + R"("

echo "$1" >> $TEST_DIR/call.log

case "$1" in
    ArtifactInstall)
        exit 1
        ;;
    SupportsRollback)
        echo "Yes"
        ;;
esac

exit 0
)"));

	{
		vector<string> args {
			"--data",
			tmpdir.Path(),
			"install",
			artifact,
		};

		mtesting::RedirectStreamOutputs output;
		int exit_status = cli::Main(
			args, [&tmpdir](context::MenderContext &ctx) { SetTestDir(tmpdir.Path(), ctx); });
		EXPECT_EQ(exit_status, 1) << exit_status;

		EXPECT_EQ(output.GetCout(), R"(Installing artifact...
Installation failed. Rolled back modifications.
)");
		EXPECT_TRUE(VerifyOnlyMessages(
			output.GetCerr(),
			{"Installation failed: Process returned non-zero exit status: ArtifactInstall: Process exited with status 1"}));
	}

	EXPECT_TRUE(mtesting::FileContainsExactly(
		path::Join(tmpdir.Path(), "call.log"), R"(ProvidePayloadFileSizes
Download
ArtifactInstall
SupportsRollback
ArtifactRollback
ArtifactFailure
Cleanup
)"));


	EXPECT_TRUE(VerifyProvides(tmpdir.Path(), R"(rootfs-image.version=previous
rootfs-image.checksum=46ca895be3a18fb50c1c6b5a3bd2e97fb637b35a22924c2f3dea3cf09e9e2e74
artifact_name=previous
)"));
}

TEST(CliTest, RollbackAfterFailureInDownload) {
	mtesting::TemporaryDirectory tmpdir;

	ASSERT_TRUE(InitDefaultProvides(tmpdir.Path()));

	string artifact = path::Join(tmpdir.Path(), "artifact.mender");
	ASSERT_TRUE(PrepareSimpleArtifact(tmpdir.Path(), artifact));

	string update_module = path::Join(tmpdir.Path(), "rootfs-image");

	ASSERT_TRUE(PrepareUpdateModule(update_module, R"(#!/bin/bash

TEST_DIR=")" + tmpdir.Path() + R"("

echo "$1" >> $TEST_DIR/call.log

case "$1" in
    Download)
        exit 1
        ;;
    SupportsRollback)
        echo "Yes"
        ;;
esac

exit 0
)"));

	{
		vector<string> args {
			"--data",
			tmpdir.Path(),
			"install",
			artifact,
		};

		mtesting::RedirectStreamOutputs output;
		int exit_status = cli::Main(
			args, [&tmpdir](context::MenderContext &ctx) { SetTestDir(tmpdir.Path(), ctx); });
		EXPECT_EQ(exit_status, 1) << exit_status;

		EXPECT_EQ(output.GetCout(), R"(Installing artifact...
Installation failed. System not modified.
)");
		EXPECT_THAT(
			output.GetCerr(),
			testing::EndsWith(
				"Update Module returned non-zero status: Process exited with status 1\n"));
	}

	EXPECT_TRUE(mtesting::FileContainsExactly(
		path::Join(tmpdir.Path(), "call.log"), R"(ProvidePayloadFileSizes
Download
Cleanup
)"));


	EXPECT_TRUE(VerifyProvides(tmpdir.Path(), R"(rootfs-image.version=previous
rootfs-image.checksum=46ca895be3a18fb50c1c6b5a3bd2e97fb637b35a22924c2f3dea3cf09e9e2e74
artifact_name=previous
)"));
}

TEST(CliTest, FailedRollbackAfterFailure) {
	mtesting::TemporaryDirectory tmpdir;
	string artifact = path::Join(tmpdir.Path(), "artifact.mender");
	ASSERT_TRUE(PrepareSimpleArtifact(tmpdir.Path(), artifact));

	string update_module = path::Join(tmpdir.Path(), "rootfs-image");

	ASSERT_TRUE(PrepareUpdateModule(update_module, R"(#!/bin/bash

TEST_DIR=")" + tmpdir.Path() + R"("

echo "$1" >> $TEST_DIR/call.log

case "$1" in
    ArtifactInstall)
        exit 1
        ;;
    ArtifactRollback)
        exit 1
        ;;
    SupportsRollback)
        echo "Yes"
        ;;
esac

exit 0
)"));

	{
		vector<string> args {
			"--data",
			tmpdir.Path(),
			"install",
			artifact,
		};

		mtesting::RedirectStreamOutputs output;
		int exit_status = cli::Main(
			args, [&tmpdir](context::MenderContext &ctx) { SetTestDir(tmpdir.Path(), ctx); });
		EXPECT_EQ(exit_status, 1) << exit_status;

		EXPECT_EQ(output.GetCout(), R"(Installing artifact...
Installation failed, and rollback also failed. System may be in an inconsistent state.
)");
	}

	EXPECT_TRUE(mtesting::FileContainsExactly(
		path::Join(tmpdir.Path(), "call.log"), R"(ProvidePayloadFileSizes
Download
ArtifactInstall
SupportsRollback
ArtifactRollback
ArtifactFailure
Cleanup
)"));

	EXPECT_TRUE(VerifyProvides(tmpdir.Path(), R"(rootfs-image.version=test
rootfs-image.checksum=f2ca1bb6c7e907d06dafe4687e579fce76b37e4e93b7605022da52e6ccc26fd2
artifact_name=test_INCONSISTENT
)"));
}

TEST(CliTest, NoRollbackAfterFailure) {
	mtesting::TemporaryDirectory tmpdir;
	string artifact = path::Join(tmpdir.Path(), "artifact.mender");
	ASSERT_TRUE(PrepareSimpleArtifact(tmpdir.Path(), artifact));

	string update_module = path::Join(tmpdir.Path(), "rootfs-image");

	ASSERT_TRUE(PrepareUpdateModule(update_module, R"(#!/bin/bash

TEST_DIR=")" + tmpdir.Path() + R"("

echo "$1" >> $TEST_DIR/call.log

case "$1" in
    ArtifactInstall)
        exit 1
        ;;
    SupportsRollback)
        echo "No"
        ;;
esac

exit 0
)"));

	{
		vector<string> args {
			"--data",
			tmpdir.Path(),
			"install",
			artifact,
		};

		mtesting::RedirectStreamOutputs output;
		int exit_status = cli::Main(
			args, [&tmpdir](context::MenderContext &ctx) { SetTestDir(tmpdir.Path(), ctx); });
		EXPECT_EQ(exit_status, 1) << exit_status;

		EXPECT_EQ(output.GetCout(), R"(Installing artifact...
Installation failed, and Update Module does not support rollback. System may be in an inconsistent state.
)");
	}

	EXPECT_TRUE(mtesting::FileContainsExactly(
		path::Join(tmpdir.Path(), "call.log"), R"(ProvidePayloadFileSizes
Download
ArtifactInstall
SupportsRollback
ArtifactFailure
Cleanup
)"));

	EXPECT_TRUE(VerifyProvides(tmpdir.Path(), R"(rootfs-image.version=test
rootfs-image.checksum=f2ca1bb6c7e907d06dafe4687e579fce76b37e4e93b7605022da52e6ccc26fd2
artifact_name=test_INCONSISTENT
)"));

	// Also, make sure we can fix it with a new update.

	ASSERT_TRUE(PrepareUpdateModule(update_module, R"(#!/bin/bash
exit 0
)"));

	{
		vector<string> args {
			"--data",
			tmpdir.Path(),
			"install",
			artifact,
		};

		mtesting::RedirectStreamOutputs output;
		int exit_status = cli::Main(
			args, [&tmpdir](context::MenderContext &ctx) { SetTestDir(tmpdir.Path(), ctx); });
		EXPECT_EQ(exit_status, 0) << exit_status;

		EXPECT_EQ(output.GetCout(), R"(Installing artifact...
Update Module doesn't support rollback. Committing immediately.
Installed and committed.
)");
	}

	EXPECT_TRUE(VerifyProvides(tmpdir.Path(), R"(rootfs-image.version=test
rootfs-image.checksum=f2ca1bb6c7e907d06dafe4687e579fce76b37e4e93b7605022da52e6ccc26fd2
artifact_name=test
)"));
}

TEST(CliTest, CommitNoExistingUpdate) {
	mtesting::TemporaryDirectory tmpdir;

	ASSERT_TRUE(InitDefaultProvides(tmpdir.Path()));

	{
		vector<string> args {
			"--data",
			tmpdir.Path(),
			"commit",
		};

		mtesting::RedirectStreamOutputs output;
		int exit_status = cli::Main(
			args, [&tmpdir](context::MenderContext &ctx) { SetTestDir(tmpdir.Path(), ctx); });
		EXPECT_EQ(exit_status, 2) << exit_status;

		EXPECT_EQ(output.GetCout(), R"(No update in progress.
)");
		EXPECT_TRUE(VerifyOnlyMessages(output.GetCerr(), {"No update in progress: Cannot commit"}));
	}

	EXPECT_TRUE(VerifyProvides(tmpdir.Path(), R"(rootfs-image.version=previous
rootfs-image.checksum=46ca895be3a18fb50c1c6b5a3bd2e97fb637b35a22924c2f3dea3cf09e9e2e74
artifact_name=previous
)"));
}

TEST(CliTest, TryToRollBackWithoutSupport) {
	// This case is pretty unlikely, since it requires an Update Module to *lose* its rollback
	// capability. Still it's there as a possible error, so let's get the code coverage!

	mtesting::TemporaryDirectory tmpdir;

	ASSERT_TRUE(InitDefaultProvides(tmpdir.Path()));

	string artifact = path::Join(tmpdir.Path(), "artifact.mender");
	ASSERT_TRUE(PrepareSimpleArtifact(tmpdir.Path(), artifact));

	string update_module = path::Join(tmpdir.Path(), "rootfs-image");

	ASSERT_TRUE(PrepareUpdateModule(update_module, R"(#!/bin/bash

TEST_DIR=")" + tmpdir.Path() + R"("

echo "$1" >> $TEST_DIR/call.log

case "$1" in
    SupportsRollback)
        echo "Yes"
        ;;
esac

exit 0
)"));

	{
		vector<string> args {
			"--data",
			tmpdir.Path(),
			"install",
			artifact,
		};

		mtesting::RedirectStreamOutputs output;
		int exit_status = cli::Main(
			args, [&tmpdir](context::MenderContext &ctx) { SetTestDir(tmpdir.Path(), ctx); });
		EXPECT_EQ(exit_status, 0) << exit_status;

		EXPECT_EQ(output.GetCout(), R"(Installing artifact...
Installed, but not committed.
Use 'commit' to update, or 'rollback' to roll back the update.
)");
		EXPECT_EQ(output.GetCerr(), "");
	}

	EXPECT_TRUE(mtesting::FileContainsExactly(
		path::Join(tmpdir.Path(), "call.log"), R"(ProvidePayloadFileSizes
Download
ArtifactInstall
NeedsArtifactReboot
SupportsRollback
)"));

	ASSERT_TRUE(PrepareUpdateModule(update_module, R"(#!/bin/bash

TEST_DIR=")" + tmpdir.Path() + R"("

echo "$1" >> $TEST_DIR/call.log

exit 0
)"));

	{
		vector<string> args {
			"--data",
			tmpdir.Path(),
			"rollback",
		};

		mtesting::RedirectStreamOutputs output;
		int exit_status = cli::Main(
			args, [&tmpdir](context::MenderContext &ctx) { SetTestDir(tmpdir.Path(), ctx); });
		EXPECT_EQ(exit_status, 1) << exit_status;

		EXPECT_EQ(output.GetCout(), R"(Update Module does not support rollback.
)");
		EXPECT_EQ(output.GetCerr(), "");
	}

	EXPECT_TRUE(mtesting::FileContainsExactly(
		path::Join(tmpdir.Path(), "call.log"), R"(ProvidePayloadFileSizes
Download
ArtifactInstall
NeedsArtifactReboot
SupportsRollback
SupportsRollback
)"));

	EXPECT_TRUE(VerifyProvides(tmpdir.Path(), R"(rootfs-image.version=previous
rootfs-image.checksum=46ca895be3a18fb50c1c6b5a3bd2e97fb637b35a22924c2f3dea3cf09e9e2e74
artifact_name=previous
)"));
}

TEST(CliTest, InstallWithRebootRequiredNoArgument) {
	mtesting::TemporaryDirectory tmpdir;
	string artifact = path::Join(tmpdir.Path(), "artifact.mender");
	ASSERT_TRUE(PrepareSimpleArtifact(tmpdir.Path(), artifact));

	string update_module = path::Join(tmpdir.Path(), "rootfs-image");

	ASSERT_TRUE(PrepareUpdateModule(update_module, R"(#!/bin/bash

TEST_DIR=")" + tmpdir.Path() + R"("

echo "$1" >> $TEST_DIR/call.log

case "$1" in
    NeedsArtifactReboot)
        echo "Automatic"
        ;;
esac

exit 0
)"));

	{
		vector<string> args {
			"--data",
			tmpdir.Path(),
			"install",
			artifact,
		};

		mtesting::RedirectStreamOutputs output;
		int exit_status = cli::Main(
			args, [&tmpdir](context::MenderContext &ctx) { SetTestDir(tmpdir.Path(), ctx); });
		EXPECT_EQ(exit_status, 0) << exit_status;

		EXPECT_EQ(output.GetCout(), R"(Installing artifact...
Update Module doesn't support rollback. Committing immediately.
Installed and committed.
At least one payload requested a reboot of the device it updated.
)");
		EXPECT_EQ(output.GetCerr(), "");
	}

	EXPECT_TRUE(mtesting::FileContainsExactly(
		path::Join(tmpdir.Path(), "call.log"), R"(ProvidePayloadFileSizes
Download
ArtifactInstall
NeedsArtifactReboot
SupportsRollback
ArtifactCommit
Cleanup
)"));

	EXPECT_TRUE(VerifyProvides(tmpdir.Path(), R"(rootfs-image.version=test
rootfs-image.checksum=f2ca1bb6c7e907d06dafe4687e579fce76b37e4e93b7605022da52e6ccc26fd2
artifact_name=test
)"));
}

TEST(CliTest, InstallWithRebootRequiredWithArgument) {
	mtesting::TemporaryDirectory tmpdir;
	string artifact = path::Join(tmpdir.Path(), "artifact.mender");
	ASSERT_TRUE(PrepareSimpleArtifact(tmpdir.Path(), artifact));

	string update_module = path::Join(tmpdir.Path(), "rootfs-image");

	ASSERT_TRUE(PrepareUpdateModule(update_module, R"(#!/bin/bash

TEST_DIR=")" + tmpdir.Path() + R"("

echo "$1" >> $TEST_DIR/call.log

case "$1" in
    NeedsArtifactReboot)
        echo "Automatic"
        ;;
esac

exit 0
)"));

	{
		vector<string> args {
			"--data",
			tmpdir.Path(),
			"install",
			artifact,
			"--reboot-exit-code",
		};

		mtesting::RedirectStreamOutputs output;
		int exit_status = cli::Main(
			args, [&tmpdir](context::MenderContext &ctx) { SetTestDir(tmpdir.Path(), ctx); });
		EXPECT_EQ(exit_status, 4) << exit_status;

		EXPECT_EQ(output.GetCout(), R"(Installing artifact...
Update Module doesn't support rollback. Committing immediately.
Installed and committed.
At least one payload requested a reboot of the device it updated.
)");
		EXPECT_EQ(output.GetCerr(), "");
	}

	EXPECT_TRUE(mtesting::FileContainsExactly(
		path::Join(tmpdir.Path(), "call.log"), R"(ProvidePayloadFileSizes
Download
ArtifactInstall
NeedsArtifactReboot
SupportsRollback
ArtifactCommit
Cleanup
)"));

	EXPECT_TRUE(VerifyProvides(tmpdir.Path(), R"(rootfs-image.version=test
rootfs-image.checksum=f2ca1bb6c7e907d06dafe4687e579fce76b37e4e93b7605022da52e6ccc26fd2
artifact_name=test
)"));
}

TEST(CliTest, InstallWhenUpdateInProgress) {
	mtesting::TemporaryDirectory tmpdir;

	ASSERT_TRUE(InitDefaultProvides(tmpdir.Path()));

	string artifact = path::Join(tmpdir.Path(), "artifact.mender");
	ASSERT_TRUE(PrepareSimpleArtifact(tmpdir.Path(), artifact));

	string update_module = path::Join(tmpdir.Path(), "rootfs-image");

	ASSERT_TRUE(PrepareUpdateModule(update_module, R"(#!/bin/bash

TEST_DIR=")" + tmpdir.Path() + R"("

echo "$1" >> $TEST_DIR/call.log

case "$1" in
    SupportsRollback)
        echo "Yes"
        ;;
esac

exit 0
)"));

	vector<string> args {
		"--data",
		tmpdir.Path(),
		"install",
		artifact,
	};

	{
		mtesting::RedirectStreamOutputs output;
		int exit_status = cli::Main(
			args, [&tmpdir](context::MenderContext &ctx) { SetTestDir(tmpdir.Path(), ctx); });
		EXPECT_EQ(exit_status, 0) << exit_status;

		EXPECT_EQ(output.GetCout(), R"(Installing artifact...
Installed, but not committed.
Use 'commit' to update, or 'rollback' to roll back the update.
)");
		EXPECT_EQ(output.GetCerr(), "");
	}

	EXPECT_TRUE(mtesting::FileContainsExactly(
		path::Join(tmpdir.Path(), "call.log"), R"(ProvidePayloadFileSizes
Download
ArtifactInstall
NeedsArtifactReboot
SupportsRollback
)"));

	{
		mtesting::RedirectStreamOutputs output;
		int exit_status = cli::Main(
			args, [&tmpdir](context::MenderContext &ctx) { SetTestDir(tmpdir.Path(), ctx); });
		EXPECT_EQ(exit_status, 1) << exit_status;

		EXPECT_EQ(output.GetCout(), R"(Installation failed. System not modified.
)");
		EXPECT_THAT(
			output.GetCerr(),
			testing::EndsWith("Update already in progress. Please commit or roll back first\n"));
	}

	EXPECT_TRUE(mtesting::FileContainsExactly(
		path::Join(tmpdir.Path(), "call.log"), R"(ProvidePayloadFileSizes
Download
ArtifactInstall
NeedsArtifactReboot
SupportsRollback
)"));
}

TEST(CliTest, InstallAndThenFailRollBack) {
	mtesting::TemporaryDirectory tmpdir;

	ASSERT_TRUE(InitDefaultProvides(tmpdir.Path()));

	string artifact = path::Join(tmpdir.Path(), "artifact.mender");
	ASSERT_TRUE(PrepareSimpleArtifact(tmpdir.Path(), artifact));

	string update_module = path::Join(tmpdir.Path(), "rootfs-image");

	ASSERT_TRUE(PrepareUpdateModule(update_module, R"(#!/bin/bash

TEST_DIR=")" + tmpdir.Path() + R"("

echo "$1" >> $TEST_DIR/call.log

case "$1" in
    ArtifactRollback)
        exit 1
        ;;
    SupportsRollback)
        echo "Yes"
        ;;
esac

exit 0
)"));

	{
		vector<string> args {
			"--data",
			tmpdir.Path(),
			"install",
			artifact,
		};

		mtesting::RedirectStreamOutputs output;
		int exit_status = cli::Main(
			args, [&tmpdir](context::MenderContext &ctx) { SetTestDir(tmpdir.Path(), ctx); });
		EXPECT_EQ(exit_status, 0) << exit_status;

		EXPECT_EQ(output.GetCout(), R"(Installing artifact...
Installed, but not committed.
Use 'commit' to update, or 'rollback' to roll back the update.
)");
		EXPECT_EQ(output.GetCerr(), "");
	}

	EXPECT_TRUE(mtesting::FileContainsExactly(
		path::Join(tmpdir.Path(), "call.log"), R"(ProvidePayloadFileSizes
Download
ArtifactInstall
NeedsArtifactReboot
SupportsRollback
)"));

	{
		vector<string> args {
			"--data",
			tmpdir.Path(),
			"rollback",
		};

		mtesting::RedirectStreamOutputs output;
		int exit_status = cli::Main(
			args, [&tmpdir](context::MenderContext &ctx) { SetTestDir(tmpdir.Path(), ctx); });
		EXPECT_EQ(exit_status, 1) << exit_status;

		EXPECT_EQ(output.GetCout(), R"(Rollback failed. System may be in an inconsistent state.
)");
		EXPECT_THAT(
			output.GetCerr(),
			testing::EndsWith(
				"Process returned non-zero exit status: ArtifactRollback: Process exited with status 1\n"));
	}

	EXPECT_TRUE(mtesting::FileContainsExactly(
		path::Join(tmpdir.Path(), "call.log"), R"(ProvidePayloadFileSizes
Download
ArtifactInstall
NeedsArtifactReboot
SupportsRollback
SupportsRollback
ArtifactRollback
Cleanup
)"));

	EXPECT_TRUE(VerifyProvides(tmpdir.Path(), R"(rootfs-image.version=test
rootfs-image.checksum=f2ca1bb6c7e907d06dafe4687e579fce76b37e4e93b7605022da52e6ccc26fd2
artifact_name=test_INCONSISTENT
)"));

	{
		vector<string> args {
			"--data",
			tmpdir.Path(),
			"rollback",
		};

		mtesting::RedirectStreamOutputs output;
		int exit_status = cli::Main(
			args, [&tmpdir](context::MenderContext &ctx) { SetTestDir(tmpdir.Path(), ctx); });
		EXPECT_EQ(exit_status, 2) << exit_status;

		EXPECT_EQ(output.GetCout(), R"(No update in progress.
)");
		EXPECT_TRUE(
			VerifyOnlyMessages(output.GetCerr(), {"No update in progress: Cannot roll back"}));
	}
}

TEST(CliTest, InstallAndFailCleanup) {
	mtesting::TemporaryDirectory tmpdir;
	string artifact = path::Join(tmpdir.Path(), "artifact.mender");
	ASSERT_TRUE(PrepareSimpleArtifact(tmpdir.Path(), artifact));

	string update_module = path::Join(tmpdir.Path(), "rootfs-image");

	ASSERT_TRUE(PrepareUpdateModule(update_module, R"(#!/bin/bash

TEST_DIR=")" + tmpdir.Path() + R"("

echo "$1" >> $TEST_DIR/call.log

case "$1" in
    Cleanup)
        exit 1
        ;;
esac

exit 0
)"));

	{
		vector<string> args {
			"--data",
			tmpdir.Path(),
			"install",
			artifact,
		};

		mtesting::RedirectStreamOutputs output;
		int exit_status = cli::Main(
			args, [&tmpdir](context::MenderContext &ctx) { SetTestDir(tmpdir.Path(), ctx); });
		EXPECT_EQ(exit_status, 1) << exit_status;

		EXPECT_EQ(output.GetCout(), R"(Installing artifact...
Update Module doesn't support rollback. Committing immediately.
Installed, but one or more post-commit steps failed.
)");
		EXPECT_THAT(
			output.GetCerr(),
			testing::EndsWith(
				"Process returned non-zero exit status: Cleanup: Process exited with status 1\n"));
	}

	EXPECT_TRUE(mtesting::FileContainsExactly(
		path::Join(tmpdir.Path(), "call.log"), R"(ProvidePayloadFileSizes
Download
ArtifactInstall
NeedsArtifactReboot
SupportsRollback
ArtifactCommit
Cleanup
)"));

	EXPECT_TRUE(VerifyProvides(tmpdir.Path(), R"(rootfs-image.version=test
rootfs-image.checksum=f2ca1bb6c7e907d06dafe4687e579fce76b37e4e93b7605022da52e6ccc26fd2
artifact_name=test
)"));
}

TEST(CliTest, FailureInArtifactFailure) {
	mtesting::TemporaryDirectory tmpdir;
	string artifact = path::Join(tmpdir.Path(), "artifact.mender");
	ASSERT_TRUE(PrepareSimpleArtifact(tmpdir.Path(), artifact));

	string update_module = path::Join(tmpdir.Path(), "rootfs-image");

	ASSERT_TRUE(PrepareUpdateModule(update_module, R"(#!/bin/bash

TEST_DIR=")" + tmpdir.Path() + R"("

echo "$1" >> $TEST_DIR/call.log

case "$1" in
    ArtifactInstall)
        exit 1
        ;;
    ArtifactFailure)
        exit 1
        ;;
    SupportsRollback)
        echo "Yes"
        ;;
esac

exit 0
)"));

	{
		vector<string> args {
			"--data",
			tmpdir.Path(),
			"install",
			artifact,
		};

		mtesting::RedirectStreamOutputs output;
		int exit_status = cli::Main(
			args, [&tmpdir](context::MenderContext &ctx) { SetTestDir(tmpdir.Path(), ctx); });
		EXPECT_EQ(exit_status, 1) << exit_status;

		EXPECT_EQ(output.GetCout(), R"(Installing artifact...
Installation failed, and rollback also failed. System may be in an inconsistent state.
)");
	}

	EXPECT_TRUE(mtesting::FileContainsExactly(
		path::Join(tmpdir.Path(), "call.log"), R"(ProvidePayloadFileSizes
Download
ArtifactInstall
SupportsRollback
ArtifactRollback
ArtifactFailure
Cleanup
)"));

	EXPECT_TRUE(VerifyProvides(tmpdir.Path(), R"(rootfs-image.version=test
rootfs-image.checksum=f2ca1bb6c7e907d06dafe4687e579fce76b37e4e93b7605022da52e6ccc26fd2
artifact_name=test_INCONSISTENT
)"));
}

TEST(CliTest, InvalidInstallArguments) {
	{
		vector<string> args {"install", "artifact1", "artifact2"};

		mtesting::RedirectStreamOutputs output;
		int exit_status = cli::Main(args);
		EXPECT_EQ(exit_status, 1) << exit_status;

		EXPECT_THAT(output.GetCerr(), testing::EndsWith("Too many arguments: artifact2\n"));
	}

	{
		vector<string> args {
			"install",
		};

		mtesting::RedirectStreamOutputs output;
		int exit_status = cli::Main(args);
		EXPECT_EQ(exit_status, 1) << exit_status;

		EXPECT_THAT(output.GetCerr(), testing::EndsWith("Need a path to an artifact\n"));
	}

	{
		vector<string> args {"install", "--bogus"};

		mtesting::RedirectStreamOutputs output;
		int exit_status = cli::Main(args);
		EXPECT_EQ(exit_status, 1) << exit_status;

		EXPECT_THAT(output.GetCerr(), testing::EndsWith("Unrecognized option '--bogus'\n"));
	}
}

TEST(CliTest, InstallAndThenCommitLegacyArtifact) {
	mtesting::TemporaryDirectory tmpdir;
	string artifact = path::Join(tmpdir.Path(), "artifact.mender");
	ASSERT_TRUE(PrepareSimpleArtifact(tmpdir.Path(), artifact, "test", true));

	string update_module = path::Join(tmpdir.Path(), "rootfs-image");

	ASSERT_TRUE(PrepareUpdateModule(update_module, R"(#!/bin/bash

TEST_DIR=")" + tmpdir.Path() + R"("

echo "$1" >> $TEST_DIR/call.log

case "$1" in
    SupportsRollback)
        echo "Yes"
        ;;
esac

exit 0
)"));

	{
		vector<string> args {
			"--data",
			tmpdir.Path(),
			"install",
			artifact,
		};

		mtesting::RedirectStreamOutputs output;
		int exit_status = cli::Main(
			args, [&tmpdir](context::MenderContext &ctx) { SetTestDir(tmpdir.Path(), ctx); });
		EXPECT_EQ(exit_status, 0) << exit_status;

		EXPECT_EQ(output.GetCout(), R"(Installing artifact...
Installed, but not committed.
Use 'commit' to update, or 'rollback' to roll back the update.
)");
		EXPECT_EQ(output.GetCerr(), "");
	}

	EXPECT_TRUE(mtesting::FileContainsExactly(
		path::Join(tmpdir.Path(), "call.log"), R"(ProvidePayloadFileSizes
Download
ArtifactInstall
NeedsArtifactReboot
SupportsRollback
)"));

	{
		vector<string> args {
			"--data",
			tmpdir.Path(),
			"commit",
		};

		mtesting::RedirectStreamOutputs output;
		int exit_status = cli::Main(
			args, [&tmpdir](context::MenderContext &ctx) { SetTestDir(tmpdir.Path(), ctx); });
		EXPECT_EQ(exit_status, 0) << exit_status;

		EXPECT_EQ(output.GetCout(), R"(Committed.
)");
		EXPECT_EQ(output.GetCerr(), "");
	}

	EXPECT_TRUE(mtesting::FileContainsExactly(
		path::Join(tmpdir.Path(), "call.log"), R"(ProvidePayloadFileSizes
Download
ArtifactInstall
NeedsArtifactReboot
SupportsRollback
ArtifactCommit
Cleanup
)"));

	EXPECT_TRUE(VerifyProvides(tmpdir.Path(), R"(artifact_name=test
)"));
}

TEST(CliTest, InstallUsingOldClientAndThenCommitArtifact) {
	mtesting::TemporaryDirectory tmpdir;
	string workdir = path::Join(tmpdir.Path(), "work");

	ASSERT_TRUE(InitDefaultProvides(tmpdir.Path()));

	string artifact = path::Join(tmpdir.Path(), "artifact.mender");
	ASSERT_TRUE(PrepareSimpleArtifact(tmpdir.Path(), artifact));

	string update_module = path::Join(tmpdir.Path(), "rootfs-image");

	ASSERT_TRUE(PrepareUpdateModule(update_module, R"(#!/bin/bash

TEST_DIR=")" + tmpdir.Path() + R"("

echo "$1" >> $TEST_DIR/call.log

case "$1" in
    SupportsRollback)
        echo "Yes"
        ;;
esac

exit 0
)"));

	{
		vector<string> args {
			"--data",
			tmpdir.Path(),
			"install",
			artifact,
		};

		mtesting::RedirectStreamOutputs output;
		int exit_status = cli::Main(args, [&tmpdir, &workdir](context::MenderContext &ctx) {
			ctx.GetConfig().paths.SetModulesPath(tmpdir.Path());
			ctx.GetConfig().paths.SetModulesWorkPath(workdir);
		});
		EXPECT_EQ(exit_status, 0) << exit_status;

		EXPECT_EQ(output.GetCout(), R"(Installing artifact...
Installed, but not committed.
Use 'commit' to update, or 'rollback' to roll back the update.
)");
		EXPECT_EQ(output.GetCerr(), "");
	}

	EXPECT_TRUE(mtesting::FileContainsExactly(
		path::Join(tmpdir.Path(), "call.log"), R"(ProvidePayloadFileSizes
Download
ArtifactInstall
NeedsArtifactReboot
SupportsRollback
)"));

	// Remove the Update Module working directory. This is what would have happened if upgrading
	// from a version < 4.0.
	std::error_code ec;
	ASSERT_TRUE(std::filesystem::remove_all(workdir, ec));

	{
		vector<string> args {
			"--data",
			tmpdir.Path(),
			"commit",
		};

		mtesting::RedirectStreamOutputs output;
		int exit_status = cli::Main(args, [&tmpdir, &workdir](context::MenderContext &ctx) {
			ctx.GetConfig().paths.SetModulesPath(tmpdir.Path());
			ctx.GetConfig().paths.SetModulesWorkPath(workdir);
		});
		EXPECT_EQ(exit_status, 0) << exit_status;

		EXPECT_EQ(output.GetCout(), R"(Committed.
)");
		EXPECT_EQ(output.GetCerr(), "");
	}

	EXPECT_TRUE(mtesting::FileContainsExactly(
		path::Join(tmpdir.Path(), "call.log"), R"(ProvidePayloadFileSizes
Download
ArtifactInstall
NeedsArtifactReboot
SupportsRollback
ArtifactCommit
Cleanup
)"));

	EXPECT_TRUE(VerifyProvides(tmpdir.Path(), R"(rootfs-image.version=test
rootfs-image.checksum=f2ca1bb6c7e907d06dafe4687e579fce76b37e4e93b7605022da52e6ccc26fd2
artifact_name=test
)"));
}

TEST(CliTest, InstallUsingOldClientAndThenRollBackArtifact) {
	mtesting::TemporaryDirectory tmpdir;
	string workdir = path::Join(tmpdir.Path(), "work");

	ASSERT_TRUE(InitDefaultProvides(tmpdir.Path()));

	string artifact = path::Join(tmpdir.Path(), "artifact.mender");
	ASSERT_TRUE(PrepareSimpleArtifact(tmpdir.Path(), artifact));

	string update_module = path::Join(tmpdir.Path(), "rootfs-image");

	ASSERT_TRUE(PrepareUpdateModule(update_module, R"(#!/bin/bash

TEST_DIR=")" + tmpdir.Path() + R"("

echo "$1" >> $TEST_DIR/call.log

case "$1" in
    SupportsRollback)
        echo "Yes"
        ;;
esac

exit 0
)"));

	{
		vector<string> args {
			"--data",
			tmpdir.Path(),
			"install",
			artifact,
		};

		mtesting::RedirectStreamOutputs output;
		int exit_status = cli::Main(args, [&tmpdir, &workdir](context::MenderContext &ctx) {
			ctx.GetConfig().paths.SetModulesPath(tmpdir.Path());
			ctx.GetConfig().paths.SetModulesWorkPath(workdir);
		});
		EXPECT_EQ(exit_status, 0) << exit_status;

		EXPECT_EQ(output.GetCout(), R"(Installing artifact...
Installed, but not committed.
Use 'commit' to update, or 'rollback' to roll back the update.
)");
		EXPECT_EQ(output.GetCerr(), "");
	}

	EXPECT_TRUE(mtesting::FileContainsExactly(
		path::Join(tmpdir.Path(), "call.log"), R"(ProvidePayloadFileSizes
Download
ArtifactInstall
NeedsArtifactReboot
SupportsRollback
)"));

	// Remove the Update Module working directory. This is what would have happened if upgrading
	// from a version < 4.0.
	std::error_code ec;
	ASSERT_TRUE(std::filesystem::remove_all(workdir, ec));

	{
		vector<string> args {
			"--data",
			tmpdir.Path(),
			"rollback",
		};

		mtesting::RedirectStreamOutputs output;
		int exit_status = cli::Main(args, [&tmpdir, &workdir](context::MenderContext &ctx) {
			ctx.GetConfig().paths.SetModulesPath(tmpdir.Path());
			ctx.GetConfig().paths.SetModulesWorkPath(workdir);
		});
		EXPECT_EQ(exit_status, 0) << exit_status;

		EXPECT_EQ(output.GetCout(), R"(Rolled back.
)");
		EXPECT_EQ(output.GetCerr(), "");
	}

	EXPECT_TRUE(mtesting::FileContainsExactly(
		path::Join(tmpdir.Path(), "call.log"), R"(ProvidePayloadFileSizes
Download
ArtifactInstall
NeedsArtifactReboot
SupportsRollback
SupportsRollback
ArtifactRollback
Cleanup
)"));

	EXPECT_TRUE(VerifyProvides(tmpdir.Path(), R"(rootfs-image.version=previous
rootfs-image.checksum=46ca895be3a18fb50c1c6b5a3bd2e97fb637b35a22924c2f3dea3cf09e9e2e74
artifact_name=previous
)"));
}

TEST(CliTest, InstallBootstrapArtifact) {
	mtesting::TemporaryDirectory tmpdir;
	string artifact = path::Join(tmpdir.Path(), "artifact.mender");
	ASSERT_TRUE(PrepareBootstrapArtifact(tmpdir.Path(), artifact));

	{
		vector<string> args {
			"--data",
			tmpdir.Path(),
			"install",
			artifact,
		};

		mtesting::RedirectStreamOutputs output;
		int exit_status = cli::Main(
			args, [&tmpdir](context::MenderContext &ctx) { SetTestDir(tmpdir.Path(), ctx); });
		EXPECT_EQ(exit_status, 0) << exit_status;

		EXPECT_EQ(output.GetCout(), R"(Installing artifact...
Artifact with empty payload. Committing immediately.
Installed and committed.
)");
		EXPECT_EQ(output.GetCerr(), "");
	}

	EXPECT_TRUE(VerifyProvides(tmpdir.Path(), R"(artifact_name=test
)"));
}

TEST(CliTest, InstallAndCommitArtifactFromNetwork) {
	mtesting::TemporaryDirectory tmpdir;
	string artifact = path::Join(tmpdir.Path(), "artifact.mender");
	ASSERT_TRUE(PrepareSimpleArtifact(tmpdir.Path(), artifact));

	string update_module = path::Join(tmpdir.Path(), "rootfs-image");

	ASSERT_TRUE(PrepareUpdateModule(update_module, R"(#!/bin/bash

TEST_DIR=")" + tmpdir.Path() + R"("

echo "$1" >> $TEST_DIR/call.log

exit 0
)"));

	mtesting::HttpFileServer file_server(tmpdir.Path());

	{
		vector<string> args {
			"--data",
			tmpdir.Path(),
			"install",
			file_server.GetBaseUrl() + "/" + path::BaseName(artifact),
		};

		mtesting::RedirectStreamOutputs output;
		int exit_status = cli::Main(
			args, [&tmpdir](context::MenderContext &ctx) { SetTestDir(tmpdir.Path(), ctx); });
		EXPECT_EQ(exit_status, 0) << exit_status;

		EXPECT_EQ(output.GetCout(), R"(Installing artifact...
Update Module doesn't support rollback. Committing immediately.
Installed and committed.
)");
		EXPECT_EQ(output.GetCerr(), "");
	}

	EXPECT_TRUE(mtesting::FileContainsExactly(
		path::Join(tmpdir.Path(), "call.log"), R"(ProvidePayloadFileSizes
Download
ArtifactInstall
NeedsArtifactReboot
SupportsRollback
ArtifactCommit
Cleanup
)"));

	EXPECT_TRUE(VerifyProvides(tmpdir.Path(), R"(rootfs-image.version=test
rootfs-image.checksum=f2ca1bb6c7e907d06dafe4687e579fce76b37e4e93b7605022da52e6ccc26fd2
artifact_name=test
)"));
}


using ExitCode = int;

struct StandaloneStateScriptTestCase {
	string case_name;
	ExitCode expected_exit_code;
	map<string, ExitCode> scripts;
	string fail_in_state;
	const string expected;
};

vector<StandaloneStateScriptTestCase> standalone_download_script_test_cases {
	{
		.case_name = "all_script_success",
		.expected_exit_code = 0,
		.scripts =
			{
				{"Download_Enter_01", ExitCode {0}},
				{"Download_Leave_01", ExitCode {0}},
				{"Download_Error_01", ExitCode {0}},
				{"ArtifactInstall_Enter_01", ExitCode {0}},
				{"ArtifactInstall_Leave_01", ExitCode {0}},
				{"ArtifactInstall_Error_01", ExitCode {0}},
				{"ArtifactCommit_Enter_01", ExitCode {0}},
				{"ArtifactCommit_Leave_01", ExitCode {0}},
				{"ArtifactCommit_Error_01", ExitCode {0}},
			},
		.expected = R"(ProvidePayloadFileSizes
Download_Enter_01
Download
Download_Leave_01
ArtifactInstall_Enter_01
ArtifactInstall
ArtifactInstall_Leave_01
NeedsArtifactReboot
SupportsRollback
ArtifactCommit_Enter_01
ArtifactCommit
ArtifactCommit_Leave_01
Cleanup
)",
	},
	{
		.case_name = "download_enter_error",
		.expected_exit_code = 1,
		.scripts =
			{
				{"Download_Enter_01", ExitCode {1}},
				{"Download_Leave_01", ExitCode {0}},
				{"Download_Error_01", ExitCode {0}},
				{"ArtifactInstall_Enter_01", ExitCode {0}},
				{"ArtifactInstall_Leave_01", ExitCode {0}},
				{"ArtifactCommit_Enter_01", ExitCode {0}},
				{"ArtifactCommit_Leave_01", ExitCode {0}},
			},
		.expected = R"(ProvidePayloadFileSizes
Download_Enter_01
Download_Error_01
Cleanup
)",
	},
	{
		.case_name = "download_error",
		.expected_exit_code = 1,
		.scripts =
			{
				{"Download_Enter_01", ExitCode {0}},
				{"Download_Leave_01", ExitCode {0}},
				{"Download_Error_01", ExitCode {0}},
				{"ArtifactInstall_Enter_01", ExitCode {0}},
				{"ArtifactInstall_Leave_01", ExitCode {0}},
				{"ArtifactCommit_Enter_01", ExitCode {0}},
				{"ArtifactCommit_Leave_01", ExitCode {0}},
			},
		.fail_in_state = "Download",
		.expected = R"(ProvidePayloadFileSizes
Download_Enter_01
Download
Download_Error_01
Cleanup
)",
	},
	{
		.case_name = "download_enter_error_with_error_script",
		.expected_exit_code = 1,
		.scripts =
			{
				{"Download_Enter_01", ExitCode {1}},
				{"Download_Error_01", ExitCode {1}}, // Exit 1 should not matter in error script
				{"Download_Error_02", ExitCode {0}},
				{"ArtifactInstall_Enter_01", ExitCode {0}},
				{"ArtifactInstall_Leave_01", ExitCode {0}},
				{"ArtifactCommit_Enter_01", ExitCode {0}},
				{"ArtifactCommit_Leave_01", ExitCode {0}},
			},
		.expected = R"(ProvidePayloadFileSizes
Download_Enter_01
Download_Error_01
Download_Error_02
Cleanup
)",
	},
	{
		.case_name = "download_leave_error",
		.expected_exit_code = 1,
		.scripts =
			{
				{"Download_Enter_01", ExitCode {0}},
				{"Download_Leave_01", ExitCode {1}},
				{"Download_Error_01", ExitCode {1}}, // Exit 1 should not matter in error script
				{"Download_Error_02", ExitCode {0}},
				{"ArtifactInstall_Enter_01", ExitCode {0}},
				{"ArtifactInstall_Leave_01", ExitCode {0}},
				{"ArtifactCommit_Enter_01", ExitCode {0}},
				{"ArtifactCommit_Leave_01", ExitCode {0}},
			},
		.expected = R"(ProvidePayloadFileSizes
Download_Enter_01
Download
Download_Leave_01
Download_Error_01
Download_Error_02
Cleanup
)",
	}};

vector<StandaloneStateScriptTestCase> standalone_install_script_test_cases {
	{
		.case_name = "install_enter_error",
		.expected_exit_code = 1,
		.scripts =
			{
				{"Download_Enter_01", ExitCode {0}},
				{"Download_Leave_01", ExitCode {0}},
				{"ArtifactInstall_Enter_01", ExitCode {1}},
				{"ArtifactInstall_Enter_02", ExitCode {1}},
				{"ArtifactInstall_Leave_01", ExitCode {0}},
				{"ArtifactInstall_Error_01", ExitCode {1}},
				{"ArtifactInstall_Error_02", ExitCode {0}},
				{"ArtifactCommit_Enter_01", ExitCode {0}},
				{"ArtifactCommit_Leave_01", ExitCode {0}},
				{"ArtifactFailure_Enter_01", ExitCode {0}},
				{"ArtifactFailure_Leave_01", ExitCode {0}},
			},
		.expected = R"(ProvidePayloadFileSizes
Download_Enter_01
Download
Download_Leave_01
ArtifactInstall_Enter_01
ArtifactInstall_Error_01
ArtifactInstall_Error_02
SupportsRollback
ArtifactFailure_Enter_01
ArtifactFailure
ArtifactFailure_Leave_01
Cleanup
)",
	},
	{
		.case_name = "install_error",
		.expected_exit_code = 1,
		.scripts =
			{
				{"Download_Enter_01", ExitCode {0}},
				{"Download_Leave_01", ExitCode {0}},
				{"ArtifactInstall_Enter_01", ExitCode {0}},
				{"ArtifactInstall_Leave_01", ExitCode {0}},
				{"ArtifactInstall_Error_01", ExitCode {1}},
				{"ArtifactInstall_Error_02", ExitCode {0}},
				{"ArtifactCommit_Enter_01", ExitCode {0}},
				{"ArtifactCommit_Leave_01", ExitCode {0}},
				{"ArtifactFailure_Enter_01", ExitCode {0}},
				{"ArtifactFailure_Leave_01", ExitCode {0}},
			},
		.fail_in_state = "ArtifactInstall",
		.expected = R"(ProvidePayloadFileSizes
Download_Enter_01
Download
Download_Leave_01
ArtifactInstall_Enter_01
ArtifactInstall
ArtifactInstall_Error_01
ArtifactInstall_Error_02
SupportsRollback
ArtifactFailure_Enter_01
ArtifactFailure
ArtifactFailure_Leave_01
Cleanup
)",
	},
	{
		.case_name = "install_leave_error",
		.expected_exit_code = 1,
		.scripts =
			{
				{"Download_Enter_01", ExitCode {0}},
				{"Download_Leave_01", ExitCode {0}},
				{"Download_Error_01", ExitCode {0}},
				{"Download_Error_02", ExitCode {0}},
				{"ArtifactInstall_Enter_01", ExitCode {0}},
				{"ArtifactInstall_Leave_01", ExitCode {0}},
				{"ArtifactInstall_Leave_02", ExitCode {1}},
				{"ArtifactInstall_Error_01", ExitCode {1}}, // exit 1 should not matter
				{"ArtifactInstall_Error_02", ExitCode {0}},
				{"ArtifactCommit_Enter_01", ExitCode {0}},
				{"ArtifactCommit_Leave_01", ExitCode {0}},
				{"ArtifactFailure_Enter_01", ExitCode {0}},
				{"ArtifactFailure_Leave_01", ExitCode {0}},
			},
		.expected = R"(ProvidePayloadFileSizes
Download_Enter_01
Download
Download_Leave_01
ArtifactInstall_Enter_01
ArtifactInstall
ArtifactInstall_Leave_01
ArtifactInstall_Leave_02
ArtifactInstall_Error_01
ArtifactInstall_Error_02
SupportsRollback
ArtifactFailure_Enter_01
ArtifactFailure
ArtifactFailure_Leave_01
Cleanup
)",
	}};

vector<StandaloneStateScriptTestCase> standalone_commit_script_test_cases {
	{
		.case_name = "commit_enter_error",
		.expected_exit_code = 1,
		.scripts =
			{
				{"Download_Enter_01", ExitCode {0}},
				{"Download_Leave_01", ExitCode {0}},
				{"ArtifactInstall_Enter_01", ExitCode {0}},
				{"ArtifactInstall_Enter_02", ExitCode {0}},
				{"ArtifactInstall_Leave_01", ExitCode {0}},
				{"ArtifactCommit_Enter_01", ExitCode {1}},
				{"ArtifactCommit_Error_01", ExitCode {0}},
				{"ArtifactCommit_Error_02", ExitCode {0}},
				{"ArtifactCommit_Leave_01", ExitCode {0}},
				{"ArtifactFailure_Enter_01", ExitCode {0}},
				{"ArtifactFailure_Leave_01", ExitCode {0}},
			},

		.expected = R"(ProvidePayloadFileSizes
Download_Enter_01
Download
Download_Leave_01
ArtifactInstall_Enter_01
ArtifactInstall_Enter_02
ArtifactInstall
ArtifactInstall_Leave_01
NeedsArtifactReboot
SupportsRollback
ArtifactCommit_Enter_01
ArtifactCommit_Error_01
ArtifactCommit_Error_02
SupportsRollback
ArtifactFailure_Enter_01
ArtifactFailure
ArtifactFailure_Leave_01
Cleanup
)",
	},
	{
		.case_name = "commit_error",
		.expected_exit_code = 1,
		.scripts =
			{
				{"Download_Enter_01", ExitCode {0}},
				{"Download_Leave_01", ExitCode {0}},
				{"ArtifactInstall_Enter_01", ExitCode {0}},
				{"ArtifactInstall_Enter_02", ExitCode {0}},
				{"ArtifactInstall_Leave_01", ExitCode {0}},
				{"ArtifactCommit_Enter_01", ExitCode {0}},
				{"ArtifactCommit_Error_01", ExitCode {0}},
				{"ArtifactCommit_Error_02", ExitCode {0}},
				{"ArtifactCommit_Leave_01", ExitCode {0}},
				{"ArtifactFailure_Enter_01", ExitCode {0}},
				{"ArtifactFailure_Leave_01", ExitCode {0}},
			},
		.fail_in_state = "ArtifactCommit",
		.expected = R"(ProvidePayloadFileSizes
Download_Enter_01
Download
Download_Leave_01
ArtifactInstall_Enter_01
ArtifactInstall_Enter_02
ArtifactInstall
ArtifactInstall_Leave_01
NeedsArtifactReboot
SupportsRollback
ArtifactCommit_Enter_01
ArtifactCommit
ArtifactCommit_Error_01
ArtifactCommit_Error_02
SupportsRollback
ArtifactFailure_Enter_01
ArtifactFailure
ArtifactFailure_Leave_01
Cleanup
)",
	},
	{
		.case_name = "commit_leave_error",
		.expected_exit_code = 1,
		.scripts =
			{
				{"Download_Enter_01", ExitCode {0}},
				{"Download_Leave_01", ExitCode {0}},
				{"Download_Error_01", ExitCode {0}},
				{"Download_Error_02", ExitCode {0}},
				{"ArtifactInstall_Enter_01", ExitCode {0}},
				{"ArtifactInstall_Leave_01", ExitCode {0}},
				{"ArtifactInstall_Leave_02", ExitCode {0}},
				{"ArtifactCommit_Enter_01", ExitCode {0}},
				{"ArtifactCommit_Leave_01", ExitCode {1}},
				{"ArtifactCommit_Error_01", ExitCode {0}},
				{"ArtifactFailure_Enter_01", ExitCode {0}},
				{"ArtifactFailure_Leave_01", ExitCode {0}},
			},
		.expected = R"(ProvidePayloadFileSizes
Download_Enter_01
Download
Download_Leave_01
ArtifactInstall_Enter_01
ArtifactInstall
ArtifactInstall_Leave_01
ArtifactInstall_Leave_02
NeedsArtifactReboot
SupportsRollback
ArtifactCommit_Enter_01
ArtifactCommit
ArtifactCommit_Leave_01
ArtifactCommit_Error_01
Cleanup
)",
	},
	{
		.case_name = "commit_leave_error_with_error_script",
		.expected_exit_code = 1,
		.scripts =
			{
				{"Download_Enter_01", ExitCode {0}},
				{"Download_Leave_01", ExitCode {0}},
				{"Download_Error_01", ExitCode {0}},
				{"Download_Error_02", ExitCode {0}},
				{"ArtifactInstall_Enter_01", ExitCode {0}},
				{"ArtifactInstall_Leave_01", ExitCode {0}},
				{"ArtifactInstall_Leave_02", ExitCode {0}},
				{"ArtifactCommit_Enter_01", ExitCode {0}},
				{"ArtifactCommit_Leave_01", ExitCode {1}},
				{"ArtifactCommit_Error_01", ExitCode {1}}, // should not matter
				{"ArtifactCommit_Error_02", ExitCode {0}},
				{"ArtifactFailure_Enter_01", ExitCode {0}},
				{"ArtifactFailure_Leave_01", ExitCode {0}},
			},
		.expected = R"(ProvidePayloadFileSizes
Download_Enter_01
Download
Download_Leave_01
ArtifactInstall_Enter_01
ArtifactInstall
ArtifactInstall_Leave_01
ArtifactInstall_Leave_02
NeedsArtifactReboot
SupportsRollback
ArtifactCommit_Enter_01
ArtifactCommit
ArtifactCommit_Leave_01
ArtifactCommit_Error_01
ArtifactCommit_Error_02
Cleanup
)",
	}};

vector<StandaloneStateScriptTestCase> standalone_failure_script_test_cases {
	{
		.case_name = "failure_enter_error",
		.expected_exit_code = 1,
		.scripts =
			{
				{"Download_Enter_01", ExitCode {0}},
				{"Download_Enter_02", ExitCode {0}},
				{"Download_Leave_01", ExitCode {0}},
				{"ArtifactInstall_Enter_01", ExitCode {0}},
				{"ArtifactInstall_Enter_02", ExitCode {0}},
				{"ArtifactInstall_Leave_01", ExitCode {0}},
				{"ArtifactCommit_Enter_01", ExitCode {1}}, // Trigger failure
				{"ArtifactCommit_Leave_01", ExitCode {0}},
				{"ArtifactFailure_Enter_01", ExitCode {1}}, // Should not matter
				{"ArtifactFailure_Enter_02", ExitCode {1}}, // Should not matter
				{"ArtifactFailure_Leave_01", ExitCode {0}},
				{"ArtifactFailure_Leave_02", ExitCode {0}},
			},
		.expected = R"(ProvidePayloadFileSizes
Download_Enter_01
Download_Enter_02
Download
Download_Leave_01
ArtifactInstall_Enter_01
ArtifactInstall_Enter_02
ArtifactInstall
ArtifactInstall_Leave_01
NeedsArtifactReboot
SupportsRollback
ArtifactCommit_Enter_01
SupportsRollback
ArtifactFailure_Enter_01
ArtifactFailure_Enter_02
ArtifactFailure
ArtifactFailure_Leave_01
ArtifactFailure_Leave_02
Cleanup
)",
	},
	{
		.case_name = "failure_leave_error",
		.expected_exit_code = 1,
		.scripts =
			{
				{"Download_Enter_01", ExitCode {0}},
				{"Download_Enter_02", ExitCode {0}},
				{"Download_Leave_01", ExitCode {0}},
				{"ArtifactInstall_Enter_01", ExitCode {0}},
				{"ArtifactInstall_Enter_02", ExitCode {0}},
				{"ArtifactInstall_Leave_01", ExitCode {0}},
				{"ArtifactCommit_Enter_01", ExitCode {1}}, // Trigger failure
				{"ArtifactCommit_Leave_01", ExitCode {0}},
				{"ArtifactFailure_Enter_01", ExitCode {0}},
				{"ArtifactFailure_Enter_02", ExitCode {0}},
				{"ArtifactFailure_Leave_01", ExitCode {1}}, // Should not matter
				{"ArtifactFailure_Leave_02", ExitCode {1}}, // Should not matter
			},
		.expected = R"(ProvidePayloadFileSizes
Download_Enter_01
Download_Enter_02
Download
Download_Leave_01
ArtifactInstall_Enter_01
ArtifactInstall_Enter_02
ArtifactInstall
ArtifactInstall_Leave_01
NeedsArtifactReboot
SupportsRollback
ArtifactCommit_Enter_01
SupportsRollback
ArtifactFailure_Enter_01
ArtifactFailure_Enter_02
ArtifactFailure
ArtifactFailure_Leave_01
ArtifactFailure_Leave_02
Cleanup
)",
	},
};

vector<StandaloneStateScriptTestCase> standalone_rollback_script_test_cases {
	{
		.case_name = "rollback_regular_success",
		.expected_exit_code = 0,
		.scripts =
			{
				{"Download_Enter_01", ExitCode {0}},
				{"Download_Leave_01", ExitCode {0}},
				{"ArtifactInstall_Enter_01", ExitCode {0}},
				{"ArtifactInstall_Enter_02", ExitCode {0}},
				{"ArtifactInstall_Leave_01", ExitCode {0}},
				{"ArtifactCommit_Enter_01", ExitCode {0}},
				{"ArtifactCommit_Leave_01", ExitCode {0}},
				{"ArtifactRollback_Enter_01", ExitCode {0}},
				{"ArtifactRollback_Leave_01", ExitCode {0}},
				{"ArtifactFailure_Enter_01", ExitCode {1}}, // Should not matter
				{"ArtifactFailure_Leave_01", ExitCode {1}}, // Should not matter
			},
		.expected = R"(ProvidePayloadFileSizes
Download_Enter_01
Download
Download_Leave_01
ArtifactInstall_Enter_01
ArtifactInstall_Enter_02
ArtifactInstall
ArtifactInstall_Leave_01
NeedsArtifactReboot
SupportsRollback
SupportsRollback
ArtifactRollback_Enter_01
ArtifactRollback
ArtifactRollback_Leave_01
Cleanup
)",
	},
};

class StandaloneStateScriptTest : public testing::TestWithParam<StandaloneStateScriptTestCase> {
public:
	void SetUp() override {
	}

	mtesting::TemporaryDirectory tmpdir;
};

INSTANTIATE_TEST_SUITE_P(
	StateScripts_Download,
	StandaloneStateScriptTest,
	::testing::ValuesIn(standalone_download_script_test_cases),
	[](const testing::TestParamInfo<StandaloneStateScriptTestCase> &test_case) {
		return test_case.param.case_name;
	});

INSTANTIATE_TEST_SUITE_P(
	StateScripts_Install,
	StandaloneStateScriptTest,
	::testing::ValuesIn(standalone_install_script_test_cases),
	[](const testing::TestParamInfo<StandaloneStateScriptTestCase> &test_case) {
		return test_case.param.case_name;
	});

INSTANTIATE_TEST_SUITE_P(
	StateScripts_Commit,
	StandaloneStateScriptTest,
	::testing::ValuesIn(standalone_commit_script_test_cases),
	[](const testing::TestParamInfo<StandaloneStateScriptTestCase> &test_case) {
		return test_case.param.case_name;
	});

INSTANTIATE_TEST_SUITE_P(
	StateScripts_Failure,
	StandaloneStateScriptTest,
	::testing::ValuesIn(standalone_failure_script_test_cases),
	[](const testing::TestParamInfo<StandaloneStateScriptTestCase> &test_case) {
		return test_case.param.case_name;
	});

INSTANTIATE_TEST_SUITE_P(
	StateScripts_Rollback,
	StandaloneStateScriptTest,
	::testing::ValuesIn(standalone_rollback_script_test_cases),
	[](const testing::TestParamInfo<StandaloneStateScriptTestCase> &test_case) {
		return test_case.param.case_name;
	});

void CreateArtifactScript(
	const string &dir, const string &log_dir, const pair<string, int> &script) {
	const string name {script.first};
	const int exit_code {script.second};
	const string script_name {path::Join(dir, name)};
	ofstream of(script_name);
	ASSERT_TRUE(of);
	of << "#! /bin/sh" << endl;
	of << "echo " << name << endl;
	of << "echo " << name << " >> " << log_dir << "/call.log" << endl;
	of << "exit " << exit_code << endl;
	EXPECT_EQ(chmod(script_name.c_str(), 0755), 0);
}

TEST_P(StandaloneStateScriptTest, AllScriptSuccess) {
	const string tmpdir_path {tmpdir.Path()};
	tmpdir.CreateSubDirectory("scripts");
	const string script_tmpdir {path::Join(tmpdir.Path(), "scripts")};
	for (const auto &script : GetParam().scripts) {
		CreateArtifactScript(script_tmpdir, tmpdir_path, script);
	}
	string artifact = path::Join(tmpdir_path, "artifact.mender");
	ASSERT_TRUE(PrepareSimpleArtifact(tmpdir_path, artifact));

	string update_module = path::Join(tmpdir_path, "rootfs-image");

	string script = R"(#!/bin/bash
TEST_DIR=")" + tmpdir_path
					+ R"("
echo "$1" >> $TEST_DIR/call.log
)";
	if (common::StartsWith<string>(GetParam().case_name, "rollback")) {
		script += R"(
case "$1" in
    SupportsRollback)
        echo "Yes"
        ;;
esac
)";
	}

	if (GetParam().fail_in_state != "") {
		script += R"(
if [ "$1" = ")" + GetParam().fail_in_state
				  + R"(" ]; then
    exit 1
fi
)";
	}

	script += R"(
exit 0
)";
	ASSERT_TRUE(PrepareUpdateModule(update_module, script));

	{
		vector<string> args {
			"--data",
			tmpdir_path,
			"install",
			artifact,
		};

		int exit_status = cli::Main(
			args, [tmpdir_path](context::MenderContext &ctx) { SetTestDir(tmpdir_path, ctx); });
		EXPECT_EQ(exit_status, GetParam().expected_exit_code) << exit_status;
	}

	if (common::StartsWith<string>(GetParam().case_name, "rollback")) {
		{
			vector<string> args {
				"--data",
				tmpdir.Path(),
				"rollback",
			};

			int exit_status = cli::Main(
				args, [tmpdir_path](context::MenderContext &ctx) { SetTestDir(tmpdir_path, ctx); });
			EXPECT_EQ(exit_status, 0) << exit_status;
		}
	}

	EXPECT_TRUE(
		mtesting::FileContainsExactly(path::Join(tmpdir_path, "call.log"), GetParam().expected));
}

TEST(CliTest, MaybeInstallBootstrapArtifactSuccess) {
	mtesting::TemporaryDirectory tmpdir;

	string bootstrap_artifact = path::Join(tmpdir.Path(), "bootstrap.mender");
	ASSERT_TRUE(PrepareBootstrapArtifact(tmpdir.Path(), bootstrap_artifact));

	conf::MenderConfig conf;
	conf.paths.SetDataStore(tmpdir.Path());
	context::MenderContext context(conf);

	auto err = context.Initialize();
	ASSERT_EQ(err, error::NoError) << err.String();

	err = cli::MaybeInstallBootstrapArtifact(context);
	EXPECT_EQ(err, error::NoError);

	EXPECT_TRUE(VerifyProvides(tmpdir.Path(), "artifact_name=test\n"));

	EXPECT_FALSE(path::FileExists(bootstrap_artifact));
}

TEST(CliTest, MaybeInstallBootstrapArtifact_NoBootstrapArtifactEmptyDatabase) {
	mtesting::TemporaryDirectory tmpdir;

	conf::MenderConfig conf;
	conf.paths.SetDataStore(tmpdir.Path());
	context::MenderContext context(conf);

	auto err = context.Initialize();
	ASSERT_EQ(err, error::NoError) << err.String();

	err = cli::MaybeInstallBootstrapArtifact(context);
	EXPECT_EQ(err, error::NoError);

	EXPECT_TRUE(VerifyProvides(tmpdir.Path(), "artifact_name=unknown\n"));
}


TEST(CliTest, MaybeInstallBootstrapArtifact_PrepopulatedDB) {
	mtesting::TemporaryDirectory tmpdir;

	conf::MenderConfig conf;
	conf.paths.SetDataStore(tmpdir.Path());
	context::MenderContext context(conf);

	error::Error err = context.Initialize();
	ASSERT_EQ(err, error::NoError) << err.String();

	auto &db = context.GetMenderStoreDB();
	err = db.Write(context.artifact_name_key, common::ByteVectorFromString("foobar"));
	ASSERT_EQ(err, error::NoError) << err.String();

	err = cli::MaybeInstallBootstrapArtifact(context);
	EXPECT_EQ(err, error::NoError);

	EXPECT_TRUE(VerifyProvides(tmpdir.Path(), "artifact_name=foobar\n"));
}

TEST(CliTest, Version) {
	{
		vector<string> args {"--version"};
		EXPECT_EQ(cli::Main(args), 0);
	}

	{
		mtesting::RedirectStreamOutputs redirect_output;
		vector<string> args {"--version", "install"};
		EXPECT_EQ(cli::Main(args), 1);
		EXPECT_THAT(
			redirect_output.GetCerr(),
			testing::EndsWith("--version can not be combined with other commands and arguments\n"))
			<< redirect_output.GetCerr();
	}
}

TEST(CliTest, Help) {
	{
		mtesting::RedirectStreamOutputs redirect_output;
		vector<string> args {"--help"};
		EXPECT_EQ(cli::Main(args), 0);
		EXPECT_THAT(
			redirect_output.GetCout(),
			testing::HasSubstr("mender-update - manage and start Mender Update"))
			<< redirect_output.GetCout();
	}

	{
		vector<string> args {"--help", "whatever-non-existent"};
		EXPECT_EQ(cli::Main(args), 0);
	}
}
