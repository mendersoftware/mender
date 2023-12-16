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

#include <string>
#include <vector>

#include <common/setup.hpp>
#include <mender-update/cli/cli.hpp>

using namespace std;

int main(int argc, char *argv[]) {
	mender::common::setup::GlobalSetup();

	vector<string> args;
	if (argc > 1) {
		args = vector<string>(argv + 1, argv + argc);
	}

	return mender::update::cli::Main(args);
}
