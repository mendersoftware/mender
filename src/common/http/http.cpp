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
	case BodyIgnoredError:
		return "HTTP stream contains a body, but a reader has not been created for it";
	case HTTPInitError:
		return "Failed to initialize the client";
	case UnsupportedMethodError:
		return "Unsupported HTTP method";
	case StreamCancelledError:
		return "Stream has been cancelled/destroyed";
	case UnsupportedBodyType:
		return "HTTP stream has a body type we don't understand";
	case MaxRetryError:
		return "Tried maximum number of times";
	case DownloadResumerError:
		return "Resume download error";
	case ProxyError:
		return "Proxy error";
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

	if (address.host.find("@") != string::npos) {
		address = {};
		return error::Error(
			make_error_condition(errc::not_supported),
			"URL Username and password is not supported");
	}

	split_index = address.host.find(":");
	if (split_index != string::npos) {
		tmp = std::move(address.host);
		address.host = tmp.substr(0, split_index);

		tmp = tmp.substr(split_index + 1);
		auto port = common::StringToLongLong(tmp);
		if (!port) {
			address = {};
			return error::Error(port.error().code, url + " contains invalid port number");
		}
		address.port = port.value();
	} else {
		if (address.protocol == "http") {
			address.port = 80;
		} else if (address.protocol == "https") {
			address.port = 443;
		} else {
			address = {};
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

string Request::GetHost() const {
	return address_.host;
}

string Request::GetProtocol() const {
	return address_.protocol;
}

int Request::GetPort() const {
	return address_.port;
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

void BaseOutgoingRequest::SetMethod(Method method) {
	method_ = method;
}

void BaseOutgoingRequest::SetHeader(const string &name, const string &value) {
	headers_[name] = value;
}

void BaseOutgoingRequest::SetBodyGenerator(BodyGenerator body_gen) {
	async_body_gen_ = nullptr;
	async_body_reader_ = nullptr;
	body_gen_ = body_gen;
}

void BaseOutgoingRequest::SetAsyncBodyGenerator(AsyncBodyGenerator body_gen) {
	body_gen_ = nullptr;
	body_reader_ = nullptr;
	async_body_gen_ = body_gen;
}

error::Error OutgoingRequest::SetAddress(const string &address) {
	orig_address_ = address;

	return BreakDownUrl(address, address_);
}

IncomingRequest::~IncomingRequest() {
	if (!*cancelled_) {
		stream_.server_.RemoveStream(stream_.shared_from_this());
	}
}

void IncomingRequest::Cancel() {
	if (!*cancelled_) {
		stream_.Cancel();
	}
}

io::ExpectedAsyncReaderPtr IncomingRequest::MakeBodyAsyncReader() {
	if (*cancelled_) {
		return expected::unexpected(MakeError(
			StreamCancelledError, "Cannot make reader for a request that doesn't exist anymore"));
	}
	return stream_.server_.MakeBodyAsyncReader(shared_from_this());
}

void IncomingRequest::SetBodyWriter(io::WriterPtr writer) {
	auto exp_reader = MakeBodyAsyncReader();
	if (!exp_reader) {
		if (exp_reader.error().code != MakeError(BodyMissingError, "").code) {
			log::Error(exp_reader.error().String());
		}
		return;
	}
	auto &reader = exp_reader.value();

	io::AsyncCopy(writer, reader, [reader](error::Error err) {
		if (err != error::NoError) {
			log::Error("Could not copy HTTP stream: " + err.String());
		}
	});
}

ExpectedOutgoingResponsePtr IncomingRequest::MakeResponse() {
	if (*cancelled_) {
		return expected::unexpected(MakeError(
			StreamCancelledError, "Cannot make response for a request that doesn't exist anymore"));
	}
	return stream_.server_.MakeResponse(shared_from_this());
}

IncomingResponse::IncomingResponse(ClientInterface &client, shared_ptr<bool> cancelled) :
	client_ {client},
	cancelled_ {cancelled} {
}

void IncomingResponse::Cancel() {
	if (!*cancelled_) {
		client_.Cancel();
	}
}

io::ExpectedAsyncReaderPtr IncomingResponse::MakeBodyAsyncReader() {
	if (*cancelled_) {
		return expected::unexpected(MakeError(
			StreamCancelledError, "Cannot make reader for a response that doesn't exist anymore"));
	}
	return client_.MakeBodyAsyncReader(shared_from_this());
}

void IncomingResponse::SetBodyWriter(io::WriterPtr writer) {
	auto exp_reader = MakeBodyAsyncReader();
	if (!exp_reader) {
		if (exp_reader.error().code != MakeError(BodyMissingError, "").code) {
			log::Error(exp_reader.error().String());
		}
		return;
	}
	auto &reader = exp_reader.value();

	io::AsyncCopy(writer, reader, [reader](error::Error err) {
		if (err != error::NoError) {
			log::Error("Could not copy HTTP stream: " + err.String());
		}
	});
}

io::ExpectedAsyncReadWriterPtr IncomingResponse::SwitchProtocol() {
	if (*cancelled_) {
		return expected::unexpected(MakeError(
			StreamCancelledError, "Cannot switch protocol when the stream doesn't exist anymore"));
	}
	return client_.GetHttpClient().SwitchProtocol(shared_from_this());
}

OutgoingResponse::~OutgoingResponse() {
	if (!*cancelled_) {
		stream_.server_.RemoveStream(stream_.shared_from_this());
	}
}

void OutgoingResponse::Cancel() {
	if (!*cancelled_) {
		stream_.Cancel();
		stream_.server_.RemoveStream(stream_.shared_from_this());
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
	async_body_reader_ = nullptr;
	body_reader_ = body_reader;
}

void OutgoingResponse::SetAsyncBodyReader(io::AsyncReaderPtr body_reader) {
	body_reader_ = nullptr;
	async_body_reader_ = body_reader;
}

error::Error OutgoingResponse::AsyncReply(ReplyFinishedHandler reply_finished_handler) {
	if (*cancelled_) {
		return MakeError(StreamCancelledError, "Cannot reply when response doesn't exist anymore");
	}
	return stream_.server_.AsyncReply(shared_from_this(), reply_finished_handler);
}

error::Error OutgoingResponse::AsyncSwitchProtocol(SwitchProtocolHandler handler) {
	if (*cancelled_) {
		return MakeError(
			StreamCancelledError, "Cannot switch protocol when response doesn't exist anymore");
	}
	return stream_.server_.AsyncSwitchProtocol(shared_from_this(), handler);
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

static expected::ExpectedString GetProxyStringFromEnvironment(
	const string &primary, const string &secondary) {
	bool primary_set = false, secondary_set = false;

	if (getenv(primary.c_str()) != nullptr && getenv(primary.c_str())[0] != '\0') {
		primary_set = true;
	}
	if (getenv(secondary.c_str()) != nullptr && getenv(secondary.c_str())[0] != '\0') {
		secondary_set = true;
	}

	if (primary_set && secondary_set) {
		return expected::unexpected(error::Error(
			make_error_condition(errc::invalid_argument),
			primary + " and " + secondary
				+ " environment variables can't both be set at the same time"));
	} else if (primary_set) {
		return getenv(primary.c_str());
	} else if (secondary_set) {
		return getenv(secondary.c_str());
	} else {
		return "";
	}
}

// The proxy variables aren't standardized, but this page was useful for the common patterns:
// https://superuser.com/questions/944958/are-http-proxy-https-proxy-and-no-proxy-environment-variables-standard
expected::ExpectedString GetHttpProxyStringFromEnvironment() {
	if (getenv("REQUEST_METHOD") != nullptr && getenv("HTTP_PROXY") != nullptr) {
		return expected::unexpected(error::Error(
			make_error_condition(errc::operation_not_permitted),
			"Using REQUEST_METHOD (CGI) together with HTTP_PROXY is insecure. See https://github.com/golang/go/issues/16405"));
	}
	return GetProxyStringFromEnvironment("http_proxy", "HTTP_PROXY");
}

expected::ExpectedString GetHttpsProxyStringFromEnvironment() {
	return GetProxyStringFromEnvironment("https_proxy", "HTTPS_PROXY");
}

expected::ExpectedString GetNoProxyStringFromEnvironment() {
	return GetProxyStringFromEnvironment("no_proxy", "NO_PROXY");
}

// The proxy variables aren't standardized, but this page was useful for the common patterns:
// https://superuser.com/questions/944958/are-http-proxy-https-proxy-and-no-proxy-environment-variables-standard
bool HostNameMatchesNoProxy(const string &host, const string &no_proxy) {
	auto entries = common::SplitString(no_proxy, " ");
	for (string &entry : entries) {
		if (entry[0] == '.') {
			// Wildcard.
			ssize_t wildcard_len = entry.size() - 1;
			if (wildcard_len == 0
				|| entry.compare(0, wildcard_len, host, host.size() - wildcard_len)) {
				return true;
			}
		} else if (host == entry) {
			return true;
		}
	}

	return false;
}

} // namespace http
} // namespace mender
