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

#include <boost/filesystem.hpp>

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
namespace procs = mender::common::processes;
namespace fs = boost::filesystem;

error::Error UpdateModule::CallState(State state, string *procOut) {
	string directory = GetModulesWorkPath();
	if (!fs::is_directory(directory)) {
		if (state == State::Cleanup) {
			return error::NoError;
		} else {
			return error::Error(
				make_error_condition(errc::no_such_file_or_directory),
				"File tree does not exist: " + directory);
		}
	}

	procs::Process proc({GetModulePath(), StateToString(state), GetModulesWorkPath()});
	proc.SetWorkDir(GetModulesWorkPath());
	error::Error processStart;
	bool first_line_captured = false;
	bool too_many_lines = false;
	if (procOut != nullptr) {
		processStart = proc.Start(
			[&procOut, &first_line_captured, &too_many_lines](const char *data, size_t size) {
				// At the moment, no state that queries output accepts more than one line,
				// so reject multiple lines here. This would have been rejected anyway due
				// to matching, but by doing it here, we also prevent using excessive memory
				// if the process dumps a large log on us.
				if (!first_line_captured) {
					auto lines = mender::common::SplitString(string(data, size), "\n");
					if (lines.size() >= 1) {
						*procOut = lines[0];
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
		return processStart;
	}

	events::EventLoop loop;
	error::Error err;
	err = proc.AsyncWait(loop, [&loop, &err](error::Error process_err) {
		err = process_err;
		loop.Stop();
	});

	events::Timer timeout(loop);
	timeout.AsyncWait(
		chrono::seconds(ctx_.GetConfig().module_timeout_seconds),
		[&loop, &proc, &err](error_code ec) {
			proc.EnsureTerminated();
			err = error::Error(
				make_error_condition(errc::timed_out),
				"Timed out while waiting for Update Module to complete");
			loop.Stop();
		});

	loop.Run();

	if (state == State::Cleanup) {
		boost::system::error_code ec;
		if (!boost::filesystem::remove_all(directory, ec)) {
			return error::Error(
				ec.default_error_condition(), "Error removing directory: " + directory);
		}
	}

	if (err == error::NoError && too_many_lines) {
		return error::Error(
			make_error_condition(errc::protocol_error),
			"Too many lines when querying " + StateToString(state));
	}

	return err;
}

} // namespace v3
} // namespace update_module
} // namespace update
} // namespace mender
