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

#include <common/common.hpp>

namespace mender {
namespace common {
namespace key_value_database {

namespace common = mender::common;

const KeyValueDatabaseErrorCategoryClass KeyValueDatabaseErrorCategory =
	KeyValueDatabaseErrorCategoryClass();

const char *KeyValueDatabaseErrorCategoryClass::name() const noexcept {
	return "KeyValueDatabaseErrorCategory";
}

string KeyValueDatabaseErrorCategoryClass::message(int code) const {
	switch (code) {
	case NoError:
		return "Success";
	case KeyError:
		return "Key error";
	case LmdbError:
		return "LMDB error";
	case AlreadyExistsError:
		return "Key already exists";
	default:
		return "Unknown";
	}
}

Error MakeError(ErrorCode code, const string &msg) {
	return Error(error_condition(code, KeyValueDatabaseErrorCategory), msg);
}

Error ReadString(Transaction &txn, const string &key, string &value_str, bool missing_ok) {
	auto ex_bytes = txn.Read(key);
	if (!ex_bytes) {
		if (!missing_ok || (ex_bytes.error().code != MakeError(KeyError, "").code)) {
			return ex_bytes.error();
		}
	} else {
		value_str = common::StringFromByteVector(ex_bytes.value());
	}
	return error::NoError;
}

} // namespace key_value_database
} // namespace common
} // namespace mender
