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

#include <mender-update/update_module/v3/update_module_call.hpp>

#include <iostream>
#include <sstream>
#include <common/processes.hpp>
#include <paths.h>
#include <boost/filesystem.hpp>

namespace mender {
namespace update {
namespace update_module {
namespace v3 {

namespace error = mender::common::error;
namespace procs = mender::common::processes;
namespace fs = boost::filesystem;

ExpectedExitStatus CallState(
	const std::string process, State state, const string directory, string &procOut) {
	if (!fs::is_directory(directory)) {
		if (state == State::Cleanup) {
			return 0;
		} else {
			return expected::unexpected(
				error::MakeError(error::GenericError, "File tree does not exist: " + directory));
		}
	}
	if (state == State::Cleanup) {
		try {
			boost::filesystem::remove_all(directory);
			return 0;
		} catch (const boost::filesystem::filesystem_error &e) {
			return expected::unexpected(error::MakeError(
				error::GenericError, "Error removing directory: " + directory + " " + e.what()));
		}
	}
	std::stringstream ss;
	procs::Process proc(
		{process, mender::update::update_module::v3::StateString[(int) state], directory});
	auto processStart = proc.Start([&ss](const char *data, size_t size) { ss.write(data, size); });
	if (error::NoError != processStart) {
		return expected::unexpected(processStart);
	}

	int exitStatus = proc.Wait();
	procOut = ss.str();
	return exitStatus;
}

} // namespace v3
} // namespace update_module
} // namespace update
} // namespace mender
