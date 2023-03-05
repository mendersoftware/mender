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
};

error::Error MakeError(ErrorCode code, const string &msg);

enum class Method {
	GET,
	POST,
	PUT,
	PATCH,
	CONNECT,
};

string MethodToString(Method method);

class Transaction {
public:
	expected::ExpectedString GetHeader(const string &name) const;

protected:
	unordered_map<string, string> headers_;

	friend class Session;
};

using BodyGenerator = function<io::ExpectedReaderPtr()>;

class Request : public Transaction {
public:
	Request(Method method);

	error::Error SetAddress(const string &address);
	void SetHeader(const string &name, const string &value);

	// Set to a function which will generate the body. Make sure that the Content-Length set in
	// the headers matches the length of the body. Using a generator instead of a direct reader
	// is needed in case of redirects.
	void SetBodyGenerator(BodyGenerator body_gen);

private:
	bool ready_ {false};

	// Original address.
	string orig_address_;

	// Broken down address.
	string protocol_;
	string host_;
	int port_;
	string path_;

	Method method_;

	BodyGenerator body_gen_;
	io::ReaderPtr body_reader_;

	friend class Session;
};

enum StatusCode {
	// Not a complete enum, we define only the ones we use.

	StatusOK = 200,
	StatusNoContent = 204,
	StatusNotFound = 404,
};

class Response : public Transaction {
public:
	Response(unsigned status_code, const string &message);

	unsigned GetStatusCode() const {
		return status_code_;
	}

	string GetStatusMessage() const {
		return status_message_;
	}

	// Set this after receiving the headers, if appropriate.
	void SetBodyWriter(io::WriterPtr body_writer);

private:
	unsigned status_code_;
	string status_message_;

	io::WriterPtr body_writer_;

	friend class Session;
};

using TransactionPtr = shared_ptr<Transaction>;
using RequestPtr = shared_ptr<Request>;
using ResponsePtr = shared_ptr<Response>;

using ExpectedResponsePtr = expected::expected<ResponsePtr, error::Error>;

using ResponseHandler = function<void(ExpectedResponsePtr)>;

// Master object that connections are made from. Configure TLS options on this object before making
// connections.
class Client {
public:
	Client();
	~Client();

	// TODO: Empty for now, but will contain TLS configuration options later.
};

// Object which manages one connection, and its requests and responses (one at a time).
class Session : public events::EventLoopObject {
public:
	Session(const Client &client, events::EventLoop &event_loop);
	~Session();

	// `header_handler` is called when header has arrived, `body_handler` is called when the
	// whole body has arrived.
	error::Error AsyncCall(
		RequestPtr req, ResponseHandler header_handler, ResponseHandler body_handler);
	void Cancel();

private:
	log::Logger logger_;

#ifdef MENDER_USE_BOOST_BEAST
	boost::asio::ip::tcp::resolver resolver_;
	boost::beast::tcp_stream stream_;

	vector<uint8_t> body_buffer_;

	// Used during connections. Must remain valid due to async nature.
	RequestPtr request_;
	ResponsePtr response_;
	ResponseHandler header_handler_;
	ResponseHandler body_handler_;
	asio::ip::tcp::resolver::results_type resolver_results_;
	shared_ptr<http::request<http::buffer_body>> http_request_;
	shared_ptr<http::request_serializer<http::buffer_body>> http_request_serializer_;
	size_t request_body_length_;

	beast::flat_buffer response_buffer_;
	http::response_parser<http::buffer_body> http_response_parser_;

	static void CallErrorHandler(
		const error_code &err, const RequestPtr &req, ResponseHandler handler);
	static void CallErrorHandler(
		const error::Error &err, const RequestPtr &req, ResponseHandler handler);
	void ResolveHandler(error_code err, const asio::ip::tcp::resolver::results_type &results);
	void ConnectHandler(error_code err, const asio::ip::tcp::endpoint &endpoint);
	void WriteHeaderHandler(error_code err, size_t num_written);
	void WriteBodyHandler(error_code err, size_t num_written);
	void WriteBody();
	void ReadHeaderHandler(error_code err, size_t num_read);
	void ReadHeader();
	void ReadBodyHandler(error_code err, size_t num_read);
#endif // MENDER_USE_BOOST_BEAST
};

} // namespace http
} // namespace mender

#endif // MENDER_COMMON_HTTP_HPP
