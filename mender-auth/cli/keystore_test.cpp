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

#include <mender-auth/cli/keystore.hpp>

#include <fstream>

#include <gtest/gtest.h>
#include <gmock/gmock.h>

#include <common/error.hpp>
#include <common/path.hpp>
#include <common/testing.hpp>

namespace cli = mender::auth::cli;
namespace error = mender::common::error;
namespace mtesting = mender::common::testing;
namespace path = mender::common::path;

using namespace std;

TEST(CliTest, KeyStoreLoad) {
	mtesting::TemporaryDirectory tmpdir;

	const string contents = "existing but invalid key";
	const string key_path = path::Join(tmpdir.Path(), "secret.key");
	{
		std::ofstream os(key_path.c_str(), std::ios::out);
		os << contents;
	}

	cli::MenderKeyStore store_no_key(key_path, "", cli::StaticKey::No, "");
	auto err = store_no_key.Load();
	EXPECT_EQ(cli::MakeError(cli::NoKeysError, "").code, err.code);

	cli::MenderKeyStore store_yes_key("./sample.key", "", cli::StaticKey::No, "");
	err = store_yes_key.Load();
	EXPECT_EQ(error::NoError, err);
}

TEST(CliTest, KeyStoreGenerate) {
	cli::MenderKeyStore store_no_static("/non/existing/path", "", cli::StaticKey::No, "");
	auto err = store_no_static.Generate();
	EXPECT_EQ(error::NoError, err);

	cli::MenderKeyStore store_yes_key("/non/existing/path", "", cli::StaticKey::Yes, "");
	err = store_yes_key.Generate();
	EXPECT_EQ(cli::MakeError(cli::StaticKeyError, "").code, err.code);
}


TEST(CliTest, KeyStoreSave) {
	mtesting::TemporaryDirectory tmpdir;
	const string contents = "old content";
	const string key_path = path::Join(tmpdir.Path(), "secret.key");
	{
		std::ofstream os(key_path.c_str(), std::ios::out);
		os << contents;
	}
	ASSERT_TRUE(mtesting::FileContainsExactly(key_path, "old content"));

	cli::MenderKeyStore store(key_path, "", cli::StaticKey::No, "");
	auto err = store.Save();
	EXPECT_EQ(cli::MakeError(cli::NoKeysError, "").code, err.code);

	err = store.Generate();
	EXPECT_EQ(error::NoError, err);

	err = store.Save();
	EXPECT_EQ(error::NoError, err);

	EXPECT_FALSE(mtesting::FileContains(key_path, "old content"));
	EXPECT_TRUE(mtesting::FileContains(key_path, "-----BEGIN RSA PRIVATE KEY-----"));
	EXPECT_TRUE(mtesting::FileContains(key_path, "-----END RSA PRIVATE KEY-----"));
}

TEST(CliTest, KeyStoreSaveNonExistingPath) {
	mtesting::TemporaryDirectory tmpdir;
	const string key_path = path::Join(tmpdir.Path(), "non", "existing", "path", "secret.key");
	cli::MenderKeyStore store(key_path, "", cli::StaticKey::No, "");
	auto err = store.Generate();
	ASSERT_EQ(error::NoError, err);

	err = store.Save();
	EXPECT_TRUE(err != error::NoError);

	EXPECT_THAT(err.message, testing::StartsWith("Failed to open the private key file:"));
	EXPECT_THAT(err.message, testing::HasSubstr("No such file or directory"));
}
