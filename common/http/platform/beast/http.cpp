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

#include <common/http.hpp>
#include <common/common.hpp>

namespace mender {
namespace http {

namespace common = mender::common;

// At the time of writing, Beast only supports HTTP/1.1, and is unlikely to support HTTP/2 according
// to this discussion: https://github.com/boostorg/beast/issues/1302.
const unsigned int BeastHttpVersion = 11;

namespace asio = boost::asio;
namespace http = boost::beast::http;

const int HTTP_BEAST_BUFFER_SIZE = 16384;

static http::verb MethodToBeastVerb(Method method) {
	switch (method) {
	case Method::GET:
		return http::verb::get;
	case Method::POST:
		return http::verb::post;
	case Method::PUT:
		return http::verb::put;
	case Method::PATCH:
		return http::verb::patch;
	case Method::CONNECT:
		return http::verb::connect;
	}
	// Don't use "default" case. This should generate a warning if we ever add any methods. But
	// still assert here for safety.
	assert(false);
	return http::verb::get;
}

Session::Session(const Client &client, events::EventLoop &event_loop) :
	logger_("http"),
	resolver_(GetAsioIoContext(event_loop)),
	stream_(GetAsioIoContext(event_loop)),
	body_buffer_(HTTP_BEAST_BUFFER_SIZE) {
	response_buffer_.reserve(body_buffer_.size());
}

Session::~Session() {
	Cancel();
}

error::Error Session::AsyncCall(
	RequestPtr req, ResponseHandler header_handler, ResponseHandler body_handler) {
	Cancel();

	if (!req->ready_) {
		return error::MakeError(error::ProgrammingError, "Request is not ready");
	}

	if (!header_handler || !body_handler) {
		return error::MakeError(
			error::ProgrammingError, "header_handler and body_handler can not be nullptr");
	}

	if (req->address_.protocol != "http") {
		return error::Error(
			make_error_condition(errc::protocol_not_supported), req->address_.protocol);
	}

	// Use url as context for logging.
	logger_ = log::Logger("http").WithFields(log::LogField("url", req->orig_address_));

	request_ = req;
	header_handler_ = header_handler;
	body_handler_ = body_handler;

	resolver_.async_resolve(
		request_->address_.host,
		to_string(request_->address_.port),
		[this](error_code err, const asio::ip::tcp::resolver::results_type &results) {
			ResolveHandler(err, results);
		});

	return error::NoError;
}

void Session::CallErrorHandler(
	const error_code &err, const RequestPtr &req, ResponseHandler handler) {
	handler(expected::unexpected(error::Error(
		err.default_error_condition(), MethodToString(req->method_) + " " + req->orig_address_)));
}

void Session::CallErrorHandler(
	const error::Error &err, const RequestPtr &req, ResponseHandler handler) {
	handler(expected::unexpected(error::Error(
		err.code, err.message + ": " + MethodToString(req->method_) + " " + req->orig_address_)));
}

void Session::ResolveHandler(error_code err, const asio::ip::tcp::resolver::results_type &results) {
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

	this->stream_.async_connect(
		resolver_results_, [this](error_code err, const asio::ip::tcp::endpoint &endpoint) {
			ConnectHandler(err, endpoint);
		});
}

void Session::ConnectHandler(error_code err, const asio::ip::tcp::endpoint &endpoint) {
	if (err) {
		CallErrorHandler(err, request_, header_handler_);
		return;
	}

	logger_.Debug("Connected to " + endpoint.address().to_string());

	http_request_ = make_shared<http::request<http::buffer_body>>(
		MethodToBeastVerb(request_->method_), request_->address_.path, BeastHttpVersion);
	http_request_serializer_ =
		make_shared<http::request_serializer<http::buffer_body>>(*http_request_);

	http::async_write_header(
		stream_, *http_request_serializer_, [this](error_code err, size_t num_written) {
			WriteHeaderHandler(err, num_written);
		});
}

void Session::WriteHeaderHandler(error_code err, size_t num_written) {
	if (err) {
		CallErrorHandler(err, request_, header_handler_);
		return;
	}

	logger_.Trace("Wrote " + to_string(num_written) + " bytes of header data to stream.");

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

	WriteBody();
}

void Session::WriteBodyHandler(error_code err, size_t num_written) {
	if (err) {
		CallErrorHandler(err, request_, header_handler_);
		return;
	}

	logger_.Trace("Wrote " + to_string(num_written) + " bytes of body data to stream.");

	if (num_written == 0) {
		// We are ready to receive the response.
		ReadHeader();
	} else {
		// We are still writing the body.
		WriteBody();
	}
}

void Session::WriteBody() {
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

	http::async_write_some(
		stream_, *http_request_serializer_, [this](error_code err, size_t num_written) {
			WriteBodyHandler(err, num_written);
		});
}

void Session::ReadHeader() {
	http_response_parser_.get().body().data = body_buffer_.data();
	http_response_parser_.get().body().size = body_buffer_.size();
	http::async_read_some(
		stream_, response_buffer_, http_response_parser_, [this](error_code err, size_t num_read) {
			ReadHeaderHandler(err, num_read);
		});
}

void Session::ReadHeaderHandler(error_code err, size_t num_read) {
	if (err) {
		CallErrorHandler(err, request_, header_handler_);
		return;
	}

	logger_.Trace("Read " + to_string(num_read) + " bytes of header data from stream.");

	if (!http_response_parser_.is_header_done()) {
		ReadHeader();
		return;
	}

	response_ = make_shared<Response>(
		http_response_parser_.get().result_int(), string(http_response_parser_.get().reason()));

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

	header_handler_(response_);

	auto content_length = http_response_parser_.content_length();
	if (!content_length || content_length.value() == 0) {
		body_handler_(response_);
		return;
	}

	if (http_response_parser_.is_done()) {
		body_handler_(response_);
		return;
	}

	if (!response_->body_writer_) {
		logger_.Debug("Response contains a body, but we are ignoring it");
	}

	http_response_parser_.get().body().data = body_buffer_.data();
	http_response_parser_.get().body().size = body_buffer_.size();
	http::async_read_some(
		stream_, response_buffer_, http_response_parser_, [this](error_code err, size_t num_read) {
			ReadBodyHandler(err, num_read);
		});
}

void Session::ReadBodyHandler(error_code err, size_t num_read) {
	if (err) {
		CallErrorHandler(err, request_, body_handler_);
		return;
	}

	logger_.Trace("Read " + to_string(num_read) + " bytes of body data from stream.");

	if (response_->body_writer_) {
		response_->body_writer_->Write(body_buffer_.begin(), body_buffer_.begin() + num_read);
	}

	if (http_response_parser_.is_done()) {
		// Release ownership of writer, which closes it if there are no other holders.
		response_->body_writer_.reset();
		body_handler_(response_);
		return;
	}

	http_response_parser_.get().body().data = body_buffer_.data();
	http_response_parser_.get().body().size = body_buffer_.size();
	http::async_read_some(
		stream_, response_buffer_, http_response_parser_, [this](error_code err, size_t num_read) {
			ReadBodyHandler(err, num_read);
		});
}

void Session::Cancel() {
	resolver_.cancel();
	stream_.cancel();

	request_.reset();
	response_.reset();

	// Reset logger to no connection.
	logger_ = log::Logger("http");
}

Client::Client() {
}

Client::~Client() {
}

} // namespace http
} // namespace mender
