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

#include <api/auth.hpp>

#include <mutex>
#include <string>
#include <vector>

#include <common/common.hpp>
#include <common/dbus.hpp>
#include <common/error.hpp>
#include <common/expected.hpp>
#include <common/log.hpp>

namespace mender {
namespace api {
namespace auth {

using namespace std;

namespace common = mender::common;
namespace dbus = mender::common::dbus;
namespace error = mender::common::error;
namespace expected = mender::common::expected;
namespace mlog = mender::common::log;

const AuthenticatorErrorCategoryClass AuthenticatorErrorCategory;

const char *AuthenticatorErrorCategoryClass::name() const noexcept {
	return "AuthenticatorErrorCategory";
}

string AuthenticatorErrorCategoryClass::message(int code) const {
	switch (code) {
	case NoError:
		return "Success";
	case APIError:
		return "API error";
	case UnauthorizedError:
		return "Unauthorized error";
	case AuthenticationError:
		return "Authentication error";
	default:
		return "Unknown";
	}
}

error::Error MakeError(AuthenticatorErrorCode code, const string &msg) {
	return error::Error(error_condition(code, AuthenticatorErrorCategory), msg);
}

void Authenticator::ExpireToken() {
	if (!token_fetch_in_progress_) {
		RequestNewToken(nullopt);
	}
}

error::Error Authenticator::StartWatchingTokenSignal() {
	auto err = dbus_client_.RegisterSignalHandler<dbus::ExpectedStringPair>(
		"io.mender.Authentication1",
		"JwtTokenStateChange",
		[this](dbus::ExpectedStringPair ex_auth_dbus_data) {
			auth_timeout_timer_.Cancel();
			token_fetch_in_progress_ = false;
			ExpectedAuthData ex_auth_data;
			if (!ex_auth_dbus_data) {
				mlog::Error(
					"Error from the JwtTokenStateChange DBus signal: "
					+ ex_auth_dbus_data.error().String());
				ex_auth_data = ExpectedAuthData(expected::unexpected(ex_auth_dbus_data.error()));
			} else {
				auto &token = ex_auth_dbus_data.value().first;
				auto &server_url = ex_auth_dbus_data.value().second;

				mlog::Debug("Got new authentication token for server " + server_url);

				AuthData auth_data {server_url, token};
				ex_auth_data = ExpectedAuthData(std::move(auth_data));
			}
			PostPendingActions(ex_auth_data);
		});

	watching_token_signal_ = (err == error::NoError);
	return err;
}

void Authenticator::PostPendingActions(ExpectedAuthData &ex_auth_data) {
	for (auto action : pending_actions_) {
		loop_.Post([action, ex_auth_data]() { action(ex_auth_data); });
	}
	pending_actions_.clear();
}

error::Error Authenticator::RequestNewToken(optional<AuthenticatedAction> opt_action) {
	if (token_fetch_in_progress_) {
		// Just make sure the action (if any) is called once the token is
		// obtained.
		if (opt_action) {
			pending_actions_.push_back(*opt_action);
		}
		return error::NoError;
	}

	auto err = dbus_client_.CallMethod<expected::ExpectedBool>(
		"io.mender.AuthenticationManager",
		"/io/mender/AuthenticationManager",
		"io.mender.Authentication1",
		"FetchJwtToken",
		[this](expected::ExpectedBool ex_value) {
			if (!ex_value) {
				token_fetch_in_progress_ = false;
				mlog::Error("Failed to request new token fetching: " + ex_value.error().String());
				ExpectedAuthData ex_auth_data = expected::unexpected(ex_value.error());
				PostPendingActions(ex_auth_data);
			} else if (!ex_value.value()) {
				// mender-auth encountered an error not sent over DBus (should never happen)
				token_fetch_in_progress_ = false;
				mlog::Error(
					"Failed to request new token fetching (see mender-auth logs for details)");
				ExpectedAuthData ex_auth_data = expected::unexpected(MakeError(
					AuthenticationError, "Failed to request new token fetching from mender-auth"));
				PostPendingActions(ex_auth_data);
			}
		});
	if (err != error::NoError) {
		// A sync DBus error.
		mlog::Error("Failed to request new token fetching: " + err.String());
		token_fetch_in_progress_ = false;
		ExpectedAuthData ex_auth_data = expected::unexpected(err);
		PostPendingActions(ex_auth_data);
		if (opt_action) {
			(*opt_action)(ex_auth_data);
		}
		return err;
	}
	// else everything went OK

	token_fetch_in_progress_ = true;

	// Make sure the action (if any) is called once the token is
	// obtained.
	if (opt_action) {
		pending_actions_.push_back(*opt_action);
	}

	// Make sure we don't wait for the token forever.
	auth_timeout_timer_.AsyncWait(auth_timeout_, [this](error::Error err) {
		if (err.code == make_error_condition(errc::operation_canceled)) {
			return;
		} else if (err == error::NoError) {
			mlog::Warning("Timed-out waiting for a new token");
			token_fetch_in_progress_ = false;
			ExpectedAuthData ex_auth_data = expected::unexpected(
				MakeError(AuthenticationError, "Timed-out waiting for a new token"));
			PostPendingActions(ex_auth_data);
		} else {
			// should never happen
			assert(false);
			mlog::Error("Authentication timer error: " + err.String());

			// In case it did happen, run the stacked up actions and unset the
			// in_progress_ flag or things may got stuck.
			token_fetch_in_progress_ = false;
			ExpectedAuthData ex_auth_data = expected::unexpected(err);
			PostPendingActions(ex_auth_data);
		}
	});
	return error::NoError;
}

error::Error Authenticator::WithToken(AuthenticatedAction action) {
	if (!watching_token_signal_) {
		auto err = StartWatchingTokenSignal();
		if (err != error::NoError) {
			// Should never fail. We rely on the signal heavily so let's fail
			// hard if it does.
			return err;
		}
	}

	if (token_fetch_in_progress_) {
		// Already waiting for a new token, just make sure the action is called
		// once it arrives (or once the wait times out).
		pending_actions_.push_back(action);
		return error::NoError;
	}
	// else => should fetch the token, cache it and call all pending actions
	// Try to get token from mender-auth
	auto err = dbus_client_.CallMethod<dbus::ExpectedStringPair>(
		"io.mender.AuthenticationManager",
		"/io/mender/AuthenticationManager",
		"io.mender.Authentication1",
		"GetJwtToken",
		[this, action](dbus::ExpectedStringPair ex_auth_dbus_data) {
			token_fetch_in_progress_ = false;
			auth_timeout_timer_.Cancel();
			if (ex_auth_dbus_data && (ex_auth_dbus_data.value().first != "")
				&& (ex_auth_dbus_data.value().second != "")) {
				// Got a valid token, let's save it and then call action and any
				// previously-pending actions (if any) with it.
				auto &token = ex_auth_dbus_data.value().first;
				auto &server_url = ex_auth_dbus_data.value().second;
				AuthData auth_data {server_url, token};

				mlog::Debug("Got authentication token for server " + server_url);

				// Post/schedule pending actions before running the given action
				// because the action can actually add more actions or even
				// expire the token, etc. So post actions stacked up before we
				// got here with the current token and only then give action a
				// chance to mess with things.
				ExpectedAuthData ex_auth_data {std::move(auth_data)};
				PostPendingActions(ex_auth_data);
				action(ex_auth_data);
			} else {
				// No valid token, let's request fetching of a new one
				RequestNewToken(action);
			}
		});
	if (err != error::NoError) {
		// No token and failed to try to get one (should never happen).
		ExpectedAuthData ex_auth_data = expected::unexpected(err);
		PostPendingActions(ex_auth_data);
		return err;
	}
	// else record that token is already being fetched (by GetJwtToken()).
	token_fetch_in_progress_ = true;

	return error::NoError;
}

} // namespace auth
} // namespace api
} // namespace mender
