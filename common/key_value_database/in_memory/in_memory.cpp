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
#include <common/key_value_database_in_memory.hpp>

#include <string>

namespace mender {
namespace common {
namespace key_value_database {

using namespace std;

class InMemoryTransaction : public Transaction {
public:
	InMemoryTransaction(KeyValueDatabaseInMemory &db, bool read_only);

	ExpectedBytes Read(const string &key) override;
	Error Write(const string &key, const vector<uint8_t> &value) override;
	Error Remove(const string &key) override;

private:
	KeyValueDatabaseInMemory &db_;
	bool read_only_;
};

InMemoryTransaction::InMemoryTransaction(KeyValueDatabaseInMemory &db, bool read_only) :
	db_(db),
	read_only_(read_only) {
}

ExpectedBytes InMemoryTransaction::Read(const string &key) {
	auto value = db_.map_.find(key);
	if (value != db_.map_.end()) {
		return ExpectedBytes(value->second);
	} else {
		return ExpectedBytes(MakeError(KeyError, "Key " + key + " not found in memory database"));
	}
}

Error InMemoryTransaction::Write(const string &key, const vector<uint8_t> &value) {
	AssertOrReturnError(!read_only_);
	db_.map_[key] = value;
	return error::NoError;
}

Error InMemoryTransaction::Remove(const string &key) {
	AssertOrReturnError(!read_only_);
	db_.map_.erase(key);
	return error::NoError;
}

ExpectedBytes KeyValueDatabaseInMemory::Read(const string &key) {
	ExpectedBytes ret {vector<uint8_t>()};
	auto err = ReadTransaction([&key, &ret](Transaction &txn) -> Error {
		ret = txn.Read(key);
		return error::NoError;
	});
	if (err) {
		return err;
	} else {
		return ret;
	}
}

Error KeyValueDatabaseInMemory::Write(const string &key, const vector<uint8_t> &value) {
	return WriteTransaction(
		[&key, &value](Transaction &txn) -> Error { return txn.Write(key, value); });
}

Error KeyValueDatabaseInMemory::Remove(const string &key) {
	return WriteTransaction([&key](Transaction &txn) -> Error { return txn.Remove(key); });
}

Error KeyValueDatabaseInMemory::WriteTransaction(function<Error(Transaction &)> txnFunc) {
	InMemoryTransaction txn(*this, false);
	// Simple, but inefficient rollback support.
	auto backup_map = this->map_;
	auto error = txnFunc(txn);
	if (error) {
		this->map_ = backup_map;
	}
	return error;
}

Error KeyValueDatabaseInMemory::ReadTransaction(function<Error(Transaction &)> txnFunc) {
	InMemoryTransaction txn(*this, true);
	return txnFunc(txn);
}

} // namespace key_value_database
} // namespace common
} // namespace mender
