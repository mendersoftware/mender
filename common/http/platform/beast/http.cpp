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

#include <algorithm>

#include <boost/asio/ip/tcp.hpp>
#include <boost/asio/ssl/verify_mode.hpp>
#include <boost/asio.hpp>

#include <common/common.hpp>
#include <common/crypto.hpp>

namespace mender {
namespace http {

namespace common = mender::common;
namespace crypto = mender::common::crypto;

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

template <typename StreamType>
class BodyAsyncReader : virtual public io::AsyncReader {
public:
	BodyAsyncReader(StreamType &stream, shared_ptr<bool> cancelled) :
		stream_ {stream},
		cancelled_ {cancelled} {
	}
	~BodyAsyncReader() {
		Cancel();
	}

	error::Error AsyncRead(
		vector<uint8_t>::iterator start,
		vector<uint8_t>::iterator end,
		io::AsyncIoHandler handler) override {
		if (*cancelled_) {
			return error::MakeError(
				error::ProgrammingError,
				"BodyAsyncReader::AsyncRead called after stream is destroyed");
		}
		stream_.AsyncReadNextBodyPart(start, end, handler);
		return error::NoError;
	}

	void Cancel() override {
		if (!*cancelled_) {
			stream_.Cancel();
		}
	}

private:
	StreamType &stream_;
	shared_ptr<bool> cancelled_;

	friend class Client;
	friend class Server;
};

template <typename StreamType>
class RawSocket : virtual public io::AsyncReadWriter {
public:
	RawSocket(shared_ptr<StreamType> stream, shared_ptr<beast::flat_buffer> buffered) :
		destroying_ {make_shared<bool>(false)},
		stream_ {stream},
		buffered_ {buffered} {
		// If there are no buffered bytes, then we don't need it.
		if (buffered_ && buffered_->size() == 0) {
			buffered_.reset();
		}
	}

	~RawSocket() {
		*destroying_ = true;
		Cancel();
	}

	error::Error AsyncRead(
		vector<uint8_t>::iterator start,
		vector<uint8_t>::iterator end,
		io::AsyncIoHandler handler) override {
		// If we have prebuffered bytes, which can happen if the HTTP parser read the
		// header and parts of the body in one block, return those first.
		if (buffered_) {
			return DrainPrebufferedData(start, end, handler);
		}

		read_buffer_ = asio::buffer(&*start, end - start);
		auto &destroying = destroying_;
		stream_->async_read_some(
			read_buffer_,
			[destroying, handler](const boost::system::error_code &ec, size_t num_read) {
				if (*destroying) {
					return;
				}

				if (ec == asio::error::operation_aborted) {
					handler(expected::unexpected(error::Error(
						make_error_condition(errc::operation_canceled),
						"Could not read from socket")));
				} else if (ec) {
					handler(expected::unexpected(
						error::Error(ec.default_error_condition(), "Could not read from socket")));
				} else {
					handler(num_read);
				}
			});
		return error::NoError;
	}

	error::Error AsyncWrite(
		vector<uint8_t>::const_iterator start,
		vector<uint8_t>::const_iterator end,
		io::AsyncIoHandler handler) override {
		write_buffer_ = asio::buffer(&*start, end - start);
		auto &destroying = destroying_;
		stream_->async_write_some(
			write_buffer_,
			[destroying, handler](const boost::system::error_code &ec, size_t num_written) {
				if (*destroying) {
					return;
				}

				if (ec == asio::error::operation_aborted) {
					handler(expected::unexpected(error::Error(
						make_error_condition(errc::operation_canceled),
						"Could not write to socket")));
				} else if (ec) {
					handler(expected::unexpected(
						error::Error(ec.default_error_condition(), "Could not write to socket")));
				} else {
					handler(num_written);
				}
			});
		return error::NoError;
	}

	void Cancel() override {
		if (stream_->lowest_layer().is_open()) {
			stream_->lowest_layer().cancel();
			stream_->lowest_layer().close();
		}
	}

private:
	error::Error DrainPrebufferedData(
		vector<uint8_t>::iterator start,
		vector<uint8_t>::iterator end,
		io::AsyncIoHandler handler) {
		size_t to_copy = min(static_cast<size_t>(end - start), buffered_->size());
		copy_n(static_cast<const uint8_t *>(buffered_->cdata().data()), to_copy, start);
		buffered_->consume(to_copy);
		if (buffered_->size() == 0) {
			// We don't need it anymore.
			buffered_.reset();
		}
		handler(to_copy);
		return error::NoError;
	}

	shared_ptr<bool> destroying_;
	shared_ptr<StreamType> stream_;
	shared_ptr<beast::flat_buffer> buffered_;
	asio::mutable_buffer read_buffer_;
	asio::const_buffer write_buffer_;
};

Client::Client(
	const ClientConfig &client, events::EventLoop &event_loop, const string &logger_name) :
	event_loop_ {event_loop},
	logger_name_ {logger_name},
	cancelled_ {make_shared<bool>(true)},
	disable_keep_alive_ {client.disable_keep_alive},
	resolver_(GetAsioIoContext(event_loop)),
	body_buffer_(HTTP_BEAST_BUFFER_SIZE) {
	ssl_ctx_.set_verify_mode(client.skip_verify ? ssl::verify_none : ssl::verify_peer);

	if (client.client_cert_path != "" and client.client_cert_key_path != "") {
		ssl_ctx_.set_options(boost::asio::ssl::context::default_workarounds);

		beast::error_code ec {};
		ssl_ctx_.use_certificate_file(
			client.client_cert_path, boost::asio::ssl::context_base::pem, ec);
		if (ec) {
			log::Error(
				"Failed to load the client certificate key: (" + client.client_cert_path
				+ "): " + ec.message());
		}

		auto exp_key =
			crypto::PrivateKey::Load({client.client_cert_key_path, "", client.ssl_engine});
		if (!exp_key) {
			log::Error(
				"Error loading private key from " + client.client_cert_key_path + ": "
				+ exp_key.error().message);
			return;
		}

		const int ret =
			SSL_CTX_use_PrivateKey(ssl_ctx_.native_handle(), exp_key.value().get()->Get());
		if (ret != 1) {
			log::Error(
				"Failed to add the PrivateKey: " + client.client_cert_key_path
				+ " to the SSL CTX. The HTTP client will not be functioning normally");
		}
	}

	beast::error_code ec {};
	ssl_ctx_.set_default_verify_paths(ec); // Load the default CAs
	if (ec) {
		log::Error("Failed to load the SSL default directory");
	}
	if (client.server_cert_path != "") {
		ssl_ctx_.load_verify_file(client.server_cert_path, ec);
		if (ec) {
			log::Error("Failed to load the server certificate!");
		}
	}
}

Client::~Client() {
	if (!*cancelled_) {
		logger_.Warning("Client destroyed while request is still active!");
	}
	DoCancel();
}

error::Error Client::AsyncCall(
	OutgoingRequestPtr req, ResponseHandler header_handler, ResponseHandler body_handler) {
	if (!*cancelled_ && status_ != TransactionStatus::Done) {
		return error::Error(
			make_error_condition(errc::operation_in_progress), "HTTP call already ongoing");
	}

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

	logger_ = log::Logger(logger_name_).WithFields(log::LogField("url", req->orig_address_));

	// NOTE: The AWS loadbalancer requires that the HOST header always be set, in order for the
	// request to route to our k8s cluster. Set this in all cases.
	req->SetHeader("HOST", req->address_.host);

	request_ = req;
	header_handler_ = header_handler;
	body_handler_ = body_handler;
	status_ = TransactionStatus::None;

	cancelled_ = make_shared<bool>(false);

	auto &cancelled = cancelled_;

	resolver_.async_resolve(
		request_->address_.host,
		to_string(request_->address_.port),
		[this, cancelled](
			const error_code &ec, const asio::ip::tcp::resolver::results_type &results) {
			if (!*cancelled) {
				ResolveHandler(ec, results);
			}
		});

	return error::NoError;
}

io::ExpectedAsyncReaderPtr Client::MakeBodyAsyncReader(IncomingResponsePtr resp) {
	if (status_ != TransactionStatus::HeaderHandlerCalled) {
		return expected::unexpected(error::Error(
			make_error_condition(errc::operation_in_progress),
			"MakeBodyAsyncReader called while reading is in progress"));
	}

	if (response_body_length_ == 0) {
		return expected::unexpected(
			MakeError(BodyMissingError, "Response does not contain a body"));
	}

	status_ = TransactionStatus::ReaderCreated;
	return make_shared<BodyAsyncReader<Client>>(resp->client_.GetHttpClient(), resp->cancelled_);
}

io::ExpectedAsyncReadWriterPtr Client::SwitchProtocol(IncomingResponsePtr req) {
	if (*cancelled_) {
		return expected::unexpected(error::Error(
			make_error_condition(errc::not_connected),
			"Cannot switch protocols if endpoint is not connected"));
	}

	// Rest of the connection is done directly on the socket, we are done here.
	status_ = TransactionStatus::Done;
	*cancelled_ = true;
	cancelled_ = make_shared<bool>(false);

	auto stream = stream_;
	// This no longer belongs to us.
	stream_.reset();

	if (is_https_) {
		return make_shared<RawSocket<ssl::stream<tcp::socket>>>(
			stream, response_data_.response_buffer_);
	} else {
		return make_shared<RawSocket<tcp::socket>>(
			make_shared<tcp::socket>(std::move(stream->next_layer())),
			response_data_.response_buffer_);
	}
}

void Client::CallHandler(ResponseHandler handler) {
	// This function exists to make sure we have a copy of the handler we're calling (in the
	// argument list). This is important in case the handler owns the client instance through a
	// capture, and it replaces the handler with a different one (using `AsyncCall`). If it
	// does, then it destroys the final copy of the handler, and therefore also the client,
	// which is why we need to make a copy here, before calling it.
	handler(response_);
}

void Client::CallErrorHandler(
	const error_code &ec, const OutgoingRequestPtr &req, ResponseHandler handler) {
	CallErrorHandler(error::Error(ec.default_error_condition(), ""), req, handler);
}

void Client::CallErrorHandler(
	const error::Error &err, const OutgoingRequestPtr &req, ResponseHandler handler) {
	*cancelled_ = true;
	cancelled_ = make_shared<bool>(true);
	stream_.reset();
	status_ = TransactionStatus::Done;
	handler(expected::unexpected(
		err.WithContext(MethodToString(req->method_) + " " + req->orig_address_)));
}

void Client::ResolveHandler(
	const error_code &ec, const asio::ip::tcp::resolver::results_type &results) {
	if (ec) {
		CallErrorHandler(ec, request_, header_handler_);
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

	stream_ = make_shared<ssl::stream<tcp::socket>>(GetAsioIoContext(event_loop_), ssl_ctx_);

	if (!response_data_.response_buffer_) {
		// We can reuse this if preexisting.
		response_data_.response_buffer_ = make_shared<beast::flat_buffer>();

		// This is equivalent to:
		//   response_data_.response_buffer_.reserve(body_buffer_.size());
		// but compatible with Boost 1.67.
		response_data_.response_buffer_->prepare(
			body_buffer_.size() - response_data_.response_buffer_->size());
	}

	response_data_.http_response_parser_ = make_shared<http::response_parser<http::buffer_body>>();

	// Don't enforce limits. Since we stream everything, limits don't generally apply, and
	// if they do, they should be handled higher up in the application logic.
	//
	// Note: There is a bug in Beast here (tested on 1.74): One is supposed to be able to
	// pass an uninitialized `optional` to mean unlimited, but they do not check for
	// `has_value()` in their code, causing their subsequent comparison operation to
	// misbehave. So pass highest possible value instead.
	response_data_.http_response_parser_->body_limit(numeric_limits<uint64_t>::max());

	auto &cancelled = cancelled_;

	asio::async_connect(
		stream_->next_layer(),
		resolver_results_,
		[this, cancelled](const error_code &ec, const asio::ip::tcp::endpoint &endpoint) {
			if (!*cancelled) {
				if (is_https_) {
					return HandshakeHandler(ec, endpoint);
				}
				return ConnectHandler(ec, endpoint);
			}
		});
}

void Client::HandshakeHandler(const error_code &ec, const asio::ip::tcp::endpoint &endpoint) {
	if (ec) {
		CallErrorHandler(ec, request_, header_handler_);
		return;
	}

	if (not disable_keep_alive_) {
		boost::asio::socket_base::keep_alive option(true);
		stream_->next_layer().set_option(option);
	}

	// Set SNI Hostname (many hosts need this to handshake successfully)
	if (!SSL_set_tlsext_host_name(stream_->native_handle(), request_->address_.host.c_str())) {
		beast::error_code ec2 {
			static_cast<int>(::ERR_get_error()), asio::error::get_ssl_category()};
		logger_.Error("Failed to set SNI host name: " + ec2.message());
	}

	auto &cancelled = cancelled_;

	stream_->async_handshake(
		ssl::stream_base::client, [this, cancelled, endpoint](const error_code &ec) {
			if (*cancelled) {
				return;
			}
			if (ec) {
				logger_.Error("https: Failed to perform the SSL handshake: " + ec.message());
				CallErrorHandler(ec, request_, header_handler_);
				return;
			}
			logger_.Debug("https: Successful SSL handshake");
			ConnectHandler(ec, endpoint);
		});
}


void Client::ConnectHandler(const error_code &ec, const asio::ip::tcp::endpoint &endpoint) {
	if (ec) {
		CallErrorHandler(ec, request_, header_handler_);
		return;
	}

	if (not disable_keep_alive_) {
		boost::asio::socket_base::keep_alive option(true);
		stream_->next_layer().set_option(option);
	}

	logger_.Debug("Connected to " + endpoint.address().to_string());

	request_data_.http_request_ = make_shared<http::request<http::buffer_body>>(
		MethodToBeastVerb(request_->method_), request_->address_.path, BeastHttpVersion);

	for (const auto &header : request_->headers_) {
		request_data_.http_request_->set(header.first, header.second);
	}

	request_data_.http_request_serializer_ =
		make_shared<http::request_serializer<http::buffer_body>>(*request_data_.http_request_);

	auto &cancelled = cancelled_;
	auto &request_data = request_data_;

	if (is_https_) {
		http::async_write_header(
			*stream_,
			*request_data_.http_request_serializer_,
			[this, cancelled, request_data](const error_code &ec, size_t num_written) {
				if (!*cancelled) {
					WriteHeaderHandler(ec, num_written);
				}
			});
	} else {
		http::async_write_header(
			stream_->next_layer(),
			*request_data_.http_request_serializer_,
			[this, cancelled, request_data](const error_code &ec, size_t num_written) {
				if (!*cancelled) {
					WriteHeaderHandler(ec, num_written);
				}
			});
	}
}

void Client::WriteHeaderHandler(const error_code &ec, size_t num_written) {
	if (num_written > 0) {
		logger_.Trace("Wrote " + to_string(num_written) + " bytes of header data to stream.");
	}

	if (ec) {
		CallErrorHandler(ec, request_, header_handler_);
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

	if (!request_->body_gen_ && !request_->async_body_gen_) {
		auto err = MakeError(BodyMissingError, "Content-Length is non-zero, but body is missing");
		CallErrorHandler(err, request_, header_handler_);
		return;
	}

	assert(!(request_->body_gen_ && request_->async_body_gen_));

	if (request_->body_gen_) {
		auto body_reader = request_->body_gen_();
		if (!body_reader) {
			CallErrorHandler(body_reader.error(), request_, header_handler_);
			return;
		}
		request_->body_reader_ = body_reader.value();
	} else {
		auto body_reader = request_->async_body_gen_();
		if (!body_reader) {
			CallErrorHandler(body_reader.error(), request_, header_handler_);
			return;
		}
		request_->async_body_reader_ = body_reader.value();
	}

	PrepareAndWriteNewBodyBuffer();
}

void Client::WriteBodyHandler(const error_code &ec, size_t num_written) {
	if (num_written > 0) {
		logger_.Trace("Wrote " + to_string(num_written) + " bytes of body data to stream.");
	}

	if (ec == http::make_error_code(http::error::need_buffer)) {
		// Write next block of the body.
		PrepareAndWriteNewBodyBuffer();
	} else if (ec) {
		CallErrorHandler(ec, request_, header_handler_);
	} else if (num_written > 0) {
		// We are still writing the body.
		WriteBody();
	} else {
		// We are ready to receive the response.
		ReadHeader();
	}
}

void Client::PrepareAndWriteNewBodyBuffer() {
	// request_->body_reader_ XOR request_->async_body_reader_
	assert(
		(request_->body_reader_ || request_->async_body_reader_)
		&& !(request_->body_reader_ && request_->async_body_reader_));

	auto cancelled = cancelled_;
	auto read_handler = [this, cancelled](io::ExpectedSize read) {
		if (!*cancelled) {
			if (!read) {
				CallErrorHandler(read.error(), request_, header_handler_);
				return;
			}
			WriteNewBodyBuffer(read.value());
		}
	};


	if (request_->body_reader_) {
		read_handler(request_->body_reader_->Read(body_buffer_.begin(), body_buffer_.end()));
	} else {
		auto err = request_->async_body_reader_->AsyncRead(
			body_buffer_.begin(), body_buffer_.end(), read_handler);
		if (err != error::NoError) {
			CallErrorHandler(err, request_, header_handler_);
		}
	}
}

void Client::WriteNewBodyBuffer(size_t size) {
	request_data_.http_request_->body().data = body_buffer_.data();
	request_data_.http_request_->body().size = size;

	if (size > 0) {
		request_data_.http_request_->body().more = true;
	} else {
		// Release ownership of Body reader.
		request_->body_reader_.reset();
		request_->async_body_reader_.reset();
		request_data_.http_request_->body().more = false;
	}

	WriteBody();
}

void Client::WriteBody() {
	auto &cancelled = cancelled_;
	auto &request_data = request_data_;

	if (is_https_) {
		http::async_write_some(
			*stream_,
			*request_data_.http_request_serializer_,
			[this, cancelled, request_data](const error_code &ec, size_t num_written) {
				if (!*cancelled) {
					WriteBodyHandler(ec, num_written);
				}
			});
	} else {
		http::async_write_some(
			stream_->next_layer(),
			*request_data_.http_request_serializer_,
			[this, cancelled, request_data](const error_code &ec, size_t num_written) {
				if (!*cancelled) {
					WriteBodyHandler(ec, num_written);
				}
			});
	}
}

void Client::ReadHeader() {
	auto &cancelled = cancelled_;
	auto &response_data = response_data_;

	if (is_https_) {
		http::async_read_some(
			*stream_,
			*response_data_.response_buffer_,
			*response_data_.http_response_parser_,
			[this, cancelled, response_data](const error_code &ec, size_t num_read) {
				if (!*cancelled) {
					ReadHeaderHandler(ec, num_read);
				}
			});
	} else {
		http::async_read_some(
			stream_->next_layer(),
			*response_data_.response_buffer_,
			*response_data_.http_response_parser_,
			[this, cancelled, response_data](const error_code &ec, size_t num_read) {
				if (!*cancelled) {
					ReadHeaderHandler(ec, num_read);
				}
			});
	}
}

void Client::ReadHeaderHandler(const error_code &ec, size_t num_read) {
	if (num_read > 0) {
		logger_.Trace("Read " + to_string(num_read) + " bytes of header data from stream.");
	}

	if (ec) {
		CallErrorHandler(ec, request_, header_handler_);
		return;
	}

	if (!response_data_.http_response_parser_->is_header_done()) {
		ReadHeader();
		return;
	}

	response_.reset(new IncomingResponse(*this, cancelled_));
	response_->status_code_ = response_data_.http_response_parser_->get().result_int();
	response_->status_message_ = string {response_data_.http_response_parser_->get().reason()};

	logger_.Debug(
		"Received response: " + to_string(response_->status_code_) + " "
		+ response_->status_message_);

	string debug_str;
	for (auto header = response_data_.http_response_parser_->get().cbegin();
		 header != response_data_.http_response_parser_->get().cend();
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

	if (response_data_.http_response_parser_->chunked()) {
		auto cancelled = cancelled_;
		status_ = TransactionStatus::HeaderHandlerCalled;
		CallHandler(header_handler_);
		if (!*cancelled) {
			auto err = MakeError(UnsupportedBodyType, "`Transfer-Encoding: chunked` not supported");
			CallErrorHandler(err, request_, body_handler_);
		}
		return;
	}

	auto content_length = response_data_.http_response_parser_->content_length();
	if (content_length) {
		response_body_length_ = content_length.value();
	} else {
		response_body_length_ = 0;
	}
	response_body_read_ = 0;

	if (response_body_read_ >= response_body_length_) {
		auto cancelled = cancelled_;
		status_ = TransactionStatus::HeaderHandlerCalled;
		CallHandler(header_handler_);
		if (!*cancelled) {
			status_ = TransactionStatus::Done;
			CallHandler(body_handler_);

			// After body handler has run, set the request to cancelled. The body
			// handler may have made a new request, so this is not necessarily the same
			// request as is currently active (note use of shared_ptr copy, not
			// `cancelled_`).
			*cancelled = true;
		}
		return;
	}

	auto cancelled = cancelled_;
	status_ = TransactionStatus::HeaderHandlerCalled;
	CallHandler(header_handler_);
	if (*cancelled) {
		return;
	}

	// We know that a body reader is required here, because of the `response_body_read_ >=
	// response_body_length_` check above.
	if (status_ == TransactionStatus::HeaderHandlerCalled) {
		CallErrorHandler(MakeError(BodyIgnoredError, ""), request_, body_handler_);
	}
}

void Client::AsyncReadNextBodyPart(
	vector<uint8_t>::iterator start, vector<uint8_t>::iterator end, io::AsyncIoHandler handler) {
	assert(AtLeast(status_, TransactionStatus::ReaderCreated));

	if (status_ == TransactionStatus::ReaderCreated) {
		status_ = TransactionStatus::BodyReadingInProgress;
	}

	if (AtLeast(status_, TransactionStatus::BodyReadingFinished)) {
		auto cancelled = cancelled_;
		handler(0);
		if (!*cancelled && status_ == TransactionStatus::BodyReadingFinished) {
			status_ = TransactionStatus::Done;
			CallHandler(body_handler_);

			// After body handler has run, set the request to cancelled. The body
			// handler may have made a new request, so this is not necessarily the same
			// request as is currently active (note use of shared_ptr copy, not
			// `cancelled_`).
			*cancelled = true;
		}
		return;
	}

	reader_buf_start_ = start;
	reader_buf_end_ = end;
	reader_handler_ = handler;
	size_t read_size = end - start;
	size_t smallest = min(body_buffer_.size(), read_size);

	response_data_.http_response_parser_->get().body().data = body_buffer_.data();
	response_data_.http_response_parser_->get().body().size = smallest;

	auto &cancelled = cancelled_;
	auto &response_data = response_data_;

	if (is_https_) {
		http::async_read_some(
			*stream_,
			*response_data_.response_buffer_,
			*response_data_.http_response_parser_,
			[this, cancelled, response_data](const error_code &ec, size_t num_read) {
				if (!*cancelled) {
					ReadBodyHandler(ec, num_read);
				}
			});
	} else {
		http::async_read_some(
			stream_->next_layer(),
			*response_data_.response_buffer_,
			*response_data_.http_response_parser_,
			[this, cancelled, response_data](const error_code &ec, size_t num_read) {
				if (!*cancelled) {
					ReadBodyHandler(ec, num_read);
				}
			});
	}
}

void Client::ReadBodyHandler(error_code ec, size_t num_read) {
	if (num_read > 0) {
		logger_.Trace("Read " + to_string(num_read) + " bytes of body data from stream.");
		response_body_read_ += num_read;
	}

	if (ec == http::make_error_code(http::error::need_buffer)) {
		// This can be ignored. We always reset the buffer between reads anyway.
		ec = error_code();
	}

	assert(reader_handler_);

	if (response_body_read_ >= response_body_length_) {
		status_ = TransactionStatus::BodyReadingFinished;
	}

	auto cancelled = cancelled_;

	size_t buf_size = reader_buf_end_ - reader_buf_start_;
	size_t smallest = min(num_read, buf_size);
	copy_n(body_buffer_.begin(), smallest, reader_buf_start_);
	if (ec) {
		auto err = error::Error(ec.default_error_condition(), "Could not read body");
		reader_handler_(expected::unexpected(err));
	} else {
		reader_handler_(smallest);
	}

	if (!*cancelled && ec) {
		CallErrorHandler(ec, request_, body_handler_);
		return;
	}
}

void Client::Cancel() {
	auto cancelled = cancelled_;

	if (!*cancelled) {
		auto err =
			error::Error(make_error_condition(errc::operation_canceled), "HTTP request cancelled");
		switch (status_) {
		case TransactionStatus::None:
			CallErrorHandler(err, request_, header_handler_);
			break;
		case TransactionStatus::HeaderHandlerCalled:
		case TransactionStatus::ReaderCreated:
		case TransactionStatus::BodyReadingInProgress:
		case TransactionStatus::BodyReadingFinished:
			CallErrorHandler(err, request_, body_handler_);
			break;
		case TransactionStatus::Replying:
		case TransactionStatus::SwitchingProtocol:
			// Not used by client.
			assert(false);
			break;
		case TransactionStatus::BodyHandlerCalled:
		case TransactionStatus::Done:
			break;
		}
	}

	if (!*cancelled) {
		DoCancel();
	}
}

void Client::DoCancel() {
	resolver_.cancel();
	if (stream_) {
		stream_->next_layer().cancel();
		stream_->next_layer().close();
		stream_.reset();
	}

	request_.reset();
	response_.reset();

	// Reset logger to no connection.
	logger_ = log::Logger(logger_name_);

	// Set cancel state and then make a new one. Those who are interested should have their own
	// pointer to the old one.
	*cancelled_ = true;
	cancelled_ = make_shared<bool>(true);
}

Stream::Stream(Server &server) :
	server_ {server},
	logger_ {"http"},
	cancelled_(make_shared<bool>(true)),
	socket_(server_.GetAsioIoContext(server_.event_loop_)),
	body_buffer_(HTTP_BEAST_BUFFER_SIZE) {
	request_data_.request_buffer_ = make_shared<beast::flat_buffer>();

	// This is equivalent to:
	//   request_data_.request_buffer_.reserve(body_buffer_.size());
	// but compatible with Boost 1.67.
	request_data_.request_buffer_->prepare(
		body_buffer_.size() - request_data_.request_buffer_->size());

	request_data_.http_request_parser_ = make_shared<http::request_parser<http::buffer_body>>();

	// Don't enforce limits. Since we stream everything, limits don't generally apply, and if
	// they do, they should be handled higher up in the application logic.
	//
	// Note: There is a bug in Beast here (tested on 1.74): One is supposed to be able to pass
	// an uninitialized `optional` to mean unlimited, but they do not check for `has_value()` in
	// their code, causing their subsequent comparison operation to misbehave. So pass highest
	// possible value instead.
	request_data_.http_request_parser_->body_limit(numeric_limits<uint64_t>::max());
}

Stream::~Stream() {
	DoCancel();
}

void Stream::Cancel() {
	auto cancelled = cancelled_;

	if (!*cancelled) {
		auto err =
			error::Error(make_error_condition(errc::operation_canceled), "HTTP response cancelled");
		switch (status_) {
		case TransactionStatus::None:
			CallErrorHandler(err, request_, server_.header_handler_);
			break;
		case TransactionStatus::HeaderHandlerCalled:
		case TransactionStatus::ReaderCreated:
		case TransactionStatus::BodyReadingInProgress:
		case TransactionStatus::BodyReadingFinished:
			CallErrorHandler(err, request_, server_.body_handler_);
			break;
		case TransactionStatus::BodyHandlerCalled:
			// In between body handler and reply finished. No one to handle the status
			// here.
			server_.RemoveStream(shared_from_this());
			break;
		case TransactionStatus::Replying:
			CallErrorHandler(err, request_, reply_finished_handler_);
			break;
		case TransactionStatus::SwitchingProtocol:
			CallErrorHandler(err, request_, switch_protocol_handler_);
			break;
		case TransactionStatus::Done:
			break;
		}
	}

	if (!*cancelled) {
		DoCancel();
	}
}

void Stream::DoCancel() {
	if (socket_.is_open()) {
		socket_.cancel();
		socket_.close();
	}

	// Set cancel state and then make a new one. Those who are interested should have their own
	// pointer to the old one.
	*cancelled_ = true;
	cancelled_ = make_shared<bool>(true);
}

void Stream::CallErrorHandler(const error_code &ec, const RequestPtr &req, RequestHandler handler) {
	CallErrorHandler(error::Error(ec.default_error_condition(), ""), req, handler);
}

void Stream::CallErrorHandler(
	const error::Error &err, const RequestPtr &req, RequestHandler handler) {
	*cancelled_ = true;
	cancelled_ = make_shared<bool>(true);
	status_ = TransactionStatus::Done;
	handler(expected::unexpected(err.WithContext(
		req->address_.host + ": " + MethodToString(req->method_) + " " + request_->GetPath())));

	server_.RemoveStream(shared_from_this());
}

void Stream::CallErrorHandler(
	const error_code &ec, const IncomingRequestPtr &req, IdentifiedRequestHandler handler) {
	CallErrorHandler(error::Error(ec.default_error_condition(), ""), req, handler);
}

void Stream::CallErrorHandler(
	const error::Error &err, const IncomingRequestPtr &req, IdentifiedRequestHandler handler) {
	*cancelled_ = true;
	cancelled_ = make_shared<bool>(true);
	status_ = TransactionStatus::Done;
	handler(
		req,
		err.WithContext(
			req->address_.host + ": " + MethodToString(req->method_) + " " + request_->GetPath()));

	server_.RemoveStream(shared_from_this());
}

void Stream::CallErrorHandler(
	const error_code &ec, const RequestPtr &req, ReplyFinishedHandler handler) {
	CallErrorHandler(error::Error(ec.default_error_condition(), ""), req, handler);
}

void Stream::CallErrorHandler(
	const error::Error &err, const RequestPtr &req, ReplyFinishedHandler handler) {
	*cancelled_ = true;
	cancelled_ = make_shared<bool>(true);
	status_ = TransactionStatus::Done;
	handler(err.WithContext(
		req->address_.host + ": " + MethodToString(req->method_) + " " + request_->GetPath()));

	server_.RemoveStream(shared_from_this());
}

void Stream::CallErrorHandler(
	const error_code &ec, const RequestPtr &req, SwitchProtocolHandler handler) {
	CallErrorHandler(error::Error(ec.default_error_condition(), ""), req, handler);
}

void Stream::CallErrorHandler(
	const error::Error &err, const RequestPtr &req, SwitchProtocolHandler handler) {
	*cancelled_ = true;
	cancelled_ = make_shared<bool>(true);
	status_ = TransactionStatus::Done;
	handler(expected::unexpected(err.WithContext(
		req->address_.host + ": " + MethodToString(req->method_) + " " + request_->GetPath())));

	server_.RemoveStream(shared_from_this());
}

void Stream::AcceptHandler(const error_code &ec) {
	if (ec) {
		log::Error("Error while accepting HTTP connection: " + ec.message());
		return;
	}

	auto ip = socket_.remote_endpoint().address().to_string();

	// Use IP as context for logging.
	logger_ = log::Logger("http_server").WithFields(log::LogField("ip", ip));

	logger_.Debug("Accepted connection.");

	request_.reset(new IncomingRequest(*this, cancelled_));

	request_->address_.host = ip;

	*cancelled_ = false;

	ReadHeader();
}

void Stream::ReadHeader() {
	auto &cancelled = cancelled_;
	auto &request_data = request_data_;

	http::async_read_some(
		socket_,
		*request_data_.request_buffer_,
		*request_data_.http_request_parser_,
		[this, cancelled, request_data](const error_code &ec, size_t num_read) {
			if (!*cancelled) {
				ReadHeaderHandler(ec, num_read);
			}
		});
}

void Stream::ReadHeaderHandler(const error_code &ec, size_t num_read) {
	if (num_read > 0) {
		logger_.Trace("Read " + to_string(num_read) + " bytes of header data from stream.");
	}

	if (ec) {
		CallErrorHandler(ec, request_, server_.header_handler_);
		return;
	}

	if (!request_data_.http_request_parser_->is_header_done()) {
		ReadHeader();
		return;
	}

	auto method_result = BeastVerbToMethod(
		request_data_.http_request_parser_->get().base().method(),
		string {request_data_.http_request_parser_->get().base().method_string()});
	if (!method_result) {
		CallErrorHandler(method_result.error(), request_, server_.header_handler_);
		return;
	}
	request_->method_ = method_result.value();
	request_->address_.path = string(request_data_.http_request_parser_->get().base().target());

	logger_ = logger_.WithFields(log::LogField("path", request_->address_.path));

	string debug_str;
	for (auto header = request_data_.http_request_parser_->get().cbegin();
		 header != request_data_.http_request_parser_->get().cend();
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

	if (request_data_.http_request_parser_->chunked()) {
		auto cancelled = cancelled_;
		status_ = TransactionStatus::HeaderHandlerCalled;
		server_.header_handler_(request_);
		if (!*cancelled) {
			auto err = MakeError(UnsupportedBodyType, "`Transfer-Encoding: chunked` not supported");
			CallErrorHandler(err, request_, server_.body_handler_);
		}
		return;
	}

	auto content_length = request_data_.http_request_parser_->content_length();
	if (content_length) {
		request_body_length_ = content_length.value();
	} else {
		request_body_length_ = 0;
	}
	request_body_read_ = 0;

	if (request_body_read_ >= request_body_length_) {
		auto cancelled = cancelled_;
		status_ = TransactionStatus::HeaderHandlerCalled;
		server_.header_handler_(request_);
		if (!*cancelled) {
			status_ = TransactionStatus::BodyHandlerCalled;
			CallBodyHandler();
		}
		return;
	}

	auto cancelled = cancelled_;
	status_ = TransactionStatus::HeaderHandlerCalled;
	server_.header_handler_(request_);
	if (*cancelled) {
		return;
	}

	// We know that a body reader is required here, because of the `request_body_read_ >=
	// request_body_length_` check above.
	if (status_ == TransactionStatus::HeaderHandlerCalled) {
		CallErrorHandler(MakeError(BodyIgnoredError, ""), request_, server_.body_handler_);
	}
}

void Stream::AsyncReadNextBodyPart(
	vector<uint8_t>::iterator start, vector<uint8_t>::iterator end, io::AsyncIoHandler handler) {
	assert(AtLeast(status_, TransactionStatus::ReaderCreated));

	if (status_ == TransactionStatus::ReaderCreated) {
		status_ = TransactionStatus::BodyReadingInProgress;
	}

	if (status_ != TransactionStatus::BodyReadingInProgress) {
		auto cancelled = cancelled_;
		handler(0);
		if (!*cancelled && status_ == TransactionStatus::BodyReadingFinished) {
			status_ = TransactionStatus::BodyHandlerCalled;
			CallBodyHandler();
		}
		return;
	}

	reader_buf_start_ = start;
	reader_buf_end_ = end;
	reader_handler_ = handler;
	size_t read_size = end - start;
	size_t smallest = min(body_buffer_.size(), read_size);

	request_data_.http_request_parser_->get().body().data = body_buffer_.data();
	request_data_.http_request_parser_->get().body().size = smallest;

	auto &cancelled = cancelled_;
	auto &request_data = request_data_;

	http::async_read_some(
		socket_,
		*request_data_.request_buffer_,
		*request_data_.http_request_parser_,
		[this, cancelled, request_data](const error_code &ec, size_t num_read) {
			if (!*cancelled) {
				ReadBodyHandler(ec, num_read);
			}
		});
}

void Stream::ReadBodyHandler(error_code ec, size_t num_read) {
	if (num_read > 0) {
		logger_.Trace("Read " + to_string(num_read) + " bytes of body data from stream.");
		request_body_read_ += num_read;
	}

	if (ec == http::make_error_code(http::error::need_buffer)) {
		// This can be ignored. We always reset the buffer between reads anyway.
		ec = error_code();
	}

	assert(reader_handler_);

	if (request_body_read_ >= request_body_length_) {
		status_ = TransactionStatus::BodyReadingFinished;
	}

	auto cancelled = cancelled_;

	size_t buf_size = reader_buf_end_ - reader_buf_start_;
	size_t smallest = min(num_read, buf_size);
	copy_n(body_buffer_.begin(), smallest, reader_buf_start_);
	if (ec) {
		auto err = error::Error(ec.default_error_condition(), "Could not read body");
		reader_handler_(expected::unexpected(err));
	} else {
		reader_handler_(smallest);
	}

	if (!*cancelled && ec) {
		CallErrorHandler(ec, request_, server_.body_handler_);
		return;
	}
}

void Stream::AsyncReply(ReplyFinishedHandler reply_finished_handler) {
	SetupResponse();

	reply_finished_handler_ = reply_finished_handler;

	auto &cancelled = cancelled_;
	auto &response_data = response_data_;

	http::async_write_header(
		socket_,
		*response_data_.http_response_serializer_,
		[this, cancelled, response_data](const error_code &ec, size_t num_written) {
			if (!*cancelled) {
				WriteHeaderHandler(ec, num_written);
			}
		});
}

void Stream::SetupResponse() {
	auto response = maybe_response_.lock();
	// Only called from existing responses, so this should always be true.
	assert(response);

	assert(status_ == TransactionStatus::BodyHandlerCalled);
	status_ = TransactionStatus::Replying;

	// From here on we take shared ownership.
	response_ = response;

	response_data_.http_response_ = make_shared<http::response<http::buffer_body>>();

	for (const auto &header : response->headers_) {
		response_data_.http_response_->base().set(header.first, header.second);
	}

	response_data_.http_response_->result(response->GetStatusCode());
	response_data_.http_response_->reason(response->GetStatusMessage());

	response_data_.http_response_serializer_ =
		make_shared<http::response_serializer<http::buffer_body>>(*response_data_.http_response_);
}

void Stream::WriteHeaderHandler(const error_code &ec, size_t num_written) {
	if (num_written > 0) {
		logger_.Trace("Wrote " + to_string(num_written) + " bytes of header data to stream.");
	}

	if (ec) {
		CallErrorHandler(ec, request_, reply_finished_handler_);
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

	if (!response_->body_reader_ && !response_->async_body_reader_) {
		auto err = MakeError(BodyMissingError, "Content-Length is non-zero, but body is missing");
		CallErrorHandler(err, request_, reply_finished_handler_);
		return;
	}

	PrepareAndWriteNewBodyBuffer();
}

void Stream::PrepareAndWriteNewBodyBuffer() {
	// response_->body_reader_ XOR response_->async_body_reader_
	assert(
		(response_->body_reader_ || response_->async_body_reader_)
		&& !(response_->body_reader_ && response_->async_body_reader_));

	auto read_handler = [this](io::ExpectedSize read) {
		if (!read) {
			CallErrorHandler(read.error(), request_, reply_finished_handler_);
			return;
		}
		WriteNewBodyBuffer(read.value());
	};

	if (response_->body_reader_) {
		read_handler(response_->body_reader_->Read(body_buffer_.begin(), body_buffer_.end()));
	} else {
		auto err = response_->async_body_reader_->AsyncRead(
			body_buffer_.begin(), body_buffer_.end(), read_handler);
		if (err != error::NoError) {
			CallErrorHandler(err, request_, reply_finished_handler_);
		}
	}
}

void Stream::WriteNewBodyBuffer(size_t size) {
	response_data_.http_response_->body().data = body_buffer_.data();
	response_data_.http_response_->body().size = size;

	if (size > 0) {
		response_data_.http_response_->body().more = true;
	} else {
		response_data_.http_response_->body().more = false;
	}

	WriteBody();
}

void Stream::WriteBody() {
	auto &cancelled = cancelled_;
	auto &response_data = response_data_;

	http::async_write_some(
		socket_,
		*response_data_.http_response_serializer_,
		[this, cancelled, response_data](const error_code &ec, size_t num_written) {
			if (!*cancelled) {
				WriteBodyHandler(ec, num_written);
			}
		});
}

void Stream::WriteBodyHandler(const error_code &ec, size_t num_written) {
	if (num_written > 0) {
		logger_.Trace("Wrote " + to_string(num_written) + " bytes of body data to stream.");
	}

	if (ec == http::make_error_code(http::error::need_buffer)) {
		// Write next body block.
		PrepareAndWriteNewBodyBuffer();
	} else if (ec) {
		CallErrorHandler(ec, request_, reply_finished_handler_);
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
	*cancelled_ = true;
	cancelled_ = make_shared<bool>(true);
	status_ = TransactionStatus::Done;
	// Release ownership of Body reader.
	response_->body_reader_.reset();
	response_->async_body_reader_.reset();
	reply_finished_handler_(error::NoError);
	server_.RemoveStream(shared_from_this());
}

error::Error Stream::AsyncSwitchProtocol(SwitchProtocolHandler handler) {
	SetupResponse();

	switch_protocol_handler_ = handler;
	status_ = TransactionStatus::SwitchingProtocol;

	auto &cancelled = cancelled_;
	auto &response_data = response_data_;

	http::async_write_header(
		socket_,
		*response_data_.http_response_serializer_,
		[this, cancelled, response_data](const error_code &ec, size_t num_written) {
			if (!*cancelled) {
				SwitchingProtocolHandler(ec, num_written);
			}
		});

	return error::NoError;
}

void Stream::SwitchingProtocolHandler(error_code ec, size_t num_written) {
	if (num_written > 0) {
		logger_.Trace("Wrote " + to_string(num_written) + " bytes of header data to stream.");
	}

	if (ec) {
		CallErrorHandler(ec, request_, switch_protocol_handler_);
		return;
	}

	auto socket = make_shared<RawSocket<tcp::socket>>(
		make_shared<tcp::socket>(std::move(socket_)), request_data_.request_buffer_);

	auto switch_protocol_handler = switch_protocol_handler_;

	// Rest of the connection is done directly on the socket, we are done here.
	status_ = TransactionStatus::Done;
	*cancelled_ = true;
	cancelled_ = make_shared<bool>(true);
	server_.RemoveStream(shared_from_this());

	switch_protocol_handler(socket);
}

void Stream::CallBodyHandler() {
	// Get a pointer to ourselves. This is just in case the body handler make a response, which
	// it immediately destroys, which would destroy this stream as well. At the end of this
	// function, it's ok to destroy it.
	auto stream_ref = shared_from_this();

	server_.body_handler_(request_, error::NoError);

	// MakeResponse() should have been called inside body handler. It can use this to generate a
	// response, either immediately, or later. Therefore it should still exist, otherwise the
	// request has not been handled correctly.
	auto response = maybe_response_.lock();
	if (!response) {
		logger_.Error("Handler produced no response. Closing stream prematurely.");
		*cancelled_ = true;
		cancelled_ = make_shared<bool>(true);
		server_.RemoveStream(shared_from_this());
	}
}

Server::Server(const ServerConfig &server, events::EventLoop &event_loop) :
	event_loop_ {event_loop},
	acceptor_(GetAsioIoContext(event_loop_)) {
}

Server::~Server() {
	Cancel();
}

error::Error Server::AsyncServeUrl(
	const string &url, RequestHandler header_handler, RequestHandler body_handler) {
	return AsyncServeUrl(
		url, header_handler, [body_handler](IncomingRequestPtr req, error::Error err) {
			if (err != error::NoError) {
				body_handler(expected::unexpected(err));
			} else {
				body_handler(req);
			}
		});
}

error::Error Server::AsyncServeUrl(
	const string &url, RequestHandler header_handler, IdentifiedRequestHandler body_handler) {
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

uint16_t Server::GetPort() const {
	return acceptor_.local_endpoint().port();
}

string Server::GetUrl() const {
	return "http://127.0.0.1:" + to_string(GetPort());
}

ExpectedOutgoingResponsePtr Server::MakeResponse(IncomingRequestPtr req) {
	if (*req->cancelled_) {
		return expected::unexpected(MakeError(StreamCancelledError, "Cannot make response"));
	}
	OutgoingResponsePtr response {new OutgoingResponse(req->stream_, req->cancelled_)};
	req->stream_.maybe_response_ = response;
	return response;
}

error::Error Server::AsyncReply(
	OutgoingResponsePtr resp, ReplyFinishedHandler reply_finished_handler) {
	if (*resp->cancelled_) {
		return MakeError(StreamCancelledError, "Cannot send response");
	}

	resp->stream_.AsyncReply(reply_finished_handler);
	return error::NoError;
}

io::ExpectedAsyncReaderPtr Server::MakeBodyAsyncReader(IncomingRequestPtr req) {
	if (*req->cancelled_) {
		return expected::unexpected(MakeError(StreamCancelledError, "Cannot make body reader"));
	}

	auto &stream = req->stream_;
	if (stream.status_ != TransactionStatus::HeaderHandlerCalled) {
		return expected::unexpected(error::Error(
			make_error_condition(errc::operation_in_progress),
			"MakeBodyAsyncReader called while reading is in progress"));
	}

	if (stream.request_body_length_ == 0) {
		return expected::unexpected(MakeError(BodyMissingError, "Request does not contain a body"));
	}

	stream.status_ = TransactionStatus::ReaderCreated;
	return make_shared<BodyAsyncReader<Stream>>(stream, req->cancelled_);
}

error::Error Server::AsyncSwitchProtocol(OutgoingResponsePtr resp, SwitchProtocolHandler handler) {
	return resp->stream_.AsyncSwitchProtocol(handler);
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

void Server::RemoveStream(StreamPtr stream) {
	streams_.erase(stream);

	stream->DoCancel();
}

} // namespace http
} // namespace mender
