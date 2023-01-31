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

#include <common/error.hpp>

namespace mender::common::error {

const Error NoError = Error(std::error_condition(), "");

const CommonErrorCategoryClass CommonErrorCategory;

Error MakeError(ErrorCode code, const std::string &msg) {
	return Error(std::error_condition(code, CommonErrorCategory), msg);
}

const char *CommonErrorCategoryClass::name() const noexcept {
	return "CommonErrorCategory";
}

std::string CommonErrorCategoryClass::message(int code) const {
	switch (code) {
	case ErrorCodeNoError:
		return "No error";
	case ProgrammingError:
		return "Programming error, should not happen";
	default:
		return "Unknown";
	}
}

} // namespace mender::common::error
