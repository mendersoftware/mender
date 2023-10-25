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

#include <artifact/v3/scripts/error.hpp>

#include <cassert>

#include <string>

namespace mender {
namespace artifact {
namespace scripts {
namespace executor {

using namespace std;

const ErrorCategoryClass ErrorCategory;

const char *ErrorCategoryClass::name() const noexcept {
	return "ArtifactScriptExecutorCategory";
}

string ErrorCategoryClass::message(int code) const {
	switch (code) {
	case NoError:
		return "Success";
	case VersionFileError:
		return "Version file error";
	case SetupError:
		return "Setup error";
	case CollectionError:
		return "Failed to collect the scripts";
	case NonZeroExitStatusError:
		return "NonZero exit code error";
	case RetryExitCodeError:
		return "Retry exit code error";
	}
	assert(false);
	return "Unknown";
}

error::Error MakeError(Code code, const string &msg) {
	return error::Error(error_condition(code, ErrorCategory), msg);
}

} // namespace executor
} // namespace scripts
} // namespace artifact
} // namespace mender
