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
#include <cctype>
#include <cstdlib>
#include <iomanip>
#include <string>

#include <common/common.hpp>

namespace mender {
namespace http {

namespace common = mender::common;

const HttpErrorCategoryClass HttpErrorCategory;

const char *HttpErrorCategoryClass::name() const noexcept {
	return "HttpErrorCategory";
}

string HttpErrorCategoryClass::message(int code) const {
	switch (code) {
	case NoError:
		return "Success";
	case NoSuchHeaderError:
		return "No such header";
	case InvalidUrlError:
		return "Malformed URL";
	case BodyMissingError:
		return "Body is missing";
	case UnsupportedMethodError:
		return "Unsupported HTTP method";
	case StreamCancelledError:
		return "Stream has been cancelled/destroyed";
	case UnsupportedBodyType:
		return "HTTP stream has a body type we don't understand";
	case MaxRetryError:
		return "Tried maximum number of times";
	}
	// Don't use "default" case. This should generate a warning if we ever add any enums. But
	// still assert here for safety.
	assert(false);
	return "Unknown";
}

error::Error MakeError(ErrorCode code, const string &msg) {
	return error::Error(error_condition(code, HttpErrorCategory), msg);
}

string MethodToString(Method method) {
	switch (method) {
	case Method::Invalid:
		return "Invalid";
	case Method::GET:
		return "GET";
	case Method::HEAD:
		return "HEAD";
	case Method::POST:
		return "POST";
	case Method::PUT:
		return "PUT";
	case Method::PATCH:
		return "PATCH";
	case Method::CONNECT:
		return "CONNECT";
	}
	// Don't use "default" case. This should generate a warning if we ever add any methods. But
	// still assert here for safety.
	assert(false);
	return "INVALID_METHOD";
}

error::Error BreakDownUrl(const string &url, BrokenDownUrl &address) {
	const string url_split {"://"};

	auto split_index = url.find(url_split);
	if (split_index == string::npos) {
		return MakeError(InvalidUrlError, url + " is not a valid URL.");
	}
	if (split_index == 0) {
		return MakeError(InvalidUrlError, url + ": missing hostname");
	}

	address.protocol = url.substr(0, split_index);

	auto tmp = url.substr(split_index + url_split.size());
	split_index = tmp.find("/");
	if (split_index == string::npos) {
		address.host = tmp;
		address.path = "/";
	} else {
		address.host = tmp.substr(0, split_index);
		address.path = tmp.substr(split_index);
	}

	split_index = address.host.find(":");
	if (split_index != string::npos) {
		tmp = std::move(address.host);
		address.host = tmp.substr(0, split_index);

		tmp = tmp.substr(split_index + 1);
		auto port = common::StringToLongLong(tmp);
		if (!port) {
			return error::Error(port.error().code, url + " contains invalid port number");
		}
		address.port = port.value();
	} else {
		if (address.protocol == "http") {
			address.port = 80;
		} else if (address.protocol == "https") {
			address.port = 443;
		} else {
			return error::Error(
				make_error_condition(errc::protocol_not_supported),
				"Cannot deduce port number from protocol " + address.protocol);
		}
	}

	log::Trace(
		"URL broken down into (protocol: " + address.protocol + "), (host: " + address.host
		+ "), (port: " + to_string(address.port) + "), (path: " + address.path + ")");

	return error::NoError;
}

string URLEncode(const string &value) {
	stringstream escaped;
	escaped << hex;

	for (auto c : value) {
		// Keep alphanumeric and other accepted characters intact
		if (isalnum(c) || c == '-' || c == '_' || c == '.' || c == '~') {
			escaped << c;
		} else {
			// Any other characters are percent-encoded
			escaped << uppercase;
			escaped << '%' << setw(2) << int((unsigned char) c);
			escaped << nouppercase;
		}
	}

	return escaped.str();
}

string JoinOneUrl(const string &prefix, const string &suffix) {
	auto prefix_end = prefix.cend();
	while (prefix_end != prefix.cbegin() && prefix_end[-1] == '/') {
		prefix_end--;
	}

	auto suffix_start = suffix.cbegin();
	while (suffix_start != suffix.cend() && *suffix_start == '/') {
		suffix_start++;
	}

	return string(prefix.cbegin(), prefix_end) + "/" + string(suffix_start, suffix.cend());
}

size_t CaseInsensitiveHasher::operator()(const string &str) const {
	string lower_str(str.length(), ' ');
	transform(
		str.begin(), str.end(), lower_str.begin(), [](unsigned char c) { return std::tolower(c); });
	return hash<string>()(lower_str);
}

bool CaseInsensitiveComparator::operator()(const string &str1, const string &str2) const {
	return strcasecmp(str1.c_str(), str2.c_str()) == 0;
}

expected::ExpectedString Transaction::GetHeader(const string &name) const {
	if (headers_.find(name) == headers_.end()) {
		return expected::unexpected(MakeError(NoSuchHeaderError, "No such header: " + name));
	}
	return headers_.at(name);
}

Method Request::GetMethod() const {
	return method_;
}

string Request::GetPath() const {
	return address_.path;
}

unsigned Response::GetStatusCode() const {
	return status_code_;
}

string Response::GetStatusMessage() const {
	return status_message_;
}

void OutgoingRequest::SetMethod(Method method) {
	method_ = method;
}

void OutgoingRequest::SetHeader(const string &name, const string &value) {
	headers_[name] = value;
}

error::Error OutgoingRequest::SetAddress(const string &address) {
	orig_address_ = address;

	return BreakDownUrl(address, address_);
}

void OutgoingRequest::SetBodyGenerator(BodyGenerator body_gen) {
	body_gen_ = body_gen;
}

IncomingResponse::IncomingResponse(weak_ptr<Client> client) :
	client_ {client} {
}

void IncomingResponse::SetBodyWriter(io::WriterPtr body_writer) {
	body_writer_ = body_writer;
}

io::AsyncReaderPtr IncomingResponse::MakeBodyAsyncReader() {
	body_async_reader_ = shared_ptr<BodyAsyncReader>(new BodyAsyncReader(client_));
	return body_async_reader_;
}

IncomingResponse::BodyAsyncReader::BodyAsyncReader(weak_ptr<Client> client) :
	client_ {client} {
}

IncomingResponse::BodyAsyncReader::~BodyAsyncReader() {
	Cancel();
}

error::Error IncomingResponse::BodyAsyncReader::AsyncRead(
	vector<uint8_t>::iterator start, vector<uint8_t>::iterator end, io::AsyncIoHandler handler) {
	if (done_) {
		handler(0);
	}

	auto client = client_.lock();
	if (!client) {
		return error::MakeError(
			error::ProgrammingError, "BodyAsyncReader::AsyncRead called after Client is destroyed");
	}
	client->AsyncReadNextBodyPart(start, end, handler);
	return error::NoError;
}

void IncomingResponse::BodyAsyncReader::Cancel() {
	auto client = client_.lock();
	if (client) {
		client->Cancel();
	}
}

void IncomingRequest::SetBodyWriter(io::WriterPtr body_writer) {
	body_writer_ = body_writer;
}

ExpectedOutgoingResponsePtr IncomingRequest::MakeResponse() {
	auto stream = stream_.lock();
	if (!stream) {
		return expected::unexpected(MakeError(StreamCancelledError, "Cannot make response"));
	}
	OutgoingResponsePtr response {new OutgoingResponse};
	response->stream_ = stream_;
	stream->maybe_response_ = response;
	return response;
}

void IncomingRequest::Cancel() {
	auto stream = stream_.lock();
	if (stream) {
		stream->Cancel();
	}
}

OutgoingResponse::~OutgoingResponse() {
	Cancel();

	auto stream = stream_.lock();
	if (!stream) {
		// It was probably cancelled. Destroying the response is normal then.
		return;
	}

	if (!has_replied_) {
		stream->logger_.Error(
			"Response was destroyed before sending anything. Closing stream prematurely");
	}

	// Remove the stream from the server.
	stream->server_.RemoveStream(stream);
}

void OutgoingResponse::Cancel() {
	auto stream = stream_.lock();
	if (stream) {
		stream->Cancel();
	}
}

void OutgoingResponse::SetStatusCodeAndMessage(unsigned code, const string &message) {
	status_code_ = code;
	status_message_ = message;
}

void OutgoingResponse::SetHeader(const string &name, const string &value) {
	headers_[name] = value;
}

void OutgoingResponse::SetBodyReader(io::ReaderPtr body_reader) {
	body_reader_ = body_reader;
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

ExponentialBackoff::ExpectedInterval ExponentialBackoff::NextInterval() {
	iteration_++;

	if (try_count_ > 0 && iteration_ > try_count_) {
		return expected::unexpected(MakeError(MaxRetryError, "Exponential backoff"));
	}

	chrono::milliseconds current_interval = smallest_interval_;
	// Backoff algorithm: Each interval is returned three times, then it's doubled, and then
	// that is returned three times, and so on. But if interval is ever higher than the max
	// interval, then return the max interval instead, and once that is returned three times,
	// produce MaxRetryError. If try_count_ is set, then that controls the total number of
	// retries, but the rest is the same, so then it simply "gets stuck" at max interval for
	// many iterations.
	for (int count = 3; count < iteration_; count += 3) {
		auto new_interval = current_interval * 2;
		if (new_interval > max_interval_) {
			new_interval = max_interval_;
		}
		if (try_count_ <= 0 && new_interval == current_interval) {
			return expected::unexpected(MakeError(MaxRetryError, "Exponential backoff"));
		}
		current_interval = new_interval;
	}

	return current_interval;
}

} // namespace http
} // namespace mender
