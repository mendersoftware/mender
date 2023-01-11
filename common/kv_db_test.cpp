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

#include <common/kv_db.hpp>

#include <gtest/gtest.h>
#include <gmock/gmock.h>

#include <fstream>


using namespace std;

TEST(KVDBTest, BasicReadWriteRemove) {
	namespace db = mender::common::kv_db;
	auto db_ = db::KVDB();

	{
		// Write
		auto ret = db_.WriteAll("key", "val");
		EXPECT_TRUE(ret);
	}

	{
		// Read
		auto entry = db_.ReadAll("key").value().Read();
		std::string string1(reinterpret_cast<const char *>(&entry[0]), entry.size());
		EXPECT_EQ(string1, "val") << "DB did not contain the expected key" << string1;
	}

	{
		// Remove the element from the DB
		auto ret = db_.Remove("key");
		EXPECT_TRUE(ret);
		const db::KVDBError expected_error = {db::KVDBErrorCode::KeyError, "Key Not found!"};
		EXPECT_EQ(db_.ReadAll("key").error(), expected_error);
	}
}

TEST(KVDBTest, TestOpenRead) {
	namespace db = mender::common::kv_db;
	auto db_ = db::KVDB();
	db_.WriteAll("testkey", "testvalue");

	auto entry = db_.OpenRead("testkey").value().Read();
	std::string string1(reinterpret_cast<const char *>(&entry[0]), entry.size());
	EXPECT_EQ(string1, "testvalue") << "DB did not contain the expected key" << string1;
}

TEST(KVDBTest, TestOpenWrite) {
	namespace db = mender::common::kv_db;
	auto db_ = db::KVDB();

	auto write_handle = db_.OpenWrite("bugs");
	write_handle.Write("bunny");
	auto ret = db_.Commit(write_handle);
	EXPECT_TRUE(ret);

	auto data = db_.ReadAll("bugs").value().Read();
	std::string string1(reinterpret_cast<const char *>(&data[0]), data.size());
	EXPECT_EQ(string1, "bunny") << "DB did not contain the expected key" << string1;
}

TEST(KVDBTest, TestWriteTransaction) {
	namespace db = mender::common::kv_db;
	auto db_ = db::KVDB();

	db_.WriteTransaction([](db::KVDB &db_handle) {
		db_handle.WriteAll("foo", "bar");
		db_handle.WriteAll("test", "val");
	});


	{
		auto data = db_.ReadAll("foo").value().Read();
		std::string string1(reinterpret_cast<const char *>(&data[0]), data.size());
		EXPECT_EQ(string1, "bar") << "DB did not contain the expected key" << string1;
	}

	{
		auto data = db_.ReadAll("test").value().Read();
		std::string string1(reinterpret_cast<const char *>(&data[0]), data.size());
		EXPECT_EQ(string1, "val") << "DB did not contain the expected key" << string1;
	}
}


TEST(KVDBTest, TestReadTransaction) {
	namespace db = mender::common::kv_db;
	auto db_ = db::KVDB();

	db_.WriteAll("foo", "bar");

	// Setup the expected value to pass in
	db::ExpectedEntry e = db::KVDBError {db::KVDBErrorCode::KeyError, "Key Not found!"};
	EXPECT_FALSE(e);
	db_.ReadTransaction([&e](const db::KVDB &db_handle) { e = db_handle.ReadAll("foo"); });
	EXPECT_TRUE(e);

	auto data = e.value().Read();
	std::string string1(reinterpret_cast<const char *>(&data[0]), data.size());
	EXPECT_EQ(string1, "bar") << "DB did not contain the expected key" << string1;
}
