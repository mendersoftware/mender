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

namespace mender::common::key_value_database {

using namespace std;

class InMemoryTransaction : public Transaction {
public:
	InMemoryTransaction(KeyValueDatabaseInMemory &db, bool read_only);

	ExpectedBytes Read(const string &key) override;
	ExpectedBool Write(const string &key, const vector<uint8_t> &value) override;
	ExpectedBool Remove(const string &key) override;

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

ExpectedBool InMemoryTransaction::Write(const string &key, const vector<uint8_t> &value) {
	assert(!read_only_);
	db_.map_[key] = value;
	return ExpectedBool(true);
}

ExpectedBool InMemoryTransaction::Remove(const string &key) {
	assert(!read_only_);
	db_.map_.erase(key);
	return ExpectedBool(true);
}

ExpectedBytes KeyValueDatabaseInMemory::Read(const string &key)
{
	ExpectedBytes ret{vector<uint8_t>()};
	auto result = ReadTransaction([&key, &ret](Transaction &txn) -> ExpectedBool {
		ret = txn.Read(key);
		return ExpectedBool(true);
	});
	return ret;
}

ExpectedBool KeyValueDatabaseInMemory::Write(const string &key, const vector<uint8_t> &value)
{
	ExpectedBool ret{true};
	auto result = WriteTransaction([&key, &value, &ret](Transaction &txn) -> ExpectedBool {
		ret = txn.Write(key, value);
		return ExpectedBool(true);
	});
	return ret;
}

ExpectedBool KeyValueDatabaseInMemory::Remove(const string &key)
{
	ExpectedBool ret{true};
	auto result = WriteTransaction([&key, &ret](Transaction &txn) -> ExpectedBool {
		ret = txn.Remove(key);
		return ExpectedBool(true);
	});
	return ret;
}

ExpectedBool KeyValueDatabaseInMemory::WriteTransaction(function<ExpectedBool(Transaction &)> txnFunc)
{
	InMemoryTransaction txn(*this, false);
	// Simple, but inefficient rollback support.
	this->backup_map_ = this->map_;
	auto result = txnFunc(txn);
	if (!result) {
		this->map_ = this->backup_map_;
	}
	return result;
}

ExpectedBool KeyValueDatabaseInMemory::ReadTransaction(function<ExpectedBool(Transaction &)> txnFunc)
{
	InMemoryTransaction txn(*this, true);
	return txnFunc(txn);
}

} // namespace mender::common::key_value_database
