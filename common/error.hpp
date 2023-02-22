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

#include <string>
#include <system_error>
#include <type_traits>

#ifndef MENDER_COMMON_ERROR_HPP
#define MENDER_COMMON_ERROR_HPP

// Note that this may cause condition to be evaluated twice!
#define AssertOrReturnError(condition) AssertOrReturnErrorOnLine(condition, __LINE__)
#define AssertOrReturnErrorOnLine(condition, line)                                           \
	{                                                                                        \
		if (!(condition)) {                                                                  \
			assert(condition);                                                               \
			return mender::common::error::MakeError(                                         \
				mender::common::error::ProgrammingError,                                     \
				"Assert `" #condition "` in " __FILE__ ":" #line " failed. This is a bug."); \
		}                                                                                    \
	}

namespace mender {
namespace common {
namespace error {

class Error {
public:
	std::error_condition code;
	std::string message;

	Error(const std::error_condition &ec, const std::string &msg) :
		code(ec),
		message(msg) {
	}
	Error(const Error &e) :
		code(e.code),
		message(e.message) {
	}

	bool operator==(const Error &other) const {
		return this->message == other.message && this->code == other.code;
	}

	operator bool() const {
		return static_cast<bool>(this->code);
	}

	std::string String() const {
		return code.message() + ": " + message;
	}
};

extern const Error NoError;

enum ErrorCode {
	ErrorCodeNoError, // Conflicts with above name, we don't really need it so prefix it.
	ProgrammingError,
	GenericError, // For when you have no underlying error code, provide message instead.
};

class CommonErrorCategoryClass : public std::error_category {
public:
	const char *name() const noexcept override;
	std::string message(int code) const override;
};
extern const CommonErrorCategoryClass CommonErrorCategory;

Error MakeError(ErrorCode code, const std::string &msg);

} // namespace error
} // namespace common
} // namespace mender

#endif // MENDER_COMMON_ERROR_HPP
