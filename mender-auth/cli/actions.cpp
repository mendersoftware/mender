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

#include <string>
#include <memory>

#include <mender-auth/context.hpp>
#include <mender-auth/cli/keystore.hpp>

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
namespace log = mender::common::log;
namespace path = mender::common::path;

namespace ipc = mender::auth::ipc;

shared_ptr<MenderKeyStore> KeystoreFromConfig(
	const conf::MenderConfig &config, const string &passphrase) {
	cli::StaticKey static_key = cli::StaticKey::No;
	string pem_file;
	string ssl_engine;

	// TODO: review and simplify logic as part of MEN-6668. See discussion at:
	// https://github.com/mendersoftware/mender/pull/1378#discussion_r1317185066
	if (config.https_client.key != "") {
		pem_file = config.https_client.key;
		ssl_engine = config.https_client.ssl_engine;
		static_key = cli::StaticKey::Yes;
	}
	if (config.security.auth_private_key != "") {
		pem_file = config.security.auth_private_key;
		ssl_engine = config.security.ssl_engine;
		static_key = cli::StaticKey::Yes;
	}
	if (config.https_client.key == "" && config.security.auth_private_key == "") {
		pem_file = path::Join(config.paths.GetDataStore(), config.paths.GetKeyFile());
		ssl_engine = config.https_client.ssl_engine;
		static_key = cli::StaticKey::No;
	}

	return make_shared<MenderKeyStore>(pem_file, ssl_engine, static_key, passphrase);
}

DaemonAction::DaemonAction(shared_ptr<MenderKeyStore> keystore) :
	keystore_(keystore) {
}

ExpectedActionPtr DaemonAction::Create(const conf::MenderConfig &config, const string &passphrase) {
	auto key_store = KeystoreFromConfig(config, passphrase);

	auto err = key_store->Load();
	if (err != error::NoError && err.code != MakeError(NoKeysError, "").code) {
		return expected::unexpected(err);
	}
	if (err.code == MakeError(NoKeysError, "").code) {
		log::Info("Generating new RSA key");
		err = key_store->Generate();
		if (err != error::NoError) {
			return expected::unexpected(err);
		}
		err = key_store->Save();
		if (err != error::NoError) {
			return expected::unexpected(err);
		}
	}
	return make_shared<DaemonAction>(key_store);
}

error::Error DaemonAction::Execute(context::MenderContext &main_context) {
	events::EventLoop loop {};

	auto ipc_server {ipc::Server(loop, main_context.GetConfig())};

	const string server_url {"http://127.0.0.1:8001"};

	auto err = ipc_server.Listen(server_url);
	if (err != error::NoError) {
		log::Error("Failed to start the listen loop");
		log::Error(err.String());
		return error::MakeError(error::ExitWithFailureError, "");
	}

	loop.Run();

	return error::NoError;
}

} // namespace cli
} // namespace auth
} // namespace mender
