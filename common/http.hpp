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

#ifndef MENDER_COMMON_HTTP_HPP
#define MENDER_COMMON_HTTP_HPP

#include <config.h>

#include <common/error.hpp>
#include <common/events.hpp>
#include <common/expected.hpp>
#include <common/io.hpp>
#include <common/log.hpp>

#ifdef MENDER_USE_BOOST_BEAST
#include <boost/asio.hpp>
#include <boost/beast.hpp>
#endif // MENDER_USE_BOOST_BEAST

#include <functional>
#include <unordered_map>
#include <unordered_set>

namespace mender {
namespace http {

using namespace std;

#ifdef MENDER_USE_BOOST_BEAST
namespace asio = boost::asio;
namespace beast = boost::beast;
namespace http = beast::http;
#endif // MENDER_USE_BOOST_BEAST

namespace error = mender::common::error;
namespace events = mender::common::events;
namespace expected = mender::common::expected;
namespace io = mender::common::io;
namespace log = mender::common::log;

class HttpErrorCategoryClass : public std::error_category {
public:
	const char *name() const noexcept override;
	string message(int code) const override;
};
extern const HttpErrorCategoryClass HttpErrorCategory;

enum ErrorCode {
	NoError = 0,
	NoSuchHeaderError,
	InvalidUrlError,
	BodyMissingError,
	UnsupportedMethodError,
	StreamCancelledError,
	UnsupportedBodyType,
};

error::Error MakeError(ErrorCode code, const string &msg);

enum class Method {
	Invalid,
	GET,
	POST,
	PUT,
	PATCH,
	CONNECT,
};

enum StatusCode {
	// Not a complete enum, we define only the ones we use.

	StatusOK = 200,
	StatusNoContent = 204,
	StatusNotFound = 404,
};

string MethodToString(Method method);

struct BrokenDownUrl {
	string protocol;
	string host;
	int port {-1};
	string path;
};

error::Error BreakDownUrl(const string &url, BrokenDownUrl &address);

class CaseInsensitiveHasher {
public:
	size_t operator()(const string &str) const;
};

class CaseInsensitiveComparator {
public:
	bool operator()(const string &str1, const string &str2) const;
};

class Transaction {
public:
	virtual ~Transaction() {
	}

	expected::ExpectedString GetHeader(const string &name) const;

protected:
	unordered_map<string, string, CaseInsensitiveHasher, CaseInsensitiveComparator> headers_;

	friend class Client;
};
using TransactionPtr = shared_ptr<Transaction>;

using BodyGenerator = function<io::ExpectedReaderPtr()>;

class Request : public Transaction {
public:
	Request() {
	}

	Method GetMethod() const;
	string GetPath() const;

protected:
	Method method_ {Method::Invalid};
	BrokenDownUrl address_;

	friend class Client;
	friend class Stream;
};
using RequestPtr = shared_ptr<Request>;
using ExpectedRequestPtr = expected::expected<RequestPtr, error::Error>;

class Response : public Transaction {
public:
	Response() {
	}

	unsigned GetStatusCode() const;
	string GetStatusMessage() const;

protected:
	unsigned status_code_;
	string status_message_;

	friend class Client;
	friend class Stream;
};
using ResponsePtr = shared_ptr<Response>;
using ExpectedResponsePtr = expected::expected<ResponsePtr, error::Error>;

class OutgoingRequest;
using OutgoingRequestPtr = shared_ptr<OutgoingRequest>;
using ExpectedOutgoingRequestPtr = expected::expected<OutgoingRequestPtr, error::Error>;
class IncomingRequest;
using IncomingRequestPtr = shared_ptr<IncomingRequest>;
using ExpectedIncomingRequestPtr = expected::expected<IncomingRequestPtr, error::Error>;
class IncomingResponse;
using IncomingResponsePtr = shared_ptr<IncomingResponse>;
using ExpectedIncomingResponsePtr = expected::expected<IncomingResponsePtr, error::Error>;
class OutgoingResponse;
using OutgoingResponsePtr = shared_ptr<OutgoingResponse>;
using ExpectedOutgoingResponsePtr = expected::expected<OutgoingResponsePtr, error::Error>;

using RequestHandler = function<void(ExpectedIncomingRequestPtr)>;
using ResponseHandler = function<void(ExpectedIncomingResponsePtr)>;

using ReplyFinishedHandler = function<void(error::Error)>;

class OutgoingRequest : public Request {
public:
	OutgoingRequest() {
	}

	void SetMethod(Method method);
	error::Error SetAddress(const string &address);
	void SetHeader(const string &name, const string &value);

	// Set to a function which will generate the body. Make sure that the Content-Length set in
	// the headers matches the length of the body. Using a generator instead of a direct reader
	// is needed in case of redirects.
	void SetBodyGenerator(BodyGenerator body_gen);

private:
	// Original address.
	string orig_address_;

	BodyGenerator body_gen_;
	io::ReaderPtr body_reader_;

	friend class Client;
};

class Stream;

class IncomingRequest : public Request {
public:
	// Set this after receiving the headers, if appropriate.
	void SetBodyWriter(io::WriterPtr body_writer);

	// Use this to get a response that can be used to reply to the request. Due to the
	// asynchronous nature, this can be done immediately or some time later.
	ExpectedOutgoingResponsePtr MakeResponse();

private:
	IncomingRequest() {
	}

	weak_ptr<Stream> stream_;

	io::WriterPtr body_writer_;

	friend class Stream;
};

class IncomingResponse : public Response {
public:
	// Set this after receiving the headers, if appropriate.
	void SetBodyWriter(io::WriterPtr body_writer);

private:
	IncomingResponse() {
	}

	io::WriterPtr body_writer_;

	friend class Client;
};

class OutgoingResponse : public Response {
public:
	~OutgoingResponse();

	error::Error AsyncReply(ReplyFinishedHandler reply_finished_handler);

	void SetStatusCodeAndMessage(unsigned code, const string &message);
	void SetHeader(const string &name, const string &value);

	// Set to a Reader which contains the body. Make sure that the Content-Length set in the
	// headers matches the length of the body.
	void SetBodyReader(io::ReaderPtr body_reader);

private:
	OutgoingResponse() {
	}

	io::ReaderPtr body_reader_;

	// Use weak pointer, so that if the server (and hence the stream) is canceled, we can detect
	// that the stream doesn't exist anymore.
	weak_ptr<Stream> stream_;

	bool has_replied_ {false};

	friend class Stream;
	friend class IncomingRequest;
};

// Master object that connections are made from. Configure TLS options on this object before making
// connections.
class ClientConfig {
public:
	ClientConfig();
	~ClientConfig();

	// TODO: Empty for now, but will contain TLS configuration options later.
};

// Object which manages one connection, and its requests and responses (one at a time).
class Client : public events::EventLoopObject {
public:
	Client(const ClientConfig &client, events::EventLoop &event_loop);
	~Client();

	// `header_handler` is called when header has arrived, `body_handler` is called when the
	// whole body has arrived.
	error::Error AsyncCall(
		OutgoingRequestPtr req, ResponseHandler header_handler, ResponseHandler body_handler);
	void Cancel();

private:
	log::Logger logger_;

	// Used during connections. Must remain valid due to async nature.
	OutgoingRequestPtr request_;
	IncomingResponsePtr response_;
	ResponseHandler header_handler_;
	ResponseHandler body_handler_;

#ifdef MENDER_USE_BOOST_BEAST
	boost::asio::ip::tcp::resolver resolver_;
	boost::beast::tcp_stream stream_;

	vector<uint8_t> body_buffer_;

	asio::ip::tcp::resolver::results_type resolver_results_;
	shared_ptr<http::request<http::buffer_body>> http_request_;
	shared_ptr<http::request_serializer<http::buffer_body>> http_request_serializer_;
	size_t request_body_length_;

	beast::flat_buffer response_buffer_;
	http::response_parser<http::buffer_body> http_response_parser_;

	static void CallErrorHandler(
		const error_code &err, const OutgoingRequestPtr &req, ResponseHandler handler);
	static void CallErrorHandler(
		const error::Error &err, const OutgoingRequestPtr &req, ResponseHandler handler);
	void ResolveHandler(
		const error_code &err, const asio::ip::tcp::resolver::results_type &results);
	void ConnectHandler(const error_code &err, const asio::ip::tcp::endpoint &endpoint);
	void WriteHeaderHandler(const error_code &err, size_t num_written);
	void WriteBodyHandler(const error_code &err, size_t num_written);
	void WriteBody();
	void ReadHeaderHandler(const error_code &err, size_t num_read);
	void ReadHeader();
	void ReadBodyHandler(const error_code &err, size_t num_read);
#endif // MENDER_USE_BOOST_BEAST
};

// Master object that servers are made from. Configure TLS options on this object before listening.
class ServerConfig {
public:
	ServerConfig();
	~ServerConfig();

	// TODO: Empty for now, but will contain TLS configuration options later.
};

class Server;

class Stream : public enable_shared_from_this<Stream> {
public:
	Stream(const Stream &) = delete;
	~Stream();

private:
	Stream(Server &server);

private:
	Server &server_;
	friend class Server;

	log::Logger logger_;

	IncomingRequestPtr request_;

	// The reason we have two pointers is this: Between receiving a request, and producing a
	// reply, an arbitrary amount of time may pass, and it is the caller's responsibility to
	// first call MakeResponse(), and then at some point later, call AsyncReply(). However, if
	// the caller never does this, and destroys the response instead, we still have ownership to
	// the response here, which means it will never be destroyed, and we will leak memory. So we
	// use a weak_ptr to bridge the gap. As long as AsyncReply() has not been called yet, we use
	// a weak pointer so if the response goes out of scope, it will be properly destroyed. After
	// AsyncReply is called, we know that a handler will eventually be called, so we take
	// ownership of the response object from that point onwards.
	OutgoingResponsePtr response_;
	weak_ptr<OutgoingResponse> maybe_response_;

	friend class IncomingRequest;
	friend class OutgoingResponse;

	ReplyFinishedHandler reply_finished_handler_;

#ifdef MENDER_USE_BOOST_BEAST
	asio::ip::tcp::socket socket_;
	beast::flat_buffer request_buffer_;
	http::request_parser<http::buffer_body> http_request_parser_;
	vector<uint8_t> body_buffer_;

	shared_ptr<http::response<http::buffer_body>> http_response_;
	shared_ptr<http::response_serializer<http::buffer_body>> http_response_serializer_;

	void CallErrorHandler(const error_code &ec, const RequestPtr &req, RequestHandler handler);
	void CallErrorHandler(const error::Error &err, const RequestPtr &req, RequestHandler handler);
	void CallErrorHandler(
		const error_code &ec, const RequestPtr &req, ReplyFinishedHandler handler);
	void CallErrorHandler(
		const error::Error &err, const RequestPtr &req, ReplyFinishedHandler handler);

	void AcceptHandler(const error_code &ec);
	void ReadHeader();
	void ReadHeaderHandler(const error_code &err, size_t num_read);
	void ReadBodyHandler(const error_code &err, size_t num_read);
	void AsyncReply(ReplyFinishedHandler reply_finished_handler);
	void WriteHeaderHandler(const error_code &err, size_t num_written);
	void WriteBody();
	void WriteBodyHandler(const error_code &err, size_t num_written);
	void CallBodyHandler();
#endif // MENDER_USE_BOOST_BEAST
};

class Server : public events::EventLoopObject {
public:
	Server(const ServerConfig &server, events::EventLoop &event_loop);
	~Server();

	error::Error AsyncServeUrl(
		const string &url, RequestHandler header_handler, RequestHandler body_handler);
	void Cancel();

private:
	events::EventLoop &event_loop_;

	BrokenDownUrl address_;

	RequestHandler header_handler_;
	RequestHandler body_handler_;

	friend class Stream;
	friend class OutgoingResponse;

	using StreamPtr = shared_ptr<Stream>;

#ifdef MENDER_USE_BOOST_BEAST
	asio::ip::tcp::acceptor acceptor_;

	unordered_set<StreamPtr> streams_;

	void PrepareNewStream();
	void AsyncAccept(StreamPtr stream);
	void RemoveStream(const StreamPtr &stream);
#endif // MENDER_USE_BOOST_BEAST
};

} // namespace http
} // namespace mender

#endif // MENDER_COMMON_HTTP_HPP
