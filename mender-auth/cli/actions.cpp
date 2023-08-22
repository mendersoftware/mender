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

#include <mender-auth/cli/actions.hpp>

#include <mender-auth/context.hpp>

#include <common/conf.hpp>
#include <common/events.hpp>
#include <common/log.hpp>
#include <common/path.hpp>

#include <mender-auth/ipc/server.hpp>

namespace mender {
namespace auth {
namespace cli {

using namespace std;

namespace events = mender::common::events;
namespace mlog = mender::common::log;
namespace path = mender::common::path;

namespace ipc = mender::auth::ipc;

DaemonAction::DaemonAction(unique_ptr<crypto::PrivateKey> &&private_key) :
	private_key_(move(private_key)) {
}

ExpectedActionPtr DaemonAction::Create(const conf::MenderConfig &config, const string &passphrase) {
	string pem_file = path::Join(config.paths.GetDataStore(), config.paths.GetKeyFile());
	auto ex_private_key = crypto::PrivateKey::LoadFromPEM(pem_file, passphrase);
	if (ex_private_key) {
		return make_shared<DaemonAction>(move(ex_private_key.value()));
	}
	return expected::unexpected(ex_private_key.error());
}

error::Error DaemonAction::Execute(context::MenderContext &main_context) {
	events::EventLoop loop {};

	auto ipc_server {ipc::Server(loop, main_context.GetConfig())};

	const string server_url {"http://127.0.0.1:8001"};

	auto err = ipc_server.Listen(server_url);
	if (err != error::NoError) {
		mlog::Error("Failed to start the listen loop");
		mlog::Error(err.String());
		return error::MakeError(error::ExitWithFailureError, "");
	}

	loop.Run();

	return error::NoError;
}

} // namespace cli
} // namespace auth
} // namespace mender
