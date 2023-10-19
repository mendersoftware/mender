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

#include <common/conf.hpp>
#include <common/dbus.hpp>
#include <common/error.hpp>
#include <common/events.hpp>
#include <common/http.hpp>
#include <common/path.hpp>

#include <api/api.hpp>
#include <api/auth.hpp>

namespace mender {
namespace auth {
namespace ipc {


using namespace std;

namespace auth_client = mender::api::auth;
namespace conf = mender::common::conf;
namespace dbus = mender::common::dbus;
namespace error = mender::common::error;
namespace events = mender::common::events;
namespace log = mender::common::log;
namespace path = mender::common::path;

class Caching {
public:
	Caching(events::EventLoop &loop, const conf::MenderConfig &config) :
		servers_ {config.servers},
		tenant_token_ {config.tenant_token},
		client_ {config.GetHttpClientConfig(), loop},
		default_identity_script_path_ {config.paths.GetIdentityScript()},
		dbus_server_ {loop, "io.mender.AuthenticationManager"} {};

	error::Error Listen(
		const string &private_key_path = "", const string &identity_script_path = "");

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

private:
	void ClearCache() {
		Cache("", "");
	}

	void CacheAPIResponse(const auth_client::APIResponse &resp) {
		if (resp) {
			Cache(resp.value().token, resp.value().server_url);
			return;
		}
		ClearCache();
	};

	string cached_jwt_token_;
	string cached_server_url_;
	bool auth_in_progress_ = false;

	const vector<string> &servers_;
	const string tenant_token_;
	http::Client client_;
	string default_identity_script_path_;
	dbus::DBusServer dbus_server_;
};

using Server = Caching;

} // namespace ipc
} // namespace auth
} // namespace mender


#endif // MENDER_AUTH_IPC_SERVER_HPP
