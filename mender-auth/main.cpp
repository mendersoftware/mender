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

#include <common/conf.hpp>
#include <common/http.hpp>
#include <common/log.hpp>
#include <common/setup.hpp>

#include <mender-auth/ipc/server.hpp>

using namespace std;

namespace conf = mender::common::conf;
namespace error = mender::common::error;
namespace events = mender::common::events;
namespace http = mender::http;
namespace ipc = mender::auth::ipc;
namespace mlog = mender::common::log;
namespace setup = mender::common::setup;

int main(int argc, char *argv[]) {
	setup::GlobalSetup();

	conf::MenderConfig config;
	if (argc > 1) {
		vector<string> args(argv + 1, argv + argc);
		auto success = config.ProcessCmdlineArgs(args.begin(), args.end());
		if (!success) {
			cerr << "Failed to process command line options: " + success.error().String() << endl;
			return 1;
		}
	} else {
		auto err = config.LoadDefaults();
		if (err != error::NoError) {
			cerr << "Failed to process command line options: " + err.String() << endl;
			return 1;
		}
	}

	events::EventLoop loop {};

	auto ipc_server {ipc::Server(loop, config)};

	const string server_url {"http://127.0.0.1:8001"};

	auto err = ipc_server.Listen(server_url);
	if (err != error::NoError) {
		mlog::Error("Failed to start the listen loop");
		mlog::Error(err.String());
		return 1;
	}

	loop.Run();
	mlog::Info("Finished running the main loop!");

	return 0;
}
