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

#include <common/common.hpp>
#include <common/error.hpp>

#include <cerrno>
#include <cstdlib>

namespace mender {
namespace common {

mender::common::expected::ExpectedLongLong StringToLongLong(const string &str, int base) {
	char *end;
	errno = 0;
	long long num = strtoll(str.c_str(), &end, base);
	if (errno != 0) {
		int int_error = errno;
		return mender::common::error::Error(
			std::error_code(int_error, std::system_category()).default_error_condition(), "");
	}
	if (end != &*str.end()) {
		return mender::common::error::Error(
			std::make_error_condition(errc::invalid_argument),
			str + " had trailing non-numeric data");
	}

	return num;
}

} // namespace common
} // namespace mender
