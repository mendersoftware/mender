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

namespace mender::common::processes {

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
	default:
		return "Unknown";
	}
}

error::Error MakeError(ProcessesErrorCode code, const string &msg) {
	return error::Error(error_condition(code, ProcessesErrorCategory), msg);
};

} // namespace mender::common::processes