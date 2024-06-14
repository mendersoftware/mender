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

#include <mender-auth/api/auth.hpp>

#include <common/log.hpp>

namespace mender {
namespace auth {
namespace api {
namespace auth {

namespace log = mender::common::log;

error::Error AuthenticatorHttp::StartWatchingTokenSignal() {
	// There is no signal when embedding the authentication. We get the data straight from the
	// HTTP response.
	return error::NoError;
}

error::Error AuthenticatorHttp::GetJwtToken() {
	loop_.Post([this]() {
		HandleReceivedToken(common::StringPair {token_, server_url_}, NoTokenAction::RequestNew);
	});
	return error::NoError;
}

void AuthenticatorHttp::FetchJwtTokenHandler(APIResponse resp) {
	if (resp) {
		token_ = resp.value().token;
		server_url_ = resp.value().server_url;

		log::Info("Successfully received new authorization data");
	} else {
		token_.clear();
		server_url_.clear();

		log::Error("Failed to fetch new token: " + resp.error().String());
	}

	PostPendingActions(mender::api::auth::AuthData {token_, server_url_});
}

error::Error AuthenticatorHttp::FetchJwtToken() {
	return FetchJWTToken(
		client_,
		config_.servers,
		crypto_args_,
		config_.paths.GetIdentityScript(),
		[this](APIResponse resp) { FetchJwtTokenHandler(resp); },
		config_.tenant_token);
}

} // namespace auth
} // namespace api
} // namespace auth
} // namespace mender
