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

#include <common/error.hpp>
#include <common/path.hpp>
#include <common/testing.hpp>

namespace cli = mender::auth::cli;
namespace error = mender::common::error;
namespace mtesting = mender::common::testing;
namespace path = mender::common::path;

using namespace std;

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
