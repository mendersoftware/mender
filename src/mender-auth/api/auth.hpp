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

#ifndef MENDER_AUTH_API_AUTH_HPP
#define MENDER_AUTH_API_AUTH_HPP

#include <functional>
#include <string>
#include <vector>

#include <common/crypto.hpp>
#include <common/error.hpp>
#include <common/http.hpp>

#include <api/auth.hpp>

namespace mender {
namespace auth {
namespace api {
namespace auth {

using namespace std;

namespace crypto = mender::common::crypto;
namespace error = mender::common::error;

enum AuthClientErrorCode {
	NoError = 0,
	ResponseError,
	APIError,
	UnauthorizedError,
	AuthenticationError,
};

class AuthClientErrorCategoryClass : public std::error_category {
public:
	const char *name() const noexcept override;
	string message(int code) const override;
};
extern const AuthClientErrorCategoryClass AuthClientErrorCategory;

error::Error MakeError(AuthClientErrorCode code, const string &msg);

using APIResponse = mender::api::auth::ExpectedAuthData;
using APIResponseHandler = function<void(APIResponse)>;

error::Error FetchJWTToken(
	mender::common::http::Client &client,
	const vector<string> &servers,
	const crypto::Args &args,
	const string &device_identity_script_path,
	APIResponseHandler api_handler,
	const string &tenant_token = "");

} // namespace auth
} // namespace api
} // namespace auth
} // namespace mender

#endif // MENDER_AUTH_API_AUTH_HPP
