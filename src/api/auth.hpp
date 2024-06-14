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

#ifndef MENDER_API_AUTH_HPP
#define MENDER_API_AUTH_HPP

#include <functional>
#include <string>
#include <vector>

#include <common/config.h>

#ifdef MENDER_USE_DBUS
#include <common/platform/dbus.hpp>
#endif

#include <common/common.hpp>
#include <common/error.hpp>
#include <common/events.hpp>
#include <common/expected.hpp>
#include <common/http.hpp>
#include <common/optional.hpp>

namespace mender {
namespace api {
namespace auth {

using namespace std;

#ifdef MENDER_USE_DBUS
namespace dbus = mender::common::dbus;
#endif

namespace error = mender::common::error;
namespace events = mender::common::events;
namespace expected = mender::common::expected;

enum AuthenticatorErrorCode {
	NoError = 0,
	APIError,
	UnauthorizedError,
	AuthenticationError,
};

class AuthenticatorErrorCategoryClass : public std::error_category {
public:
	const char *name() const noexcept override;
	string message(int code) const override;
};
extern const AuthenticatorErrorCategoryClass AuthenticatorErrorCategory;

error::Error MakeError(AuthenticatorErrorCode code, const string &msg);
struct AuthData {
	string server_url;
	string token;
};
using ExpectedAuthData = expected::expected<AuthData, error::Error>;

using AuthenticatedAction = function<void(ExpectedAuthData)>;

class Authenticator {
public:
	Authenticator(events::EventLoop &loop, chrono::seconds auth_timeout = chrono::minutes {1}) :
		loop_ {loop},
		auth_timeout_ {auth_timeout},
		auth_timeout_timer_ {loop} {
	}

	void ExpireToken();

	error::Error WithToken(AuthenticatedAction action);

protected:
	enum class NoTokenAction {
		Finish,
		RequestNew,
	};

	void PostPendingActions(const ExpectedAuthData &ex_auth_data);
	error::Error RequestNewToken();

	virtual error::Error StartWatchingTokenSignal() = 0;
	virtual error::Error GetJwtToken() = 0;
	virtual error::Error FetchJwtToken() = 0;

	void HandleReceivedToken(common::ExpectedStringPair ex_auth_dbus_data, NoTokenAction no_token);

	events::EventLoop &loop_;

	bool token_fetch_in_progress_ = false;
	vector<AuthenticatedAction> pending_actions_;
	chrono::seconds auth_timeout_;
	events::Timer auth_timeout_timer_;
};

#ifdef MENDER_USE_DBUS
class AuthenticatorDBus : public Authenticator {
public:
	AuthenticatorDBus(events::EventLoop &loop, chrono::seconds auth_timeout = chrono::minutes {1}) :
		Authenticator(loop, auth_timeout),
		dbus_client_ {loop} {
	}

protected:
	error::Error StartWatchingTokenSignal() override;
	error::Error GetJwtToken() override;
	error::Error FetchJwtToken() override;

	dbus::DBusClient dbus_client_;
	bool watching_token_signal_ {false};
};
#endif

} // namespace auth
} // namespace api
} // namespace mender

#endif // MENDER_API_AUTH_HPP
