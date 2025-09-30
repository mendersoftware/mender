// Copyright 2025 Northern.tech AS
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

#include <common/device_tier.hpp>

namespace mender {
namespace common {
namespace device_tier {

const std::string kStandard = "standard";
const std::string kSystem = "system";
const std::string kMicro = "micro";

bool IsValid(const std::string &tier) {
	return tier == kStandard || tier == kSystem || tier == kMicro;
}

} // namespace device_tier
} // namespace common
} // namespace mender
