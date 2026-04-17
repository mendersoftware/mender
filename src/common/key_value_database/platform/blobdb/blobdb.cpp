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

#include <common/key_value_database_blobdb.hpp>
#include <common/key_value_database/platform/blobdb/file_blob.hpp>

#include <limits>
#include <unordered_map>

#include <common/common.hpp>
#include <common/error.hpp>
#include <common/expected.hpp>
#include <common/io.hpp>
#include <common/log.hpp>
#include <common/path.hpp>

namespace mender {
namespace common {
namespace key_value_database {

namespace common = mender::common;
namespace io = mender::common::io;
namespace log = mender::common::log;
namespace path = mender::common::path;


expected::ExpectedBytes BlobdbTransaction::Read(const string &key) {
	if (!db_) {
		auto ex_db = DeserializeDB();
		if (!ex_db) {
			return expected::unexpected(ex_db.error().WithContext("Cannot read DB contents"));
		}
		db_ = make_unique<DB>(std::move(ex_db.value()));
	}
	if (!common::MapContainsStringKey(*db_, key)) {
		return expected::unexpected(MakeError(KeyError, "Key " + key + " not found in database"));
	}

	return db_->at(key);
}

error::Error BlobdbTransaction::Write(const string &key, const vector<uint8_t> &value) {
	if (!write_) {
		return MakeError(TransactionError, "Cannot write in a read transaction");
	}
	if (!db_) {
		auto ex_db = DeserializeDB();
		if (!ex_db) {
			return ex_db.error().WithContext("Cannot read DB contents");
		}
		db_ = make_unique<DB>(std::move(ex_db.value()));
	}

	(*db_)[key] = value;
	return error::NoError;
}

error::Error BlobdbTransaction::Remove(const string &key) {
	if (!write_) {
		return MakeError(TransactionError, "Cannot remove in a read transaction");
	}

	if (!db_) {
		auto ex_db = DeserializeDB();
		if (!ex_db) {
			return ex_db.error().WithContext("Cannot read DB contents");
		}
		db_ = make_unique<DB>(std::move(ex_db.value()));
	}
	db_->erase(key);
	return error::NoError;
}

error::Error BlobdbTransaction::Commit() {
	if (!db_) {
		// nothing to commit
		return error::NoError;
	}
	// write the in-memory DB to persistent storage
	auto err = SerializeDB(*db_);
	db_.reset();
	return err;
}

error::Error BlobdbTransaction::Abort() {
	// just throw away the in-memory DB
	db_.reset();
	return error::NoError;
}

BlobdbTransaction::~BlobdbTransaction() {
	auto err = Abort();
	if (err != error::NoError) {
		log::Error(
			"Uncommitted key-value DB transaction destroyed and abort failed: " + err.message);
	}
}

error::Error KeyValueDatabaseBlobdb::Open(const string &db_path_or_name) {
	db_path_or_name_ = db_path_or_name;

	return error::NoError;
}

error::Error KeyValueDatabaseBlobdb::Close() {
	return error::NoError;
}

error::Error KeyValueDatabaseBlobdb::RunTransaction(
	bool write, function<error::Error(Transaction &)> txnFunc) {
	FileBlobdbTransaction txn {db_path_or_name_, write};
	auto err = txn.LockDB();
	if (err != error::NoError) {
		return err;
	}
	err = txnFunc(txn);
	if (err == error::NoError) {
		err = txn.Commit();
	} else {
		// There is no way txn.Abort() could return an error now, but let's be
		// safe in case of future changes.
		auto ab_err = txn.Abort();
		if (ab_err != error::NoError) {
			err = err.FollowedBy(ab_err);
		}
	}
	auto unlock_err = txn.UnlockDB();
	if (unlock_err != error::NoError) {
		if (err != error::NoError) {
			return err.FollowedBy(unlock_err);
		} else {
			return unlock_err;
		}
	}
	return err;
}

error::Error KeyValueDatabaseBlobdb::WriteTransaction(
	function<error::Error(Transaction &)> txnFunc) {
	return RunTransaction(true, txnFunc);
}

error::Error KeyValueDatabaseBlobdb::ReadTransaction(
	function<error::Error(Transaction &)> txnFunc) {
	return RunTransaction(false, txnFunc);
}

} // namespace key_value_database
} // namespace common
} // namespace mender
