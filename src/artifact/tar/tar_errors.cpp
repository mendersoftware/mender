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

#include <artifact/tar/tar_errors.hpp>

#include <cassert>

namespace mender {
namespace tar {

const ErrorCategoryClass ErrorCategory {};

const char *ErrorCategoryClass::name() const noexcept {
	return "TarErrorCategory";
}

string ErrorCategoryClass::message(int code) const {
	switch (code) {
	case NoError:
		return "Success";
	case TarReaderError:
		return "Error reading the tar stream";
	case TarShortReadError:
		return "Short read error";
	case TarEntryError:
		return "Error reading the tar entry";
	case TarEOFError:
		return "Archive EOF reached";
	case TarExtraDataError:
		return "Superfluous data at the end of the archive";
	}
	assert(false);
	return "Unknown";
}

Error MakeError(ErrorCode code, const string &msg) {
	return error::Error(error_condition(code, ErrorCategory), msg);
}

} // namespace tar
} // namespace mender
