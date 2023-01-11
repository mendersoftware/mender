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

#ifndef MENDER_COMMON_KV_DB_HPP
#define MENDER_COMMON_KV_DB_HPP

#include <common/error.hpp>
#include <common/expected.hpp>
#include <config.h>

#include <string>
#include <mutex>
#include <vector>
#include <map>
#include <functional>

namespace mender::common::kv_db {

using namespace std;

namespace expected = mender::common::expected;

enum KVDBErrorCode {
	NoError = 0,
	ParseError,
	KeyError,
};
using KVDBError = mender::common::error::Error<KVDBErrorCode>;

class DBEntry {
public:
	std::vector<std::byte> buf;
	std::string key;

public:
	DBEntry() {
	}
	DBEntry(const string &key) :
		key {key} {
	}
	DBEntry(const std::vector<std::byte> &val) :
		buf {val} {
	}
	DBEntry(const string &key, const std::vector<std::byte> &val) :
		key {key},
		buf {val} {
	}
	void Write(const string &value) {
		for (char c : value) {
			buf.push_back(std::byte(c));
		}
	}

	std::vector<std::byte> Read() {
		return buf;
	}
};

using ExpectedEntry = expected::Expected<DBEntry, KVDBError>;
using ExpectedBool = expected::Expected<bool, KVDBError>;

class KVDB {
private:
	mutex m {};
#ifdef MENDER_DB_IN_MEM
	map<string, std::vector<std::byte>> map_ {};
#endif


public:
	ExpectedEntry ReadAll(const string &key) const;
	ExpectedBool WriteAll(const string &key, const string &value);
	ExpectedBool Remove(const string &key);

	ExpectedBool Commit(const DBEntry &entry);

	const ExpectedEntry OpenRead(const string &key) const;
	DBEntry OpenWrite(const string &key);

	ExpectedBool Close() const;

	void WriteTransaction(std::function<void(KVDB &)> transaction) {
		this->m.lock();
		transaction(*this);
		this->m.unlock();
	}

	void ReadTransaction(std::function<void(const KVDB &)> transaction) {
		this->m.lock();
		transaction(*this);
		this->m.unlock();
	}
};

} // namespace mender::common::kv_db

#endif // MENDER_COMMON_KV_DB_HPP
