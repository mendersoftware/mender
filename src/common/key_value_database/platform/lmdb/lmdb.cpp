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

#include <common/key_value_database_lmdb.hpp>

#include <filesystem>

#include <lmdbxx/lmdb++.h>

#include <common/common.hpp>
#include <common/log.hpp>

namespace mender {
namespace common {
namespace key_value_database {

namespace fs = std::filesystem;

namespace log = mender::common::log;

const string kBrokenSuffix {"-broken"};

class LmdbTransaction : public Transaction {
public:
	LmdbTransaction(lmdb::txn &txn, lmdb::dbi &dbi);

	expected::ExpectedBytes Read(const string &key) override;
	error::Error Write(const string &key, const vector<uint8_t> &value) override;
	error::Error Remove(const string &key) override;

private:
	lmdb::txn &txn_;
	lmdb::dbi &dbi_;
};

LmdbTransaction::LmdbTransaction(lmdb::txn &txn, lmdb::dbi &dbi) :
	txn_ {txn},
	dbi_ {dbi} {
}

expected::ExpectedBytes LmdbTransaction::Read(const string &key) {
	try {
		std::string_view value;
		bool exists = dbi_.get(txn_, key, value);
		if (!exists) {
			return expected::unexpected(
				MakeError(KeyError, "Key " + key + " not found in database"));
		}

		return common::ByteVectorFromString(value);
	} catch (std::runtime_error &e) {
		return expected::unexpected(MakeError(LmdbError, e.what()));
	}
}

error::Error LmdbTransaction::Write(const string &key, const vector<uint8_t> &value) {
	try {
		string_view data(reinterpret_cast<const char *>(value.data()), value.size());
		bool exists = dbi_.put(txn_, key, data);
		if (!exists) {
			return MakeError(AlreadyExistsError, "Key " + key + " already exists");
		}

		return error::NoError;
	} catch (std::runtime_error &e) {
		return MakeError(LmdbError, e.what());
	}
}

error::Error LmdbTransaction::Remove(const string &key) {
	try {
		// We don't treat !exists as an error, just ignore return code.
		dbi_.del(txn_, key);

		return error::NoError;
	} catch (std::runtime_error &e) {
		return MakeError(LmdbError, e.what());
	}
}

KeyValueDatabaseLmdb::KeyValueDatabaseLmdb() {
}

KeyValueDatabaseLmdb::~KeyValueDatabaseLmdb() {
	Close();
}

error::Error KeyValueDatabaseLmdb::Open(const string &path) {
	return OpenInternal(path, true);
}

error::Error KeyValueDatabaseLmdb::OpenInternal(const string &path, bool try_recovery) {
	Close();

	env_ = make_unique<lmdb::env>(lmdb::env::create());

	try {
		env_->open(path.c_str(), MDB_NOSUBDIR, 0600);
	} catch (std::runtime_error &e) {
		auto err {MakeError(LmdbError, e.what()).WithContext("Opening LMDB database failed")};

		if (not try_recovery) {
			env_.reset();
			return err;
		}

		try {
			if (not fs::exists(path)) {
				env_.reset();
				return err;
			}

			log::Warning(
				"Failure to open database. Attempting to recover by resetting. The original database can be found at `"
				+ path + kBrokenSuffix + "`");

			fs::rename(path, path + kBrokenSuffix);

			auto err2 = OpenInternal(path, false);
			if (err2 != error::NoError) {
				return err.FollowedBy(err2);
			}
			return error::NoError;
		} catch (fs::filesystem_error &e) {
			env_.reset();
			return err.FollowedBy(error::Error(e.code().default_error_condition(), e.what())
									  .WithContext("Opening LMDB database failed"));
		} catch (std::runtime_error &e) {
			env_.reset();
			return err.FollowedBy(error::MakeError(error::GenericError, e.what())
									  .WithContext("Opening LMDB database failed"));
		}
	}

	return error::NoError;
}

void KeyValueDatabaseLmdb::Close() {
	env_.reset();
}

expected::ExpectedBytes KeyValueDatabaseLmdb::Read(const string &key) {
	vector<uint8_t> ret;
	auto err = ReadTransaction([&key, &ret](Transaction &txn) -> error::Error {
		auto result = txn.Read(key);
		if (result) {
			ret = std::move(result.value());
			return error::NoError;
		} else {
			return result.error();
		}
	});
	if (mender::common::error::NoError != err) {
		return expected::unexpected(err);
	} else {
		return ret;
	}
}

error::Error KeyValueDatabaseLmdb::Write(const string &key, const vector<uint8_t> &value) {
	return WriteTransaction(
		[&key, &value](Transaction &txn) -> error::Error { return txn.Write(key, value); });
}

error::Error KeyValueDatabaseLmdb::Remove(const string &key) {
	return WriteTransaction([&key](Transaction &txn) -> error::Error { return txn.Remove(key); });
}

error::Error KeyValueDatabaseLmdb::WriteTransaction(function<error::Error(Transaction &)> txnFunc) {
	AssertOrReturnError(env_);

	try {
		lmdb::txn lmdb_txn = lmdb::txn::begin(*env_, nullptr, 0);
		lmdb::dbi lmdb_dbi = lmdb::dbi::open(lmdb_txn, nullptr, 0);
		LmdbTransaction txn(lmdb_txn, lmdb_dbi);
		auto error = txnFunc(txn);
		if (error::NoError != error) {
			lmdb_txn.abort();
		} else {
			lmdb_txn.commit();
		}
		return error;
	} catch (std::runtime_error &e) {
		return MakeError(LmdbError, e.what());
	}
}

error::Error KeyValueDatabaseLmdb::ReadTransaction(function<error::Error(Transaction &)> txnFunc) {
	AssertOrReturnError(env_);

	try {
		lmdb::txn lmdb_txn = lmdb::txn::begin(*env_, nullptr, MDB_RDONLY);
		lmdb::dbi lmdb_dbi = lmdb::dbi::open(lmdb_txn, nullptr, 0);
		LmdbTransaction txn(lmdb_txn, lmdb_dbi);
		return txnFunc(txn);
	} catch (std::runtime_error &e) {
		return MakeError(LmdbError, e.what());
	}
}

} // namespace key_value_database
} // namespace common
} // namespace mender
