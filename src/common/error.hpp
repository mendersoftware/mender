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

#include <functional>
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

// Note that this may cause condition to be evaluated twice!
#define AssertOrReturnUnexpected(condition) AssertOrReturnUnexpectedOnLine(condition, __LINE__)
#define AssertOrReturnUnexpectedOnLine(condition, line)                                       \
	{                                                                                         \
		if (!(condition)) {                                                                   \
			assert(condition);                                                                \
			return expected::unexpected(mender::common::error::MakeError(                     \
				mender::common::error::ProgrammingError,                                      \
				"Assert `" #condition "` in " __FILE__ ":" #line " failed. This is a bug.")); \
		}                                                                                     \
	}

namespace mender {
namespace common {
namespace error {

class Error {
public:
	std::error_condition code;
	std::string message;

	Error() {
	}
	Error(const std::error_condition &ec, const std::string &msg) :
		code {ec},
		message {msg} {
	}
	Error(const Error &e) :
		code {e.code},
		message {e.message} {
	}

	bool operator==(const Error &other) const {
		return this->message == other.message && this->code == other.code;
	}

	bool operator!=(const Error &other) const {
		return this->message != other.message || this->code != other.code;
	}

	std::string String() const {
		return code.message() + ": " + message;
	}

	bool IsErrno(int errno_value) {
		return (
			(this->code.category() == std::generic_category())
			&& (this->code.value() == errno_value));
	}

	Error FollowedBy(const Error &err) const;

	// Produces a new error with a context prefix, with the same error code.
	Error WithContext(const std::string &context) const;
};
std::ostream &operator<<(std::ostream &os, const Error &err);

extern const Error NoError;

enum ErrorCode {
	ErrorCodeNoError, // Conflicts with above name, we don't really need it so prefix it.
	ProgrammingError,
	GenericError, // For when you have no underlying error code, provide message instead.
	// Means that we do have an error, but don't print anything. Used for errors where the cli
	// already prints a nicely formatted message.
	ExitWithFailureError,
	// Means that we want to prevent further processing. Used for `--version` option.
	ExitWithSuccessError,
};

class CommonErrorCategoryClass : public std::error_category {
public:
	const char *name() const noexcept override;
	std::string message(int code) const override;
};
extern const CommonErrorCategoryClass CommonErrorCategory;

Error MakeError(ErrorCode code, const std::string &msg);

// Some parts of standard C++, such as regex, require exceptions. Use this function in cases where
// an exception cannot be avoided; it will automatically catch exceptions thrown by the given
// function, and produce an error from it. On platforms where exceptions are disabled, any attempt
// to throw them will abort instead, so if you use this function, do your utmost to avoid an
// exception ahead of time (make sure the regex is valid, for instance).
Error ExceptionToErrorOrAbort(std::function<void()> func);

} // namespace error
} // namespace common
} // namespace mender

#endif // MENDER_COMMON_ERROR_HPP
