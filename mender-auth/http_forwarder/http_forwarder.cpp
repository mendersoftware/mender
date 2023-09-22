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

#include <mender-auth/http_forwarder.hpp>

namespace mender {
namespace auth {
namespace http_forwarder {

ForwardObject::ForwardObject(const http::ClientConfig &config, events::EventLoop &event_loop) :
	client_(config, event_loop),
	logger_("http_forwarder") {
}

Server::Server(
	const http::ServerConfig &server_config,
	const http::ClientConfig &client_config,
	events::EventLoop &loop) :
	logger_("http_forwarder"),
	event_loop_ {loop},
	server_ {server_config, loop},
	cancelled_ {make_shared<bool>(true)},
	client_config_ {client_config} {
}

Server::~Server() {
	*cancelled_ = true;
	cancelled_ = make_shared<bool>(true);
	Cancel();
}

void Server::Cancel() {
	*cancelled_ = true;
	cancelled_ = make_shared<bool>(true);
	connections_.clear();
	server_.Cancel();
}

error::Error Server::AsyncForward(const string &listen_url, const string &target_url) {
	if (!*cancelled_) {
		return error::Error(
			make_error_condition(errc::operation_in_progress),
			"HTTP forwarding already in progress");
	}

	*cancelled_ = false;

	http::BrokenDownUrl target_address;

	// We don't actually need target_address here, but break it down here anyway to avoid that
	// the error shows up much later when the first connection is made.
	auto err = http::BreakDownUrl(target_url, target_address);
	if (err != error::NoError) {
		return err.WithContext("HTTP forwarder: Invalid target address");
	}
	target_url_ = target_url;

	auto &cancelled = cancelled_;
	err = server_.AsyncServeUrl(
		listen_url,
		[this, cancelled](http::ExpectedIncomingRequestPtr exp_req) {
			if (!*cancelled) {
				RequestHeaderHandler(exp_req);
			}
		},
		[this, cancelled](http::IncomingRequestPtr req, error::Error err) {
			if (!*cancelled) {
				RequestBodyHandler(req, err);
			}
		});
	if (err != error::NoError) {
		return err.WithContext("Unable to start HTTP forwarding server");
	}

	return error::NoError;
}

uint16_t Server::GetPort() const {
	return server_.GetPort();
}

string Server::GetUrl() const {
	return server_.GetUrl();
}

void Server::RequestHeaderHandler(http::ExpectedIncomingRequestPtr exp_req) {
	if (!exp_req) {
		logger_.Error("Error in incoming request: " + exp_req.error().String());
		return;
	}
	auto &req_in = exp_req.value();

	ForwardObjectPtr connection {new ForwardObject(client_config_, event_loop_)};
	connections_[req_in] = connection;
	connection->logger_ = logger_.WithFields(log::LogField {"request", req_in->GetPath()});
	connection->req_in_ = req_in;

	auto final_url = http::JoinUrl(target_url_, req_in->GetPath());
	auto req_out = make_shared<http::OutgoingRequest>();
	req_out->SetMethod(req_in->GetMethod());
	req_out->SetAddress(final_url);
	for (auto header : req_in->GetHeaders()) {
		req_out->SetHeader(header.first, header.second);
	}
	connection->req_out_ = req_out;

	auto exp_body_reader = req_in->MakeBodyAsyncReader();
	if (exp_body_reader) {
		auto body_reader = exp_body_reader.value();
		auto generated = make_shared<bool>(false);
		req_out->SetAsyncBodyGenerator([body_reader, generated]() -> io::ExpectedAsyncReaderPtr {
			// We can only do this once, because the incoming request body is not
			// seekable.
			if (*generated) {
				return expected::unexpected(error::Error(
					make_error_condition(errc::invalid_seek),
					"Cannot rewind HTTP stream to regenerate body"));
			} else {
				*generated = true;
				return body_reader;
			}
		});
	} else if (exp_body_reader.error().code != http::MakeError(http::BodyMissingError, "").code) {
		connection->logger_.Error(
			"Could not get body reader for request: " + exp_body_reader.error().String());
		connections_.erase(req_in);
		return;
	} // else: if body is missing we don't need to do anything.

	auto &cancelled = cancelled_;
	auto err = connection->client_.AsyncCall(
		req_out,
		[this, cancelled, req_in](http::ExpectedIncomingResponsePtr exp_resp) {
			if (!*cancelled) {
				ResponseHeaderHandler(req_in, exp_resp);
			}
		},
		[this, cancelled, req_in](http::ExpectedIncomingResponsePtr exp_resp) {
			if (!*cancelled) {
				ResponseBodyHandler(req_in, exp_resp);
			}
		});
}

void Server::RequestBodyHandler(http::IncomingRequestPtr req_in, error::Error err) {
	auto maybe_connection = connections_.find(req_in);
	if (maybe_connection == connections_.end()) {
		// Can happen if the request was cancelled.
		return;
	}
	auto &connection = maybe_connection->second;

	if (err != error::NoError) {
		connection->logger_.Error("Error while reading incoming request body: " + err.String());
		connections_.erase(req_in);
		return;
	}

	auto exp_resp_out = connection->req_in_->MakeResponse();
	if (!exp_resp_out) {
		connection->logger_.Error(
			"Could not make outgoing response: " + exp_resp_out.error().String());
		connections_.erase(req_in);
		return;
	}
	connection->resp_out_ = exp_resp_out.value();
}

void Server::ResponseHeaderHandler(
	http::IncomingRequestPtr req_in, http::ExpectedIncomingResponsePtr exp_resp_in) {
	auto connection = connections_[req_in];

	if (!exp_resp_in) {
		connection->logger_.Error("Error in incoming response: " + exp_resp_in.error().String());
		connections_.erase(req_in);
		return;
	}
	connection->resp_in_ = exp_resp_in.value();
	auto &resp_in = connection->resp_in_;

	auto &resp_out = connection->resp_out_;

	resp_out->SetStatusCodeAndMessage(resp_in->GetStatusCode(), resp_in->GetStatusMessage());
	for (auto header : resp_in->GetHeaders()) {
		resp_out->SetHeader(header.first, header.second);
	}

	auto exp_body_reader = resp_in->MakeBodyAsyncReader();

	if (resp_in->GetStatusCode() == http::StatusSwitchingProtocols) {
		if (exp_body_reader) {
			connection->logger_.Error(
				"Response both requested to switch protocol, and has a body, which is not supported");
			exp_body_reader.value()->Cancel();
			connections_.erase(req_in);
		} else {
			SwitchProtocol(req_in, resp_in, resp_out);
		}
		return;
	} else if (exp_body_reader) {
		resp_out->SetAsyncBodyReader(exp_body_reader.value());
	} else if (exp_body_reader.error().code != http::MakeError(http::BodyMissingError, "").code) {
		connection->logger_.Error(
			"Could not get body reader for response: " + exp_body_reader.error().String());
		connections_.erase(req_in);
		return;
	} // else: if body is missing we don't need to do anything.

	auto &cancelled = cancelled_;
	auto err = resp_out->AsyncReply([cancelled, this, req_in](error::Error err) {
		if (*cancelled) {
			return;
		}

		if (err != error::NoError) {
			connections_[req_in]->logger_.Error(
				"Error while forwarding response to client: " + err.String());
			connections_.erase(req_in);
			return;
		}

		auto &connection = connections_[req_in];
		connection->incoming_request_finished_ = true;
		if (connection->outgoing_request_finished_) {
			// We are done, remove connection.
			connections_.erase(req_in);
		}
	});
	if (err != error::NoError) {
		connection->logger_.Error("Could not forward response to client: " + err.String());
		connections_.erase(req_in);
		return;
	}
}

void Server::SwitchProtocol(
	http::IncomingRequestPtr req_in,
	http::IncomingResponsePtr resp_in,
	http::OutgoingResponsePtr resp_out) {
	auto exp_remote_socket = resp_in->SwitchProtocol();
	if (!exp_remote_socket) {
		connections_[req_in]->logger_.Error(
			"Could not switch protocol: " + exp_remote_socket.error().String());
		connections_.erase(req_in);
		return;
	}
	auto &remote_socket = exp_remote_socket.value();

	auto &cancelled = cancelled_;

	auto err = resp_out->AsyncSwitchProtocol([cancelled, this, req_in, resp_out, remote_socket](
												 io::ExpectedAsyncReadWriterPtr exp_local_socket) {
		if (*cancelled) {
			return;
		}

		if (!exp_local_socket) {
			connections_[req_in]->logger_.Error(
				"Could not switch protocol: " + exp_local_socket.error().String());
			connections_.erase(req_in);
			return;
		}
		auto &local_socket = exp_local_socket.value();

		auto finished_handler =
			[this, req_in, cancelled, local_socket, remote_socket](error::Error err) {
				if (!*cancelled && err != error::NoError) {
					log::Error("Error during network socket forwarding: " + err.String());
				}

				local_socket->Cancel();
				remote_socket->Cancel();

				if (!*cancelled) {
					connections_.erase(req_in);
				}
			};

		// Forward in both directions.
		io::AsyncCopy(local_socket, remote_socket, finished_handler);
		io::AsyncCopy(remote_socket, local_socket, finished_handler);
	});
	if (err != error::NoError) {
		connections_[req_in]->logger_.Error("Could not switch protocol: " + err.String());
		connections_.erase(req_in);
		return;
	}
}

void Server::ResponseBodyHandler(
	http::IncomingRequestPtr req_in, http::ExpectedIncomingResponsePtr exp_resp_in) {
	auto &connection = connections_[req_in];

	if (!exp_resp_in) {
		connection->logger_.Error(
			"Error while reading incoming response body: " + exp_resp_in.error().String());
		connections_.erase(req_in);
		return;
	}

	connection->outgoing_request_finished_ = true;
	if (connection->incoming_request_finished_) {
		// We are done, remove connection.
		connections_.erase(req_in);
	}
}

} // namespace http_forwarder
} // namespace auth
} // namespace mender
