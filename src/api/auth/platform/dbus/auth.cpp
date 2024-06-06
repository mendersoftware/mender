// Copyright 2024 Northern.tech AS
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

#include <string>
#include <vector>

#include <common/common.hpp>
#include <common/error.hpp>
#include <common/log.hpp>
#include <common/expected.hpp>

namespace mender {
namespace api {
namespace auth {


using namespace std;
namespace error = mender::common::error;
namespace common = mender::common;


namespace mlog = mender::common::log;
namespace expected = mender::common::expected;

error::Error AuthenticatorDBus::StartWatchingTokenSignal() {
	if (watching_token_signal_) {
		return error::NoError;
	}

	auto err = dbus_client_.RegisterSignalHandler<dbus::ExpectedStringPair>(
		"io.mender.Authentication1",
		"JwtTokenStateChange",
		[this](dbus::ExpectedStringPair ex_auth_dbus_data) {
			HandleReceivedToken(ex_auth_dbus_data, NoTokenAction::Finish);
		});

	watching_token_signal_ = (err == error::NoError);
	return err;
}

error::Error AuthenticatorDBus::GetJwtToken() {
	// Try to get token from mender-auth
	return dbus_client_.CallMethod<dbus::ExpectedStringPair>(
		"io.mender.AuthenticationManager",
		"/io/mender/AuthenticationManager",
		"io.mender.Authentication1",
		"GetJwtToken",
		[this](dbus::ExpectedStringPair ex_auth_dbus_data) {
			HandleReceivedToken(ex_auth_dbus_data, NoTokenAction::RequestNew);
		});
}

error::Error AuthenticatorDBus::FetchJwtToken() {
	return dbus_client_.CallMethod<expected::ExpectedBool>(
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
}

} // namespace auth
} // namespace api
} // namespace mender
