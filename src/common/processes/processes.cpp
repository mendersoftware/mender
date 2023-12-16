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

#include <common/log.hpp>
#include <common/common.hpp>

namespace mender {
namespace common {
namespace processes {

namespace log = mender::common::log;

const chrono::seconds DEFAULT_GENERATE_LINE_DATA_TIMEOUT {10};

const ProcessesErrorCategoryClass ProcessesErrorCategory;

const char *ProcessesErrorCategoryClass::name() const noexcept {
	return "ProcessesErrorCategory";
}

string ProcessesErrorCategoryClass::message(int code) const {
	switch (code) {
	case NoError:
		return "Success";
	case SpawnError:
		return "Spawn error";
	case ProcessAlreadyStartedError:
		return "Process already started";
	case NonZeroExitStatusError:
		return "Process returned non-zero exit status";
	}
	assert(false);
	return "Unknown";
}

error::Error MakeError(ProcessesErrorCode code, const string &msg) {
	return error::Error(error_condition(code, ProcessesErrorCategory), msg);
};

void OutputHandler::operator()(const char *data, size_t size) {
	if (size == 0) {
		return;
	}
	// Get rid of exactly one trailing newline, if there is one. This is because
	// we unconditionally print one at the end of every log line. If the string
	// does not contain a trailing newline, add a "{...}" instead, since we
	// cannot avoid breaking the line apart then.
	string content(data, size);
	if (content.back() == '\n') {
		content.pop_back();
	} else {
		content.append("{...}");
	}
	auto lines = mender::common::SplitString(content, "\n");
	for (auto line : lines) {
		log::Info(prefix + line);
	}
}

} // namespace processes
} // namespace common
} // namespace mender
