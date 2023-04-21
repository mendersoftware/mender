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

#ifndef UPDATE_MODULE_CALL_HPP
#define UPDATE_MODULE_CALL_HPP

#include <mender-update/update_module/v3/update_module.hpp>


namespace mender {
namespace update {
namespace update_module {
namespace v3 {

using ExpectedExitStatus = expected::expected<int, error::Error>;

ExpectedExitStatus CallState(
	const string process, State state, const string directory, string &procOut);

} // namespace v3
} // namespace update_module
} // namespace update
} // namespace mender


#endif
