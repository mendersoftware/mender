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

#include <iostream>
#include <string>
#include <vector>

#include <common/conf.hpp>
#include <mender-update/context.hpp>

using namespace std;

int main(int argc, char *argv[]) {
	mender::common::conf::MenderConfig config;
	if (argc > 1) {
		vector<string> args(argv + 1, argv + argc);
		auto err = config.ProcessCmdlineArgs(args);
		if (mender::common::error::NoError != err) {
			cerr << "Failed to process command line options: " + err.message << endl;
			return 1;
		}
	} else {
		auto err = config.LoadDefaults();
		if (mender::common::error::NoError != err) {
			cerr << "Failed to process command line options: " + err.message << endl;
			return 1;
		}
	}

	mender::update::context::MenderContext main_context(config);
	auto err = main_context.Initialize();
	if (mender::common::error::NoError != err) {
		cerr << "Failed to intialize main context: " + err.message << endl;
		return 1;
	}

	return 0;
}
