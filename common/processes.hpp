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

#ifndef MENDER_COMMON_PROCESSES_HPP
#define MENDER_COMMON_PROCESSES_HPP
#include <config.h>
#include <chrono>
#include <future>
#include <memory>
#include <string>
#include <vector>

#ifdef MENDER_USE_TINY_PROC_LIB
#include <process.hpp>
#endif

#include <common/error.hpp>
#include <common/events.hpp>
#include <common/expected.hpp>
#include <common/io.hpp>
#include <common/expected.hpp>

// For friend declaration below, used in tests.
class ProcessesTestsHelper;

namespace mender {
namespace common {
namespace processes {

using namespace std;

extern const chrono::seconds DEFAULT_GENERATE_LINE_DATA_TIMEOUT;

#ifdef MENDER_USE_TINY_PROC_LIB
namespace tpl = TinyProcessLib;
#endif

namespace error = mender::common::error;
namespace events = mender::common::events;
namespace expected = mender::common::expected;
namespace io = mender::common::io;

enum ProcessesErrorCode {
	NoError = 0,
	SpawnError,
	ProcessAlreadyStartedError,
	NonZeroExitStatusError,
};

class ProcessesErrorCategoryClass : public std::error_category {
public:
	const char *name() const noexcept override;
	string message(int code) const override;
};
extern const ProcessesErrorCategoryClass ProcessesErrorCategory;

error::Error MakeError(ProcessesErrorCode code, const string &msg);

using LineData = vector<string>;
using ExpectedLineData = expected::expected<LineData, error::Error>;

using AsyncWaitHandler = function<void(int status_code)>;

using OutputCallback = function<void(const char *, size_t)>;

class Process : virtual public io::Canceller {
public:
	Process(vector<string> args);
	~Process();

	// Note: The callbacks will be called from a different thread.
	error::Error Start(
		OutputCallback stdout_callback = nullptr, OutputCallback stderr_callback = nullptr);

	int Wait();
	expected::ExpectedInt Wait(chrono::nanoseconds timeout);

	int GetExitStatus() {
		return Wait();
	};

	error::Error AsyncWait(events::EventLoop &loop, AsyncWaitHandler handler);
	// Only cancels AsyncWait, not readers. They have their own cancellers.
	void Cancel() override;

	ExpectedLineData GenerateLineData(
		chrono::nanoseconds timeout = DEFAULT_GENERATE_LINE_DATA_TIMEOUT);

	io::ExpectedAsyncReaderPtr GetAsyncStdoutReader(events::EventLoop &loop);
	io::ExpectedAsyncReaderPtr GetAsyncStderrReader(events::EventLoop &loop);

	// Terminate and make sure it is so before returning.
	int EnsureTerminated();

	void Terminate();
	void Kill();

private:
	friend class ::ProcessesTestsHelper;

	io::ExpectedAsyncReaderPtr GetProcessReader(events::EventLoop &loop, int &pipe_ref);

	void SetupAsyncWait();

#ifdef MENDER_USE_TINY_PROC_LIB
	unique_ptr<tpl::Process> proc_;

	int stdout_pipe_ {-1};
	int stderr_pipe_ {-1};

	future<int> future_exit_status_;

	struct AsyncWaitData {
		mutex data_mutex;
		events::EventLoop *event_loop {nullptr};
		AsyncWaitHandler handler;
	};
	shared_ptr<AsyncWaitData> async_wait_data_;
#endif

	vector<string> args_;
	int exit_status_ {-1};

	chrono::seconds max_termination_time_;
};

} // namespace processes
} // namespace common
} // namespace mender

#endif // MENDER_COMMON_PROCESSES_HPP
