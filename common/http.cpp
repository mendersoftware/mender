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

#include <common/common.hpp>
#include <common/http.hpp>

#include <cstdlib>

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

string VerbToString(Verb verb) {
	switch (verb) {
	case Verb::GET:
		return "GET";
	case Verb::POST:
		return "POST";
	case Verb::PUT:
		return "PUT";
	case Verb::PATCH:
		return "PATCH";
	case Verb::CONNECT:
		return "CONNECT";
	}
	// Don't use "default" case. This should generate a warning if we ever add any verbs. But
	// still assert here for safety.
	assert(false);
	return "INVALID_VERB";
}

Request::Request(Verb method) :
	method_(method) {
}

expected::ExpectedString Transaction::GetHeader(const string &name) const {
	if (headers_.find(name) == headers_.end()) {
		return MakeError(NoSuchHeaderError, "No such header: " + name);
	}
	return headers_.at(name);
}

void Request::SetHeader(const string &name, const string &value) {
	headers_[name] = value;
}

error::Error Request::SetAddress(const string &address) {
	ready_ = false;

	const string url_split {"://"};

	auto split_index = address.find(url_split);
	if (split_index == string::npos) {
		return MakeError(InvalidUrlError, address + " is not a valid URL.");
	}
	if (split_index == 0) {
		return MakeError(InvalidUrlError, address + ": missing hostname");
	}

	protocol_ = address.substr(0, split_index);

	auto tmp = address.substr(split_index + url_split.size());
	split_index = tmp.find("/");
	if (split_index == string::npos) {
		host_ = tmp;
		path_ = "/";
	} else {
		host_ = tmp.substr(0, split_index);
		path_ = tmp.substr(split_index);
	}

	split_index = host_.find(":");
	if (split_index != string::npos) {
		tmp = move(host_);
		host_ = tmp.substr(0, split_index);

		tmp = tmp.substr(split_index + 1);
		auto port = common::StringToLong(tmp);
		if (!port) {
			return error::Error(port.error().code, address + " contains invalid port number");
		}
		port_ = port.value();
	} else {
		if (protocol_ == "http") {
			port_ = 80;
		} else if (protocol_ == "https") {
			port_ = 443;
		} else {
			return error::Error(
				make_error_condition(errc::protocol_not_supported),
				"Cannot deduce port number from protocol " + protocol_);
		}
	}

	log::Trace(
		"URL broken down into (protocol: " + protocol_ + "), (host: " + host_
		+ "), (port: " + to_string(port_) + "), (path: " + path_ + ")");

	orig_address_ = address;

	ready_ = true;
	return error::NoError;
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
