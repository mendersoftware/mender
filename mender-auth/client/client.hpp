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

#include <functional>
#include <string>

#include <common/conf.hpp>
#include <common/error.hpp>
#include <common/expected.hpp>
#include <common/events.hpp>
#include <common/http.hpp>

namespace mender {
namespace auth {
namespace http_client {

using namespace std;

namespace error = mender::common::error;
namespace expected = mender::common::expected;
namespace events = mender::common::events;
namespace http = mender::http;


expected::ExpectedString GetPrivateKey();

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

using APIResponse = expected::expected<string, error::Error>;
using APIResponseHandler = function<void(APIResponse)>;

error::Error GetJWTToken(
	http::Client &client,
	const string &server_url,
	const string &private_key_path,
	const string &device_identity_script_path,
	events::EventLoop &loop,
	APIResponseHandler api_handler,
	const string &tenant_token = "",
	const string &server_certificate_path = "");

} // namespace http_client
} // namespace auth
} // namespace mender
