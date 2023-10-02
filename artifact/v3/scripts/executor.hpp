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
#include <common/conf.hpp>
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

enum class RunError {
	Ignore,
	Fail,
};

string Name(const State, const Action);

class ScriptRunner {
public:
	ScriptRunner(
		events::EventLoop &loop,
		chrono::seconds state_script_timeout,
		const string &artifact_script_path,
		const string &rootfs_script_path,
		mender::common::processes::OutputCallback stdout_callback =
			mender::common::processes::OutputHandler {
				"Collected output (stdout) while running script: "},
		mender::common::processes::OutputCallback sterr_callback =
			mender::common::processes::OutputHandler {
				"Collected output (stderr) while running script: "});


	// Returns an Error from the first erroring script, or a NoError in the case
	// when all scripts are successful.
	using HandlerFunction = function<void(Error)>;

	Error AsyncRunScripts(
		State state, Action action, HandlerFunction handler, RunError on_error = RunError::Fail);

	Error RunScripts(State state, Action action, RunError on_error = RunError::Fail);


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

	string ScriptPath(State state);

	events::EventLoop &loop_;
	bool is_artifact_script_;
	chrono::seconds state_script_timeout_;
	string artifact_script_path_;
	string rootfs_script_path_;
	mender::common::processes::OutputCallback stdout_callback_;
	mender::common::processes::OutputCallback stderr_callback_;
	Error error_script_error_;
	vector<string> collected_scripts_;
	unique_ptr<mender::common::processes::Process> script_;
};

} // namespace executor
} // namespace scripts
} // namespace artifact
} // namespace mender


#endif // MENDER_ARTIFACT_V3_SCRIPT_EXECUTOR_HPP
