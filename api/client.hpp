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

#include <memory>
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

// Inheritance here allows us to avoid re-implementing things like SetHeader(),
// SetMethod() (and getters that come from the Request class) and we can just
// use APIRequest instances where OutgoingRequest is needed (i.e. for HTTP).
class APIRequest : public http::OutgoingRequest {
public:
	APIRequest() {};

	error::Error SetAddress(const string &address) override {
		// SetPath() followed by SetAuthData() should be used instead.
		return error::MakeError(
			error::ProgrammingError,
			"Can't set full address on an API request, only path can be set");
	}

	void SetPath(const string &path) {
		address_.path = path;
	}

	error::Error SetAuthData(const auth::AuthData &auth_data);
};
using APIRequestPtr = shared_ptr<APIRequest>;

// Abstract class (interface) mostly needed so that we can mock this in tests
// with a class skipping authentication.
class Client {
public:
	virtual error::Error AsyncCall(
		APIRequestPtr req,
		http::ResponseHandler header_handler,
		http::ResponseHandler body_handler) = 0;

	virtual ~Client() {};
};

class HTTPClient : public Client {
public:
	HTTPClient(
		const http::ClientConfig &config,
		events::EventLoop &event_loop,
		auth::Authenticator &authenticator,
		const string &logger_name = "api_http_client") :
		event_loop_ {event_loop},
		http_client_ {config, event_loop, logger_name},
		authenticator_ {authenticator} {};

	// see http::Client::AsyncCall() for details about the handlers
	error::Error AsyncCall(
		APIRequestPtr req,
		http::ResponseHandler header_handler,
		http::ResponseHandler body_handler) override;

	void ExpireToken() {
		authenticator_.ExpireToken();
	}

private:
	events::EventLoop &event_loop_;
	http::Client http_client_;
	auth::Authenticator &authenticator_;
};

} // namespace api
} // namespace mender

#endif // MENDER_API_CLIENT_HPP
