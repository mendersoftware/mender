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

#include <mender-update/standalone.hpp>

#include <common/common.hpp>
#include <common/events_io.hpp>
#include <common/http.hpp>
#include <common/log.hpp>
#include <common/path.hpp>

#include <artifact/v3/scripts/executor.hpp>

namespace mender {
namespace update {
namespace standalone {

using namespace std;

namespace common = mender::common;
namespace events = mender::common::events;
namespace executor = mender::artifact::scripts::executor;
namespace http = mender::common::http;
namespace io = mender::common::io;
namespace log = mender::common::log;
namespace path = mender::common::path;

ExpectedOptionalStateData LoadStateData(database::KeyValueDatabase &db) {
	StateDataKeys keys;
	StateData dst;

	auto exp_bytes = db.Read(context::MenderContext::standalone_state_key);
	if (!exp_bytes) {
		auto &err = exp_bytes.error();
		if (err.code == database::MakeError(database::KeyError, "").code) {
			return optional<StateData>();
		} else {
			return expected::unexpected(err);
		}
	}

	auto exp_json = json::Load(common::StringFromByteVector(exp_bytes.value()));
	if (!exp_json) {
		return expected::unexpected(exp_json.error());
	}
	auto &json = exp_json.value();

	auto exp_int = json::Get<int64_t>(json, keys.version, json::MissingOk::No);
	if (!exp_int) {
		return expected::unexpected(exp_int.error());
	}
	dst.version = exp_int.value();

	if (dst.version != 1 && dst.version != context::MenderContext::standalone_data_version) {
		return expected::unexpected(error::Error(
			make_error_condition(errc::not_supported),
			"State data has a version which is not supported by this client"));
	}

	auto exp_string = json::Get<string>(json, keys.artifact_name, json::MissingOk::No);
	if (!exp_string) {
		return expected::unexpected(exp_string.error());
	}
	dst.artifact_name = exp_string.value();

	exp_string = json::Get<string>(json, keys.artifact_group, json::MissingOk::Yes);
	if (!exp_string) {
		return expected::unexpected(exp_string.error());
	}
	dst.artifact_group = exp_string.value();

	auto exp_map = json::Get<json::KeyValueMap>(json, keys.artifact_provides, json::MissingOk::No);
	if (exp_map) {
		dst.artifact_provides = exp_map.value();
	} else {
		dst.artifact_provides.reset();
	}

	auto exp_array =
		json::Get<vector<string>>(json, keys.artifact_clears_provides, json::MissingOk::No);
	if (exp_array) {
		dst.artifact_clears_provides = exp_array.value();
	} else {
		dst.artifact_clears_provides.reset();
	}

	exp_array = json::Get<vector<string>>(json, keys.payload_types, json::MissingOk::No);
	if (!exp_array) {
		return expected::unexpected(exp_array.error());
	}
	dst.payload_types = exp_array.value();

	if (dst.version == 1) {
		// In version 1, if there is any data at all, it is equivalent to this:
		dst.in_state = "ArtifactCommit";
		dst.failed = false;
		dst.rolled_back = false;

		// Additionally, there is never any situation where we want to save version 1 data,
		// because it only has one state: The one we just loaded in the previous
		// statement. In a rollback situation, all states are always carried out and the
		// data is removed instead. Therefore, always set it to version 2, so we can't even
		// theoretically save it wrongly (and we don't need to handle it in the saving
		// code).
		dst.version = context::MenderContext::standalone_data_version;
	} else {
		exp_string = json::Get<string>(json, keys.in_state, json::MissingOk::No);
		if (!exp_string) {
			return expected::unexpected(exp_string.error());
		}
		dst.in_state = exp_string.value();

		auto exp_bool = json::Get<bool>(json, keys.failed, json::MissingOk::No);
		if (!exp_bool) {
			return expected::unexpected(exp_bool.error());
		}
		dst.failed = exp_bool.value();

		exp_bool = json::Get<bool>(json, keys.rolled_back, json::MissingOk::No);
		if (!exp_bool) {
			return expected::unexpected(exp_bool.error());
		}
		dst.rolled_back = exp_bool.value();
	}

	if (dst.artifact_name == "") {
		return expected::unexpected(context::MakeError(
			context::DatabaseValueError, "`" + keys.artifact_name + "` is empty"));
	}

	if (dst.payload_types.size() == 0) {
		return expected::unexpected(context::MakeError(
			context::DatabaseValueError, "`" + keys.payload_types + "` is empty"));
	}
	if (dst.payload_types.size() >= 2) {
		return expected::unexpected(error::Error(
			make_error_condition(errc::not_supported),
			"`" + keys.payload_types + "` contains multiple payloads"));
	}

	return dst;
}

error::Error SaveStateData(database::KeyValueDatabase &db, const StateData &data) {
	StateDataKeys keys;
	stringstream ss;
	ss << "{";
	ss << "\"" << keys.version << "\":" << data.version;

	ss << ",";
	ss << "\"" << keys.artifact_name << "\":\"" << data.artifact_name << "\"";

	ss << ",";
	ss << "\"" << keys.artifact_group << "\":\"" << data.artifact_group << "\"";

	ss << ",";
	ss << "\"" << keys.payload_types << "\": [";
	bool first = true;
	for (auto elem : data.payload_types) {
		if (!first) {
			ss << ",";
		}
		ss << "\"" << elem << "\"";
		first = false;
	}
	ss << "]";

	if (data.artifact_provides) {
		ss << ",";
		ss << "\"" << keys.artifact_provides << "\": {";
		bool first = true;
		for (auto elem : data.artifact_provides.value()) {
			if (!first) {
				ss << ",";
			}
			ss << "\"" << elem.first << "\":\"" << elem.second << "\"";
			first = false;
		}
		ss << "}";
	}

	if (data.artifact_clears_provides) {
		ss << ",";
		ss << "\"" << keys.artifact_clears_provides << "\": [";
		bool first = true;
		for (auto elem : data.artifact_clears_provides.value()) {
			if (!first) {
				ss << ",";
			}
			ss << "\"" << elem << "\"";
			first = false;
		}
		ss << "]";
	}

	ss << R"(,")" << keys.in_state << R"(":")" << data.in_state << R"(")";

	ss << R"(,")" << keys.failed << R"(":)" << (data.failed ? "true" : "false");

	ss << R"(,")" << keys.rolled_back << R"(":)" << (data.rolled_back ? "true" : "false");

	ss << "}";

	string strdata = ss.str();
	vector<uint8_t> bytedata(common::ByteVectorFromString(strdata));

	return db.Write(context::MenderContext::standalone_state_key, bytedata);
}

error::Error RemoveStateData(database::KeyValueDatabase &db) {
	return db.Remove(context::MenderContext::standalone_state_key);
}

StateMachine::StateMachine(Context &ctx) :
	context_ {ctx},
	download_enter_state_ {
		executor::State::Download,
		executor::Action::Enter,
		executor::OnError::Fail,
		Result::Failed | Result::NoRollbackNecessary},
	download_leave_state_ {
		executor::State::Download,
		executor::Action::Leave,
		executor::OnError::Fail,
		Result::Failed | Result::NoRollbackNecessary},
	download_error_state_ {
		executor::State::Download,
		executor::Action::Error,
		executor::OnError::Ignore,
		Result::NoResult},
	save_artifact_install_state_ {"ArtifactInstall"},
	artifact_install_enter_state_ {
		executor::State::ArtifactInstall,
		executor::Action::Enter,
		executor::OnError::Fail,
		Result::Failed},
	artifact_install_leave_state_ {
		executor::State::ArtifactInstall,
		executor::Action::Leave,
		executor::OnError::Fail,
		Result::Failed},
	artifact_install_error_state_ {
		executor::State::ArtifactInstall,
		executor::Action::Error,
		executor::OnError::Ignore,
		Result::NoResult},
	save_artifact_commit_state_ {"ArtifactCommit"},
	artifact_commit_enter_state_ {
		executor::State::ArtifactCommit,
		executor::Action::Enter,
		executor::OnError::Fail,
		Result::Failed},
	save_artifact_commit_leave_state_ {"ArtifactCommit_Leave"},
	artifact_commit_leave_state_ {
		executor::State::ArtifactCommit,
		executor::Action::Leave,
		executor::OnError::Ignore,
		Result::Failed | Result::FailedInPostCommit},
	artifact_commit_error_state_ {
		executor::State::ArtifactCommit,
		executor::Action::Error,
		executor::OnError::Ignore,
		Result::NoResult},
	save_artifact_rollback_state_ {"ArtifactRollback"},
	artifact_rollback_enter_state_ {
		executor::State::ArtifactRollback,
		executor::Action::Enter,
		executor::OnError::Ignore,
		Result::Failed | Result::RollbackFailed},
	artifact_rollback_leave_state_ {
		executor::State::ArtifactRollback,
		executor::Action::Leave,
		executor::OnError::Ignore,
		Result::NoResult},
	save_artifact_failure_state_ {"ArtifactFailure"},
	artifact_failure_enter_state_ {
		executor::State::ArtifactFailure,
		executor::Action::Enter,
		executor::OnError::Ignore,
		Result::Failed | Result::RollbackFailed},
	artifact_failure_leave_state_ {
		executor::State::ArtifactFailure,
		executor::Action::Leave,
		executor::OnError::Ignore,
		Result::NoResult},
	save_cleanup_state_ {"Cleanup"},
	exit_state_ {loop_},
	start_state_ {&prepare_download_state_},
	state_machine_ {*start_state_} {
	using tf = common::state_machine::TransitionFlag;
	using se = StateEvent;
	auto &s = state_machine_;

	// clang-format off
	s.AddTransition(prepare_download_state_,          se::Success,              download_enter_state_,            tf::Immediate);
	s.AddTransition(prepare_download_state_,          se::Failure,              exit_state_,                      tf::Immediate);
	s.AddTransition(prepare_download_state_,          se::EmptyPayloadArtifact, exit_state_,                      tf::Immediate);

	s.AddTransition(download_enter_state_,            se::Success,              download_state_,                  tf::Immediate);
	s.AddTransition(download_enter_state_,            se::Failure,              download_error_state_,            tf::Immediate);

	s.AddTransition(download_state_,                  se::Success,              download_leave_state_,            tf::Immediate);
	s.AddTransition(download_state_,                  se::Failure,              download_error_state_,            tf::Immediate);

	s.AddTransition(download_leave_state_,            se::Success,              save_artifact_install_state_,     tf::Immediate);
	s.AddTransition(download_leave_state_,            se::Failure,              download_error_state_,            tf::Immediate);

	s.AddTransition(download_error_state_,            se::Success,              save_cleanup_state_,              tf::Immediate);
	s.AddTransition(download_error_state_,            se::Failure,              save_cleanup_state_,              tf::Immediate);

	s.AddTransition(save_artifact_install_state_,     se::Success,              artifact_install_enter_state_,    tf::Immediate);
	s.AddTransition(save_artifact_install_state_,     se::Failure,              save_cleanup_state_,              tf::Immediate);

	s.AddTransition(artifact_install_enter_state_,    se::Success,              artifact_install_state_,          tf::Immediate);
	s.AddTransition(artifact_install_enter_state_,    se::Failure,              artifact_install_error_state_,    tf::Immediate);

	s.AddTransition(artifact_install_state_,          se::Success,              artifact_install_leave_state_,    tf::Immediate);
	s.AddTransition(artifact_install_state_,          se::Failure,              artifact_install_error_state_,    tf::Immediate);

	s.AddTransition(artifact_install_leave_state_,    se::Success,              save_artifact_commit_state_,      tf::Immediate);
	s.AddTransition(artifact_install_leave_state_,    se::Failure,              artifact_install_error_state_,    tf::Immediate);

	s.AddTransition(artifact_install_error_state_,    se::Success,              rollback_query_state_,            tf::Immediate);
	s.AddTransition(artifact_install_error_state_,    se::Failure,              rollback_query_state_,            tf::Immediate);

	s.AddTransition(save_artifact_commit_state_,      se::Success,              reboot_and_rollback_query_state_, tf::Immediate);
	s.AddTransition(save_artifact_commit_state_,      se::Failure,              reboot_and_rollback_query_state_, tf::Immediate);

	s.AddTransition(reboot_and_rollback_query_state_, se::Success,              artifact_commit_enter_state_,     tf::Immediate);
	s.AddTransition(reboot_and_rollback_query_state_, se::Failure,              rollback_query_state_,            tf::Immediate);
	s.AddTransition(reboot_and_rollback_query_state_, se::NeedsInteraction,     exit_state_,                      tf::Immediate);

	s.AddTransition(artifact_commit_enter_state_,     se::Success,              artifact_commit_state_,           tf::Immediate);
	s.AddTransition(artifact_commit_enter_state_,     se::Failure,              artifact_commit_error_state_,     tf::Immediate);

	s.AddTransition(artifact_commit_state_,           se::Success,              artifact_commit_leave_state_,     tf::Immediate);
	s.AddTransition(artifact_commit_state_,           se::Failure,              artifact_commit_error_state_,     tf::Immediate);

	s.AddTransition(artifact_commit_leave_state_,     se::Success,              save_cleanup_state_,              tf::Immediate);
	s.AddTransition(artifact_commit_leave_state_,     se::Failure,              save_cleanup_state_,              tf::Immediate);

	s.AddTransition(rollback_query_state_,            se::Success,              save_artifact_rollback_state_,    tf::Immediate);
	s.AddTransition(rollback_query_state_,            se::NothingToDo,          save_artifact_failure_state_,     tf::Immediate);
	s.AddTransition(rollback_query_state_,            se::Failure,              save_artifact_failure_state_,     tf::Immediate);
	s.AddTransition(rollback_query_state_,            se::NeedsInteraction,     exit_state_,                      tf::Immediate);

	s.AddTransition(artifact_commit_error_state_,     se::Success,              rollback_query_state_,            tf::Immediate);
	s.AddTransition(artifact_commit_error_state_,     se::Failure,              rollback_query_state_,            tf::Immediate);

	s.AddTransition(save_artifact_rollback_state_,    se::Success,              artifact_rollback_enter_state_,   tf::Immediate);
	s.AddTransition(save_artifact_rollback_state_,    se::Failure,              artifact_rollback_enter_state_,   tf::Immediate);

	s.AddTransition(artifact_rollback_enter_state_,   se::Success,              artifact_rollback_state_,         tf::Immediate);
	s.AddTransition(artifact_rollback_enter_state_,   se::Failure,              artifact_rollback_state_,         tf::Immediate);

	s.AddTransition(artifact_rollback_state_,         se::Success,              artifact_rollback_leave_state_,   tf::Immediate);
	s.AddTransition(artifact_rollback_state_,         se::Failure,              artifact_rollback_leave_state_,   tf::Immediate);

	s.AddTransition(artifact_rollback_leave_state_,   se::Success,              save_artifact_failure_state_,     tf::Immediate);
	s.AddTransition(artifact_rollback_leave_state_,   se::Failure,              save_artifact_failure_state_,     tf::Immediate);

	s.AddTransition(save_artifact_failure_state_,     se::Success,              artifact_failure_enter_state_,    tf::Immediate);
	s.AddTransition(save_artifact_failure_state_,     se::Failure,              artifact_failure_enter_state_,    tf::Immediate);

	s.AddTransition(artifact_failure_enter_state_,    se::Success,              artifact_failure_state_,          tf::Immediate);
	s.AddTransition(artifact_failure_enter_state_,    se::Failure,              artifact_failure_state_,          tf::Immediate);

	s.AddTransition(artifact_failure_state_,          se::Success,              artifact_failure_leave_state_,    tf::Immediate);
	s.AddTransition(artifact_failure_state_,          se::Failure,              artifact_failure_leave_state_,    tf::Immediate);

	s.AddTransition(artifact_failure_leave_state_,    se::Success,              save_cleanup_state_,              tf::Immediate);
	s.AddTransition(artifact_failure_leave_state_,    se::Failure,              save_cleanup_state_,              tf::Immediate);

	s.AddTransition(save_cleanup_state_,              se::Success,              cleanup_state_,                   tf::Immediate);
	s.AddTransition(save_cleanup_state_,              se::Failure,              cleanup_state_,                   tf::Immediate);

	s.AddTransition(cleanup_state_,                   se::Success,              exit_state_,                      tf::Immediate);
	s.AddTransition(cleanup_state_,                   se::Failure,              exit_state_,                      tf::Immediate);
	// clang-format on
}

void StateMachine::Run() {
	common::state_machine::StateMachineRunner<Context, StateEvent> runner {context_};
	runner.AddStateMachine(state_machine_);
	runner.AttachToEventLoop(loop_);

	state_machine_.SetState(*start_state_);

	loop_.Run();
}

error::Error StateMachine::SetStartStateFromStateData(const string &in_state) {
	if (in_state == "ArtifactInstall") {
		start_state_ = &artifact_install_enter_state_;
	} else if (in_state == "ArtifactCommit") {
		start_state_ = &artifact_commit_enter_state_;
	} else if (in_state == "ArtifactCommit_Leave") {
		start_state_ = &artifact_commit_leave_state_;
	} else if (in_state == "Cleanup") {
		start_state_ = &cleanup_state_;
	} else if (in_state == "ArtifactRollback") {
		start_state_ = &artifact_rollback_enter_state_;
	} else if (in_state == "ArtifactFailure") {
		start_state_ = &artifact_failure_enter_state_;
	} else {
		return context::MakeError(
			context::DatabaseValueError, "Invalid InState in database " + in_state);
	}

	return error::NoError;
}

error::Error StateMachine::SetStopAfterState(const string &state) {
	using tf = common::state_machine::TransitionFlag;
	using se = StateEvent;
	auto &s = state_machine_;

	// Replace transition in state machine in order to exit at given point.
	if (state == "Download") {
		s.AddTransition(save_artifact_install_state_, se::Success, exit_state_, tf::Immediate);

	} else if (state == "ArtifactInstall") {
		s.AddTransition(save_artifact_commit_state_, se::Success, exit_state_, tf::Immediate);

	} else if (state == "ArtifactCommit") {
		s.AddTransition(save_cleanup_state_, se::Success, exit_state_, tf::Immediate);

	} else if (state != "") {
		return context::MakeError(context::ValueError, "Cannot stop after unknown state " + state);
	}

	return error::NoError;
}

void StateMachine::StartOnRollback() {
	start_state_ = &rollback_query_state_;
}

error::Error PrepareContext(Context &ctx) {
	const auto &default_paths {ctx.main_context.GetConfig().paths};
	ctx.script_runner.reset(new executor::ScriptRunner(
		ctx.loop,
		chrono::seconds {ctx.main_context.GetConfig().state_script_timeout_seconds},
		chrono::seconds {ctx.main_context.GetConfig().state_script_retry_interval_seconds},
		chrono::seconds {ctx.main_context.GetConfig().state_script_retry_timeout_seconds},
		default_paths.GetArtScriptsPath(),
		default_paths.GetRootfsScriptsPath()));

	return error::NoError;
}

error::Error PrepareContextFromStateData(Context &ctx, const StateData &data) {
	ctx.update_module.reset(
		new update_module::UpdateModule(ctx.main_context, data.payload_types[0]));

	if (data.payload_types[0] == "rootfs-image") {
		// Special case for rootfs-image upgrades. See comments inside the function.
		auto err = ctx.update_module->EnsureRootfsImageFileTree(
			ctx.update_module->GetUpdateModuleWorkDir());
		if (err != error::NoError) {
			return err;
		}
	}

	if (data.failed) {
		ctx.result_and_error.result = ctx.result_and_error.result | Result::Failed;
	}

	if (data.rolled_back) {
		ctx.result_and_error.result = ctx.result_and_error.result | Result::RolledBack;
	}

	return error::NoError;
}

ResultAndError Install(
	Context &ctx,
	const string &src,
	const artifact::config::Signature verify_signature,
	InstallOptions options) {
	auto exp_in_progress = LoadStateData(ctx.main_context.GetMenderStoreDB());
	if (!exp_in_progress) {
		return {Result::Failed, exp_in_progress.error()};
	}
	auto &in_progress = exp_in_progress.value();

	if (in_progress) {
		return {
			Result::Failed | Result::NoRollbackNecessary,
			error::Error(
				make_error_condition(errc::operation_in_progress),
				"Update already in progress. Please commit or roll back first")};
	}

	auto err = PrepareContext(ctx);
	if (err != error::NoError) {
		return {Result::Failed, err};
	}

	ctx.artifact_src = src;
	ctx.verify_signature = verify_signature;
	ctx.options = options;

	StateMachine state_machine {ctx};

	err = state_machine.SetStopAfterState(ctx.stop_after);
	if (err != error::NoError) {
		return {Result::Failed, err};
	}

	state_machine.Run();

	return ctx.result_and_error;
}

ResultAndError Resume(Context &ctx) {
	auto exp_in_progress = LoadStateData(ctx.main_context.GetMenderStoreDB());
	if (!exp_in_progress) {
		return {Result::Failed, exp_in_progress.error()};
	}
	auto &in_progress = exp_in_progress.value();

	if (!in_progress) {
		return {
			Result::NoUpdateInProgress,
			context::MakeError(context::NoUpdateInProgressError, "Cannot resume")};
	}
	ctx.state_data = in_progress.value();

	auto err = PrepareContext(ctx);
	if (err != error::NoError) {
		return {Result::Failed, err};
	}

	err = PrepareContextFromStateData(ctx, ctx.state_data);
	if (err != error::NoError) {
		return {Result::Failed, err};
	}

	StateMachine state_machine {ctx};

	err = state_machine.SetStartStateFromStateData(ctx.state_data.in_state);
	if (err != error::NoError) {
		return {Result::Failed, err};
	}

	err = state_machine.SetStopAfterState(ctx.stop_after);
	if (err != error::NoError) {
		return {Result::Failed, err};
	}

	state_machine.Run();

	return ctx.result_and_error;
}

ResultAndError Commit(Context &ctx) {
	auto exp_in_progress = LoadStateData(ctx.main_context.GetMenderStoreDB());
	if (!exp_in_progress) {
		return {Result::Failed, exp_in_progress.error()};
	}
	auto &in_progress = exp_in_progress.value();

	if (!in_progress) {
		return {
			Result::NoUpdateInProgress,
			context::MakeError(context::NoUpdateInProgressError, "Cannot commit")};
	}
	ctx.state_data = in_progress.value();

	if (ctx.state_data.in_state != "ArtifactCommit") {
		return {
			Result::Failed,
			context::MakeError(
				context::WrongOperationError,
				"Cannot commit from this state. "
				"Make sure that the `install` command has run successfully and the device is expecting a commit.")};
	}

	auto err = PrepareContext(ctx);
	if (err != error::NoError) {
		return {Result::Failed, err};
	}

	err = PrepareContextFromStateData(ctx, ctx.state_data);
	if (err != error::NoError) {
		return {Result::Failed, err};
	}

	StateMachine state_machine {ctx};

	err = state_machine.SetStartStateFromStateData(ctx.state_data.in_state);
	if (err != error::NoError) {
		return {Result::Failed, err};
	}

	state_machine.Run();

	return ctx.result_and_error;
}

ResultAndError Rollback(Context &ctx) {
	auto exp_in_progress = LoadStateData(ctx.main_context.GetMenderStoreDB());
	if (!exp_in_progress) {
		return {Result::Failed, exp_in_progress.error()};
	}
	auto &in_progress = exp_in_progress.value();

	if (!in_progress) {
		return {
			Result::NoUpdateInProgress,
			context::MakeError(context::NoUpdateInProgressError, "Cannot roll back")};
	}
	ctx.state_data = in_progress.value();

	if (ctx.state_data.in_state != "ArtifactCommit") {
		return {
			Result::Failed,
			context::MakeError(
				context::WrongOperationError,
				"Cannot commit from this state. "
				"Make sure that the `install` command has run successfully and the device is expecting a commit.")};
	}

	auto err = PrepareContext(ctx);
	if (err != error::NoError) {
		return {Result::Failed, err};
	}

	err = PrepareContextFromStateData(ctx, ctx.state_data);
	if (err != error::NoError) {
		return {Result::Failed, err};
	}

	StateMachine state_machine {ctx};
	state_machine.StartOnRollback();
	if (err != error::NoError) {
		return {Result::Failed, err};
	}
	state_machine.Run();

	return ctx.result_and_error;
}

} // namespace standalone
} // namespace update
} // namespace mender
