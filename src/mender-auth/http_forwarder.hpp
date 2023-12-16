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

#ifndef MENDER_AUTH_HTTP_FORWARDER_HPP
#define MENDER_AUTH_HTTP_FORWARDER_HPP

#include <common/error.hpp>
#include <common/events.hpp>
#include <common/expected.hpp>
#include <common/http.hpp>
#include <common/io.hpp>
#include <common/log.hpp>

namespace mender {
namespace auth {
namespace http_forwarder {

namespace error = mender::common::error;
namespace events = mender::common::events;
namespace expected = mender::common::expected;
namespace http = mender::http;
namespace io = mender::common::io;
namespace log = mender::common::log;

using namespace std;

class ForwardObject {
private:
	ForwardObject(const http::ClientConfig &config, events::EventLoop &event_loop);

	http::Client client_;

	log::Logger logger_;

	http::IncomingRequestPtr req_in_;
	http::OutgoingRequestPtr req_out_;
	http::IncomingResponsePtr resp_in_;
	http::OutgoingResponsePtr resp_out_;

	bool incoming_request_finished_ {false};
	bool outgoing_request_finished_ {false};

	friend class Server;
};
using ForwardObjectPtr = shared_ptr<ForwardObject>;

class Server : virtual public io::Canceller {
public:
	Server(
		const http::ServerConfig &server_config,
		const http::ClientConfig &client_config,
		events::EventLoop &loop);
	~Server();

	void Cancel() override;

	error::Error AsyncForward(const string &listen_url, const string &target_url);

	uint16_t GetPort() const;
	string GetUrl() const;
	const string &GetTargetUrl() const {
		return target_url_;
	}

private:
	void RequestHeaderHandler(http::ExpectedIncomingRequestPtr exp_req);
	void RequestBodyHandler(http::IncomingRequestPtr req, error::Error err);
	void ResponseHeaderHandler(
		http::IncomingRequestPtr req_in, http::ExpectedIncomingResponsePtr exp_resp_in);
	void ResponseBodyHandler(
		http::IncomingRequestPtr req_in, http::ExpectedIncomingResponsePtr exp_resp_in);

	void SwitchProtocol(
		http::IncomingRequestPtr req_in,
		http::IncomingResponsePtr resp_in,
		http::OutgoingResponsePtr resp_out);

	log::Logger logger_;
	events::EventLoop &event_loop_;
	http::Server server_;
	shared_ptr<bool> cancelled_;
	const http::ClientConfig &client_config_;
	string target_url_;

	unordered_map<http::IncomingRequestPtr, ForwardObjectPtr> connections_;

	friend class ForwardObject;
	friend class TestServer;
};

} // namespace http_forwarder
} // namespace auth
} // namespace mender

#endif // MENDER_AUTH_HTTP_FORWARDER_HPP
