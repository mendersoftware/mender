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

#include <mender-auth/cli/cli.hpp>

#include <fstream>

#include <gtest/gtest.h>
#include <gmock/gmock.h>

#include <common/error.hpp>
#include <common/http.hpp>
#include <common/io.hpp>
#include <common/path.hpp>
#include <common/testing.hpp>

namespace cli = mender::auth::cli;
namespace context = mender::auth::context;
namespace error = mender::common::error;
namespace http = mender::http;
namespace io = mender::common::io;
namespace mtesting = mender::common::testing;
namespace path = mender::common::path;

using namespace std;

const string TEST_PORT = "8088";

TEST(CliTest, NoAction) {
	vector<string> args = {};
	auto err = cli::DoMain(args);
	EXPECT_EQ("Need an action", err.message);
}

TEST(CliTest, InvalidAction) {
	vector<string> args = {"something"};
	auto err = cli::DoMain(args);
	EXPECT_EQ("No such action: something", err.message);
}

TEST(CliTest, BootstrapActionGenerateKey) {
	mtesting::TemporaryDirectory tmpdir;

	vector<string> args = {"--data", tmpdir.Path(), "bootstrap"};
	auto err = cli::DoMain(args);
	EXPECT_EQ(error::NoError, err);

	string key_path = path::Join(tmpdir.Path(), "mender-agent.pem");

	EXPECT_TRUE(mtesting::FileContains(key_path, "-----BEGIN RSA PRIVATE KEY-----"));
	EXPECT_TRUE(mtesting::FileContains(key_path, "-----END RSA PRIVATE KEY-----"));
}

TEST(CliTest, BootstrapActionExistingKey) {
	mtesting::TemporaryDirectory tmpdir;
	string key_path = path::Join(tmpdir.Path(), "mender-agent.pem");

	ifstream sample_key("./sample.key");
	ofstream test_key(key_path);
	std::string line;
	ASSERT_TRUE(sample_key.good());
	ASSERT_TRUE(test_key.good());
	while (getline(sample_key, line)) {
		test_key << line;
		test_key << '\n';
	}
	sample_key.close();
	test_key.close();

	vector<string> args = {"--data", tmpdir.Path(), "bootstrap"};
	auto err = cli::DoMain(args);
	EXPECT_EQ(error::NoError, err);

	EXPECT_TRUE(mtesting::FilesEqual("./sample.key", key_path));

	// Now force new key with --forcebootstrap
	args = {"--data", tmpdir.Path(), "bootstrap", "--forcebootstrap"};
	err = cli::DoMain(args);
	EXPECT_EQ(error::NoError, err);

	EXPECT_TRUE(mtesting::FileContains(key_path, "-----BEGIN RSA PRIVATE KEY-----"));
	EXPECT_TRUE(mtesting::FileContains(key_path, "-----END RSA PRIVATE KEY-----"));
	EXPECT_TRUE(mtesting::FilesNotEqual("./sample.key", key_path));
}


TEST(CliTest, DoAuthenticationCycleOnBootstrap) {
	mtesting::TemporaryDirectory tmpdir;

	const string JWT_TOKEN = "FOOBARJWTTOKEN";

	mender::common::testing::TestEventLoop loop;

	// Setup a test server
	const string server_url {"http://127.0.0.1:" + TEST_PORT};
	http::ServerConfig server_config;
	http::Server server(server_config, loop);
	server.AsyncServeUrl(
		server_url,
		[](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			exp_req.value()->SetBodyWriter(make_shared<io::Discard>());
		},
		[JWT_TOKEN](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();

			auto result = exp_req.value()->MakeResponse();
			ASSERT_TRUE(result);
			auto resp = result.value();

			resp->SetStatusCodeAndMessage(200, "OK");
			resp->SetBodyReader(make_shared<io::StringReader>(JWT_TOKEN));
			resp->SetHeader("Content-Length", to_string(JWT_TOKEN.size()));
			resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
		});

	testing::internal::CaptureStderr();

	auto server_loop_thread = std::thread([&loop]() { loop.Run(); });

	vector<string> args = {"--data", tmpdir.Path(), "bootstrap"};
	auto err = cli::DoMain(args, [&tmpdir](context::MenderContext &ctx) {
		ctx.GetConfig().paths.SetPathConfDir(tmpdir.Path());
		ctx.GetConfig().server_url = "http://127.0.0.1:" + TEST_PORT;
	});
	EXPECT_EQ(error::NoError, err) << err.String();

	string output = testing::internal::GetCapturedStderr();

	EXPECT_THAT(output, testing::HasSubstr("Successfully authorized with the server"));

	loop.Stop();
	server_loop_thread.join();
}
