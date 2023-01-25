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

#include <string>
#include <vector>
#include <common/error.hpp>
#include <common/expected.hpp>

namespace mender::common::processes {

using namespace std;

enum ProcessErrorCode {
	SpawnError,
};

using ProcessError = mender::common::error::Error<ProcessErrorCode>;
using LineData = vector<string>;
using ExpectedLineData = mender::common::expected::Expected<LineData, ProcessError>;

class Process {
public:
	Process(vector<string> args) :
		args_(args) {};

	int GetExitStatus() const {
		return this->exit_status_;
	};
	ExpectedLineData GenerateLineData();

private:
	vector<string> args_;
	int exit_status_ = -1;
};

} // namespace mender::common::processes

#endif // MENDER_COMMON_PROCESSES_HPP
