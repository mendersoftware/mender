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

#ifndef JSON_HPP
#define JSON_HPP

#include <common/error.hpp>
#include <common/expected.hpp>

namespace json {

enum JsonErrorCode {
	NoError = 0,
	KeyNoExist,
	TypeError,
};
using JsonError = mender::common::error::Error<JsonErrorCode>;

class Json {
public:
	using ExpectedJson = mender::common::expected::Expected<Json, JsonError>;
	void hello_world();
};

using ExpectedJson = mender::common::expected::Expected<Json, JsonError>;

} // namespace json

#endif // JSON_HPP
