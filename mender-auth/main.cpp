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

#include <memory>

using namespace std;

#include <common/json.hpp>
#include <common/kv_db.hpp>

void hello_world(std::shared_ptr<json::Json> j, std::shared_ptr<kv_db::KeyValueDB> db) {
	j->hello_world();
	db->hello_world();
}

int main() {
	shared_ptr<json::Json> j = make_shared<json::Json>();
	shared_ptr<kv_db::KeyValueDB> db = make_shared<kv_db::KeyValueDB>();

	hello_world(j, db);

	return 0;
}
