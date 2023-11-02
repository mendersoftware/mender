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

#include <common/processes.hpp>

#include <string>
#include <string_view>

#include <common/events_io.hpp>
#include <common/io.hpp>
#include <common/log.hpp>
#include <common/path.hpp>

using namespace std;

namespace mender::common::processes {

const chrono::seconds MAX_TERMINATION_TIME(10);

namespace io = mender::common::io;
namespace log = mender::common::log;
namespace path = mender::common::path;

class ProcessReaderFunctor {
public:
	void operator()(const char *bytes, size_t n);

	// Note: ownership is not held here, but in Process.
	int fd_;

	OutputCallback callback_;
};

Process::Process(const vector<string> &args) :
	args_ {args},
	max_termination_time_ {MAX_TERMINATION_TIME} {
	async_wait_data_ = make_shared<AsyncWaitData>();
}

Process::~Process() {
	{
		unique_lock lock(async_wait_data_->data_mutex);
		// DoCancel() requires being locked.
		DoCancel();
	}

	EnsureTerminated();

	if (stdout_pipe_ >= 0) {
		close(stdout_pipe_);
	}
	if (stderr_pipe_ >= 0) {
		close(stderr_pipe_);
	}
}

error::Error Process::Start(OutputCallback stdout_callback, OutputCallback stderr_callback) {
	if (proc_) {
		return MakeError(ProcessAlreadyStartedError, "Cannot start process");
	}

	// Tiny-process-library doesn't give a good error if the command isn't found (just returns
	// exit code 1). If the path is absolute, it's pretty easy to check if it exists. This won't
	// cover all errors (non-absolute or unset executable bit, for example), but helps a little,
	// at least.
	if (args_.size() > 0 && path::IsAbsolute(args_[0])) {
		ifstream f(args_[0]);
		if (!f.good()) {
			int errnum = errno;
			return error::Error(
				generic_category().default_error_condition(errnum), "Cannot launch " + args_[0]);
		}
	}

	OutputCallback maybe_stdout_callback;
	if (stdout_pipe_ >= 0 || stdout_callback) {
		maybe_stdout_callback = ProcessReaderFunctor {stdout_pipe_, stdout_callback};
	}
	OutputCallback maybe_stderr_callback;
	if (stderr_pipe_ >= 0 || stderr_callback) {
		maybe_stderr_callback = ProcessReaderFunctor {stderr_pipe_, stderr_callback};
	}

	proc_ =
		make_unique<tpl::Process>(args_, work_dir_, maybe_stdout_callback, maybe_stderr_callback);

	if (proc_->get_id() == -1) {
		proc_.reset();
		return MakeError(
			ProcessesErrorCode::SpawnError,
			"Failed to spawn '" + (args_.size() >= 1 ? args_[0] : "<null>") + "'");
	}

	SetupAsyncWait();

	return error::NoError;
}

error::Error Process::Run() {
	auto err = Start();
	if (err != error::NoError) {
		log::Error(err.String());
	}
	return Wait();
}

static error::Error ErrorBasedOnExitStatus(int exit_status) {
	if (exit_status != 0) {
		return MakeError(
			NonZeroExitStatusError, "Process exited with status " + to_string(exit_status));
	} else {
		return error::NoError;
	}
}

error::Error Process::Wait() {
	if (proc_) {
		exit_status_ = future_exit_status_.get();
		proc_.reset();
		if (stdout_pipe_ >= 0) {
			close(stdout_pipe_);
			stdout_pipe_ = -1;
		}
		if (stderr_pipe_ >= 0) {
			close(stderr_pipe_);
			stderr_pipe_ = -1;
		}
	}
	return ErrorBasedOnExitStatus(exit_status_);
}

error::Error Process::Wait(chrono::nanoseconds timeout) {
	if (proc_) {
		auto result = future_exit_status_.wait_for(timeout);
		if (result == future_status::timeout) {
			return error::Error(
				make_error_condition(errc::timed_out), "Timed out while waiting for process");
		}
		AssertOrReturnError(result == future_status::ready);
		exit_status_ = future_exit_status_.get();
		proc_.reset();
		if (stdout_pipe_ >= 0) {
			close(stdout_pipe_);
			stdout_pipe_ = -1;
		}
		if (stderr_pipe_ >= 0) {
			close(stderr_pipe_);
			stderr_pipe_ = -1;
		}
	}
	return ErrorBasedOnExitStatus(exit_status_);
}

error::Error Process::AsyncWait(events::EventLoop &loop, AsyncWaitHandler handler) {
	unique_lock lock(async_wait_data_->data_mutex);

	if (async_wait_data_->handler) {
		return error::Error(make_error_condition(errc::operation_in_progress), "Cannot AsyncWait");
	}

	async_wait_data_->event_loop = &loop;
	async_wait_data_->handler = handler;

	if (async_wait_data_->process_ended) {
		// The process has already ended. Schedule the handler immediately.
		auto &async_wait_data = async_wait_data_;
		async_wait_data->event_loop->Post(
			[async_wait_data, this]() { AsyncWaitInternalHandler(async_wait_data); });
	}

	return error::NoError;
}

error::Error Process::AsyncWait(
	events::EventLoop &loop, AsyncWaitHandler handler, chrono::nanoseconds timeout) {
	timeout_timer_.reset(new events::Timer(loop));

	auto err = AsyncWait(loop, handler);
	if (err != error::NoError) {
		return err;
	}

	timeout_timer_->AsyncWait(timeout, [this, handler](error::Error err) {
		// Move timer here so it gets destroyed after this handler.
		auto timer = std::move(timeout_timer_);
		// Cancel normal AsyncWait() (the process part of it).
		{
			// DoCancel() requires being locked.
			unique_lock lock(async_wait_data_->data_mutex);
			DoCancel();
		}
		if (err != error::NoError) {
			handler(err.WithContext("Process::Timer"));
		}

		handler(error::Error(make_error_condition(errc::timed_out), "Process::Timer"));
	});

	return error::NoError;
}

void Process::Cancel() {
	unique_lock lock(async_wait_data_->data_mutex);

	if (async_wait_data_->handler && !async_wait_data_->process_ended) {
		auto &async_wait_data = async_wait_data_;
		auto &handler = async_wait_data->handler;
		async_wait_data_->event_loop->Post([handler]() {
			handler(error::Error(
				make_error_condition(errc::operation_canceled), "Process::AsyncWait canceled"));
		});
	}

	// DoCancel() requires being locked.
	DoCancel();
}

void Process::DoCancel() {
	// Should already be locked by caller.
	//   unique_lock lock(async_wait_data_->data_mutex);

	timeout_timer_.reset();

	async_wait_data_->event_loop = nullptr;
	async_wait_data_->handler = nullptr;
	async_wait_data_->process_ended = false;
}

void Process::SetupAsyncWait() {
	future_exit_status_ = async(launch::async, [this]() -> int {
		// This function is executed in a separate thread, but the object is guaranteed to
		// still exist while we're in here (because we call `future_exit_status_.get()`
		// during destruction.

		auto status = proc_->get_exit_status();

		auto &async_wait_data = async_wait_data_;

		unique_lock lock(async_wait_data->data_mutex);

		if (async_wait_data->handler) {
			async_wait_data_->event_loop->Post(
				[async_wait_data, this]() { AsyncWaitInternalHandler(async_wait_data); });
		}

		async_wait_data->process_ended = true;

		return status;
	});
}

void Process::AsyncWaitInternalHandler(shared_ptr<AsyncWaitData> async_wait_data) {
	timeout_timer_.reset();

	// This function is meant to be executed on the event loop, and the object may have been
	// either cancelled or destroyed before we get here. By having our own copy of the
	// async_wait_data pointer pointer, it survives destruction, and we can test whether we
	// should still proceed here. So note the use of `async_wait_data` instead of
	// `async_wait_data_`.
	unique_lock lock(async_wait_data->data_mutex);

	if (async_wait_data->handler) {
		auto handler = async_wait_data->handler;

		// For next iteration.
		async_wait_data->event_loop = nullptr;
		async_wait_data->handler = nullptr;
		async_wait_data->process_ended = false;

		auto status = GetExitStatus();

		// Unlock in case the handler calls back into this object.
		lock.unlock();
		handler(ErrorBasedOnExitStatus(status));
	}
}

static void CollectLineData(
	string &trailing_line, vector<string> &lines, const char *bytes, size_t len) {
	auto bytes_view = string_view(bytes, len);
	size_t line_start_idx = 0;
	size_t line_end_idx = bytes_view.find("\n", 0);
	if ((trailing_line != "") && (line_end_idx != string_view::npos)) {
		lines.push_back(trailing_line + string(bytes_view, 0, line_end_idx));
		line_start_idx = line_end_idx + 1;
		line_end_idx = bytes_view.find("\n", line_start_idx);
		trailing_line = "";
	}

	while ((line_start_idx < (len - 1)) && (line_end_idx != string_view::npos)) {
		lines.push_back(string(bytes_view, line_start_idx, (line_end_idx - line_start_idx)));
		line_start_idx = line_end_idx + 1;
		line_end_idx = bytes_view.find("\n", line_start_idx);
	}

	if ((line_end_idx == string_view::npos) && (line_start_idx != (len - 1))) {
		trailing_line += string(bytes_view, line_start_idx, (len - line_start_idx));
	}
}

ExpectedLineData Process::GenerateLineData(chrono::nanoseconds timeout) {
	if (proc_) {
		return expected::unexpected(
			MakeError(ProcessAlreadyStartedError, "Cannot generate line data"));
	}

	if (args_.size() == 0) {
		return expected::unexpected(MakeError(
			ProcessesErrorCode::SpawnError, "No arguments given, cannot spawn a process"));
	}

	string trailing_line;
	vector<string> ret;
	proc_ = make_unique<tpl::Process>(
		args_, work_dir_, [&trailing_line, &ret](const char *bytes, size_t len) {
			CollectLineData(trailing_line, ret, bytes, len);
		});

	if (proc_->get_id() == -1) {
		proc_.reset();
		return expected::unexpected(MakeError(
			ProcessesErrorCode::SpawnError,
			"Failed to spawn '" + (args_.size() >= 1 ? args_[0] : "<null>") + "'"));
	}

	SetupAsyncWait();

	auto err = Wait(timeout);
	if (err != error::NoError) {
		return expected::unexpected(err);
	}

	if (trailing_line != "") {
		ret.push_back(trailing_line);
	}

	return ExpectedLineData(ret);
}

io::ExpectedAsyncReaderPtr Process::GetProcessReader(events::EventLoop &loop, int &pipe_ref) {
	if (proc_) {
		return expected::unexpected(
			MakeError(ProcessAlreadyStartedError, "Cannot get process output"));
	}

	if (pipe_ref >= 0) {
		close(pipe_ref);
		pipe_ref = -1;
	}

	int fds[2];
	int ret = pipe(fds);
	if (ret < 0) {
		int err = errno;
		return expected::unexpected(error::Error(
			generic_category().default_error_condition(err),
			"Could not get process stdout reader"));
	}

	pipe_ref = fds[1];

	return make_shared<events::io::AsyncFileDescriptorReader>(loop, fds[0]);
}

io::ExpectedAsyncReaderPtr Process::GetAsyncStdoutReader(events::EventLoop &loop) {
	return GetProcessReader(loop, stdout_pipe_);
}

io::ExpectedAsyncReaderPtr Process::GetAsyncStderrReader(events::EventLoop &loop) {
	return GetProcessReader(loop, stderr_pipe_);
}

int Process::EnsureTerminated() {
	if (!proc_) {
		return exit_status_;
	}

	log::Info("Sending SIGTERM to PID " + to_string(proc_->get_id()));

	Terminate();

	auto result = future_exit_status_.wait_for(max_termination_time_);
	if (result == future_status::timeout) {
		log::Info("Sending SIGKILL to PID " + to_string(proc_->get_id()));
		Kill();
		result = future_exit_status_.wait_for(max_termination_time_);
		if (result != future_status::ready) {
			// This should not be possible, SIGKILL always terminates.
			log::Error(
				"PID " + to_string(proc_->get_id()) + " still not terminated after SIGKILL.");
			return -1;
		}
	}
	assert(result == future_status::ready);
	exit_status_ = future_exit_status_.get();

	log::Info(
		"PID " + to_string(proc_->get_id()) + " exited with status " + to_string(exit_status_));

	proc_.reset();

	return exit_status_;
}

void Process::Terminate() {
	if (proc_) {
		// At the time of writing, tiny-process-library kills using SIGINT and SIGTERM, for
		// `force = false/true`, respectively. But we want to kill with SIGTERM and SIGKILL,
		// because:
		//
		// 1. SIGINT is not meant to kill interactive processes, whereas SIGTERM is.
		// 2. SIGKILL is required in order to really force, since SIGTERM can be ignored by
		//    the process.
		//
		// If tiny-process-library is fixed, then this can be restored and the part below
		// removed.
		// proc_->kill(false);

		::kill(proc_->get_id(), SIGTERM);
		::kill(-proc_->get_id(), SIGTERM);
	}
}

void Process::Kill() {
	if (proc_) {
		// See comment in Terminate().
		// proc_->kill(true);

		::kill(proc_->get_id(), SIGKILL);
		::kill(-proc_->get_id(), SIGKILL);
	}
}

void ProcessReaderFunctor::operator()(const char *bytes, size_t n) {
	if (callback_) {
		callback_(bytes, n);
	}

	if (fd_ < 0) {
		return;
	}

	size_t written = 0;
	while (written < n) {
		auto ret = write(fd_, bytes, n);
		if (ret < 0) {
			int err = errno;
			log::Error(
				string {"Error while writing process output to main thread: "} + strerror(err));
			fd_ = -1;
			return;
		}

		written += ret;
	}
}

} // namespace mender::common::processes
