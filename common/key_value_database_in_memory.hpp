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

#ifndef MENDER_COMMON_KEY_VALUE_DATABASE_IN_MEMORY_HPP
#define MENDER_COMMON_KEY_VALUE_DATABASE_IN_MEMORY_HPP

#include <common/key_value_database.hpp>

#include <unordered_map>

namespace mender::common::key_value_database {

using namespace std;

class KeyValueDatabaseInMemory : public KeyValueDatabase {
public:
	ExpectedBytes Read(const string &key) override;
	Error Write(const string &key, const vector<uint8_t> &value) override;
	Error Remove(const string &key) override;
	Error WriteTransaction(function<Error(Transaction &)> txnFunc) override;
	Error ReadTransaction(function<Error(Transaction &)> txnFunc) override;

private:
	unordered_map<string, vector<uint8_t>> map_;
	unordered_map<string, vector<uint8_t>> backup_map_;
	friend class InMemoryTransaction;
};

} // namespace mender::common::key_value_database

#endif // MENDER_COMMON_KEY_VALUE_DATABASE_IN_MEMORY_HPP
