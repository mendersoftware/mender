// Copyright 2022 Northern.tech AS
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

#include <iostream>
#include <fstream>
#include <nlohmann/json.hpp>
using njson = nlohmann::json;

#include <common/json/impl/nlohmann/nlohmann_json.hpp>

namespace json {

void NlohmannJson::hello_world() {
	njson data = njson::parse(R"(
  {
    "Hello": "World"
  }
)");

	int spaces_indent{4};

	std::cout << data.dump(spaces_indent) << std::endl;
}

} // namespace json
