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

#include <functional>
#include <string>
#include <memory>
#include <unordered_map>
#include <unordered_set>
#include <vector>

#ifdef MENDER_USE_BOOST_BEAST
#include <boost/asio.hpp>
#include <boost/beast.hpp>
#include <boost/asio/ssl.hpp>
#include <boost/asio/ssl/error.hpp>
#include <boost/asio/ssl/stream.hpp>
#endif // MENDER_USE_BOOST_BEAST

#include <config.h>

#include <common/error.hpp>
#include <common/events.hpp>
#include <common/expected.hpp>
#include <common/io.hpp>
#include <common/log.hpp>

namespace mender {
namespace update {
namespace http_resumer {
class DownloadResumerClient;
class HeaderHandlerFunctor;
class BodyHandlerFunctor;
} // namespace http_resumer
} // namespace update
} // namespace mender

namespace mender {
namespace http {

using namespace std;

#ifdef MENDER_USE_BOOST_BEAST
namespace asio = boost::asio;
namespace beast = boost::beast;
namespace http = beast::http;
namespace ssl = asio::ssl;
using tcp = asio::ip::tcp;
#endif // MENDER_USE_BOOST_BEAST

namespace error = mender::common::error;
namespace events = mender::common::events;
namespace expected = mender::common::expected;
namespace io = mender::common::io;
namespace log = mender::common::log;

class Client;
class ClientInterface;

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
	BodyIgnoredError,
	UnsupportedMethodError,
	StreamCancelledError,
	UnsupportedBodyType,
	MaxRetryError,
	DownloadResumerError,
};

error::Error MakeError(ErrorCode code, const string &msg);

enum class Method {
	Invalid,
	GET,
	HEAD,
	POST,
	PUT,
	PATCH,
	CONNECT,
};

enum StatusCode {
	// Not a complete enum, we define only the ones we use.

	StatusSwitchingProtocols = 101,

	StatusOK = 200,
	StatusNoContent = 204,
	StatusPartialContent = 206,

	StatusBadRequest = 400,
	StatusUnauthorized = 401,
	StatusNotFound = 404,
	StatusConflict = 409,

	StatusInternalServerError = 500,
	StatusNotImplemented = 501,
};

string MethodToString(Method method);

struct BrokenDownUrl {
	string protocol;
	string host;
	int port {-1};
	string path;
};

error::Error BreakDownUrl(const string &url, BrokenDownUrl &address);

string URLEncode(const string &value);

string JoinOneUrl(const string &prefix, const string &url);

template <typename... Urls>
string JoinUrl(const string &prefix, const Urls &...urls) {
	string final_url {prefix};
	for (const auto &url : {urls...}) {
		final_url = JoinOneUrl(final_url, url);
	}
	return final_url;
}

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

	using HeaderMap =
		unordered_map<string, string, CaseInsensitiveHasher, CaseInsensitiveComparator>;

	const HeaderMap &GetHeaders() const {
		return headers_;
	}

protected:
	HeaderMap headers_;

	friend class Client;
};
using TransactionPtr = shared_ptr<Transaction>;

using BodyGenerator = function<io::ExpectedReaderPtr()>;
using AsyncBodyGenerator = function<io::ExpectedAsyncReaderPtr()>;

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
	unsigned status_code_ {StatusInternalServerError};
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
using IdentifiedRequestHandler = function<void(IncomingRequestPtr, error::Error)>;
using ResponseHandler = function<void(ExpectedIncomingResponsePtr)>;

using ReplyFinishedHandler = function<void(error::Error)>;
using SwitchProtocolHandler = function<void(io::ExpectedAsyncReadWriterPtr)>;

// Usually you want to cancel the connection when there is an error during body writing, but there
// are some cases in tests where it's useful to keep the connection alive in order to let the
// original error make it to the body handler.
enum class BodyWriterErrorMode {
	Cancel,
	KeepAlive,
};

class OutgoingRequest : public Request {
public:
	OutgoingRequest() {
	}

	void SetMethod(Method method);
	error::Error SetAddress(const string &address);
	void SetHeader(const string &name, const string &value);

	// Set to a function which will generate the body. Make sure that the Content-Length set in
	// the headers matches the length of the body. Using a generator instead of a direct reader
	// is needed in case of redirects. Note that it is not possible to set both; setting one
	// unsets the other.
	void SetBodyGenerator(BodyGenerator body_gen);
	void SetAsyncBodyGenerator(AsyncBodyGenerator body_gen);

private:
	// Original address.
	string orig_address_;

	BodyGenerator body_gen_;
	io::ReaderPtr body_reader_;
	AsyncBodyGenerator async_body_gen_;
	io::AsyncReaderPtr async_body_reader_;

	friend class Client;
};

class Stream;

class IncomingRequest :
	public Request,
	virtual public io::Canceller,
	public enable_shared_from_this<IncomingRequest> {
public:
	~IncomingRequest();

	// Set this after receiving the headers to automatically write the body. If there is no
	// body, nothing will be written. Mutually exclusive with `MakeBodyAsyncReader()`.
	void SetBodyWriter(
		io::WriterPtr body_writer, BodyWriterErrorMode mode = BodyWriterErrorMode::Cancel);

	// Use this to get an async reader for the body. If there is no body, it returns a
	// `BodyMissingError`; it's safe to continue afterwards, but without a reader. Mutually
	// exclusive with `SetBodyWriter()`.
	io::ExpectedAsyncReaderPtr MakeBodyAsyncReader();

	// Use this to get a response that can be used to reply to the request. Due to the
	// asynchronous nature, this can be done immediately or some time later.
	ExpectedOutgoingResponsePtr MakeResponse();

	void Cancel() override;

private:
	IncomingRequest(Stream &stream, shared_ptr<bool> cancelled) :
		stream_(stream),
		cancelled_(cancelled) {
	}

	Stream &stream_;
	shared_ptr<bool> cancelled_;

	friend class Server;
	friend class Stream;
};

class IncomingResponse :
	public Response,
	virtual public io::Canceller,
	public enable_shared_from_this<IncomingResponse> {
public:
	void Cancel() override;

	// Set this after receiving the headers to automatically write the body. If there is no
	// body, nothing will be written. Mutually exclusive with `MakeBodyAsyncReader()`.
	void SetBodyWriter(
		io::WriterPtr body_writer, BodyWriterErrorMode mode = BodyWriterErrorMode::Cancel);

	// Use this to get an async reader for the body. If there is no body, it returns a
	// `BodyMissingError`; it's safe to continue afterwards, but without a reader. Mutually
	// exclusive with `SetBodyWriter()`.
	io::ExpectedAsyncReaderPtr MakeBodyAsyncReader();

	// Gets the underlying socket after a 101 Switching Protocols response. This detaches the
	// socket from `Client`, and both can be used independently from then on.
	io::ExpectedAsyncReadWriterPtr SwitchProtocol();

private:
	IncomingResponse(ClientInterface &client, shared_ptr<bool> cancelled);

private:
	ClientInterface &client_;
	shared_ptr<bool> cancelled_;

	friend class Client;
	friend class mender::update::http_resumer::DownloadResumerClient;
	// The DownloadResumer's handlers needs to manipulate internals of IncomingResponse
	friend class mender::update::http_resumer::HeaderHandlerFunctor;
	friend class mender::update::http_resumer::BodyHandlerFunctor;
};

class OutgoingResponse :
	public Response,
	virtual public io::Canceller,
	public enable_shared_from_this<OutgoingResponse> {
public:
	~OutgoingResponse();

	error::Error AsyncReply(ReplyFinishedHandler reply_finished_handler);
	void Cancel() override;

	void SetStatusCodeAndMessage(unsigned code, const string &message);
	void SetHeader(const string &name, const string &value);

	// Set to a Reader which contains the body. Make sure that the Content-Length set in the
	// headers matches the length of the body. Note that it is not possible to set both; setting
	// one unsets the other.
	void SetBodyReader(io::ReaderPtr body_reader);
	void SetAsyncBodyReader(io::AsyncReaderPtr body_reader);

	// An alternative to AsyncReply. `resp` should already contain the correct status and
	// headers to perform the switch, and the handler will be called after the HTTP headers have
	// been written.
	error::Error AsyncSwitchProtocol(SwitchProtocolHandler handler);

private:
	OutgoingResponse(Stream &stream, shared_ptr<bool> cancelled) :
		stream_ {stream},
		cancelled_ {cancelled} {
	}

	io::ReaderPtr body_reader_;
	io::AsyncReaderPtr async_body_reader_;

	Stream &stream_;
	shared_ptr<bool> cancelled_;

	friend class Server;
	friend class Stream;
	friend class IncomingRequest;
};

template <typename StreamType>
class BodyAsyncReader;

// Master object that connections are made from. Configure TLS options on this object before making
// connections.
struct ClientConfig {
	string server_cert_path;
	string client_cert_path;
	string client_cert_key_path;
	string ssl_engine;
	// C++11 cannot mix default member initializers with designated initializers (named
	// parameters). But the default of bools in C++ is always false regardless, so we still get
	// intended behavior, it's just not explicit.
	bool skip_verify;        // {false};
	bool disable_keep_alive; // {false};
};

enum class TransactionStatus {
	None,
	HeaderHandlerCalled,
	ReaderCreated,
	BodyReadingInProgress,
	BodyReadingFinished,
	BodyHandlerCalled, // Only used by server.
	Replying,          // Only used by server.
	SwitchingProtocol,
	Done,
};
static inline bool AtLeast(TransactionStatus status, TransactionStatus expected_status) {
	return static_cast<int>(status) >= static_cast<int>(expected_status);
}

// Interface which manages one connection, and its requests and responses (one at a time).
class ClientInterface {
public:
	virtual ~ClientInterface() {};

	// `header_handler` is called when header has arrived, `body_handler` is called when the
	// whole body has arrived.
	virtual error::Error AsyncCall(
		OutgoingRequestPtr req, ResponseHandler header_handler, ResponseHandler body_handler) = 0;
	virtual void Cancel() = 0;

	// Use this to get an async reader for the body. If there is no body, it returns a
	// `BodyMissingError`; it's safe to continue afterwards, but without a reader.
	virtual io::ExpectedAsyncReaderPtr MakeBodyAsyncReader(IncomingResponsePtr resp) = 0;

	// Returns the real HTTP client.
	virtual Client &GetHttpClient() = 0;
};

class Client :
	virtual public ClientInterface,
	public events::EventLoopObject,
	virtual public io::Canceller {
public:
	Client(
		const ClientConfig &client,
		events::EventLoop &event_loop,
		const string &logger_name = "http_client");
	virtual ~Client();

	Client(Client &&) = default;

	error::Error AsyncCall(
		OutgoingRequestPtr req,
		ResponseHandler header_handler,
		ResponseHandler body_handler) override;
	void Cancel() override;

	io::ExpectedAsyncReaderPtr MakeBodyAsyncReader(IncomingResponsePtr resp) override;

	// Gets the underlying socket after a 101 Switching Protocols response. This detaches the
	// socket from `Client`, and both can be used independently from then on.
	virtual io::ExpectedAsyncReadWriterPtr SwitchProtocol(IncomingResponsePtr req);

	Client &GetHttpClient() override {
		return *this;
	};

protected:
	events::EventLoop &event_loop_;
	string logger_name_;
	log::Logger logger_ {logger_name_};

private:
	bool is_https_ {false};

	// Used during connections. Must remain valid due to async nature.
	OutgoingRequestPtr request_;
	IncomingResponsePtr response_;
	ResponseHandler header_handler_;
	ResponseHandler body_handler_;

	vector<uint8_t>::iterator reader_buf_start_;
	vector<uint8_t>::iterator reader_buf_end_;
	io::AsyncIoHandler reader_handler_;

	// Each time we cancel something, we set this to true, and then make a new one. This ensures
	// that for everyone who has a copy, it will stay true even after a new request is made, or
	// after things have been destroyed.
	shared_ptr<bool> cancelled_;

	const bool disable_keep_alive_;

#ifdef MENDER_USE_BOOST_BEAST

	ssl::context ssl_ctx_ {ssl::context::tls_client};

	boost::asio::ip::tcp::resolver resolver_;
	shared_ptr<ssl::stream<tcp::socket>> stream_;

	vector<uint8_t> body_buffer_;

	asio::ip::tcp::resolver::results_type resolver_results_;

	// The reason that these are inside a struct is a bit complicated. We need to deal with what
	// may be a bug in Boost Beast: Parsers and serializers can access the corresponding request
	// and response structures even after they have been cancelled. This means two things:
	//
	// 1. We need to make sure that the response/request and the parser/serializer both survive
	//    until the handler is called, even if they are not used in the handler, and even if
	//    the handler returns `operation_aborted` (cancelled).
	//
	// 2. We need to make sure that the parser/serializer is destroyed before the
	//    response/request, since the former accesses the latter.
	//
	// For point number 1, it is enough to simply make a copy of the shared pointers in the
	// handler function, which will keep them alive long enough.
	//
	// For point 2 however, even though it may seem logical that a lambda would destroy its
	// captured variables in the reverse order they are captured, the order is in fact
	// unspecified. That means we need to enforce the order, and that's what the struct is
	// for: Struct members are always destroyed in reverse declaration order.
	struct {
		shared_ptr<http::request<http::buffer_body>> http_request_;
		shared_ptr<http::request_serializer<http::buffer_body>> http_request_serializer_;
	} request_data_;

	size_t request_body_length_;

	// See `Client::request_data_` for why this is a struct.
	struct {
		shared_ptr<beast::flat_buffer> response_buffer_;
		shared_ptr<http::response_parser<http::buffer_body>> http_response_parser_;
	} response_data_;
	size_t response_body_length_;
	size_t response_body_read_;
	TransactionStatus status_ {TransactionStatus::None};

	void DoCancel();

	void CallHandler(ResponseHandler handler);
	void CallErrorHandler(
		const error_code &ec, const OutgoingRequestPtr &req, ResponseHandler handler);
	void CallErrorHandler(
		const error::Error &err, const OutgoingRequestPtr &req, ResponseHandler handler);
	void ResolveHandler(const error_code &ec, const asio::ip::tcp::resolver::results_type &results);
	void ConnectHandler(const error_code &ec, const asio::ip::tcp::endpoint &endpoint);
	void HandshakeHandler(const error_code &ec, const asio::ip::tcp::endpoint &endpoint);
	void WriteHeaderHandler(const error_code &ec, size_t num_written);
	void WriteBodyHandler(const error_code &ec, size_t num_written);
	void PrepareAndWriteNewBodyBuffer();
	void WriteNewBodyBuffer(size_t size);
	void WriteBody();
	void ReadHeaderHandler(const error_code &ec, size_t num_read);
	void ReadHeader();
	void AsyncReadNextBodyPart(
		vector<uint8_t>::iterator start, vector<uint8_t>::iterator end, io::AsyncIoHandler handler);
	void ReadBodyHandler(error_code ec, size_t num_read);
#endif // MENDER_USE_BOOST_BEAST

	friend class IncomingResponse;
	friend class BodyAsyncReader<Client>;
};
using ClientPtr = shared_ptr<Client>;

// Master object that servers are made from. Configure TLS options on this object before listening.
struct ServerConfig {
	// TODO: Empty for now, but will contain TLS configuration options later.
};

class Server;

class Stream : public enable_shared_from_this<Stream> {
public:
	Stream(const Stream &) = delete;
	~Stream();

	void Cancel();

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
	friend class BodyAsyncReader<Stream>;

	ReplyFinishedHandler reply_finished_handler_;
	SwitchProtocolHandler switch_protocol_handler_;

	vector<uint8_t>::iterator reader_buf_start_;
	vector<uint8_t>::iterator reader_buf_end_;
	io::AsyncIoHandler reader_handler_;

	// Each time we cancel something, we set this to true, and then make a new one. This ensures
	// that for everyone who has a copy, it will stay true even after a new request is made, or
	// after things have been destroyed.
	shared_ptr<bool> cancelled_;

#ifdef MENDER_USE_BOOST_BEAST
	asio::ip::tcp::socket socket_;

	// See `Client::request_data_` for why this is a struct.
	struct {
		shared_ptr<beast::flat_buffer> request_buffer_;
		shared_ptr<http::request_parser<http::buffer_body>> http_request_parser_;
	} request_data_;
	vector<uint8_t> body_buffer_;
	size_t request_body_length_;
	size_t request_body_read_;
	TransactionStatus status_ {TransactionStatus::None};

	// See `Client::request_data_` for why this is a struct.
	struct {
		shared_ptr<http::response<http::buffer_body>> http_response_;
		shared_ptr<http::response_serializer<http::buffer_body>> http_response_serializer_;
	} response_data_;

	void DoCancel();

	void CallErrorHandler(const error_code &ec, const RequestPtr &req, RequestHandler handler);
	void CallErrorHandler(const error::Error &err, const RequestPtr &req, RequestHandler handler);
	void CallErrorHandler(
		const error_code &ec, const IncomingRequestPtr &req, IdentifiedRequestHandler handler);
	void CallErrorHandler(
		const error::Error &err, const IncomingRequestPtr &req, IdentifiedRequestHandler handler);
	void CallErrorHandler(
		const error_code &ec, const RequestPtr &req, ReplyFinishedHandler handler);
	void CallErrorHandler(
		const error::Error &err, const RequestPtr &req, ReplyFinishedHandler handler);
	void CallErrorHandler(
		const error_code &ec, const RequestPtr &req, SwitchProtocolHandler handler);
	void CallErrorHandler(
		const error::Error &err, const RequestPtr &req, SwitchProtocolHandler handler);

	void AcceptHandler(const error_code &ec);
	void ReadHeader();
	void ReadHeaderHandler(const error_code &ec, size_t num_read);
	void AsyncReadNextBodyPart(
		vector<uint8_t>::iterator start, vector<uint8_t>::iterator end, io::AsyncIoHandler handler);
	void ReadBodyHandler(error_code ec, size_t num_read);
	void AsyncReply(ReplyFinishedHandler reply_finished_handler);
	void SetupResponse();
	void WriteHeaderHandler(const error_code &ec, size_t num_written);
	void PrepareAndWriteNewBodyBuffer();
	void WriteNewBodyBuffer(size_t size);
	void WriteBody();
	void WriteBodyHandler(const error_code &ec, size_t num_written);
	void CallBodyHandler();
	void FinishReply();
	error::Error AsyncSwitchProtocol(SwitchProtocolHandler handler);
	void SwitchingProtocolHandler(error_code ec, size_t num_written);
#endif // MENDER_USE_BOOST_BEAST
};

class Server : public events::EventLoopObject, virtual public io::Canceller {
public:
	Server(const ServerConfig &server, events::EventLoop &event_loop);
	~Server();

	Server(Server &&) = default;

	error::Error AsyncServeUrl(
		const string &url, RequestHandler header_handler, RequestHandler body_handler);
	// Same as the above, except that the body handler has the `IncomingRequestPtr` included
	// even when there is an error, so that the request can be matched with the request which
	// was received in the header handler.
	error::Error AsyncServeUrl(
		const string &url, RequestHandler header_handler, IdentifiedRequestHandler body_handler);
	void Cancel() override;

	uint16_t GetPort() const;
	// Can differ from the passed in URL if a 0 (random) port number was used.
	string GetUrl() const;

	// Use this to get a response that can be used to reply to the request. Due to the
	// asynchronous nature, this can be done immediately or some time later.
	virtual ExpectedOutgoingResponsePtr MakeResponse(IncomingRequestPtr req);
	virtual error::Error AsyncReply(
		OutgoingResponsePtr resp, ReplyFinishedHandler reply_finished_handler);

	// Use this to get an async reader for the body. If there is no body, it returns a
	// `BodyMissingError`; it's safe to continue afterwards, but without a reader.
	virtual io::ExpectedAsyncReaderPtr MakeBodyAsyncReader(IncomingRequestPtr req);

	// An alternative to AsyncReply. `resp` should already contain the correct status and
	// headers to perform the switch, and the handler will be called after the HTTP headers have
	// been written.
	virtual error::Error AsyncSwitchProtocol(
		OutgoingResponsePtr resp, SwitchProtocolHandler handler);

private:
	events::EventLoop &event_loop_;

	BrokenDownUrl address_;

	RequestHandler header_handler_;
	IdentifiedRequestHandler body_handler_;

	friend class IncomingRequest;
	friend class Stream;
	friend class OutgoingResponse;

	using StreamPtr = shared_ptr<Stream>;

	friend class TestInspector;

#ifdef MENDER_USE_BOOST_BEAST
	asio::ip::tcp::acceptor acceptor_;

	unordered_set<StreamPtr> streams_;

	void DoCancel();

	void PrepareNewStream();
	void AsyncAccept(StreamPtr stream);
	void RemoveStream(StreamPtr stream);
#endif // MENDER_USE_BOOST_BEAST
};

class ExponentialBackoff {
public:
	ExponentialBackoff(chrono::milliseconds max_interval, int try_count = -1) :
		try_count_ {try_count} {
		SetMaxInterval(max_interval);
	}

	void Reset() {
		SetIteration(0);
	}

	int TryCount() {
		return try_count_;
	}
	void SetTryCount(int count) {
		try_count_ = count;
	}

	chrono::milliseconds SmallestInterval() {
		return smallest_interval_;
	}
	void SetSmallestInterval(chrono::milliseconds interval) {
		smallest_interval_ = interval;
		if (max_interval_ < smallest_interval_) {
			max_interval_ = smallest_interval_;
		}
	}

	chrono::milliseconds MaxInterval() {
		return max_interval_;
	}
	void SetMaxInterval(chrono::milliseconds interval) {
		max_interval_ = interval;
		if (max_interval_ < smallest_interval_) {
			max_interval_ = smallest_interval_;
		}
	}

	using ExpectedInterval = expected::expected<chrono::milliseconds, error::Error>;
	ExpectedInterval NextInterval();

	// Set which iteration we're at. Mainly for use in tests.
	void SetIteration(int iteration) {
		iteration_ = iteration;
	}

private:
	chrono::milliseconds smallest_interval_ {chrono::minutes(1)};
	chrono::milliseconds max_interval_;
	int try_count_;

	int iteration_ {0};
};

} // namespace http
} // namespace mender

#endif // MENDER_COMMON_HTTP_HPP
