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

#include <mender-auth/ipc/server.hpp>

#include <functional>
#include <iostream>
#include <string>

#include <common/crypto.hpp>
#include <common/platform/dbus.hpp>
#include <common/error.hpp>
#include <common/expected.hpp>
#include <common/log.hpp>

namespace mender {
namespace auth {
namespace ipc {

namespace crypto = mender::common::crypto;
namespace dbus = mender::common::dbus;
namespace error = mender::common::error;
namespace expected = mender::common::expected;


using namespace std;

// Register DBus object handling auth methods and signals
error::Error AuthenticatingForwarder::Listen(
	const crypto::Args &args, const string &identity_script_path) {
	// Cannot serve new tokens when not knowing where to fetch them from.
	AssertOrReturnError(servers_.size() > 0);

	auto dbus_obj = make_shared<dbus::DBusObject>("/io/mender/AuthenticationManager");
	dbus_obj->AddMethodHandler<dbus::ExpectedStringPair>(
		"io.mender.Authentication1", "GetJwtToken", [this]() {
			return dbus::StringPair {GetJWTToken(), GetServerURL()};
		});
	dbus_obj->AddMethodHandler<expected::ExpectedBool>(
		"io.mender.Authentication1", "FetchJwtToken", [this, args, identity_script_path]() {
			if (auth_in_progress_) {
				// Already authenticating, nothing to do here.
				return true;
			}
			auto err = auth_client::FetchJWTToken(
				client_,
				servers_,
				args,
				identity_script_path == "" ? default_identity_script_path_ : identity_script_path,
				[this](auth_client::APIResponse resp) { FetchJwtTokenHandler(resp); },
				tenant_token_);
			if (err != error::NoError) {
				log::Error("Failed to trigger token fetching: " + err.String());
				return false;
			}
			auth_in_progress_ = true;
			return true;
		});

	return dbus_server_.AdvertiseObject(dbus_obj);
}

void AuthenticatingForwarder::FetchJwtTokenHandler(auth_client::APIResponse &resp) {
	auth_in_progress_ = false;

	forwarder_.Cancel();

	if (resp) {
		// ":0" port number means pick random port in user range.
		auto err = forwarder_.AsyncForward("http://127.0.0.1:0", resp.value().server_url);
		if (err == error::NoError) {
			Cache(resp.value().token, forwarder_.GetUrl());
		} else {
			// Should not happen, but as a desperate response, give the remote url to
			// clients instead of our local one. At least then they might be able to
			// connect, it just won't be through the proxy.
			log::Error("Unable to start a local HTTP proxy: " + err.String());
			Cache(resp.value().token, resp.value().server_url);
		}

		log::Info("Successfully received new authorization data");
	} else {
		ClearCache();
		log::Error("Failed to fetch new token: " + resp.error().String());
	}
	// Emit signal either with valid token and server url or with empty strings
	dbus_server_.EmitSignal<dbus::StringPair>(
		"/io/mender/AuthenticationManager",
		"io.mender.Authentication1",
		"JwtTokenStateChange",
		dbus::StringPair {cached_jwt_token_, cached_server_url_});
}

} // namespace ipc
} // namespace auth
} // namespace mender
