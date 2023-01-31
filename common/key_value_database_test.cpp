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

#include <common/common.hpp>
#include <common/key_value_database.hpp>
#include <common/key_value_database_in_memory.hpp>

#include <gtest/gtest.h>
#include <gmock/gmock.h>

#include <fstream>

using namespace std;

namespace common = mender::common;
namespace error = mender::common::error;
namespace kvdb = mender::common::key_value_database;

TEST(KeyValueDatabaseTest, BasicReadWriteRemove) {
	kvdb::KeyValueDatabaseInMemory mem_db;
	kvdb::KeyValueDatabase &db = mem_db;

	{
		// Write
		auto error = db.Write("key", mender::common::ByteVectorFromString("val"));
		EXPECT_FALSE(error);
	}

	{
		// Read
		auto entry = db.Read("key");
		ASSERT_TRUE(entry);
		std::string string1(common::StringFromByteVector(entry.value()));
		EXPECT_EQ(string1, "val") << "DB did not contain the expected key" << string1;
	}

	{
		// Remove the element from the DB
		auto error = db.Remove("key");
		EXPECT_FALSE(error);
		kvdb::Error expected_error(kvdb::MakeError(kvdb::KeyError, "Key Not found"));
		auto entry = db.Read("key");
		EXPECT_EQ(entry.error().code, expected_error.code);
	}
}

TEST(KeyValueDatabaseTest, TestWriteTransactionCommit) {
	kvdb::KeyValueDatabaseInMemory mem_db;
	kvdb::KeyValueDatabase &db = mem_db;

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

TEST(KeyValueDatabaseTest, TestWriteTransactionRollback) {
	kvdb::KeyValueDatabaseInMemory mem_db;
	kvdb::KeyValueDatabase &db = mem_db;

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

TEST(KeyValueDatabaseTest, TestReadTransaction) {
	kvdb::KeyValueDatabaseInMemory mem_db;
	kvdb::KeyValueDatabase &db = mem_db;

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

	EXPECT_FALSE(db_error);
}

// ReadTransaction failure should not have any effect.
TEST(KeyValueDatabaseTest, TestReadTransactionFailure) {
	kvdb::KeyValueDatabaseInMemory mem_db;
	kvdb::KeyValueDatabase &db = mem_db;

	db.Write("foo", common::ByteVectorFromString("bar"));
	db.Write("test", common::ByteVectorFromString("val"));

	auto err = kvdb::MakeError(kvdb::ParseError, "Some error");

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

	EXPECT_TRUE(db_error);
	EXPECT_EQ(db_error, err);
}
