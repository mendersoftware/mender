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
#include <memory>

using namespace std;

#include <common/json/json.hpp>

// This function only knows about the interface Json
void hello_world(std::shared_ptr<json::Json> j) {
	j->hello_world();
}

#include <common/json/impl/nlohmann/nlohmann_json.hpp>

int main() {
	// It is here that we make an object from a concrete type, BoostJson.
	shared_ptr<json::NlohmannJson> j = make_shared<json::NlohmannJson>();

	hello_world(j);

	return 0;
}
