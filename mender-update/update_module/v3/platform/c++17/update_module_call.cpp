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

#include <mender-update/update_module/v3/update_module.hpp>

#include <iostream>
#include <sstream>

#include <filesystem>

#include <common/common.hpp>
#include <common/events.hpp>
#include <common/log.hpp>
#include <common/processes.hpp>

namespace mender {
namespace update {
namespace update_module {
namespace v3 {

namespace error = mender::common::error;
namespace events = mender::common::events;
namespace log = mender::common::log;
namespace fs = std::filesystem;

UpdateModule::StateRunner::StateRunner(
	events::EventLoop &loop,
	State state,
	const string &module_path,
	const string &module_work_path) :
	loop(loop),
	module_work_path(module_work_path),
	proc({module_path, StateToString(state), module_work_path}),
	timeout(loop) {
	proc.SetWorkDir(module_work_path);
}

error::Error UpdateModule::StateRunner::AsyncCallState(
	State state, bool procOut, chrono::seconds timeout_seconds, HandlerFunction handler) {
	this->handler = handler;

	string state_string = StateToString(state);
	if (!fs::is_directory(module_work_path)) {
		if (state == State::Cleanup) {
			return error::NoError;
		} else {
			return error::Error(
				make_error_condition(errc::no_such_file_or_directory),
				state_string + ": File tree does not exist: " + module_work_path);
		}
	}

	error::Error processStart;
	if (procOut) {
		// Provide string to put content in.
		output.emplace(string());
		processStart = proc.Start([this](const char *data, size_t size) {
			// At the moment, no state that queries output accepts more than one line,
			// so reject multiple lines here. This would have been rejected anyway due
			// to matching, but by doing it here, we also prevent using excessive memory
			// if the process dumps a large log on us.
			if (!first_line_captured) {
				auto lines = mender::common::SplitString(string(data, size), "\n");
				if (lines.size() >= 1) {
					*output = lines[0];
					first_line_captured = true;
				}
				if (lines.size() > 2 || (lines.size() == 2 && lines[1] != "")) {
					too_many_lines = true;
				}
			} else {
				too_many_lines = true;
			}
		});
	} else {
		processStart = proc.Start([](const char *data, size_t size) {
			auto lines = mender::common::SplitString(string(data, size), "\n");
			for (auto line : lines) {
				log::Info("Update Module output: " + line);
			}
		});
	}
	if (processStart != error::NoError) {
		return GetProcessError(processStart).WithContext(state_string);
	}

	error::Error err;
	err = proc.AsyncWait(loop, [this, state, handler](error::Error process_err) {
		timeout.Cancel();
		auto err = process_err.WithContext(StateToString(state));
		ProcessFinishedHandler(state, err);
	});

	timeout.AsyncWait(timeout_seconds, [this, state, handler](error::Error inner_err) {
		proc.EnsureTerminated();
		auto err = error::Error(
			make_error_condition(errc::timed_out),
			StateToString(state) + ": Timed out while waiting for Update Module to complete");
		ProcessFinishedHandler(state, err);
	});

	return err;
}

void UpdateModule::StateRunner::ProcessFinishedHandler(State state, error::Error err) {
	if (state == State::Cleanup) {
		std::error_code ec;
		if (!fs::remove_all(module_work_path, ec)) {
			err = error::Error(
				ec.default_error_condition(),
				StateToString(state) + ": Error removing directory: " + module_work_path);
		}
	}

	if (err == error::NoError && too_many_lines) {
		err = error::Error(
			make_error_condition(errc::protocol_error),
			"Too many lines when querying " + StateToString(state));
	}

	if (err != error::NoError) {
		handler(expected::unexpected(err));
	} else {
		handler(output);
	}
}

} // namespace v3
} // namespace update_module
} // namespace update
} // namespace mender
