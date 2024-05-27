// Copyright 2024 Northern.tech AS
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

#include <string>
#include <vector>

#include <artifact/sha/sha.hpp>

#include <common/io.hpp>


namespace mender {
namespace sha {

namespace io = mender::common::io;

const ErrorCategoryClass ErrorCategory = ErrorCategoryClass();

const char *ErrorCategoryClass::name() const noexcept {
	return "ShaSumErrorCategory";
}

string ErrorCategoryClass::message(int code) const {
	switch (code) {
	case NoError:
		return "Success";
	case InitializationError:
		return "Initialization error";
	case ShasumCreationError:
		return "Shasum creation error";
	case ShasumMismatchError:
		return "Shasum mismatch error";
	default:
		return "Unknown";
	}
}

error::Error MakeError(ErrorCode code, const string &msg) {
	return error::Error(error_condition(code, ErrorCategory), msg);
}


Reader::Reader(io::Reader &reader) :
	Reader::Reader {reader, ""} {
}

ExpectedSHA Shasum(const vector<uint8_t> &data) {
	string in {data.begin(), data.end()};

	io::StringReader is {in};

	Reader r {is};

	auto discard_writer = io::Discard {};

	auto err = io::Copy(discard_writer, r);
	if (err != error::NoError) {
		return expected::unexpected(err);
	}

	return r.ShaSum();
}

} // namespace sha
} // namespace mender
