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

namespace mender {
namespace common {
namespace error {

Error ExceptionToErrorOrAbort(std::function<void()> func) {
	try {
		func();
	} catch (exception &e) {
		return MakeError(GenericError, e.what());
	} catch (...) {
		return MakeError(GenericError, "Unknown exception");
	}
	return NoError;
}

} // namespace error
} // namespace common
} // namespace mender
