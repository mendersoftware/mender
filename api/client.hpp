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

#ifndef MENDER_API_CLIENT_HPP
#define MENDER_API_CLIENT_HPP

#include <string>

#include <api/auth.hpp>
#include <common/error.hpp>
#include <common/events.hpp>
#include <common/http.hpp>

namespace mender {
namespace api {

namespace error = mender::common::error;
namespace events = mender::common::events;
namespace http = mender::http;

using namespace std;

class Client : http::Client {
public:
	Client(
		http::ClientConfig &config,
		events::EventLoop &event_loop,
		auth::Authenticator &authenticator,
		const string &logger_name = "api_client") :
		http::Client(config, event_loop, logger_name),
		authenticator_ {authenticator} {};

	// see http::Client::AsyncCall() for details
	error::Error AsyncCall(
		http::OutgoingRequestPtr req,
		http::ResponseHandler header_handler,
		http::ResponseHandler body_handler) override;

private:
	auth::Authenticator &authenticator_;
};

} // namespace api
} // namespace mender

#endif // MENDER_API_CLIENT_HPP
