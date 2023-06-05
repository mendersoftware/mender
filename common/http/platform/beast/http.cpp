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

#include <boost/asio/ip/tcp.hpp>
#include <boost/asio/ssl/verify_mode.hpp>
#include <boost/beast/core/stream_traits.hpp>
#include <boost/asio.hpp>
#include <common/http.hpp>

#include <common/common.hpp>

namespace mender {
namespace http {

namespace common = mender::common;

// At the time of writing, Beast only supports HTTP/1.1, and is unlikely to support HTTP/2
// according to this discussion: https://github.com/boostorg/beast/issues/1302.
const unsigned int BeastHttpVersion = 11;

namespace asio = boost::asio;
namespace http = boost::beast::http;

const int HTTP_BEAST_BUFFER_SIZE = MENDER_BUFSIZE;

static http::verb MethodToBeastVerb(Method method) {
	switch (method) {
	case Method::GET:
		return http::verb::get;
	case Method::HEAD:
		return http::verb::head;
	case Method::POST:
		return http::verb::post;
	case Method::PUT:
		return http::verb::put;
	case Method::PATCH:
		return http::verb::patch;
	case Method::CONNECT:
		return http::verb::connect;
	case Method::Invalid:
		// Fallthrough to end (no-op).
		break;
	}
	// Don't use "default" case. This should generate a warning if we ever add any methods. But
	// still assert here for safety.
	assert(false);
	return http::verb::get;
}

static expected::expected<Method, error::Error> BeastVerbToMethod(
	http::verb verb, const string &verb_string) {
	switch (verb) {
	case http::verb::get:
		return Method::GET;
	case http::verb::head:
		return Method::HEAD;
	case http::verb::post:
		return Method::POST;
	case http::verb::put:
		return Method::PUT;
	case http::verb::patch:
		return Method::PATCH;
	case http::verb::connect:
		return Method::CONNECT;
	default:
		return expected::unexpected(MakeError(UnsupportedMethodError, verb_string));
	}
}

error::Error OutgoingResponse::AsyncReply(ReplyFinishedHandler reply_finished_handler) {
	auto stream = stream_.lock();
	if (!stream) {
		return MakeError(StreamCancelledError, "Cannot send response");
	}

	stream->AsyncReply(reply_finished_handler);
	has_replied_ = true;
	return error::NoError;
}

Client::Client(ClientConfig &client, events::EventLoop &event_loop) :
	logger_("http"),
	resolver_(GetAsioIoContext(event_loop)),
	stream_(GetAsioIoContext(event_loop), client.ctx_),
	body_buffer_(HTTP_BEAST_BUFFER_SIZE) {
	response_buffer_.reserve(body_buffer_.size());

	// Don't enforce limits. Since we stream everything, limits don't generally apply, and
	// if they do, they should be handled higher up in the application logic.
	//
	// Note: There is a bug in Beast here (tested on 1.74): One is supposed to be able to
	// pass an uninitialized `optional` to mean unlimited, but they do not check for
	// `has_value()` in their code, causing their subsequent comparison operation to
	// misbehave. So pass highest possible value instead.
	http_response_parser_.body_limit(numeric_limits<uint64_t>::max());
}

Client::~Client() {
	Cancel();
}

error::Error Client::AsyncCall(
	OutgoingRequestPtr req, ResponseHandler header_handler, ResponseHandler body_handler) {
	Cancel();

	if (req->address_.protocol == "" || req->address_.host == "" || req->address_.port < 0) {
		return error::MakeError(error::ProgrammingError, "Request is not ready");
	}

	if (!header_handler || !body_handler) {
		return error::MakeError(
			error::ProgrammingError, "header_handler and body_handler can not be nullptr");
	}

	if (req->address_.protocol != "http" && req->address_.protocol != "https") {
		return error::Error(
			make_error_condition(errc::protocol_not_supported), req->address_.protocol);
	}

	if (req->address_.protocol == "https") {
		is_https_ = true;
	}

	logger_ = log::Logger("http_client").WithFields(log::LogField("url", req->orig_address_));

	// NOTE: The AWS loadbalancer requires that the HOST header always be set, in order for the
	// request to route to our k8s cluster. Set this in all cases.
	req->SetHeader("HOST", req->address_.host);

	request_ = req;
	header_handler_ = header_handler;
	body_handler_ = body_handler;

	// See comment in header.
	stream_active_.reset(this, [](Client *) {});

	resolver_.async_resolve(
		request_->address_.host,
		to_string(request_->address_.port),
		[this](const error_code &err, const asio::ip::tcp::resolver::results_type &results) {
			ResolveHandler(err, results);
		});

	return error::NoError;
}

void Client::CallErrorHandler(
	const error_code &err, const OutgoingRequestPtr &req, ResponseHandler handler) {
	handler(expected::unexpected(error::Error(
		err.default_error_condition(), MethodToString(req->method_) + " " + req->orig_address_)));
	stream_active_.reset();
}

void Client::CallErrorHandler(
	const error::Error &err, const OutgoingRequestPtr &req, ResponseHandler handler) {
	handler(expected::unexpected(error::Error(
		err.code, err.message + ": " + MethodToString(req->method_) + " " + req->orig_address_)));
	stream_active_.reset();
}

void Client::ResolveHandler(
	const error_code &err, const asio::ip::tcp::resolver::results_type &results) {
	if (err) {
		CallErrorHandler(err, request_, header_handler_);
		return;
	}

	if (logger_.Level() >= log::LogLevel::Debug) {
		string ips = "[";
		string sep;
		for (auto r : results) {
			ips += sep;
			ips += r.endpoint().address().to_string();
			sep = ", ";
		}
		ips += "]";
		logger_.Debug("Hostname " + request_->address_.host + " resolved to " + ips);
	}

	resolver_results_ = results;

	weak_ptr<Client> weak_client(stream_active_);

	beast::get_lowest_layer(this->stream_)
		.async_connect(
			resolver_results_,
			[weak_client](const error_code &err, const asio::ip::tcp::endpoint &endpoint) {
				auto client = weak_client.lock();
				if (client) {
					if (client->is_https_) {
						return client->HandshakeHandler(err, endpoint);
					}
					return client->ConnectHandler(err, endpoint);
				}
			});
}

void Client::HandshakeHandler(const error_code &err, const asio::ip::tcp::endpoint &endpoint) {
	if (err) {
		CallErrorHandler(err, request_, header_handler_);
		return;
	}

	// Set SNI Hostname (many hosts need this to handshake successfully)
	if (!SSL_set_tlsext_host_name(stream_.native_handle(), request_->address_.host.c_str())) {
		beast::error_code ec {static_cast<int>(::ERR_get_error()), asio::error::get_ssl_category()};
		logger_.Error("Failed to set SNI host name: " + ec.message());
	}

	weak_ptr<Client> weak_client(stream_active_);

	stream_.async_handshake(
		ssl::stream_base::client, [weak_client, endpoint](const error_code &ec) {
			auto client = weak_client.lock();
			if (!client) {
				return;
			}
			if (ec) {
				client->logger_.Error(
					"https: Failed to perform the SSL handshake: " + ec.message());
				client->CallErrorHandler(ec, client->request_, client->header_handler_);
				return;
			}
			client->logger_.Debug("https: Successful SSL handshake");
			client->ConnectHandler(ec, endpoint);
		});
}


void Client::ConnectHandler(const error_code &err, const asio::ip::tcp::endpoint &endpoint) {
	if (err) {
		CallErrorHandler(err, request_, header_handler_);
		return;
	}

	logger_.Debug("Connected to " + endpoint.address().to_string());

	http_request_ = make_shared<http::request<http::buffer_body>>(
		MethodToBeastVerb(request_->method_), request_->address_.path, BeastHttpVersion);

	for (const auto &header : request_->headers_) {
		http_request_->set(header.first, header.second);
	}

	http_request_serializer_ =
		make_shared<http::request_serializer<http::buffer_body>>(*http_request_);

	weak_ptr<Client> weak_client(stream_active_);

	if (is_https_) {
		http::async_write_header(
			stream_,
			*http_request_serializer_,
			[weak_client](const error_code &err, size_t num_written) {
				auto client = weak_client.lock();
				if (client) {
					client->WriteHeaderHandler(err, num_written);
				}
			});
	} else {
		http::async_write_header(
			beast::get_lowest_layer(stream_),
			*http_request_serializer_,
			[weak_client](const error_code &err, size_t num_written) {
				auto client = weak_client.lock();
				if (client) {
					client->WriteHeaderHandler(err, num_written);
				}
			});
	}
}

void Client::WriteHeaderHandler(const error_code &err, size_t num_written) {
	if (num_written > 0) {
		logger_.Trace("Wrote " + to_string(num_written) + " bytes of header data to stream.");
	}

	if (err) {
		CallErrorHandler(err, request_, header_handler_);
		return;
	}

	auto header = request_->GetHeader("Content-Length");
	if (!header || header.value() == "0") {
		ReadHeader();
		return;
	}

	auto length = common::StringToLongLong(header.value());
	if (!length || length.value() < 0) {
		auto err = error::Error(
			length.error().code, "Content-Length contains invalid number: " + header.value());
		CallErrorHandler(err, request_, header_handler_);
		return;
	}
	request_body_length_ = length.value();

	if (!request_->body_gen_) {
		auto err = MakeError(BodyMissingError, "Content-Length is non-zero, but body is missing");
		CallErrorHandler(err, request_, header_handler_);
		return;
	}

	auto body_reader = request_->body_gen_();
	if (!body_reader) {
		CallErrorHandler(body_reader.error(), request_, header_handler_);
		return;
	}
	request_->body_reader_ = body_reader.value();

	PrepareBufferAndWriteBody();
}

void Client::WriteBodyHandler(const error_code &err, size_t num_written) {
	if (num_written > 0) {
		logger_.Trace("Wrote " + to_string(num_written) + " bytes of body data to stream.");
	}

	if (err == http::make_error_code(http::error::need_buffer)) {
		// Write next block of the body.
		PrepareBufferAndWriteBody();
	} else if (err) {
		CallErrorHandler(err, request_, header_handler_);
	} else if (num_written > 0) {
		// We are still writing the body.
		WriteBody();
	} else {
		// We are ready to receive the response.
		ReadHeader();
	}
}

void Client::PrepareBufferAndWriteBody() {
	auto read = request_->body_reader_->Read(body_buffer_.begin(), body_buffer_.end());
	if (!read) {
		CallErrorHandler(read.error(), request_, header_handler_);
		return;
	}

	http_request_->body().data = body_buffer_.data();
	http_request_->body().size = read.value();

	if (read.value() > 0) {
		http_request_->body().more = true;
	} else {
		// Release ownership of Body reader.
		request_->body_reader_.reset();
		http_request_->body().more = false;
	}

	WriteBody();
}

void Client::WriteBody() {
	weak_ptr<Client> weak_client(stream_active_);

	if (is_https_) {
		http::async_write_some(
			stream_,
			*http_request_serializer_,
			[weak_client](const error_code &err, size_t num_written) {
				auto client = weak_client.lock();
				if (client) {
					client->WriteBodyHandler(err, num_written);
				}
			});
	} else {
		http::async_write_some(
			beast::get_lowest_layer(stream_),
			*http_request_serializer_,
			[weak_client](const error_code &err, size_t num_written) {
				auto client = weak_client.lock();
				if (client) {
					client->WriteBodyHandler(err, num_written);
				}
			});
	}
}

void Client::ReadHeader() {
	http_response_parser_.get().body().data = body_buffer_.data();
	http_response_parser_.get().body().size = body_buffer_.size();

	weak_ptr<Client> weak_client(stream_active_);

	if (is_https_) {
		http::async_read_some(
			stream_,
			response_buffer_,
			http_response_parser_,
			[weak_client](const error_code &err, size_t num_read) {
				auto client = weak_client.lock();
				if (client) {
					client->ReadHeaderHandler(err, num_read);
				}
			});
	} else {
		http::async_read_some(
			beast::get_lowest_layer(stream_),
			response_buffer_,
			http_response_parser_,
			[weak_client](const error_code &err, size_t num_read) {
				auto client = weak_client.lock();
				if (client) {
					client->ReadHeaderHandler(err, num_read);
				}
			});
	}
}

void Client::ReadHeaderHandler(const error_code &err, size_t num_read) {
	if (num_read > 0) {
		logger_.Trace("Read " + to_string(num_read) + " bytes of header data from stream.");
	}

	if (err) {
		CallErrorHandler(err, request_, header_handler_);
		return;
	}

	if (!http_response_parser_.is_header_done()) {
		ReadHeader();
		return;
	}

	response_.reset(new IncomingResponse);
	response_->status_code_ = http_response_parser_.get().result_int();
	response_->status_message_ = string {http_response_parser_.get().reason()};

	string debug_str;
	for (auto header = http_response_parser_.get().cbegin();
		 header != http_response_parser_.get().cend();
		 header++) {
		response_->headers_[string {header->name_string()}] = string {header->value()};
		if (logger_.Level() >= log::LogLevel::Debug) {
			debug_str += string {header->name_string()};
			debug_str += ": ";
			debug_str += string {header->value()};
			debug_str += "\n";
		}
	}

	logger_.Debug("Received headers:\n" + debug_str);
	debug_str.clear();

	if (http_response_parser_.chunked()) {
		header_handler_(response_);
		auto err = MakeError(UnsupportedBodyType, "`Transfer-Encoding: chunked` not supported");
		CallErrorHandler(err, request_, body_handler_);
		return;
	}

	if (http_response_parser_.is_done()) {
		header_handler_(response_);
		body_handler_(response_);
		return;
	}

	auto content_length = http_response_parser_.content_length();
	if (content_length && content_length.value() > 0 && !response_->body_writer_) {
		logger_.Debug("Response contains a body, but we are ignoring it");
	}

	http_response_parser_.get().body().data = body_buffer_.data();
	http_response_parser_.get().body().size = body_buffer_.size();

	weak_ptr<Client> weak_client(stream_active_);

	if (is_https_) {
		http::async_read_some(
			stream_,
			response_buffer_,
			http_response_parser_,
			[weak_client](const error_code &err, size_t num_read) {
				auto client = weak_client.lock();
				if (client) {
					client->ReadBodyHandler(err, num_read);
				}
			});
	} else {
		http::async_read_some(
			beast::get_lowest_layer(stream_),
			response_buffer_,
			http_response_parser_,
			[weak_client](const error_code &err, size_t num_read) {
				auto client = weak_client.lock();
				if (client) {
					client->ReadBodyHandler(err, num_read);
				}
			});
	}
	// Call this after scheduling the read above, so that the handler can cancel it if
	// necessary.
	header_handler_(response_);
}

void Client::ReadBodyHandler(const error_code &err, size_t num_read) {
	if (num_read > 0) {
		logger_.Trace("Read " + to_string(num_read) + " bytes of body data from stream.");
	}

	if (err) {
		CallErrorHandler(err, request_, body_handler_);
		return;
	}

	if (response_->body_writer_) {
		response_->body_writer_->Write(body_buffer_.begin(), body_buffer_.begin() + num_read);
	}

	if (http_response_parser_.is_done()) {
		// Release ownership of writer, which closes it if there are no other
		// holders.
		response_->body_writer_.reset();
		body_handler_(response_);
		stream_active_.reset();
		return;
	}

	http_response_parser_.get().body().data = body_buffer_.data();
	http_response_parser_.get().body().size = body_buffer_.size();

	weak_ptr<Client> weak_client(stream_active_);

	if (is_https_) {
		http::async_read_some(
			stream_,
			response_buffer_,
			http_response_parser_,
			[weak_client](const error_code &err, size_t num_read) {
				auto client = weak_client.lock();
				if (client) {
					client->ReadBodyHandler(err, num_read);
				}
			});
	} else {
		http::async_read_some(
			beast::get_lowest_layer(stream_),
			response_buffer_,
			http_response_parser_,
			[weak_client](const error_code &err, size_t num_read) {
				auto client = weak_client.lock();
				if (client) {
					client->ReadBodyHandler(err, num_read);
				}
			});
	}
}

void Client::Cancel() {
	resolver_.cancel();
	beast::get_lowest_layer(stream_).cancel();
	beast::get_lowest_layer(stream_).close();
	stream_active_.reset();

	request_.reset();
	response_.reset();

	// Reset logger to no connection.
	logger_ = log::Logger("http_client");
}

ClientConfig::ClientConfig() :
	ClientConfig("") {
}

ClientConfig::ClientConfig(string server_cert_path) {
	ctx_.set_verify_mode(ssl::verify_peer);

	beast::error_code ec {};
	ctx_.set_default_verify_paths(ec); // Load the default CAs
	if (ec) {
		log::Error("Failed to load the SSl default directory");
	}
	if (server_cert_path != "") {
		ctx_.load_verify_file(server_cert_path, ec);
		if (ec) {
			log::Error("Failed to load the server certificate!");
		}
	}
}

ClientConfig::~ClientConfig() {
}

ServerConfig::ServerConfig() {
}

ServerConfig::~ServerConfig() {
}

Stream::Stream(Server &server) :
	server_(server),
	logger_("http"),
	socket_(server_.GetAsioIoContext(server_.event_loop_)),
	body_buffer_(HTTP_BEAST_BUFFER_SIZE) {
	request_buffer_.reserve(body_buffer_.size());

	// Don't enforce limits. Since we stream everything, limits don't generally apply, and if
	// they do, they should be handled higher up in the application logic.
	//
	// Note: There is a bug in Beast here (tested on 1.74): One is supposed to be able to pass
	// an uninitialized `optional` to mean unlimited, but they do not check for `has_value()` in
	// their code, causing their subsequent comparison operation to misbehave. So pass highest
	// possible value instead.
	http_request_parser_.body_limit(numeric_limits<uint64_t>::max());
}

Stream::~Stream() {
	Cancel();
}

void Stream::Cancel() {
	if (socket_.is_open()) {
		socket_.cancel();
		socket_.close();
	}
	stream_active_.reset();
}

void Stream::CallErrorHandler(const error_code &ec, const RequestPtr &req, RequestHandler handler) {
	handler(expected::unexpected(error::Error(
		ec.default_error_condition(),
		req->address_.host + ": " + MethodToString(req->method_) + " " + request_->GetPath())));
	stream_active_.reset();

	server_.RemoveStream(shared_from_this());
}

void Stream::CallErrorHandler(
	const error::Error &err, const RequestPtr &req, RequestHandler handler) {
	handler(expected::unexpected(error::Error(
		err.code,
		err.message + ": " + req->address_.host + ": " + MethodToString(req->method_) + " "
			+ request_->GetPath())));
	stream_active_.reset();

	server_.RemoveStream(shared_from_this());
}

void Stream::CallErrorHandler(
	const error_code &ec, const RequestPtr &req, ReplyFinishedHandler handler) {
	handler(error::Error(
		ec.default_error_condition(),
		req->address_.host + ": " + MethodToString(req->method_) + " " + request_->GetPath()));
	stream_active_.reset();

	server_.RemoveStream(shared_from_this());
}

void Stream::CallErrorHandler(
	const error::Error &err, const RequestPtr &req, ReplyFinishedHandler handler) {
	handler(error::Error(
		err.code,
		err.message + ": " + req->address_.host + ": " + MethodToString(req->method_) + " "
			+ request_->GetPath()));
	stream_active_.reset();

	server_.RemoveStream(shared_from_this());
}

void Stream::AcceptHandler(const error_code &err) {
	if (err) {
		log::Error("Error while accepting HTTP connection: " + err.message());
		return;
	}

	auto ip = socket_.remote_endpoint().address().to_string();

	// Use IP as context for logging.
	logger_ = log::Logger("http_server").WithFields(log::LogField("ip", ip));

	logger_.Debug("Accepted connection.");

	request_.reset(new IncomingRequest);
	request_->stream_ = shared_from_this();

	request_->address_.host = ip;

	stream_active_.reset(this, [](Stream *) {});

	ReadHeader();
}

void Stream::ReadHeader() {
	http_request_parser_.get().body().data = body_buffer_.data();
	http_request_parser_.get().body().size = body_buffer_.size();

	weak_ptr<Stream> weak_stream(stream_active_);

	http::async_read_some(
		socket_,
		request_buffer_,
		http_request_parser_,
		[weak_stream](const error_code &err, size_t num_read) {
			auto stream = weak_stream.lock();
			if (stream) {
				stream->ReadHeaderHandler(err, num_read);
			}
		});
}

void Stream::ReadHeaderHandler(const error_code &err, size_t num_read) {
	if (num_read > 0) {
		logger_.Trace("Read " + to_string(num_read) + " bytes of header data from stream.");
	}

	if (err) {
		CallErrorHandler(err, request_, server_.header_handler_);
		return;
	}

	if (!http_request_parser_.is_header_done()) {
		ReadHeader();
		return;
	}

	auto method_result = BeastVerbToMethod(
		http_request_parser_.get().base().method(),
		string {http_request_parser_.get().base().method_string()});
	if (!method_result) {
		CallErrorHandler(method_result.error(), request_, server_.header_handler_);
		return;
	}
	request_->method_ = method_result.value();
	request_->address_.path = string(http_request_parser_.get().base().target());

	string debug_str;
	for (auto header = http_request_parser_.get().cbegin();
		 header != http_request_parser_.get().cend();
		 header++) {
		request_->headers_[string {header->name_string()}] = string {header->value()};
		if (logger_.Level() >= log::LogLevel::Debug) {
			debug_str += string {header->name_string()};
			debug_str += ": ";
			debug_str += string {header->value()};
			debug_str += "\n";
		}
	}

	logger_.Debug("Received headers:\n" + debug_str);
	debug_str.clear();

	if (http_request_parser_.chunked()) {
		server_.header_handler_(request_);
		auto err = MakeError(UnsupportedBodyType, "`Transfer-Encoding: chunked` not supported");
		CallErrorHandler(err, request_, server_.body_handler_);
		return;
	}

	if (http_request_parser_.is_done()) {
		server_.header_handler_(request_);
		CallBodyHandler();
		return;
	}

	auto content_length = http_request_parser_.content_length();
	if (content_length && content_length.value() > 0 && !request_->body_writer_) {
		logger_.Debug("Request contains a body, but we are ignoring it");
	}

	http_request_parser_.get().body().data = body_buffer_.data();
	http_request_parser_.get().body().size = body_buffer_.size();

	weak_ptr<Stream> weak_stream(stream_active_);

	http::async_read_some(
		socket_,
		request_buffer_,
		http_request_parser_,
		[weak_stream](const error_code &err, size_t num_read) {
			auto stream = weak_stream.lock();
			if (stream) {
				stream->ReadBodyHandler(err, num_read);
			}
		});

	// Call this after scheduling the read above, so that the handler can cancel it if
	// necessary.
	server_.header_handler_(request_);
}

void Stream::ReadBodyHandler(const error_code &err, size_t num_read) {
	if (num_read > 0) {
		logger_.Trace("Read " + to_string(num_read) + " bytes of body data from stream.");
	}

	if (err) {
		CallErrorHandler(err, request_, server_.body_handler_);
		return;
	}

	if (request_->body_writer_) {
		request_->body_writer_->Write(body_buffer_.begin(), body_buffer_.begin() + num_read);
	}

	if (!http_request_parser_.is_done()) {
		http_request_parser_.get().body().data = body_buffer_.data();
		http_request_parser_.get().body().size = body_buffer_.size();

		weak_ptr<Stream> weak_stream(stream_active_);

		http::async_read_some(
			socket_,
			request_buffer_,
			http_request_parser_,
			[weak_stream](const error_code &err, size_t num_read) {
				auto stream = weak_stream.lock();
				if (stream) {
					stream->ReadBodyHandler(err, num_read);
				}
			});
		return;
	}

	CallBodyHandler();
}

void Stream::AsyncReply(ReplyFinishedHandler reply_finished_handler) {
	auto response = maybe_response_.lock();
	// Only called from existing responses, so this should always be true.
	assert(response);

	// From here on we take shared ownership.
	response_ = response;

	reply_finished_handler_ = reply_finished_handler;

	http_response_ = make_shared<http::response<http::buffer_body>>();

	for (const auto &header : response->headers_) {
		http_response_->base().set(header.first, header.second);
	}

	http_response_->result(response->GetStatusCode());
	http_response_->reason(response->GetStatusMessage());

	http_response_serializer_ =
		make_shared<http::response_serializer<http::buffer_body>>(*http_response_);

	weak_ptr<Stream> weak_stream(stream_active_);

	http::async_write_header(
		socket_,
		*http_response_serializer_,
		[weak_stream](const error_code &err, size_t num_written) {
			auto stream = weak_stream.lock();
			if (stream) {
				stream->WriteHeaderHandler(err, num_written);
			}
		});
}

void Stream::WriteHeaderHandler(const error_code &err, size_t num_written) {
	if (num_written > 0) {
		logger_.Trace("Wrote " + to_string(num_written) + " bytes of header data to stream.");
	}

	if (err) {
		CallErrorHandler(err, request_, reply_finished_handler_);
		return;
	}

	auto header = response_->GetHeader("Content-Length");
	if (!header || header.value() == "0") {
		FinishReply();
		return;
	}

	auto length = common::StringToLongLong(header.value());
	if (!length || length.value() < 0) {
		auto err = error::Error(
			length.error().code, "Content-Length contains invalid number: " + header.value());
		CallErrorHandler(err, request_, reply_finished_handler_);
		return;
	}

	if (!response_->body_reader_) {
		auto err = MakeError(BodyMissingError, "Content-Length is non-zero, but body is missing");
		CallErrorHandler(err, request_, reply_finished_handler_);
		return;
	}

	PrepareBufferAndWriteBody();
}

void Stream::PrepareBufferAndWriteBody() {
	auto read = response_->body_reader_->Read(body_buffer_.begin(), body_buffer_.end());
	if (!read) {
		CallErrorHandler(read.error(), request_, reply_finished_handler_);
		return;
	}

	http_response_->body().data = body_buffer_.data();
	http_response_->body().size = read.value();

	if (read.value() > 0) {
		http_response_->body().more = true;
	} else {
		// Release ownership of Body reader.
		response_->body_reader_.reset();
		http_response_->body().more = false;
	}

	WriteBody();
}

void Stream::WriteBody() {
	weak_ptr<Stream> weak_stream(stream_active_);

	http::async_write_some(
		socket_,
		*http_response_serializer_,
		[weak_stream](const error_code &err, size_t num_written) {
			auto stream = weak_stream.lock();
			if (stream) {
				stream->WriteBodyHandler(err, num_written);
			}
		});
}

void Stream::WriteBodyHandler(const error_code &err, size_t num_written) {
	if (num_written > 0) {
		logger_.Trace("Wrote " + to_string(num_written) + " bytes of body data to stream.");
	}

	if (err == http::make_error_code(http::error::need_buffer)) {
		// Write next body block.
		PrepareBufferAndWriteBody();
	} else if (err) {
		CallErrorHandler(err, request_, reply_finished_handler_);
	} else if (num_written > 0) {
		// We are still writing the body.
		WriteBody();
	} else {
		// We are finished.
		FinishReply();
	}
}

void Stream::FinishReply() {
	// We are done.
	reply_finished_handler_(error::NoError);
	stream_active_.reset();
	server_.RemoveStream(shared_from_this());
}

void Stream::CallBodyHandler() {
	// Release ownership of writer, which closes it if there are no other holders.
	request_->body_writer_.reset();

	// Get a pointer to ourselves. This is just in case the body handler make a response, which
	// it immediately destroys, which would destroy this stream as well. At the end of this
	// function, it's ok to destroy it.
	auto stream_ref = shared_from_this();

	server_.body_handler_(request_);

	// MakeResponse() should have been called inside body handler. It can use this to generate a
	// response, either immediately, or later. Therefore it should still exist, otherwise the
	// request has not been handled correctly.
	auto response = maybe_response_.lock();
	if (!response) {
		logger_.Error("Handler produced no response. Closing stream prematurely.");
		server_.RemoveStream(shared_from_this());
	}
}

Server::Server(const ServerConfig &server, events::EventLoop &event_loop) :
	event_loop_(event_loop),
	acceptor_(GetAsioIoContext(event_loop_)) {
}

Server::~Server() {
	Cancel();
}

error::Error Server::AsyncServeUrl(
	const string &url, RequestHandler header_handler, RequestHandler body_handler) {
	auto err = BreakDownUrl(url, address_);
	if (error::NoError != err) {
		return MakeError(InvalidUrlError, "Could not parse URL " + url + ": " + err.String());
	}

	if (address_.protocol != "http") {
		return error::Error(make_error_condition(errc::protocol_not_supported), address_.protocol);
	}

	if (address_.path.size() > 0 && address_.path != "/") {
		return MakeError(InvalidUrlError, "URLs with paths are not supported when listening.");
	}

	boost::system::error_code ec;
	auto address = asio::ip::make_address(address_.host, ec);
	if (ec) {
		return error::Error(
			ec.default_error_condition(),
			"Could not construct endpoint from address " + address_.host);
	}

	asio::ip::tcp::endpoint endpoint(address, address_.port);

	ec.clear();
	acceptor_.open(endpoint.protocol(), ec);
	if (ec) {
		return error::Error(ec.default_error_condition(), "Could not open acceptor");
	}

	// Allow address reuse, otherwise we can't re-bind later.
	ec.clear();
	acceptor_.set_option(asio::socket_base::reuse_address(true), ec);
	if (ec) {
		return error::Error(ec.default_error_condition(), "Could not set socket options");
	}

	ec.clear();
	acceptor_.bind(endpoint, ec);
	if (ec) {
		return error::Error(ec.default_error_condition(), "Could not bind socket");
	}

	ec.clear();
	acceptor_.listen(asio::socket_base::max_listen_connections, ec);
	if (ec) {
		return error::Error(ec.default_error_condition(), "Could not start listening");
	}

	header_handler_ = header_handler;
	body_handler_ = body_handler;

	PrepareNewStream();

	return error::NoError;
}

void Server::Cancel() {
	if (acceptor_.is_open()) {
		acceptor_.cancel();
		acceptor_.close();
	}
	streams_.clear();
}

void Server::PrepareNewStream() {
	StreamPtr new_stream {new Stream(*this)};
	streams_.insert(new_stream);
	AsyncAccept(new_stream);
}

void Server::AsyncAccept(StreamPtr stream) {
	acceptor_.async_accept(stream->socket_, [this, stream](const error_code &ec) {
		if (ec) {
			log::Error("Could not accept connection: " + ec.message());
			return;
		}

		stream->AcceptHandler(ec);

		this->PrepareNewStream();
	});
}

void Server::RemoveStream(const StreamPtr &stream) {
	streams_.erase(stream);
}

} // namespace http
} // namespace mender
