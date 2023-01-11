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

#include <string>

namespace mender::common::kv_db {

using namespace std;

ExpectedEntry KVDB::ReadAll(const string &key) const {
	return OpenRead(key);
}

ExpectedBool KVDB::WriteAll(const string &key, const string &value) {
	auto db_entry = OpenWrite(key);

	db_entry.Write(value);

	this->Commit(db_entry);

	return ExpectedBool(true);
}

ExpectedBool KVDB::Commit(const DBEntry &entry) {
	this->map_[entry.key] = entry.buf;

	return ExpectedBool(true);
}

ExpectedBool KVDB::Remove(const string &key) {
	auto fs = map_.find(key);
	map_.erase(fs);
	return ExpectedBool(true);
}

const ExpectedEntry KVDB::OpenRead(const string &key) const {
	if (auto search = map_.find(key); search != map_.end()) {
		return ExpectedEntry(DBEntry {search->second});
	}
	// TODO - This should mirror the I/O errors in mender-go
	KVDBError err = {KVDBErrorCode::KeyError, "Key Not found!"};
	return ExpectedEntry(err);
}

DBEntry KVDB::OpenWrite(const string &key) {
	// TODO - Make sure we don't do this on a read-only db
	return DBEntry {key};
}

ExpectedBool KVDB::Close() const {
	return ExpectedBool(true);
}

} // namespace mender::common::kv_db
