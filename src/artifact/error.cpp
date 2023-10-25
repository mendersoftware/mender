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

#include <artifact/error.hpp>

#include <cassert>
#include <string>

namespace mender {
namespace artifact {
namespace parser_error {

using namespace std;

namespace error = mender::common::error;

const ErrorCategoryClass ErrorCategory;

const char *ErrorCategoryClass::name() const noexcept {
	return "ParserErrorCategory";
}

string ErrorCategoryClass::message(int code) const {
	switch (code) {
	case NoError:
		return "Success";
	case ParseError:
		return "Parse error";
	case TypeError:
		return "Type error";
	case EOFError:
		return "EOF error";
	}
	assert(false);
	return "Unknown";
}

error::Error MakeError(Code code, const string &msg) {
	return error::Error(error_condition(code, ErrorCategory), msg);
}
} // namespace parser_error
} // namespace artifact
} // namespace mender
