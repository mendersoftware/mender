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

#ifndef MENDER_COMMON_OPTIONAL_HPP
#define MENDER_COMMON_OPTIONAL_HPP

#include <nonstd/optional.hpp>

namespace mender {
namespace common {
namespace optional {

using nonstd::nullopt;
using nonstd::optional;

} // namespace optional
} // namespace common
} // namespace mender

#endif // MENDER_COMMON_OPTIONAL_HPP
