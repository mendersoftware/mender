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

#ifndef MENDER_SHA_HPP
#define MENDER_SHA_HPP

#include <config.h>

#include <ctime>
#include <iomanip>
#include <memory>
#include <string>
#include <sstream>
#include <vector>

#include <common/io.hpp>
#include <common/error.hpp>
#include <common/log.hpp>
#include <common/expected.hpp>

#ifdef MENDER_SHA_OPENSSL
#include <openssl/evp.h>
#endif

namespace mender {
namespace sha {

using namespace std;

namespace io = mender::common::io;
namespace expected = mender::common::expected;

namespace error = mender::common::error;

enum ErrorCode {
	NoError = 0,
	InitializationError,
	ShasumCreationError,
	ShasumMismatchError,
};

class ErrorCategoryClass : public std::error_category {
public:
	const char *name() const noexcept override;
	string message(int code) const override;
};

extern const ErrorCategoryClass KeyValueParserErrorCategory;

error::Error MakeError(ErrorCode code, const string &msg);

class SHA {
public:
	SHA() :
		sha_ {} {};
	SHA(vector<uint8_t> &v) :
		sha_ {v} {};
	string String() const {
		std::stringstream ss {};
		for (unsigned int i = 0; i < 32; ++i) {
			ss << std::hex << std::setw(2) << std::setfill('0') << (int) sha_.at(i);
		}
		return ss.str();
	}
	operator vector<uint8_t>() const {
		return sha_;
	}
	bool operator==(const string &other) const {
		return this->String() == other;
	}

	bool operator!=(const string &other) const {
		return not this->operator==(other);
	}

	bool operator==(const vector<uint8_t> other) const {
		return this->sha_ == other;
	}
	bool operator!=(const vector<uint8_t> other) const {
		return not this->operator==(other);
	}

private:
	vector<uint8_t> sha_ {};
};

using ExpectedSHA = expected::expected<SHA, error::Error>;

class Reader : virtual public io::Reader {
private:
#ifdef MENDER_SHA_OPENSSL
	std::unique_ptr<EVP_MD_CTX, void (*)(EVP_MD_CTX *)> sha_handle_;
#endif
	io::Reader &wrapped_reader_;
	std::string expected_sha_ {};
	bool initialized_ {false};
	bool done_ {false};
	SHA shasum_ {};

public:
	Reader(io::Reader &reader);
	Reader(io::Reader &reader, const std::string &expected_sha);

	expected::ExpectedSize Read(
		vector<uint8_t>::iterator start, vector<uint8_t>::iterator end) override;

	ExpectedSHA ShaSum();
};

} // namespace sha
} // namespace mender


#endif // MENDER_SHA_HPP
