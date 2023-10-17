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

#include <api/auth.hpp>

#include <mender-auth/context.hpp>
#include <mender-auth/cli/keystore.hpp>

#include <common/conf.hpp>
#include <common/events.hpp>
#include <common/log.hpp>

#include <mender-auth/ipc/server.hpp>

namespace mender {
namespace auth {
namespace cli {

using namespace std;

namespace auth_client = mender::api::auth;
namespace events = mender::common::events;
namespace ipc = mender::auth::ipc;
namespace log = mender::common::log;

shared_ptr<MenderKeyStore> KeystoreFromConfig(
	const conf::MenderConfig &config, const string &passphrase) {
	cli::StaticKey static_key = cli::StaticKey::No;
	string pem_file;
	string ssl_engine;

	if (config.security.auth_private_key != "") {
		pem_file = config.security.auth_private_key;
		ssl_engine = config.security.ssl_engine;
		static_key = cli::StaticKey::Yes;
	} else {
		pem_file = config.paths.GetKeyFile();
		static_key = cli::StaticKey::No;
	}

	return make_shared<MenderKeyStore>(pem_file, ssl_engine, static_key, passphrase);
}

error::Error DoBootstrap(shared_ptr<MenderKeyStore> keystore, const bool force) {
	log::Trace("DoBootstrap");
	auto err = keystore->Load();
	if (err != error::NoError && err.code != MakeError(NoKeysError, "").code) {
		return err;
	}
	if (err.code == MakeError(NoKeysError, "").code || force) {
		log::Info("Generating new RSA key");
		err = keystore->Generate();
		if (err != error::NoError) {
			return err;
		}
		err = keystore->Save();
		if (err != error::NoError) {
			return err;
		}
	}
	return err;
}

error::Error DoAuthenticate(context::MenderContext &main_context) {
	events::EventLoop loop;
	auto &config = main_context.GetConfig();
	if (config.server_url == "") {
		log::Info("No server set in the configuration, skipping authentication");
		return error::NoError;
	}
	log::Info("Trying to authenticate with the server: '" + config.server_url + "'");
	mender::common::events::Timer timer {loop};
	http::ClientConfig client_config {
		config.server_certificate, config.https_client.certificate, config.https_client.key};
	http::Client client {client_config, loop};
	auto err = auth_client::FetchJWTToken(
		client,
		config.server_url,
		{
			config.security.auth_private_key == "" ? config.paths.GetKeyFile()
												   : config.security.auth_private_key,
			"", // Empty passphrase
			config.security.ssl_engine,
		},
		config.paths.GetInventoryScriptsDir(),
		[&loop, &config, &timer](auth_client::APIResponse resp) {
			log::Info("Got Auth response");
			if (resp) {
				log::Info(
					"Successfully authorized with the server: " + config.server_url + resp.value());
			} else {
				log::Error(resp.error().String());
			}
			timer.Cancel();
			loop.Stop();
		},
		config.tenant_token);
	if (err != error::NoError) {
		return err;
	}
	timer.AsyncWait(chrono::seconds {30}, [&loop](error::Error err) { loop.Stop(); });
	loop.Run();
	return error::NoError;
}

DaemonAction::DaemonAction(shared_ptr<MenderKeyStore> keystore, const bool force_bootstrap) :
	keystore_(keystore),
	force_bootstrap_(force_bootstrap) {
}

ExpectedActionPtr DaemonAction::Create(
	const conf::MenderConfig &config, const string &passphrase, const bool force_bootstrap) {
	auto key_store = KeystoreFromConfig(config, passphrase);

	return make_shared<DaemonAction>(key_store, force_bootstrap);
}

error::Error DaemonAction::Execute(context::MenderContext &main_context) {
	auto &config = main_context.GetConfig();
	if (!none_of(config.servers.cbegin(), config.servers.cend(), [](const string &it) {
			return it != "";
		})) {
		log::Error("Cannot run in daemon mode with no server URL specified");
		return error::MakeError(error::ExitWithFailureError, "");
	}

	auto err = DoBootstrap(keystore_, force_bootstrap_);
	if (err != error::NoError) {
		log::Error("Failed to bootstrap: " + err.String());
		return error::MakeError(error::ExitWithFailureError, "");
	}

	events::EventLoop loop {};

	ipc::Server ipc_server {loop, config};

	const string empty_passphrase {};
	err = ipc_server.Listen(
		{config.security.auth_private_key, empty_passphrase, config.security.ssl_engine});
	if (err != error::NoError) {
		log::Error("Failed to start the listen loop");
		log::Error(err.String());
		return error::MakeError(error::ExitWithFailureError, "");
	}

	loop.Run();

	return error::NoError;
}

BootstrapAction::BootstrapAction(shared_ptr<MenderKeyStore> keystore, bool force_bootstrap) :
	keystore_(keystore),
	force_bootstrap_(force_bootstrap) {
}

ExpectedActionPtr BootstrapAction::Create(
	const conf::MenderConfig &config, const string &passphrase, bool force_bootstrap) {
	auto key_store = KeystoreFromConfig(config, passphrase);

	return make_shared<BootstrapAction>(key_store, force_bootstrap);
}

error::Error BootstrapAction::Execute(context::MenderContext &main_context) {
	auto err = DoBootstrap(keystore_, force_bootstrap_);
	if (err != error::NoError) {
		return err;
	}
	return DoAuthenticate(main_context);
}

} // namespace cli
} // namespace auth
} // namespace mender
