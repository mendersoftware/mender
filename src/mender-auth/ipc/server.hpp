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


#ifndef MENDER_AUTH_IPC_SERVER_HPP
#define MENDER_AUTH_IPC_SERVER_HPP

#include <functional>
#include <string>

#include <client_shared/conf.hpp>
#include <common/platform/dbus.hpp>
#include <common/error.hpp>
#include <common/events.hpp>
#include <common/http.hpp>

#include <api/api.hpp>

#include <mender-auth/api/auth.hpp>
#include <mender-auth/http_forwarder.hpp>

namespace mender {
namespace auth {
namespace ipc {


using namespace std;

namespace conf = mender::client_shared::conf;
namespace crypto = mender::common::crypto;
namespace dbus = mender::common::dbus;
namespace error = mender::common::error;
namespace events = mender::common::events;
namespace http = mender::common::http;
namespace log = mender::common::log;

namespace auth_client = mender::auth::api::auth;

namespace http_forwarder = mender::auth::http_forwarder;

class AuthenticatingForwarder {
public:
	AuthenticatingForwarder(events::EventLoop &loop, const conf::MenderConfig &config) :
		servers_ {config.servers},
		tenant_token_ {config.tenant_token},
		client_ {config.GetHttpClientConfig(), loop},
		forwarder_ {http::ServerConfig {}, config.GetHttpClientConfig(), loop},
		default_identity_script_path_ {config.paths.GetIdentityScript()},
		dbus_server_ {loop, "io.mender.AuthenticationManager"} {};

	error::Error Listen(const crypto::Args &args, const string &identity_script_path = "");

	string GetServerURL() {
		return this->cached_server_url_;
	}

	string GetJWTToken() {
		return this->cached_jwt_token_;
	}

	void Cache(const string &token, const string &url) {
		this->cached_jwt_token_ = token;
		this->cached_server_url_ = url;
	}

	const http_forwarder::Server &GetForwarder() const {
		return forwarder_;
	}

private:
	void ClearCache() {
		Cache("", "");
	}

	void FetchJwtTokenHandler(auth_client::APIResponse &resp);

	string cached_jwt_token_;
	string cached_server_url_;
	bool auth_in_progress_ = false;

	const vector<string> &servers_;
	const string tenant_token_;
	http::Client client_;
	http_forwarder::Server forwarder_;
	string default_identity_script_path_;
	dbus::DBusServer dbus_server_;
};

using Server = AuthenticatingForwarder;

} // namespace ipc
} // namespace auth
} // namespace mender


#endif // MENDER_AUTH_IPC_SERVER_HPP
