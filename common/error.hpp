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
#include <type_traits>

#ifndef MENDER_COMMON_ERROR_HPP
#define MENDER_COMMON_ERROR_HPP

namespace mender::common::error {

template <typename ErrorCodeType>
class Error {
public:
	static_assert(
		std::is_enum<ErrorCodeType>::value, "Error requires an enum type for error codes");
	ErrorCodeType error_code;
	std::string message;

	Error(const ErrorCodeType &ec, const std::string &msg) {
		this->error_code = ec;
		this->message = msg;
	};
	Error(const Error &e) {
		this->error_code = e.error_code;
		this->message = e.message;
	};

	bool operator==(const Error &other) const {
		return this->message == other.message && this->error_code == other.error_code;
	}
};

} // namespace mender::common::error

#endif // MENDER_COMMON_ERROR_HPP
