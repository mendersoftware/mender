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

#ifndef MENDER_UPDATE_HTTP_RESUMER_HPP
#define MENDER_UPDATE_HTTP_RESUMER_HPP

#include <string>
#include <memory>
#include <vector>

#include <common/error.hpp>
#include <common/io.hpp>
#include <common/log.hpp>
#include <common/events.hpp>
#include <common/http.hpp>

namespace mender {
namespace update {
namespace http_resumer {

using namespace std;

namespace error = mender::common::error;
namespace io = mender::common::io;
namespace log = mender::common::log;
namespace events = mender::common::events;
namespace http = mender::http;

enum class DownloadResumerActiveStatus { None, Inactive, Resuming };
enum class DownloadResumerUserHandlersStatus {
	None,
	HeaderHandlerCalled,
	BodyHandlerCalled,
};

struct DownloadResumerClientState {
	DownloadResumerActiveStatus active_state {DownloadResumerActiveStatus::None};
	ssize_t content_length {0};
	ssize_t offset {0};
	DownloadResumerUserHandlersStatus user_handlers_state {DownloadResumerUserHandlersStatus::None};
};

class DownloadResumerClient;

class DownloadResumerAsyncReader : virtual public io::AsyncReader {
public:
	DownloadResumerAsyncReader(
		shared_ptr<io::AsyncReader> reader,
		shared_ptr<DownloadResumerClientState> state,
		shared_ptr<bool> cancelled,
		shared_ptr<DownloadResumerClient> resumer_client) :
		inner_reader_ {reader},
		resumer_state_ {state},
		cancelled_ {cancelled},
		logger_ {"http_resumer:reader"},
		resumer_client_ {resumer_client} {
	}

	error::Error AsyncRead(
		vector<uint8_t>::iterator start,
		vector<uint8_t>::iterator end,
		io::AsyncIoHandler handler) override;

	void Cancel() override;

private:
	error::Error AsyncReadResume();

	shared_ptr<io::AsyncReader> inner_reader_;
	shared_ptr<DownloadResumerClientState> resumer_state_;

	shared_ptr<bool> cancelled_;

	log::Logger logger_;

	weak_ptr<DownloadResumerClient> resumer_client_;

	// The header handler needs to manipulate inner_reader_ in order to replace it in
	// subsequent requests.
	friend class HeaderHandlerFunctor;
};

// Main class to download the Artifact, which will react to server
// disconnections or other sorts of short read by scheduling new HTTP
// requests with `Range` header.
// It needs to be used from a shared_ptr
class DownloadResumerClient :
	virtual public http::ClientInterface,
	public enable_shared_from_this<DownloadResumerClient> {
public:
	DownloadResumerClient(const http::ClientConfig &config, events::EventLoop &event_loop);

	virtual ~DownloadResumerClient();

	error::Error AsyncCall(
		http::OutgoingRequestPtr req,
		http::ResponseHandler header_handler,
		http::ResponseHandler body_handler) override;

	io::ExpectedAsyncReaderPtr MakeBodyAsyncReader(http::IncomingResponsePtr resp) override;

	void Cancel() override;

	http::Client &GetHttpClient() override {
		return client_;
	};

	// Set wait interval for resuming the download. For use in tests.
	void SetSmallestWaitInterval(chrono::milliseconds interval) {
		retry_.backoff.SetSmallestInterval(interval);
	};

private:
	// Generate a Range request from the original user request, requesting for the missing data
	// See https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Range
	http::OutgoingRequestPtr RemainingRangeRequest() const;

	// Schedule the next request using GetWaitCallback() from the child class
	error::Error ScheduleNextResumeRequest();

	// Takes care of not calling each user handler (header and body) more than once.
	void CallUserHandler(http::ExpectedIncomingResponsePtr exp_resp);

	void DoCancel();

	shared_ptr<DownloadResumerClientState> resumer_state_;
	weak_ptr<DownloadResumerAsyncReader> resumer_reader_;

	http::Client client_;
	log::Logger logger_;

	http::IncomingResponsePtr response_;

	// Each time we cancel something, we set this to true, and then make a new one. This ensures
	// that for everyone who has a copy, it will stay true even after a new request is made, or
	// after things have been destroyed.
	shared_ptr<bool> cancelled_;

	http::ResponseHandler user_header_handler_;
	http::ResponseHandler user_body_handler_;
	http::OutgoingRequestPtr user_request_;

	struct {
		http::ExponentialBackoff backoff;
		events::Timer wait_timer;
	} retry_;

	// Parameters from the last time that user called AsyncRead.
	// They are re-used when resuming the download
	struct {
		vector<uint8_t>::iterator start;
		vector<uint8_t>::iterator end;
		io::AsyncIoHandler handler;
	} last_read_;

	friend class DownloadResumerAsyncReader;

	friend class HeaderHandlerFunctor;
	friend class BodyHandlerFunctor;
};

} // namespace http_resumer
} // namespace update
} // namespace mender

#endif // MENDER_UPDATE_HTTP_RESUMER_HPP
