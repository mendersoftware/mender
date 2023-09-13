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

using AsyncWaitHandler = function<void(error::Error err)>;

using OutputCallback = function<void(const char *, size_t)>;

class OutputHandler {
public:
	void operator()(const char *data, size_t size);

	string prefix;
};

class Process : virtual public io::Canceller {
public:
	Process(vector<string> args);
	~Process();

	// Only takes effect at the next process launch.
	void SetWorkDir(const string &path) {
		work_dir_ = path;
	}

	// Note: The callbacks will be called from a different thread.
	error::Error Start(
		OutputCallback stdout_callback = nullptr, OutputCallback stderr_callback = nullptr);

	// If Start() returns an error, it will be logged, and process returns 255.
	error::Error Run();

	error::Error Wait();
	error::Error Wait(chrono::nanoseconds timeout);

	int GetExitStatus() {
		Wait();
		return exit_status_;
	};

	error::Error AsyncWait(events::EventLoop &loop, AsyncWaitHandler handler);
	error::Error AsyncWait(
		events::EventLoop &loop, AsyncWaitHandler handler, chrono::nanoseconds timeout);
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

#ifdef MENDER_USE_TINY_PROC_LIB
	unique_ptr<tpl::Process> proc_;

	int stdout_pipe_ {-1};
	int stderr_pipe_ {-1};

	future<int> future_exit_status_;

	struct AsyncWaitData {
		mutex data_mutex;
		events::EventLoop *event_loop {nullptr};
		AsyncWaitHandler handler;
		bool process_ended {false};
	};
	shared_ptr<AsyncWaitData> async_wait_data_;
#endif

	vector<string> args_;
	string work_dir_;
	int exit_status_ {-1};

	unique_ptr<events::Timer> timeout_timer_;

	chrono::seconds max_termination_time_;

	void DoCancel();

	io::ExpectedAsyncReaderPtr GetProcessReader(events::EventLoop &loop, int &pipe_ref);

	void SetupAsyncWait();
	void AsyncWaitInternalHandler(shared_ptr<AsyncWaitData> async_wait_data);
};

} // namespace processes
} // namespace common
} // namespace mender

#endif // MENDER_COMMON_PROCESSES_HPP
