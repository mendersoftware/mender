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

#include <artifact/v3/scripts/executor.hpp>

#include <algorithm>
#include <chrono>
#include <regex>
#include <string>

#include <common/common.hpp>
#include <common/expected.hpp>
#include <common/path.hpp>


namespace mender {
namespace artifact {
namespace scripts {
namespace executor {

namespace expected = mender::common::expected;


using expected::ExpectedBool;

namespace common = mender::common;
namespace error = mender::common::error;
namespace path = mender::common::path;


const string expected_state_script_version {"3"};
const int state_script_retry_exit_code {21};

unordered_map<const State, string> state_map {
	{State::Idle, "Idle"},
	{State::Sync, "Sync"},
	{State::Download, "Download"},
	{State::ArtifactInstall, "ArtifactInstall"},
	{State::ArtifactReboot, "ArtifactReboot"},
	{State::ArtifactCommit, "ArtifactCommit"},
	{State::ArtifactRollback, "ArtifactRollback"},
	{State::ArtifactRollbackReboot, "ArtifactRollbackReboot"},
	{State::ArtifactFailure, "ArtifactFailure"},
};

unordered_map<const Action, string> action_map {
	{Action::Enter, "Enter"},
	{Action::Leave, "Leave"},
	{Action::Error, "Error"},
};

error::Error CorrectVersionFile(const string &path) {
	// Missing file is OK
	// This is because previous versions of the client wrote no
	// version file, so no-file=v3
	if (!path::FileExists(path)) {
		return error::NoError;
	}

	ifstream vf {path};

	if (!vf) {
		auto errnum {errno};
		return error::Error(
			generic_category().default_error_condition(errnum), "Failed to open the version file");
	}

	string version;
	vf >> version;
	if (!vf) {
		auto errnum {errno};
		return error::Error(
			generic_category().default_error_condition(errnum),
			"Error reading the version number from the version file");
	}

	if (version != expected_state_script_version) {
		return executor::MakeError(
			executor::VersionFileError, "Unexpected Artifact script version found: " + version);
	}
	return error::NoError;
}

bool isValidStateScript(const string &file, State state, Action action) {
	string expression {
		"(" + state_map.at(state) + ")" + "_(" + action_map.at(action) + ")_[0-9][0-9](_\\S+)?"};
	log::Trace(
		"verifying the State script format of the file: " + file
		+ " using the regular expression: " + expression);
	const regex artifact_script_regexp {expression, std::regex_constants::ECMAScript};
	return regex_match(path::BaseName(file), artifact_script_regexp);
}

function<bool(const string &)> Matcher(State state, Action action) {
	return [state, action](const string &file) {
		const bool is_valid {isValidStateScript(file, state, action)};
		if (!is_valid) {
			log::Trace(file + " is not a valid State Script for the state: " + Name(state, action));
			return false;
		}
		auto exp_executable = path::IsExecutable(file, true);
		if (!exp_executable) {
			log::Debug("Issue figuring the executable bits of: " + exp_executable.error().String());
			return false;
		}
		return is_valid and exp_executable.value();
	};
}

bool IsArtifactScript(State state) {
	switch (state) {
	case State::Idle:
	case State::Sync:
	case State::Download:
		return false;
	case State::ArtifactInstall:
	case State::ArtifactReboot:
	case State::ArtifactCommit:
	case State::ArtifactRollback:
	case State::ArtifactRollbackReboot:
	case State::ArtifactFailure:
		return true;
	}
	assert(false);
	return false;
}

string ScriptRunner::ScriptPath(State state) {
	if (IsArtifactScript(state)) {
		return this->artifact_script_path_;
	}
	return this->rootfs_script_path_;
}

string Name(const State state, const Action action) {
	return state_map.at(state) + action_map.at(action);
}

ScriptRunner::ScriptRunner(
	events::EventLoop &loop,
	chrono::seconds state_script_timeout,
	const string &artifact_script_path,
	const string &rootfs_script_path,
	mender::common::processes::OutputCallback stdout_callback,
	mender::common::processes::OutputCallback stderr_callback) :
	loop_ {loop},
	state_script_timeout_ {state_script_timeout},
	artifact_script_path_ {artifact_script_path},
	rootfs_script_path_ {rootfs_script_path},
	stdout_callback_ {stdout_callback},
	stderr_callback_ {stderr_callback},
	error_script_error_ {error::NoError} {};

void ScriptRunner::LogErrAndExecuteNext(
	Error err,
	vector<string>::iterator current_script,
	vector<string>::iterator end,
	bool ignore_error,
	HandlerFunction handler) {
	// Collect the error and carry on
	if (err.code
		== common::processes::MakeError(common::processes::NonZeroExitStatusError, "").code) {
		this->error_script_error_ = this->error_script_error_.FollowedBy(executor::MakeError(
			executor::NonZeroExitStatusError,
			"Got non zero exit code from script: " + *current_script));
	} else {
		this->error_script_error_ = this->error_script_error_.FollowedBy(err);
	}

	// Schedule the next script execution
	auto local_err = Execute(std::next(current_script), end, ignore_error, handler);
	if (local_err != error::NoError) {
		return handler(local_err);
	}
}

void ScriptRunner::HandleScriptError(Error err, HandlerFunction handler) {
	if (err.code
		== common::processes::MakeError(common::processes::NonZeroExitStatusError, "").code) {
		if (this->script_.get()->GetExitStatus() == state_script_retry_exit_code) {
			return handler(executor::MakeError(
				executor::RetryExitCodeError,
				"Received exit code: " + to_string(state_script_retry_exit_code)));
		}
		return handler(executor::MakeError(
			executor::NonZeroExitStatusError,
			"Received error code: " + to_string(this->script_.get()->GetExitStatus())));
	}
	return handler(err);
}

Error ScriptRunner::Execute(
	vector<string>::iterator current_script,
	vector<string>::iterator end,
	bool ignore_error,
	HandlerFunction handler) {
	// No more scripts to execute
	if (current_script == end) {
		handler(this->error_script_error_); // Success
		return error::NoError;
	}

	log::Info("Running State Script: " + *current_script);

	this->script_.reset(new mender::common::processes::Process({*current_script}));
	auto err {this->script_->Start(stdout_callback_, stderr_callback_)};
	if (err != error::NoError) {
		return err;
	}

	return this->script_.get()->AsyncWait(
		this->loop_,
		[this, current_script, end, ignore_error, handler](Error err) {
			if (err != error::NoError) {
				if (ignore_error) {
					return LogErrAndExecuteNext(err, current_script, end, ignore_error, handler);
				}
				return HandleScriptError(err, handler);
			}
			// Schedule the next script execution
			auto local_err = Execute(std::next(current_script), end, ignore_error, handler);
			if (local_err != error::NoError) {
				return handler(local_err);
			}
		},
		this->state_script_timeout_);
}

Error ScriptRunner::AsyncRunScripts(
	State state, Action action, HandlerFunction handler, RunError on_error) {
	if (IsArtifactScript(state)) {
		// Verify the version in the version file (OK if no version file present)
		auto version_file_error {
			CorrectVersionFile(path::Join(this->artifact_script_path_, "version"))};
		if (version_file_error != error::NoError) {
			return version_file_error;
		}
	}

	// Collect
	const auto script_path {ScriptPath(state)};
	auto exp_scripts {path::ListFiles(script_path, Matcher(state, action))};
	if (!exp_scripts) {
		// Missing directory is OK
		if (exp_scripts.error().IsErrno(ENOENT)) {
			log::Warning("Found no state script directory (" + script_path + "). Continuing on");
			handler(error::NoError);
			return error::NoError;
		}
		return executor::MakeError(
			executor::Code::CollectionError,
			"Failed to get the scripts, error: " + exp_scripts.error().String());
	}

	// Sort
	{
		auto &unsorted_scripts {exp_scripts.value()};

		vector<string> sorted_scripts(unsorted_scripts.begin(), unsorted_scripts.end());

		sort(sorted_scripts.begin(), sorted_scripts.end());
		this->collected_scripts_ = std::move(sorted_scripts);
	}

	bool ignore_error = on_error == RunError::Ignore || action == Action::Error;

	// Execute
	auto scripts_iterator {this->collected_scripts_.begin()};
	auto scripts_iterator_end {this->collected_scripts_.end()};
	return Execute(scripts_iterator, scripts_iterator_end, ignore_error, handler);
}


Error ScriptRunner::RunScripts(State state, Action action, RunError on_error) {
	auto run_err {error::NoError};
	auto err = AsyncRunScripts(
		state,
		action,
		[this, &run_err](Error error) {
			run_err = error;
			this->loop_.Stop();
		},
		on_error);
	if (err != error::NoError) {
		return err;
	}
	this->loop_.Run();
	return run_err;
}


} // namespace executor
} // namespace scripts
} // namespace artifact
} // namespace mender
