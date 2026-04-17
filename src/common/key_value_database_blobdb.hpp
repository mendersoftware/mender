// Copyright 2026 Northern.tech AS
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

#ifndef MENDER_COMMON_BLOBDB_HPP
#define MENDER_COMMON_BLOBDB_HPP

#include <common/error.hpp>
#include <common/expected.hpp>
#include <common/key_value_database.hpp>

namespace mender {
namespace common {
namespace key_value_database {

namespace error = mender::common::error;
namespace expected = mender::common::expected;

using DB = unordered_map<string, vector<uint8_t>>;
using ExpectedDB = expected::expected<DB, error::Error>;

class BlobdbTransaction : public Transaction {
public:
	BlobdbTransaction(const string &path_or_name, bool write) :
		path_or_name_ {path_or_name},
		write_ {write} {};

	virtual ~BlobdbTransaction();

	expected::ExpectedBytes Read(const string &key) override;
	error::Error Write(const string &key, const vector<uint8_t> &value) override;
	error::Error Remove(const string &key) override;

	error::Error Commit();
	error::Error Abort();

	virtual error::Error SerializeDB(const DB &db) = 0;
	virtual ExpectedDB DeserializeDB() = 0;
	virtual error::Error LockDB() = 0;
	virtual error::Error UnlockDB() = 0;

protected:
	const string path_or_name_;
	bool write_;

private:
	unique_ptr<DB> db_;
};

// Note: Using KeyValueDatabaseBlobdb from multiple threads is not
// safe, but using separate instances to access the same database is safe.
class KeyValueDatabaseBlobdb : public KeyValueDatabase {
public:
	error::Error Open(const string &path_or_name);
	error::Error Close();

	error::Error WriteTransaction(function<error::Error(Transaction &)> txnFunc) override;
	error::Error ReadTransaction(function<error::Error(Transaction &)> txnFunc) override;

private:
	error::Error RunTransaction(bool write, function<error::Error(Transaction &)> txnFunc);
	string db_path_or_name_;
};

} // namespace key_value_database
} // namespace common
} // namespace mender

#endif // MENDER_COMMON_BLOBDB_HPP
