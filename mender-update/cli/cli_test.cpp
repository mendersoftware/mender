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

#include <fstream>

#include <gtest/gtest.h>

#include <common/conf.hpp>
#include <common/error.hpp>
#include <common/path.hpp>
#include <common/testing.hpp>

#include <mender-update/context.hpp>

namespace cli = mender::update::cli;
namespace conf = mender::common::conf;
namespace context = mender::update::context;
namespace error = mender::common::error;
namespace mtesting = mender::common::testing;
namespace path = mender::common::path;

using namespace std;

TEST(CliTest, NoAction) {
	mtesting::TemporaryDirectory tmpdir;

	conf::MenderConfig conf;
	conf.data_store_dir = tmpdir.Path();
	context::MenderContext context(conf);

	auto err = context.Initialize();
	ASSERT_EQ(err, error::NoError) << err.String();

	{
		mtesting::RedirectStreamOutputs redirect_output;
		vector<string> args {"--data", tmpdir.Path()};
		EXPECT_EQ(cli::Main(args), 1);
		EXPECT_EQ(
			redirect_output.GetCerr(),
			"Failed to process command line options: Invalid options given: Need an action\n");
	}
}

TEST(CliTest, ShowArtifact) {
	mtesting::TemporaryDirectory tmpdir;

	conf::MenderConfig conf;
	conf.data_store_dir = tmpdir.Path();
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
	conf.data_store_dir = tmpdir.Path();

	{
		mtesting::RedirectStreamOutputs redirect_output;
		vector<string> args {"--data", tmpdir.Path(), "show-artifact", "--bogus-option"};
		EXPECT_EQ(cli::Main(args), 1);
		EXPECT_EQ(
			redirect_output.GetCerr(),
			"Failed to process command line options: Invalid options given: Unrecognized option '--bogus-option'\n");
	}

	{
		mtesting::RedirectStreamOutputs redirect_output;
		vector<string> args {"--data", tmpdir.Path(), "show-artifact", "bogus-argument"};
		EXPECT_EQ(cli::Main(args), 1);
		EXPECT_EQ(
			redirect_output.GetCerr(),
			"Failed to process command line options: Invalid options given: Unexpected argument 'bogus-argument'\n");
	}
}

TEST(CliTest, ShowProvides) {
	mtesting::TemporaryDirectory tmpdir;

	conf::MenderConfig conf;
	conf.data_store_dir = tmpdir.Path();
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
	conf.data_store_dir = tmpdir.Path();

	{
		mtesting::RedirectStreamOutputs redirect_output;
		vector<string> args {"--data", tmpdir.Path(), "show-provides", "--bogus-option"};
		EXPECT_EQ(cli::Main(args), 1);
		EXPECT_EQ(
			redirect_output.GetCerr(),
			"Failed to process command line options: Invalid options given: Unrecognized option '--bogus-option'\n");
	}

	{
		mtesting::RedirectStreamOutputs redirect_output;
		vector<string> args {"--data", tmpdir.Path(), "show-provides", "bogus-argument"};
		EXPECT_EQ(cli::Main(args), 1);
		EXPECT_EQ(
			redirect_output.GetCerr(),
			"Failed to process command line options: Invalid options given: Unexpected argument 'bogus-argument'\n");
	}
}
