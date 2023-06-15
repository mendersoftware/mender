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

#ifndef MENDER_API_HPP
#define MENDER_API_HPP

#include <vector>

#include <common/common.hpp>
#include <common/expected.hpp>
#include <common/json.hpp>

namespace mender {
namespace api {

namespace common = mender::common;
namespace expected = mender::common::expected;
namespace json = mender::common::json;

static inline expected::ExpectedString ErrorMsgFromErrorResponse(
	const std::vector<uint8_t> &response) {
	auto ex_j = json::Load(common::StringFromByteVector(response));
	if (!ex_j) {
		return expected::unexpected(ex_j.error());
	} else {
		return ex_j.value().Get("error").and_then(json::ToString);
	}
}

} // namespace api
} // namespace mender

#endif // MENDER_API_HPP
