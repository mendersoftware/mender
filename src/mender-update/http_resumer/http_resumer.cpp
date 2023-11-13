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

#include <mender-update/http_resumer.hpp>

#include <regex>

#include <common/common.hpp>
#include <common/expected.hpp>

namespace mender {
namespace update {
namespace http_resumer {

namespace common = mender::common;
namespace expected = mender::common::expected;
namespace http = mender::http;

// Represents the parts of a Content-Range HTTP header
// See https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Content-Range
struct RangeHeader {
	long long int range_start {0};
	long long int range_end {0};
	long long int size {0};
};
using ExpectedRangeHeader = expected::expected<RangeHeader, error::Error>;

// Parses the HTTP Content-Range header
// For an alternative implementation without regex dependency see:
// https://github.com/mendersoftware/mender/pull/1372/commits/ea711fc4dafa943266e9013fd6704da3d4518a27
ExpectedRangeHeader ParseRangeHeader(string header) {
	RangeHeader range_header {};

	std::regex content_range_regexp {R"(bytes\s+(\d+)\s?-\s?(\d+)\s?\/?\s?(\d+|\*)?)"};

	std::smatch range_matches;
	if (!regex_match(header, range_matches, content_range_regexp)) {
		return expected::unexpected(http::MakeError(
			http::NoSuchHeaderError, "Invalid Content-Range returned from server: " + header));
	}

	auto exp_range_start = common::StringToLongLong(range_matches[1].str());
	auto exp_range_end = common::StringToLongLong(range_matches[2].str());
	if (!exp_range_start || !exp_range_end) {
		return expected::unexpected(http::MakeError(
			http::NoSuchHeaderError, "Content-Range contains invalid number: " + header));
	}
	range_header.range_start = exp_range_start.value();
	range_header.range_end = exp_range_end.value();

	if (range_header.range_start > range_header.range_end) {
		return expected::unexpected(http::MakeError(
			http::NoSuchHeaderError, "Invalid Content-Range returned from server: " + header));
	}

	if ((range_matches[3].matched) && (range_matches[3].str() != "*")) {
		auto exp_size = common::StringToLongLong(range_matches[3].str());
		if (!exp_size) {
			return expected::unexpected(http::MakeError(
				http::NoSuchHeaderError, "Content-Range contains invalid number: " + header));
		}
		range_header.size = exp_size.value();
	}

	return range_header;
}

class HeaderHandlerFunctor {
public:
	HeaderHandlerFunctor(weak_ptr<DownloadResumerClient> resumer) :
		resumer_client_ {resumer} {};

	void operator()(http::ExpectedIncomingResponsePtr exp_resp);

private:
	void HandleFirstResponse(
		const shared_ptr<DownloadResumerClient> &resumer_client,
		http::ExpectedIncomingResponsePtr exp_resp);
	void HandleNextResponse(
		const shared_ptr<DownloadResumerClient> &resumer_client,
		http::ExpectedIncomingResponsePtr exp_resp);

	weak_ptr<DownloadResumerClient> resumer_client_;
};

class BodyHandlerFunctor {
public:
	BodyHandlerFunctor(weak_ptr<DownloadResumerClient> resumer) :
		resumer_client_ {resumer} {};

	void operator()(http::ExpectedIncomingResponsePtr exp_resp);

private:
	weak_ptr<DownloadResumerClient> resumer_client_;
};

void HeaderHandlerFunctor::operator()(http::ExpectedIncomingResponsePtr exp_resp) {
	auto resumer_client = resumer_client_.lock();
	if (resumer_client) {
		// If an error has already occurred, schedule the next AsyncCall directly
		if (!exp_resp) {
			resumer_client->logger_.Warning(exp_resp.error().String());
			auto err = resumer_client->ScheduleNextResumeRequest();
			if (err != error::NoError) {
				resumer_client->logger_.Error(err.String());
				resumer_client->CallUserHandler(expected::unexpected(err));
			}
			return;
		}

		if (resumer_client->resumer_state_->active_state == DownloadResumerActiveStatus::Resuming) {
			HandleNextResponse(resumer_client, exp_resp);
		} else {
			HandleFirstResponse(resumer_client, exp_resp);
		}
	}
}

void HeaderHandlerFunctor::HandleFirstResponse(
	const shared_ptr<DownloadResumerClient> &resumer_client,
	http::ExpectedIncomingResponsePtr exp_resp) {
	// The first response shall always call the user header callback. On resumable responses, we
	// create a our own incoming response and call the user header handler. On errors, we log a
	// warning and call the user handler with the original response

	auto resp = exp_resp.value();
	if (resp->GetStatusCode() != mender::http::StatusOK) {
		// Non-resumable response
		resumer_client->CallUserHandler(exp_resp);
		return;
	}

	auto exp_header = resp->GetHeader("Content-Length");
	if (!exp_header || exp_header.value() == "0") {
		resumer_client->logger_.Warning("Response does not contain Content-Length header");
		resumer_client->CallUserHandler(exp_resp);
		return;
	}

	auto exp_length = common::StringToLongLong(exp_header.value());
	if (!exp_length || exp_length.value() < 0) {
		resumer_client->logger_.Warning(
			"Content-Length contains invalid number: " + exp_header.value());
		resumer_client->CallUserHandler(exp_resp);
		return;
	}

	// Resumable response
	resumer_client->resumer_state_->active_state = DownloadResumerActiveStatus::Resuming;
	resumer_client->resumer_state_->offset = 0;
	resumer_client->resumer_state_->content_length = exp_length.value();

	// Prepare a modified response and call user handler
	resumer_client->response_.reset(new http::IncomingResponse(*resumer_client, resp->cancelled_));
	resumer_client->response_->status_code_ = resp->GetStatusCode();
	resumer_client->response_->status_message_ = resp->GetStatusMessage();
	resumer_client->response_->headers_ = resp->GetHeaders();
	resumer_client->CallUserHandler(resumer_client->response_);
}

void HeaderHandlerFunctor::HandleNextResponse(
	const shared_ptr<DownloadResumerClient> &resumer_client,
	http::ExpectedIncomingResponsePtr exp_resp) {
	// If an error occurs during handling here, cancel the resuming and call the user handler.

	auto resp = exp_resp.value();
	auto resumer_reader = resumer_client->resumer_reader_.lock();
	if (!resumer_reader) {
		// Errors should already have been handled as part of the Cancel() inside the
		// destructor of the reader.
		return;
	}

	auto exp_content_range = resp->GetHeader("Content-Range").and_then(ParseRangeHeader);
	if (!exp_content_range) {
		resumer_client->logger_.Error(exp_content_range.error().String());
		resumer_client->CallUserHandler(expected::unexpected(exp_content_range.error()));
		return;
	}

	auto content_range = exp_content_range.value();
	if (content_range.size != 0
		&& content_range.size != resumer_client->resumer_state_->content_length) {
		auto size_changed_err = http::MakeError(
			http::DownloadResumerError,
			"Size of artifact changed after download was resumed (expected "
				+ to_string(resumer_client->resumer_state_->content_length) + ", got "
				+ to_string(content_range.size) + ")");
		resumer_client->logger_.Error(size_changed_err.String());
		resumer_client->CallUserHandler(expected::unexpected(size_changed_err));
		return;
	}

	if ((content_range.range_end != resumer_client->resumer_state_->content_length - 1)
		|| (content_range.range_start != resumer_client->resumer_state_->offset)) {
		auto bad_range_err = http::MakeError(
			http::DownloadResumerError,
			"HTTP server returned an different range than requested. Requested "
				+ to_string(resumer_client->resumer_state_->offset) + "-"
				+ to_string(resumer_client->resumer_state_->content_length - 1) + ", got "
				+ to_string(content_range.range_start) + "-" + to_string(content_range.range_end));
		resumer_client->logger_.Error(bad_range_err.String());
		resumer_client->CallUserHandler(expected::unexpected(bad_range_err));
		return;
	}

	// Get the reader for the new response
	auto exp_reader = resumer_client->client_.MakeBodyAsyncReader(resp);
	if (!exp_reader) {
		auto bad_range_err = exp_reader.error().WithContext("cannot get the reader after resume");
		resumer_client->logger_.Error(bad_range_err.String());
		resumer_client->CallUserHandler(expected::unexpected(bad_range_err));
		return;
	}
	// Update the inner reader of the user reader
	resumer_reader->inner_reader_ = exp_reader.value();

	// Resume reading reusing last user data (start, end, handler)
	auto err = resumer_reader->AsyncReadResume();
	if (err != error::NoError) {
		auto bad_read_err = err.WithContext("error reading after resume");
		resumer_client->logger_.Error(bad_read_err.String());
		resumer_client->CallUserHandler(expected::unexpected(bad_read_err));
		return;
	}
}

void BodyHandlerFunctor::operator()(http::ExpectedIncomingResponsePtr exp_resp) {
	auto resumer_client = resumer_client_.lock();
	if (!resumer_client) {
		return;
	}

	if (*resumer_client->cancelled_) {
		resumer_client->CallUserHandler(exp_resp);
		return;
	}

	if (resumer_client->resumer_state_->active_state == DownloadResumerActiveStatus::Inactive) {
		resumer_client->CallUserHandler(exp_resp);
		return;
	}

	// We resume the download if either:
	// * there is any error or
	// * successful read with status code Partial Content and there is still data missing
	const bool is_range_response =
		exp_resp && exp_resp.value()->GetStatusCode() == mender::http::StatusPartialContent;
	const bool is_data_missing =
		resumer_client->resumer_state_->offset < resumer_client->resumer_state_->content_length;
	if (!exp_resp || (is_range_response && is_data_missing)) {
		if (!exp_resp) {
			auto resumer_reader = resumer_client->resumer_reader_.lock();
			if (resumer_reader) {
				resumer_reader->inner_reader_.reset();
			}
			if (exp_resp.error().code == make_error_condition(errc::operation_canceled)) {
				// We don't want to resume cancelled requests, as these were
				// cancelled for a reason.
				resumer_client->CallUserHandler(exp_resp);
				return;
			}
			resumer_client->logger_.Info(
				"Will try to resume after error " + exp_resp.error().String());
		}

		auto err = resumer_client->ScheduleNextResumeRequest();
		if (err != error::NoError) {
			resumer_client->logger_.Error(err.String());
			resumer_client->CallUserHandler(expected::unexpected(err));
			return;
		}
	} else {
		// Update headers with the last received server response. When resuming has taken place,
		// the user will get different headers on header and body handlers, representing (somehow)
		// what the resumer has been doing in its behalf.
		auto resp = exp_resp.value();
		resumer_client->response_->status_code_ = resp->GetStatusCode();
		resumer_client->response_->status_message_ = resp->GetStatusMessage();
		resumer_client->response_->headers_ = resp->GetHeaders();

		// Finished, call the user handler \o/
		resumer_client->logger_.Debug("Download resumed and completed successfully");
		resumer_client->CallUserHandler(resumer_client->response_);
	}
}

DownloadResumerAsyncReader::~DownloadResumerAsyncReader() {
	Cancel();
}

void DownloadResumerAsyncReader::Cancel() {
	auto resumer_client = resumer_client_.lock();
	if (!*cancelled_ && resumer_client) {
		resumer_client->Cancel();
	}
}

error::Error DownloadResumerAsyncReader::AsyncRead(
	vector<uint8_t>::iterator start, vector<uint8_t>::iterator end, io::AsyncIoHandler handler) {
	if (eof_) {
		handler(0);
		return error::NoError;
	}

	auto resumer_client = resumer_client_.lock();
	if (!resumer_client || *cancelled_) {
		return error::MakeError(
			error::ProgrammingError,
			"DownloadResumerAsyncReader::AsyncRead called after stream is destroyed");
	}
	// Save user parameters for further resumes of the body read
	resumer_client->last_read_ = {.start = start, .end = end, .handler = handler};
	return AsyncReadResume();
}

error::Error DownloadResumerAsyncReader::AsyncReadResume() {
	auto resumer_client = resumer_client_.lock();
	if (!resumer_client) {
		return error::MakeError(
			error::ProgrammingError,
			"DownloadResumerAsyncReader::AsyncReadResume called after client is destroyed");
	}
	return inner_reader_->AsyncRead(
		resumer_client->last_read_.start,
		resumer_client->last_read_.end,
		[this](io::ExpectedSize result) {
			if (!result) {
				logger_.Warning(
					"Reading error, a new request will be re-scheduled. "
					+ result.error().String());
			} else {
				if (result.value() == 0) {
					eof_ = true;
				}
				resumer_state_->offset += result.value();
				logger_.Debug("read " + to_string(result.value()) + " bytes");
				auto resumer_client = resumer_client_.lock();
				if (resumer_client) {
					resumer_client->last_read_.handler(result);
				} else {
					logger_.Error(
						"AsyncRead finish handler called after resumer client has been destroyed.");
				}
			}
		});
}

DownloadResumerClient::DownloadResumerClient(
	const http::ClientConfig &config, events::EventLoop &event_loop) :
	resumer_state_ {make_shared<DownloadResumerClientState>()},
	client_(config, event_loop, "http_resumer:client"),
	logger_ {"http_resumer:client"},
	cancelled_ {make_shared<bool>(true)},
	retry_ {
		.backoff = http::ExponentialBackoff(chrono::minutes(1), 10),
		.wait_timer = events::Timer(event_loop)} {
}

DownloadResumerClient::~DownloadResumerClient() {
	if (!*cancelled_) {
		logger_.Warning("DownloadResumerClient destroyed while request is still active!");
	}
	client_.Cancel();
}

error::Error DownloadResumerClient::AsyncCall(
	http::OutgoingRequestPtr req,
	http::ResponseHandler user_header_handler,
	http::ResponseHandler user_body_handler) {
	HeaderHandlerFunctor resumer_header_handler {shared_from_this()};
	BodyHandlerFunctor resumer_body_handler {shared_from_this()};

	user_request_ = req;
	user_header_handler_ = user_header_handler;
	user_body_handler_ = user_body_handler;

	if (!*cancelled_) {
		return error::Error(
			make_error_condition(errc::operation_in_progress), "HTTP resumer call already ongoing");
	}

	*cancelled_ = false;
	retry_.backoff.Reset();
	resumer_state_->active_state = DownloadResumerActiveStatus::Inactive;
	resumer_state_->user_handlers_state = DownloadResumerUserHandlersStatus::None;
	return client_.AsyncCall(req, resumer_header_handler, resumer_body_handler);
}

io::ExpectedAsyncReaderPtr DownloadResumerClient::MakeBodyAsyncReader(
	http::IncomingResponsePtr resp) {
	auto exp_reader = client_.MakeBodyAsyncReader(resp);
	if (!exp_reader) {
		return exp_reader;
	}
	auto resumer_reader = make_shared<DownloadResumerAsyncReader>(
		exp_reader.value(), resumer_state_, cancelled_, shared_from_this());
	resumer_reader_ = resumer_reader;
	return resumer_reader;
}

http::OutgoingRequestPtr DownloadResumerClient::RemainingRangeRequest() const {
	auto range_req = make_shared<http::OutgoingRequest>(*user_request_);
	range_req->SetHeader(
		"Range",
		"bytes=" + to_string(resumer_state_->offset) + "-"
			+ to_string(resumer_state_->content_length - 1));
	return range_req;
};

error::Error DownloadResumerClient::ScheduleNextResumeRequest() {
	auto exp_interval = retry_.backoff.NextInterval();
	if (!exp_interval) {
		return http::MakeError(
			http::DownloadResumerError,
			"Giving up on resuming the download: " + exp_interval.error().String());
	}

	auto interval = exp_interval.value();
	logger_.Info(
		"Resuming download after "
		+ to_string(chrono::duration_cast<chrono::seconds>(interval).count()) + " seconds");

	HeaderHandlerFunctor resumer_next_header_handler {shared_from_this()};
	BodyHandlerFunctor resumer_next_body_handler {shared_from_this()};

	retry_.wait_timer.AsyncWait(
		interval, [this, resumer_next_header_handler, resumer_next_body_handler](error::Error err) {
			if (err != error::NoError) {
				auto err_user = http::MakeError(
					http::DownloadResumerError, "Unexpected error in wait timer: " + err.String());
				logger_.Error(err_user.String());
				CallUserHandler(expected::unexpected(err_user));
				return;
			}

			auto next_call_err = client_.AsyncCall(
				RemainingRangeRequest(), resumer_next_header_handler, resumer_next_body_handler);
			if (next_call_err != error::NoError) {
				// Schedule once more
				auto err = ScheduleNextResumeRequest();
				if (err != error::NoError) {
					logger_.Error(err.String());
					CallUserHandler(expected::unexpected(err));
				}
			}
		});

	return error::NoError;
}

void DownloadResumerClient::CallUserHandler(http::ExpectedIncomingResponsePtr exp_resp) {
	if (!exp_resp) {
		DoCancel();
	}
	if (resumer_state_->user_handlers_state == DownloadResumerUserHandlersStatus::None) {
		resumer_state_->user_handlers_state =
			DownloadResumerUserHandlersStatus::HeaderHandlerCalled;
		user_header_handler_(exp_resp);
	} else if (
		resumer_state_->user_handlers_state
		== DownloadResumerUserHandlersStatus::HeaderHandlerCalled) {
		resumer_state_->user_handlers_state = DownloadResumerUserHandlersStatus::BodyHandlerCalled;
		DoCancel();
		user_body_handler_(exp_resp);
	} else {
		string msg;
		if (!exp_resp) {
			msg = "error: " + exp_resp.error().String();
		} else {
			auto &resp = exp_resp.value();
			msg = "response: " + to_string(resp->GetStatusCode()) + " " + resp->GetStatusMessage();
		}
		logger_.Warning("Cannot call any user handler with " + msg);
	}
}

void DownloadResumerClient::Cancel() {
	DoCancel();
	client_.Cancel();
};

void DownloadResumerClient::DoCancel() {
	// Set cancel state and then make a new one. Those who are interested should have their own
	// pointer to the old one.
	*cancelled_ = true;
	cancelled_ = make_shared<bool>(true);
};

} // namespace http_resumer
} // namespace update
} // namespace mender
