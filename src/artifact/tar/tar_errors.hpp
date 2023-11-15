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

#ifndef MENDER_COMMON_TAR_ERRORS_HPP
#define MENDER_COMMON_TAR_ERRORS_HPP

#include <common/error.hpp>

namespace mender {
namespace tar {

using namespace std;

namespace error = mender::common::error;

using Error = error::Error;

enum ErrorCode {
	NoError = 0,
	TarReaderError,
	TarShortReadError,
	TarEntryError,
	TarEOFError,
	TarExtraDataError,
};

class ErrorCategoryClass : public std::error_category {
public:
	const char *name() const noexcept override;
	string message(int code) const override;
};

extern const ErrorCategoryClass ErrorCategory;

Error MakeError(ErrorCode code, const string &msg);
} // namespace tar
} // namespace mender

#endif // MENDER_COMMON_TAR_ERRORS_HPP
