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

#include <common/log.hpp>

namespace mender {
namespace api {
namespace auth {

namespace mlog = mender::common::log;

using namespace std;

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
		RequestNewToken();
	}
}

void Authenticator::WithToken(AuthenticatedAction action) {
	auto err = StartWatchingTokenSignal();
	if (err != error::NoError) {
		// StartWatchingTokenSignal can fail while D-Bus is unavailable (e.g.
		// dbus.socket is down). Deliver the failure to THIS action only, via the
		// event loop, and leave pending_actions_ and token_fetch_in_progress_
		// untouched: any actions already queued are waiting on an unrelated, in-flight
		// token fetch and must not be failed with this error. They are delivered when
		// that fetch resolves, or failed by whichever token-fetch timeout is pending
		// if it never does resolve.
		//
		// This branch is currently never reached while other actions are queued: a
		// non-empty queue means a fetch is in progress, and the serialized state
		// machine issues no further WithToken() until that fetch's callback drains the
		// queue (no WithToken will take place because we are still in the same state, so no new
		// requests are made). Delivering only this action is therefore defensive, but it keeps the
		// behavior correct should that ever change.
		ExpectedAuthData ex_auth_data = expected::unexpected(err);
		loop_.Post([action, ex_auth_data]() { action(ex_auth_data); });
		return;
	}

	if (token_fetch_in_progress_) {
		// Already waiting for a new token.
		pending_actions_.push_back(action);
		return;
	}
	// No fetch is in progress, so this call starts one. GetJwtToken() requests the
	// token asynchronously; the action is queued below and delivered, together with
	// any others waiting, once the token resolves (via HandleReceivedToken ->
	// PostPendingActions).

	err = GetJwtToken();
	if (err != error::NoError) {
		// No token and failed to try to get one, can happen while dbus.socket is down.
		ExpectedAuthData ex_auth_data = expected::unexpected(err);
		loop_.Post([action, ex_auth_data]() { action(ex_auth_data); });
		return;
	}
	// else record that token is already being fetched (by GetJwtToken()).
	pending_actions_.push_back(action);
	token_fetch_in_progress_ = true;
	return;
}

void Authenticator::PostPendingActions(const ExpectedAuthData &ex_auth_data) {
	token_fetch_in_progress_ = false;
	for (auto action : pending_actions_) {
		loop_.Post([action, ex_auth_data]() { action(ex_auth_data); });
	}
	pending_actions_.clear();
}

error::Error Authenticator::RequestNewToken() {
	auto err = FetchJwtToken();
	if (err != error::NoError) {
		mlog::Error("Failed to request new token fetching: " + err.String());
		ExpectedAuthData ex_auth_data = expected::unexpected(err);
		PostPendingActions(ex_auth_data);
		return err;
	}

	token_fetch_in_progress_ = true;

	// Make sure we don't wait for the token forever.
	auth_timeout_timer_.AsyncWait(auth_timeout_, [this](error::Error err) {
		if (err.code == make_error_condition(errc::operation_canceled)) {
			return;
		} else if (err == error::NoError) {
			mlog::Warning("Timed-out waiting for a new token");
			ExpectedAuthData ex_auth_data = expected::unexpected(
				MakeError(AuthenticationError, "Timed-out waiting for a new token"));
			PostPendingActions(ex_auth_data);
		} else {
			// should never happen
			assert(false);
			mlog::Error("Authentication timer error: " + err.String());

			// In case it did happen, run the stacked up actions and unset the
			// in_progress_ flag or things may got stuck.
			ExpectedAuthData ex_auth_data = expected::unexpected(err);
			PostPendingActions(ex_auth_data);
		}
	});
	return error::NoError;
}

void Authenticator::HandleReceivedToken(
	common::ExpectedStringPair ex_auth_dbus_data, NoTokenAction no_token) {
	auth_timeout_timer_.Cancel();
	ExpectedAuthData ex_auth_data;
	if (!ex_auth_dbus_data) {
		mlog::Error("Error receiving the JWT token: " + ex_auth_dbus_data.error().String());
		ex_auth_data = expected::unexpected(ex_auth_dbus_data.error());
	} else {
		auto &token = ex_auth_dbus_data.value().first;
		auto &server_url = ex_auth_dbus_data.value().second;

		if (server_url.empty()) {
			mlog::Debug("Got empty server url /token in the response");
		} else {
			mlog::Debug("Got new authentication token for server " + server_url);
		}

		AuthData auth_data {server_url, token};
		ex_auth_data = ExpectedAuthData(std::move(auth_data));

		if (no_token == NoTokenAction::RequestNew and (token == "" or server_url == "")) {
			RequestNewToken();
			return;
		}
	}
	if (no_token == NoTokenAction::Finish && action_ != nullptr) {
		action_();
	}
	PostPendingActions(ex_auth_data);
}

} // namespace auth
} // namespace api
} // namespace mender
