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

#ifndef MENDER_COMMON_KEY_VALUE_DATABASE_HPP
#define MENDER_COMMON_KEY_VALUE_DATABASE_HPP

#include <common/error.hpp>
#include <common/expected.hpp>
#include <common/io.hpp>
#include <config.h>

#include <functional>
#include <string>
#include <vector>

namespace mender::common::key_value_database {

using namespace std;

namespace expected = mender::common::expected;

class KeyValueDatabaseErrorCategoryClass : public std::error_category {
public:
	const char *name() const noexcept override;
	string message(int code) const override;
};
extern const KeyValueDatabaseErrorCategoryClass KeyValueDatabaseErrorCategory;

enum ErrorCode {
	NoError = 0,
	ParseError,
	KeyError,
};
using Error = mender::common::error::Error;

using ExpectedBytes = expected::ExpectedBytes;

class Transaction {
public:
	virtual ~Transaction() {};

	virtual ExpectedBytes Read(const string &key) = 0;
	virtual Error Write(const string &key, const vector<uint8_t> &value) = 0;
	virtual Error Remove(const string &key) = 0;
};

// Works as a transaction interface as well, which auto-creates a transaction
// for each operation.
class KeyValueDatabase : virtual public Transaction {
public:
	virtual Error WriteTransaction(function<Error(Transaction &)> txnFunc) = 0;
	virtual Error ReadTransaction(function<Error(Transaction &)> txnFunc) = 0;
};

Error MakeError(ErrorCode code, const string &msg);

} // namespace mender::common::key_value_database

#endif // MENDER_COMMON_KEY_VALUE_DATABASE_HPP
