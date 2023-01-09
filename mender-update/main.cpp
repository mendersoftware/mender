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

#include <memory>
#include <iostream>

using namespace std;

#include <common/json.hpp>
#include <common/kv_db.hpp>
#include <common/expected.hpp>

enum ExampleErrorCode {
	NoError = 0,
	SomeError,
};

void hello_world(std::shared_ptr<json::Json> j, std::shared_ptr<kv_db::KeyValueDB> db) {
	j->hello_world();
	db->hello_world();
}

int main() {
	shared_ptr<json::Json> j = make_shared<json::Json>();
	shared_ptr<kv_db::KeyValueDB> db = make_shared<kv_db::KeyValueDB>();

	hello_world(j, db);

	using ExampleError = mender::common::error::Error<ExampleErrorCode>;
	using ExpectedExampleString = mender::common::expected::Expected<string, ExampleError>;

	ExpectedExampleString ex_s = ExpectedExampleString("Hello, world!");

	ExampleError err = {ExampleErrorCode::SomeError, "Something wrong happened"};
	ExpectedExampleString ex_s_err = ExpectedExampleString(err);

	if (ex_s) {
		std::cout << "Got expected string value: '" << ex_s.value() << "'" << std::endl;
	} else {
		std::cout << "Got (un)expected error: '" << ex_s.error().message << "'" << std::endl;
	}

	if (ex_s_err) {
		std::cout << "Got expected string value: '" << ex_s_err.value() << "'" << std::endl;
	} else {
		std::cout << "Got (un)expected error: '" << ex_s_err.error().message << "'" << std::endl;
	}

	return 0;
}
