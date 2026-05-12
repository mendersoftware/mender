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

#ifndef MENDER_COMMON_LMDB_HPP
#define MENDER_COMMON_LMDB_HPP

#include <common/error.hpp>
#include <common/expected.hpp>
#include <common/key_value_database.hpp>

#include <iostream>
#include <cstdio>

namespace lmdb {
class env;
}

namespace mender {
namespace common {
namespace key_value_database {

namespace error = mender::common::error;
namespace expected = mender::common::expected;

// Note: Using one instance of KeyValueDatabaseLmdb in multiple threads is not
// safe, but using separate instances to access the same database should be safe.

// However, LMDB versions 0.9.26 through 0.9.31 contain a bug (ITS#9278) where multiple
// `MDB_env` instances pointing to the same database file within a single
// process can fail. When one environment is closed, it destroys shared POSIX
// mutexes, causing subsequent operations on other environments to fail with
// "Invalid argument" errors.
// See:
// * https://github.com/LMDB/lmdb/commit/2fd44e325195ae81664eb5dc36e7d265927c5ebc
// * https://github.com/LMDB/lmdb/commit/3dde6c46e6c55458eadaf7f81492c822414be2c7
class KeyValueDatabaseLmdb : public KeyValueDatabase {
public:
	KeyValueDatabaseLmdb();
	~KeyValueDatabaseLmdb();

	error::Error Open(const string &path);
	void Close();

	expected::ExpectedBytes Read(const string &key) override;
	error::Error Write(const string &key, const vector<uint8_t> &value) override;
	error::Error Remove(const string &key) override;
	error::Error WriteTransaction(function<error::Error(Transaction &)> txnFunc) override;
	error::Error ReadTransaction(function<error::Error(Transaction &)> txnFunc) override;

private:
	error::Error OpenInternal(const string &path, bool try_recovery);

	unique_ptr<lmdb::env> env_;
};

} // namespace key_value_database
} // namespace common
} // namespace mender

#endif // MENDER_COMMON_LMDB_HPP
