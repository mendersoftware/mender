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

#include <process.hpp>

namespace tpl = TinyProcessLib;
using namespace std;

namespace mender::common::processes {

ExpectedLineData Process::GenerateLineData() {
	if (this->args_.size() == 0) {
		return expected::unexpected(MakeError(
			ProcessesErrorCode::SpawnError, "No arguments given, cannot spawn a process"));
	}

	string trailing_line;
	vector<string> ret;
	auto proc =
		tpl::Process(this->args_, "", [&trailing_line, &ret](const char *bytes, size_t len) {
			auto bytes_view = string_view(bytes, len);
			size_t line_start_idx = 0;
			size_t line_end_idx = bytes_view.find("\n", 0);
			if ((trailing_line != "") && (line_end_idx != string_view::npos)) {
				ret.push_back(trailing_line + string(bytes_view, 0, line_end_idx));
				line_start_idx = line_end_idx + 1;
				line_end_idx = bytes_view.find("\n", line_start_idx);
				trailing_line = "";
			}

			while ((line_start_idx < (len - 1)) && (line_end_idx != string_view::npos)) {
				ret.push_back(string(bytes_view, line_start_idx, (line_end_idx - line_start_idx)));
				line_start_idx = line_end_idx + 1;
				line_end_idx = bytes_view.find("\n", line_start_idx);
			}

			if ((line_end_idx == string_view::npos) && (line_start_idx != (len - 1))) {
				trailing_line += string(bytes_view, line_start_idx, (len - line_start_idx));
			}
		});

	// waits for the process to finish
	// TODO: log exit status != 0? Or error?
	this->exit_status_ = proc.get_exit_status();

	if (trailing_line != "") {
		ret.push_back(trailing_line);
	}

	if (proc.get_id() == -1) {
		return expected::unexpected(
			MakeError(ProcessesErrorCode::SpawnError, "Failed to spawn '" + this->args_[0] + "'"));
	}

	return ExpectedLineData(ret);
}

} // namespace mender::common::processes
