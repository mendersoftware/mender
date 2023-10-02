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
#include <mutex>
#include <string>
#include <vector>

#include <common/conf.hpp>
#include <common/error.hpp>
#include <common/expected.hpp>
#include <common/events.hpp>
#include <common/http.hpp>
#include <common/optional.hpp>

namespace mender {
namespace api {
namespace auth {

using namespace std;

namespace error = mender::common::error;
namespace expected = mender::common::expected;
namespace events = mender::common::events;


enum AuthClientErrorCode {
	NoError = 0,
	SetupError,
	RequestError,
	ResponseError,
	APIError,
	UnauthorizedError,
};

class AuthClientErrorCategoryClass : public std::error_category {
public:
	const char *name() const noexcept override;
	string message(int code) const override;
};
extern const AuthClientErrorCategoryClass AuthClientErrorCategory;

error::Error MakeError(AuthClientErrorCode code, const string &msg);

using ExpectedToken = expected::expected<string, error::Error>;
using APIResponse = ExpectedToken;
using APIResponseHandler = function<void(APIResponse)>;
using AuthenticatedAction = function<void(ExpectedToken)>;

error::Error FetchJWTToken(
	mender::http::Client &client,
	const string &server_url,
	const string &private_key_path,
	const string &device_identity_script_path,
	APIResponseHandler api_handler,
	const string &tenant_token = "");

class Authenticator {
public:
	Authenticator(
		events::EventLoop &loop,
		const mender::http::ClientConfig &client_config,
		const string &server_url,
		const string &private_key_path,
		const string &device_identity_script_path,
		const string &tenant_token = "") :
		loop_ {loop},
		client_ {client_config, loop_, "auth_client"},
		server_url_ {server_url},
		private_key_path_ {private_key_path},
		device_identity_script_path_ {device_identity_script_path},
		tenant_token_ {tenant_token} {};

	void ExpireToken();

	error::Error WithToken(AuthenticatedAction action);

private:
	void RunPendingActions(ExpectedToken ex_token);

	bool auth_in_progress_ = false;
	events::EventLoop &loop_;
	mender::http::Client client_;
	optional<string> token_ = nullopt;
	vector<AuthenticatedAction> pending_actions_;
	string server_url_;
	string private_key_path_;
	string device_identity_script_path_;
	string tenant_token_;
	mutex auth_lock_;
};

} // namespace auth
} // namespace api
} // namespace mender

#endif // MENDER_API_AUTH_HPP
