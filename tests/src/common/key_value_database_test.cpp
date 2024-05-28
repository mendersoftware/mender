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

#include <common/key_value_database.hpp>

#include <common/common.hpp>
#include <common/config.h>

#ifdef MENDER_USE_LMDB
#include <common/key_value_database_lmdb.hpp>
#endif

#include <cstdio>
#include <fstream>

#include <gtest/gtest.h>
#include <gmock/gmock.h>

#include <common/path.hpp>
#include <common/testing.hpp>

using namespace std;

namespace common = mender::common;
namespace error = mender::common::error;
namespace kvdb = mender::common::key_value_database;
namespace path = mender::common::path;
namespace mtesting = mender::common::testing;

struct KeyValueDatabaseSetup {
	string name;
	// Order is important here: db should be destroyed before tmpdir.
	shared_ptr<mender::common::testing::TemporaryDirectory> tmpdir;
	shared_ptr<kvdb::KeyValueDatabase> db;
};

class KeyValueDatabaseTest : public testing::TestWithParam<KeyValueDatabaseSetup> {};

static vector<KeyValueDatabaseSetup> GenerateDatabaseSetups() {
	vector<KeyValueDatabaseSetup> ret;
	KeyValueDatabaseSetup elem;

#ifdef MENDER_USE_LMDB
	elem.name = "LMDB";
	elem.tmpdir = std::make_shared<mender::common::testing::TemporaryDirectory>();
	auto lmdb_db = std::make_shared<kvdb::KeyValueDatabaseLmdb>();
	auto err = lmdb_db->Open(elem.tmpdir->Path() + "mender-store");
	assert(err == error::NoError);
	elem.db = lmdb_db;
	ret.push_back(elem);
#endif

	return ret;
}

INSTANTIATE_TEST_SUITE_P(
	,
	KeyValueDatabaseTest,
	testing::ValuesIn(GenerateDatabaseSetups()),
	[](const testing::TestParamInfo<KeyValueDatabaseSetup> &info) { return info.param.name; });

TEST_P(KeyValueDatabaseTest, BasicReadWriteRemove) {
	kvdb::KeyValueDatabase &db = *GetParam().db;

	{
		// Write
		auto error = db.Write("key", mender::common::ByteVectorFromString("val"));
		ASSERT_EQ(error::NoError, error);
	}

	{
		// Read
		auto entry = db.Read("key");
		ASSERT_TRUE(entry) << entry.error().message;
		std::string string1(common::StringFromByteVector(entry.value()));
		EXPECT_EQ(string1, "val") << "DB did not contain the expected key" << string1;
	}

	{
		// Remove the element from the DB
		auto error = db.Remove("key");
		ASSERT_EQ(error::NoError, error);
		kvdb::Error expected_error(kvdb::MakeError(kvdb::KeyError, "Key Not found"));
		auto entry = db.Read("key");
		EXPECT_EQ(entry.error().code, expected_error.code);
	}
}

TEST_P(KeyValueDatabaseTest, TestWriteTransactionCommit) {
	kvdb::KeyValueDatabase &db = *GetParam().db;

	db.WriteTransaction([](kvdb::Transaction &txn) -> error::Error {
		auto data = txn.Read("foo");
		EXPECT_FALSE(data);

		txn.Write("foo", common::ByteVectorFromString("bar"));

		data = txn.Read("foo");
		EXPECT_TRUE(data);
		EXPECT_EQ(data.value(), common::ByteVectorFromString("bar"));

		txn.Write("test", common::ByteVectorFromString("val"));
		return error::NoError;
	});

	auto data = db.Read("foo");
	EXPECT_TRUE(data);
	EXPECT_EQ(data.value(), common::ByteVectorFromString("bar"));
	data = db.Read("test");
	EXPECT_TRUE(data);
	EXPECT_EQ(data.value(), common::ByteVectorFromString("val"));
	data = db.Read("bogus");
	EXPECT_FALSE(data);
	EXPECT_EQ(data.error().code, kvdb::MakeError(kvdb::KeyError, "Key Not found").code);
}

TEST_P(KeyValueDatabaseTest, TestWriteTransactionRollback) {
	kvdb::KeyValueDatabase &db = *GetParam().db;

	db.WriteTransaction([](kvdb::Transaction &txn) -> error::Error {
		txn.Write("foo", common::ByteVectorFromString("bar"));
		return error::NoError;
	});
	db.WriteTransaction([](kvdb::Transaction &txn) -> error::Error {
		txn.Write("test", common::ByteVectorFromString("val"));
		return kvdb::Error(make_error_condition(errc::io_error), "Some test error from I/O");
	});

	auto data = db.Read("foo");
	EXPECT_TRUE(data);
	EXPECT_EQ(data.value(), common::ByteVectorFromString("bar"));
	data = db.Read("test");
	EXPECT_FALSE(data);
	EXPECT_EQ(data.error().code, kvdb::MakeError(kvdb::KeyError, "Key Not found").code);
}

TEST_P(KeyValueDatabaseTest, TestReadTransaction) {
	kvdb::KeyValueDatabase &db = *GetParam().db;

	db.Write("foo", common::ByteVectorFromString("bar"));
	db.Write("test", common::ByteVectorFromString("val"));

	auto db_error = db.ReadTransaction([](kvdb::Transaction &txn) -> error::Error {
		auto data = txn.Read("foo");
		EXPECT_TRUE(data);
		EXPECT_EQ(data.value(), common::ByteVectorFromString("bar"));
		data = txn.Read("test");
		EXPECT_TRUE(data);
		EXPECT_EQ(data.value(), common::ByteVectorFromString("val"));
		data = txn.Read("bogus");
		EXPECT_FALSE(data);
		EXPECT_EQ(data.error().code, kvdb::MakeError(kvdb::KeyError, "Key Not found").code);
		return error::NoError;
	});

	ASSERT_EQ(error::NoError, db_error);
}

// ReadTransaction failure should not have any effect.
TEST_P(KeyValueDatabaseTest, TestReadTransactionFailure) {
	kvdb::KeyValueDatabase &db = *GetParam().db;

	db.Write("foo", common::ByteVectorFromString("bar"));
	db.Write("test", common::ByteVectorFromString("val"));

	auto err = kvdb::MakeError(kvdb::KeyError, "Some error");

	auto db_error = db.ReadTransaction([&err](kvdb::Transaction &txn) -> error::Error {
		auto data = txn.Read("foo");
		EXPECT_TRUE(data);
		EXPECT_EQ(data.value(), common::ByteVectorFromString("bar"));
		data = txn.Read("test");
		EXPECT_TRUE(data);
		EXPECT_EQ(data.value(), common::ByteVectorFromString("val"));
		data = txn.Read("bogus");
		EXPECT_FALSE(data);
		EXPECT_EQ(data.error().code, kvdb::MakeError(kvdb::KeyError, "Key Not found").code);
		return err;
	});

	ASSERT_NE(error::NoError, db_error);
	EXPECT_EQ(db_error, err);
}

#ifdef MENDER_USE_LMDB
TEST(KeyValueDatabaseLmdbTest, TestSomeLmdbExceptionPaths) {
	kvdb::KeyValueDatabaseLmdb db;
	auto err = db.Open("/non-existing-junk-path/leaf");
	ASSERT_NE(error::NoError, err);
	EXPECT_EQ(err.code, kvdb::MakeError(kvdb::LmdbError, "").code);
	EXPECT_THAT(err.message, testing::HasSubstr("No such file or directory")) << err.message;
}

TEST(KeyValueDatabaseLmdbTest, CorruptedDatabaseRecovery) {
	mtesting::TemporaryDirectory tmpdir;

	string db_path = path::Join(tmpdir.Path(), "db");
	string broken_db_path = db_path + "-broken";

	EXPECT_FALSE(path::FileExists(broken_db_path));

	kvdb::KeyValueDatabaseLmdb db;
	auto err = db.Open(db_path);
	ASSERT_EQ(error::NoError, err);

	EXPECT_FALSE(path::FileExists(broken_db_path));

	vector<uint8_t> data {'a', 'b', 'c'};
	EXPECT_EQ(error::NoError, db.Write("test_key", data));

	EXPECT_FALSE(path::FileExists(broken_db_path));

	db.Close();

	EXPECT_FALSE(path::FileExists(broken_db_path));

	{
		ofstream raw_db(db_path);
		for (int index = 0; index < 1024; index++) {
			// Yes, this will wrap, but we're just inserting random (but predictable)
			// data.
			raw_db << static_cast<uint8_t>(index);
		}
		ASSERT_TRUE(raw_db.good());
	}

	err = db.Open(db_path);
	ASSERT_EQ(error::NoError, err);

	EXPECT_TRUE(path::FileExists(broken_db_path));

	db.Close();

	// Restore the broken database, but this time block the backup.
	ASSERT_EQ(0, rename(broken_db_path.c_str(), db_path.c_str()));
	ASSERT_EQ(0, mkdir(broken_db_path.c_str(), 0755));

	err = db.Open(db_path);
	ASSERT_NE(error::NoError, err);
	EXPECT_THAT(err.String(), testing::HasSubstr("MDB_INVALID"));
	EXPECT_THAT(err.String(), testing::HasSubstr("Is a directory"));
}
#endif // MENDER_USE_LMDB
