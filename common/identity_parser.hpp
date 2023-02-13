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

#ifndef MENDER_COMMON_IDENTITY_PARSER_HPP
#define MENDER_COMMON_IDENTITY_PARSER_HPP

#include <common/key_value_parser.hpp>

namespace mender {
namespace common {
namespace identity_parser {

using namespace std;
namespace kvp = mender::common::key_value_parser;

kvp::ExpectedKeyValuesMap GetIdentityData(const string &identity_data_generator);

} // namespace identity_parser
} // namespace common
} // namespace mender

#endif // MENDER_COMMON_IDENTITY_PARSER_HPP
