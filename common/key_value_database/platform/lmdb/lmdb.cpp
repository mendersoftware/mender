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
#include <common/key_value_database_lmdb.hpp>

namespace mender {
namespace common {
namespace key_value_database {

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
	txn_(txn),
	dbi_(dbi) {
}

expected::ExpectedBytes LmdbTransaction::Read(const string &key) {
	try {
		std::string_view value;
		bool exists = dbi_.get(txn_, key, value);
		if (!exists) {
			return MakeError(KeyError, "Key " + key + " not found in database");
		}

		return common::ByteVectorFromString(value);
	} catch (std::runtime_error &e) {
		return MakeError(LmdbError, e.what());
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

KeyValueDatabaseLmdb::KeyValueDatabaseLmdb() :
	env_(lmdb::env::create()),
	successfully_opened_(false) {
}

KeyValueDatabaseLmdb::~KeyValueDatabaseLmdb() {
	Close();
}

error::Error KeyValueDatabaseLmdb::Open(const string &path) {
	Close();

	try {
		env_.open(path.c_str(), MDB_NOSUBDIR, 0600);
	} catch (std::runtime_error &e) {
		return MakeError(LmdbError, e.what());
	}

	successfully_opened_ = true;
	return error::NoError;
}

void KeyValueDatabaseLmdb::Close() {
	if (!successfully_opened_) {
		return;
	}

	env_.close();
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
	if (err) {
		return err;
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
	AssertOrReturnError(successfully_opened_);

	try {
		lmdb::txn lmdb_txn = lmdb::txn::begin(env_, nullptr, 0);
		lmdb::dbi lmdb_dbi = lmdb::dbi::open(lmdb_txn, nullptr, 0);
		LmdbTransaction txn(lmdb_txn, lmdb_dbi);
		auto error = txnFunc(txn);
		if (error) {
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
	AssertOrReturnError(successfully_opened_);

	try {
		lmdb::txn lmdb_txn = lmdb::txn::begin(env_, nullptr, MDB_RDONLY);
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
