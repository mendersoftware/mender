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

#ifndef MENDER_ARTIFACT_V3_SCRIPT_EXECUTOR_HPP
#define MENDER_ARTIFACT_V3_SCRIPT_EXECUTOR_HPP

#include <memory>
#include <string>

#include <common/common.hpp>
#include <common/events.hpp>
#include <common/expected.hpp>
#include <common/log.hpp>
#include <common/optional.hpp>
#include <common/processes.hpp>

#include <artifact/v3/scripts/error.hpp>

namespace mender {
namespace artifact {
namespace scripts {
namespace executor {

using namespace std;

namespace events = mender::common::events;
namespace expected = mender::common::expected;
namespace log = mender::common::log;
namespace processes = mender::common::processes;

using Error = mender::common::error::Error;

enum class State {
	Idle,
	Sync,
	Download,
	ArtifactInstall,
	ArtifactReboot,
	ArtifactCommit,
	ArtifactRollback,
	ArtifactRollbackReboot,
	ArtifactFailure,
};

enum class Action {
	Enter,
	Leave,
	Error,
};

enum class OnError {
	Fail,
	Ignore,
};

string Name(const State, const Action);

class ScriptRunner {
public:
	ScriptRunner(
		events::EventLoop &loop,
		chrono::milliseconds script_timeout,
		chrono::milliseconds retry_interval,
		chrono::milliseconds retry_timeout,
		const string &artifact_script_path,
		const string &rootfs_script_path,
		processes::OutputCallback stdout_callback =
			processes::OutputHandler {"Collected output (stdout) while running script: "},
		processes::OutputCallback sterr_callback = processes::OutputHandler {
			"Collected output (stderr) while running script: "});


	// Returns an Error from the first erroring script, or a NoError in the case
	// when all scripts are successful.
	using HandlerFunction = function<void(Error)>;

	Error AsyncRunScripts(
		State state, Action action, HandlerFunction handler, OnError on_error = OnError::Fail);

	Error RunScripts(State state, Action action, OnError on_error = OnError::Fail);


private:
	Error Execute(
		vector<string>::iterator current_script,
		vector<string>::iterator end,
		bool ignore_error,
		HandlerFunction handler);

	void CollectError(Error err);

	void LogErrAndExecuteNext(
		Error err,
		vector<string>::iterator current_script,
		vector<string>::iterator end,
		bool ignore_error,
		HandlerFunction handler);

	void HandleScriptError(Error err, HandlerFunction handler);
	void HandleScriptRetry(
		vector<string>::iterator current_script,
		vector<string>::iterator end,
		bool ignore_error,
		HandlerFunction handler);
	void HandleScriptNext(
		vector<string>::iterator current_script,
		vector<string>::iterator end,
		bool ignore_error,
		HandlerFunction handler);
	void MaybeSetupRetryTimeoutTimer();

	string ScriptPath(State state);

	events::EventLoop &loop_;
	chrono::milliseconds script_timeout_;
	chrono::milliseconds retry_interval_;
	chrono::milliseconds retry_timeout_;
	string artifact_script_path_;
	string rootfs_script_path_;
	processes::OutputCallback stdout_callback_;
	processes::OutputCallback stderr_callback_;
	Error error_script_error_;
	vector<string> collected_scripts_;
	unique_ptr<processes::Process> script_;
	unique_ptr<events::Timer> retry_interval_timer_;
	unique_ptr<events::Timer> retry_timeout_timer_;
};

} // namespace executor
} // namespace scripts
} // namespace artifact
} // namespace mender


#endif // MENDER_ARTIFACT_V3_SCRIPT_EXECUTOR_HPP
