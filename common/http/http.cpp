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

#include <algorithm>
#include <cctype>
#include <cstdlib>
#include <string>

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
	case Method::GET:
		return "GET";
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
		tmp = move(address.host);
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

size_t CaseInsensitiveHasher::operator()(const string &str) const {
	string lower_str(str.length(), ' ');
	transform(
		str.begin(), str.end(), lower_str.begin(), [](unsigned char c) { return std::tolower(c); });
	return hash<string>()(lower_str);
}

bool CaseInsensitiveComparator::operator()(const string &str1, const string &str2) const {
	return strcasecmp(str1.c_str(), str2.c_str()) < 0;
}

Request::Request(Method method) :
	method_(method) {
}

expected::ExpectedString Transaction::GetHeader(const string &name) const {
	if (headers_.find(name) == headers_.end()) {
		return expected::unexpected(MakeError(NoSuchHeaderError, "No such header: " + name));
	}
	return headers_.at(name);
}

void Request::SetHeader(const string &name, const string &value) {
	headers_[name] = value;
}

error::Error Request::SetAddress(const string &address) {
	orig_address_ = address;

	auto err = BreakDownUrl(address, address_);
	if (err) {
		ready_ = false;
	} else {
		ready_ = true;
	}
	return err;
}

void Request::SetBodyGenerator(BodyGenerator body_gen) {
	body_gen_ = body_gen;
}

Response::Response(unsigned status_code, const string &message) :
	status_code_(status_code),
	status_message_(message) {
}

void Response::SetBodyWriter(io::WriterPtr body_writer) {
	body_writer_ = body_writer;
}

} // namespace http
} // namespace mender
