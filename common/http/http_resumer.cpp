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

#include <regex>

#include <common/http.hpp>

#include <common/common.hpp>

namespace mender {
namespace http {

namespace common = mender::common;

// Represents the parts of a Content-Range HTTP header
// See https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Content-Range
struct RangeHeader {
	long long int range_start {0};
	long long int range_end {0};
	long long int size {0};
};
using ExpectedRangeHeader = expected::expected<RangeHeader, error::Error>;

ExpectedRangeHeader ParseRangeHeader(string header) {
	RangeHeader range_header {};

	if (header.rfind("bytes ") != 0) {
		return expected::unexpected(
			MakeError(NoSuchHeaderError, "Invalid Content-Range returned from server: " + header));
	}

	auto content = header.substr(string("bytes ").length(), header.length());

	// Split 100-200/300 into range (100-200) and size (300)
	auto range_and_size = common::SplitString(content, "/");
	if (range_and_size.size() > 2) {
		return expected::unexpected(
			MakeError(NoSuchHeaderError, "Invalid Content-Range returned from server: " + header));
	} else if (range_and_size.size() == 2) {
		if (range_and_size[1] != "*") {
			auto exp_size = common::StringToLongLong(range_and_size[1]);
			if (!exp_size) {
				return expected::unexpected(MakeError(
					NoSuchHeaderError, "Content-Range contains invalid number: " + content));
			}
			range_header.size = exp_size.value();
		}
		content = range_and_size[0];
	}

	// Split 100-200 into range start (100) and end (200)
	auto start_and_end = common::SplitString(content, "-");
	if (start_and_end.size() != 2) {
		return expected::unexpected(
			MakeError(NoSuchHeaderError, "Invalid Content-Range returned from server: " + content));
	}

	auto exp_range_start = common::StringToLongLong(start_and_end[0]);
	auto exp_range_end = common::StringToLongLong(start_and_end[1]);
	if (!exp_range_start || !exp_range_end) {
		return expected::unexpected(
			MakeError(NoSuchHeaderError, "Content-Range contains invalid number: " + content));
	}
	range_header.range_start = exp_range_start.value();
	range_header.range_end = exp_range_end.value();

	if (range_header.range_start > range_header.range_end) {
		return expected::unexpected(
			MakeError(NoSuchHeaderError, "Invalid Content-Range returned from server: " + content));
	}

	return range_header;
}

// Base class for handler functors (header and body)
class HandlerFunctor {
protected:
	HandlerFunctor(
		ResponseHandler user_header_handler,
		ResponseHandler user_body_handler,
		OutgoingRequestPtr user_request,
		DownloadResumerClient &client) :
		user_header_handler_ {user_header_handler},
		user_body_handler_ {user_body_handler},
		user_request_ {user_request},
		client_ {client} {};

	virtual ~HandlerFunctor() = default;

	// Entrypoint. Must be implemented by child classes.
	virtual void operator()(ExpectedIncomingResponsePtr exp_resp) = 0;

	// Return a wait callback to be passed to the timer for successive calls. Must be implemented
	// by child classes.
	virtual events::EventHandler GetWaitCallback() = 0;

	// Generate a Range request from the original user request, requesting for the missing data
	// See https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Range
	OutgoingRequestPtr RemainingRangeRequest() const;

	// Schedule the next request using GetWaitCallback() from the child class
	error::Error ScheduleNextResumeRequest();

protected:
	ResponseHandler user_header_handler_;
	ResponseHandler user_body_handler_;
	OutgoingRequestPtr user_request_;
	DownloadResumerClient &client_;
};

class HeaderHandlerFunctor : virtual public HandlerFunctor {
public:
	HeaderHandlerFunctor(
		ResponseHandler user_header_handler,
		ResponseHandler user_body_handler,
		OutgoingRequestPtr user_request,
		DownloadResumerClient &client) :
		HandlerFunctor(user_header_handler, user_body_handler, user_request, client) {};

	void operator()(ExpectedIncomingResponsePtr exp_resp) override;
	events::EventHandler GetWaitCallback() override;

private:
	void HandleFirstResponse(ExpectedIncomingResponsePtr exp_resp);
	void HandleNextResponse(ExpectedIncomingResponsePtr exp_resp);
};

class BodyHandlerFunctor : virtual public HandlerFunctor {
public:
	BodyHandlerFunctor(
		ResponseHandler user_header_handler,
		ResponseHandler user_body_handler,
		OutgoingRequestPtr user_request,
		DownloadResumerClient &client) :
		HandlerFunctor(user_header_handler, user_body_handler, user_request, client) {};

	void operator()(ExpectedIncomingResponsePtr exp_resp) override;
	events::EventHandler GetWaitCallback() override;
};

OutgoingRequestPtr HandlerFunctor::RemainingRangeRequest() const {
	auto range_req = make_shared<OutgoingRequest>(*user_request_);
	range_req->SetHeader(
		"Range",
		"bytes=" + to_string(client_.state_.offset) + "-"
			+ to_string(client_.state_.content_length - 1));
	return range_req;
};

error::Error HandlerFunctor::ScheduleNextResumeRequest() {
	auto exp_interval = client_.retry_.backoff.NextInterval();
	if (!exp_interval) {
		return MakeError(
			DownloadResumerError,
			"Giving up on resuming the download: " + exp_interval.error().String());
	}

	auto interval = exp_interval.value();
	client_.logger_.Info(
		"Resuming download after " + to_string(chrono::milliseconds(interval).count() / 1000)
		+ " seconds");

	client_.retry_.wait_timer.AsyncWait(interval, GetWaitCallback());

	return error::NoError;
}

events::EventHandler HeaderHandlerFunctor::GetWaitCallback() {
	HeaderHandlerFunctor resumer_next_header_handler {
		user_header_handler_, user_body_handler_, user_request_, client_};
	BodyHandlerFunctor resumer_next_body_handler {
		user_header_handler_, user_body_handler_, user_request_, client_};

	return [resumer_next_header_handler,
			resumer_next_body_handler,
			range_req = RemainingRangeRequest()](error::Error err) {
		if (err != error::NoError) {
			auto err_user =
				MakeError(DownloadResumerError, "Unexpected error in wait timer: " + err.String());
			resumer_next_header_handler.client_.DisableWithError(err_user);
			resumer_next_header_handler.user_body_handler_(expected::unexpected(err_user));
			return;
		}

		auto next_call_err = resumer_next_header_handler.client_.client_.AsyncCall(
			range_req, resumer_next_header_handler, resumer_next_body_handler);
		if (next_call_err != error::NoError) {
			auto err_user = MakeError(
				DownloadResumerError,
				"Failed to schedule the next resumer call: " + next_call_err.String());
			resumer_next_header_handler.client_.DisableWithError(err_user);
			resumer_next_header_handler.user_body_handler_(expected::unexpected(err_user));
		}
	};
}

void HeaderHandlerFunctor::operator()(ExpectedIncomingResponsePtr exp_resp) {
	if (!client_.IsOngoing()) {
		HandleFirstResponse(exp_resp);
	} else {
		HandleNextResponse(exp_resp);
	}

	if (client_.IsOngoing() && exp_resp) {
		// Set resumer BodyWriter
		auto resp = exp_resp.value();
		auto body_writer =
			make_shared<io::ByteOffsetWriter>(client_.state_.buffer, client_.state_.buffer->size());
		body_writer.get()->SetUnlimited(true);
		resp->SetBodyWriter(body_writer);
	}
}

void HeaderHandlerFunctor::HandleFirstResponse(ExpectedIncomingResponsePtr exp_resp) {
	// The first response shall call the user header callback. On errors, we log at Warning level,
	// disable the functionality, and call the user handler with the original response

	if (!exp_resp) {
		client_.DisableWithWarning(exp_resp.error().String());
		user_header_handler_(exp_resp);
		return;
	}
	auto resp = exp_resp.value();

	if (resp->GetStatusCode() != mender::http::StatusOK) {
		client_.DisableWithWarning("Unexpected status code " + to_string(resp->GetStatusCode()));
		user_header_handler_(exp_resp);
		return;
	}

	auto exp_header = resp->GetHeader("Content-Length");
	if (!exp_header || exp_header.value() == "0") {
		client_.DisableWithWarning("Response does not contain Content-Length header");
		user_header_handler_(exp_resp);
		return;
	}

	auto exp_length = common::StringToLongLong(exp_header.value());
	if (!exp_length || exp_length.value() < 0) {
		client_.DisableWithWarning("Content-Length contains invalid number: " + exp_header.value());
		user_header_handler_(exp_resp);
		return;
	}

	// Prepare state and call user handler
	client_.state_ = {.ongoing = true};
	client_.state_.content_length = exp_length.value();
	client_.state_.offset = 0;
	user_header_handler_(exp_resp);

	io::WriterPtr user_writer = resp->GetBodyWriter();
	if (!user_writer) {
		client_.DisableWithWarning("The user did not set a BodyWriter to write the data into");
		return;
	}
	client_.state_.user_body_writer = user_writer;
}
void HeaderHandlerFunctor::HandleNextResponse(ExpectedIncomingResponsePtr exp_resp) {
	// Subsequent responses shall not call the user handler (with one exception).
	// If an error occurs, either schedule the next AsyncCall directly or save it
	// and let the body handler forward it to the user.
	// The exception is when the actual re-scheduling fails, where we need to call
	// the user body handler as we are sure we won't be re-scheduled again.
	if (!exp_resp) {
		client_.logger_.Warning(exp_resp.error().String());

		auto err = ScheduleNextResumeRequest();
		if (err != error::NoError) {
			client_.DisableWithError(err);
			user_body_handler_(expected::unexpected(err));
		}
		return;
	}
	auto resp = exp_resp.value();

	auto exp_content_range = resp->GetHeader("Content-Range").and_then(ParseRangeHeader);
	if (!exp_content_range) {
		client_.DisableWithError(exp_content_range.error());
		return;
	}

	auto content_range = exp_content_range.value();
	if (content_range.size != 0 && content_range.size != client_.state_.content_length) {
		client_.DisableWithError(MakeError(
			DownloadResumerError,
			"Size of artifact changed after download was resumed (expected "
				+ to_string(client_.state_.content_length) + ", got "
				+ to_string(content_range.size) + ")"));
		return;
	}

	if ((content_range.range_end != client_.state_.content_length - 1)
		|| (content_range.range_start != client_.state_.offset)) {
		client_.DisableWithError(MakeError(
			DownloadResumerError,
			"HTTP server returned an different range than requested. Requested "
				+ to_string(client_.state_.offset) + "-"
				+ to_string(client_.state_.content_length - 1) + ", got "
				+ to_string(content_range.range_start) + "-" + to_string(content_range.range_end)));
	}
}

events::EventHandler BodyHandlerFunctor::GetWaitCallback() {
	HeaderHandlerFunctor resumer_next_header_handler {
		user_header_handler_, user_body_handler_, user_request_, client_};
	BodyHandlerFunctor resumer_next_body_handler {
		user_header_handler_, user_body_handler_, user_request_, client_};

	return [resumer_next_header_handler,
			resumer_next_body_handler,
			range_req = RemainingRangeRequest()](error::Error err) {
		if (err != error::NoError) {
			resumer_next_body_handler.client_.DisableWithError(
				MakeError(DownloadResumerError, "Unexpected error in wait timer: " + err.String()));
			return;
		}

		auto next_call_err = resumer_next_body_handler.client_.client_.AsyncCall(
			range_req, resumer_next_header_handler, resumer_next_body_handler);
		if (next_call_err != error::NoError) {
			resumer_next_body_handler.client_.DisableWithError(MakeError(
				DownloadResumerError,
				"Failed to schedule the next resumer call: " + next_call_err.String()));
		}
	};
}

void BodyHandlerFunctor::operator()(ExpectedIncomingResponsePtr exp_resp) {
	// It was intentionally cancelled or otherwise the header handler errored.
	if (!client_.IsOngoing()) {
		if (client_.state_.err != error::NoError) {
			user_body_handler_(expected::unexpected(client_.state_.err));
		}
		return;
	}

	// Copy data from the current HTTP response to the user
	auto err = io::Copy(
		*client_.state_.user_body_writer.get(), *client_.state_.resumer_buffer_reader.get());
	if (err != error::NoError) {
		auto err_user =
			MakeError(DownloadResumerError, "Failed to copy data to user writer: " + err.String());
		client_.DisableWithError(err_user);
		user_body_handler_(expected::unexpected(err_user));
		return;
	}

	// We resume the download if either:
	// * there is any error or
	// * successful read with status code Partial Content and there is still data missing
	const bool is_range_response =
		exp_resp && exp_resp.value().get()->GetStatusCode() == mender::http::StatusPartialContent;
	const auto buffer_size = (size_t) client_.state_.buffer.get()->size();
	const bool is_data_missing = buffer_size < client_.state_.content_length;
	if (!exp_resp || (is_range_response && is_data_missing)) {
		// Update resume offset
		client_.state_.offset = buffer_size;
		client_.logger_.Trace(
			"Received " + to_string(buffer_size) + " bytes in buffer, missing "
			+ to_string(client_.state_.content_length - buffer_size) + " bytes, resuming from byte "
			+ to_string(client_.state_.offset));

		auto err = ScheduleNextResumeRequest();
		if (err != error::NoError) {
			client_.DisableWithError(err);
			user_body_handler_(expected::unexpected(err));
			return;
		}
	} else {
		// Finished, call the user handler
		client_.DisableWithError(error::NoError);
		user_body_handler_(exp_resp);
	}
}

void DownloadResumerClient::Cancel() {
	DisableWithError(error::NoError);
	client_.Cancel();
};

void DownloadResumerClient::DisableWithWarning(string reason) {
	logger_.Warning(reason);
	state_.ongoing = false;
	retry_.backoff.Reset();
};
void DownloadResumerClient::DisableWithError(error::Error err) {
	if (err != error::NoError) {
		logger_.Error(err.String());
		state_.err = err;
	}
	state_.ongoing = false;
	retry_.backoff.Reset();
};

error::Error DownloadResumerClient::AsyncCall(
	OutgoingRequestPtr req,
	ResponseHandler user_header_handler,
	ResponseHandler user_body_handler) {
	HeaderHandlerFunctor resumer_header_handler {
		user_header_handler, user_body_handler, req, *this};
	BodyHandlerFunctor resumer_body_handler {user_header_handler, user_body_handler, req, *this};

	return [this, req, resumer_header_handler, resumer_body_handler]() {
		return client_.AsyncCall(req, resumer_header_handler, resumer_body_handler);
	}();
}

} // namespace http
} // namespace mender
