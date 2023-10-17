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

#include <api/auth.hpp>
#include <common/crypto.hpp>
#include <common/dbus.hpp>
#include <common/error.hpp>
#include <common/expected.hpp>
#include <common/log.hpp>

namespace mender {
namespace auth {
namespace ipc {

namespace crypto = mender::common::crypto;
namespace auth_client = mender::api::auth;
namespace dbus = mender::common::dbus;
namespace error = mender::common::error;
namespace expected = mender::common::expected;


using namespace std;

// Register DBus object handling auth methods and signals
error::Error Caching::Listen(const crypto::Args &args, const string &identity_script_path) {
	// Cannot serve new tokens when not knowing where to fetch them from.
	AssertOrReturnError(server_url_ != "");

	auto dbus_obj = make_shared<dbus::DBusObject>("/io/mender/AuthenticationManager");
	dbus_obj->AddMethodHandler<dbus::ExpectedStringPair>(
		"io.mender.AuthenticationManager", "io.mender.Authentication1", "GetJwtToken", [this]() {
			return dbus::StringPair {GetJWTToken(), GetServerURL()};
		});
	dbus_obj->AddMethodHandler<expected::ExpectedBool>(
		"io.mender.AuthenticationManager",
		"io.mender.Authentication1",
		"FetchJwtToken",
		[this, args, identity_script_path]() {
			auto err = auth_client::FetchJWTToken(
				client_,
				server_url_,
				args,
				identity_script_path == "" ? default_identity_script_path_ : identity_script_path,
				[this](auth_client::APIResponse resp) {
					CacheAPIResponse(server_url_, resp);
					if (resp) {
						dbus_server_.EmitSignal<dbus::StringPair>(
							"/io/mender/AuthenticationManager",
							"io.mender.Authentication1",
							"JwtTokenStateChange",
							dbus::StringPair {resp.value(), server_url_});
					} else {
						log::Error("Failed to fetch new token: " + resp.error().String());
					}
				},
				tenant_token_);
			if (err != error::NoError) {
				log::Error("Failed to trigger token fetching: " + err.String());
			}
			return (err == error::NoError);
		});

	return dbus_server_.AdvertiseObject(dbus_obj);
}

} // namespace ipc
} // namespace auth
} // namespace mender
