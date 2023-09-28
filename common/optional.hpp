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

// We need a dedicated define to track the standard. We can't detect it using regular compiler
// macros, because even when compiling for C++11, certain files need to compile under C++17 due to
// requirements by libraries. But in that case we still need to use `nonstd::optional` instead of
// `std::optional`, even though the latter is available, otherwise we can't link it all at the end.
#if MENDER_CXX_STANDARD >= 17

#include <optional>

#else // MENDER_CXX_STANDARD >= 17

// optional-lite is not binary compatible between C++ versions. This is important, since (as per
// 2023-05) we build cross-platform files with C++11, and platform files with (optionally) a later
// version. And then mix them.
//
// Luckily, it's possible to force optional-lite to stick with C++11 regardless of what the compiler
// is using. We don't need any later features in this particular library, so just stick to C++11 all
// the time.
#define optional_CPLUSPLUS 201103L
#include <nonstd/optional.hpp>

#endif // MENDER_CXX_STANDARD >= 17

namespace mender {

#if MENDER_CXX_STANDARD >= 17

using std::nullopt;
using std::optional;

#else // MENDER_CXX_STANDARD >= 17

using nonstd::nullopt;
using nonstd::optional;

#endif // MENDER_CXX_STANDARD >= 17

} // namespace mender

#endif // MENDER_COMMON_OPTIONAL_HPP
