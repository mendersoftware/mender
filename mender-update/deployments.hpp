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

#ifndef MENDER_UPDATE_DEPLOYMENTS_HPP
#define MENDER_UPDATE_DEPLOYMENTS_HPP

#include <string>

#include <common/error.hpp>
#include <common/events.hpp>
#include <common/expected.hpp>
#include <common/http.hpp>
#include <common/json.hpp>
#include <common/optional.hpp>
#include <mender-update/context.hpp>

namespace mender {
namespace update {
namespace deployments {

using namespace std;

namespace context = mender::update::context;
namespace error = mender::common::error;
namespace events = mender::common::events;
namespace expected = mender::common::expected;
namespace json = mender::common::json;
namespace optional = mender::common::optional;

enum DeploymentsErrorCode {
	NoError = 0,
	InvalidDataError,
	BadResponseError,
};

class DeploymentsErrorCategoryClass : public std::error_category {
public:
	const char *name() const noexcept override;
	string message(int code) const override;
};
extern const DeploymentsErrorCategoryClass DeploymentsErrorCategory;

error::Error MakeError(DeploymentsErrorCode code, const string &msg);

using APIResponse = expected::expected<optional::optional<json::Json>, error::Error>;
using APIResponseHandler = function<void(APIResponse)>;

error::Error CheckNewDeployments(
	context::MenderContext &ctx,
	const string &server_url,
	http::Client &client,
	events::EventLoop &loop,
	APIResponseHandler api_handler);

} // namespace deployments
} // namespace update
} // namespace mender

#endif // MENDER_UPDATE_DEPLOYMENTS_HPP
