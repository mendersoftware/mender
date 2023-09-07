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

#ifndef MENDER_UPDATE_STATE_MACHINE_HPP
#define MENDER_UPDATE_STATE_MACHINE_HPP

#include <common/error.hpp>
#include <common/events.hpp>
#include <common/state_machine.hpp>

#include <mender-update/context.hpp>

#include <mender-update/daemon/context.hpp>
#include <mender-update/daemon/state_events.hpp>
#include <mender-update/daemon/states.hpp>

namespace mender {
namespace update {
namespace daemon {

namespace error = mender::common::error;
namespace events = mender::common::events;
namespace sm = mender::common::state_machine;

namespace context = mender::update::context;

class StateMachine {
public:
	StateMachine(Context &ctx, events::EventLoop &event_loop);
	// For tests: Use a state machine with custom minimum wait times.
	StateMachine(
		Context &ctx, events::EventLoop &event_loop, chrono::milliseconds minimum_wait_time);

	void LoadStateFromDb();

	error::Error Run();

	// Mainly for tests.
	void StopAfterDeployment();

private:
	Context &ctx_;
	events::EventLoop &event_loop_;
	events::SignalHandler check_update_handler_;
	events::SignalHandler inventory_update_handler_;
	events::SignalHandler termination_handler_;

	error::Error RegisterSignalHandlers();

	///////////////////////////////////////////////////////////////////////////////////////////
	// Main states
	///////////////////////////////////////////////////////////////////////////////////////////

	InitState init_state_;

	IdleState idle_state_;
	SubmitInventoryState submit_inventory_state_;
	PollForDeploymentState poll_for_deployment_state_;
	SendStatusUpdateState send_download_status_state_;
	UpdateDownloadState update_download_state_;
	SendStatusUpdateState send_install_status_state_;
	UpdateInstallState update_install_state_;

	// Currently used same state code for checking NeedsReboot both before normal reboot, and
	// before rollback reboot, since currently they have the same behavior, only different state
	// transitions.
	UpdateCheckRebootState update_check_reboot_state_;
	UpdateCheckRebootState update_check_rollback_reboot_state_;

	SendStatusUpdateState send_reboot_status_state_;
	UpdateRebootState update_reboot_state_;
	UpdateVerifyRebootState update_verify_reboot_state_;
	SendStatusUpdateState send_commit_status_state_;
	UpdateBeforeCommitState update_before_commit_state_;
	UpdateCommitState update_commit_state_;
	UpdateAfterCommitState update_after_commit_state_;
	UpdateCheckRollbackState update_check_rollback_state_;
	UpdateRollbackState update_rollback_state_;
	UpdateRollbackRebootState update_rollback_reboot_state_;
	UpdateVerifyRollbackRebootState update_verify_rollback_reboot_state_;
	UpdateRollbackSuccessfulState update_rollback_successful_state_;
	UpdateFailureState update_failure_state_;
	UpdateSaveProvidesState update_save_provides_state_;
	UpdateRollbackSuccessfulState update_rollback_not_needed_state_;
	UpdateCleanupState update_cleanup_state_;
	SendStatusUpdateState send_final_status_state_;
	ClearArtifactDataState clear_artifact_data_state_;

	StateLoopState state_loop_state_;

	EndOfDeploymentState end_of_deployment_state_;

	ExitState exit_state_;

	sm::StateMachine<Context, StateEvent> main_states_;

	///////////////////////////////////////////////////////////////////////////////////////////
	// Deployment tracking states
	///////////////////////////////////////////////////////////////////////////////////////////

	class StateScripts {
	public:
		StateScripts(
			events::EventLoop &loop,
			chrono::seconds retry_interval,
			const string &artifact_script_path,
			const string &rootfs_script_path) :
			idle_enter_(
				loop,
				script_executor::State::Idle,
				script_executor::Action::Enter,
				retry_interval,
				artifact_script_path,
				rootfs_script_path),
			first_idle_enter_(
				loop,
				script_executor::State::Idle,
				script_executor::Action::Enter,
				retry_interval,
				artifact_script_path,
				rootfs_script_path),
			idle_leave_deploy_(
				loop,
				script_executor::State::Idle,
				script_executor::Action::Leave,
				retry_interval,
				artifact_script_path,
				rootfs_script_path),
			idle_leave_inv_(
				loop,
				script_executor::State::Idle,
				script_executor::Action::Leave,
				retry_interval,
				artifact_script_path,
				rootfs_script_path),
			sync_enter_deployment_(
				loop,
				script_executor::State::Sync,
				script_executor::Action::Enter,
				retry_interval,
				artifact_script_path,
				rootfs_script_path),
			sync_enter_inventory_(
				loop,
				script_executor::State::Sync,
				script_executor::Action::Enter,
				retry_interval,
				artifact_script_path,
				rootfs_script_path),
			sync_leave_(
				loop,
				script_executor::State::Sync,
				script_executor::Action::Leave,
				retry_interval,
				artifact_script_path,
				rootfs_script_path),
			sync_leave_download_(
				loop,
				script_executor::State::Sync,
				script_executor::Action::Leave,
				retry_interval,
				artifact_script_path,
				rootfs_script_path),
			sync_error_(
				loop,
				script_executor::State::Sync,
				script_executor::Action::Error,
				retry_interval,
				artifact_script_path,
				rootfs_script_path),
			download_enter_(
				loop,
				script_executor::State::Download,
				script_executor::Action::Enter,
				retry_interval,
				artifact_script_path,
				rootfs_script_path,
				Context::kUpdateStateDownload),
			download_leave_(
				loop,
				script_executor::State::Download,
				script_executor::Action::Leave,
				retry_interval,
				artifact_script_path,
				rootfs_script_path),
			download_leave_for_save_provides(
				loop,
				script_executor::State::Download,
				script_executor::Action::Leave,
				retry_interval,
				artifact_script_path,
				rootfs_script_path),
			download_error_(
				loop,
				script_executor::State::Download,
				script_executor::Action::Error,
				retry_interval,
				artifact_script_path,
				rootfs_script_path),
			install_enter_(
				loop,
				script_executor::State::ArtifactInstall,
				script_executor::Action::Enter,
				retry_interval,
				artifact_script_path,
				rootfs_script_path,
				Context::kUpdateStateArtifactInstall),
			install_leave_(
				loop,
				script_executor::State::ArtifactInstall,
				script_executor::Action::Leave,
				retry_interval,
				artifact_script_path,
				rootfs_script_path),
			install_error_(
				loop,
				script_executor::State::ArtifactInstall,
				script_executor::Action::Error,
				retry_interval,
				artifact_script_path,
				rootfs_script_path),
			install_error_rollback_(
				loop,
				script_executor::State::ArtifactInstall,
				script_executor::Action::Error,
				retry_interval,
				artifact_script_path,
				rootfs_script_path),
			reboot_enter_(
				loop,
				script_executor::State::ArtifactReboot,
				script_executor::Action::Enter,
				retry_interval,
				artifact_script_path,
				rootfs_script_path,
				Context::kUpdateStateArtifactReboot),
			reboot_leave_(
				loop,
				script_executor::State::ArtifactReboot,
				script_executor::Action::Leave,
				retry_interval,
				artifact_script_path,
				rootfs_script_path),
			reboot_error_(
				loop,
				script_executor::State::ArtifactReboot,
				script_executor::Action::Error,
				retry_interval,
				artifact_script_path,
				rootfs_script_path),
			rollback_enter_(
				loop,
				script_executor::State::ArtifactRollback,
				script_executor::Action::Enter,
				retry_interval,
				artifact_script_path,
				rootfs_script_path),
			rollback_leave_(
				loop,
				script_executor::State::ArtifactRollback,
				script_executor::Action::Leave,
				retry_interval,
				artifact_script_path,
				rootfs_script_path),
			rollback_leave_error_(
				loop,
				script_executor::State::ArtifactRollback,
				script_executor::Action::Leave,
				retry_interval,
				artifact_script_path,
				rootfs_script_path),
			commit_enter_(
				loop,
				script_executor::State::ArtifactCommit,
				script_executor::Action::Enter,
				retry_interval,
				artifact_script_path,
				rootfs_script_path),
			commit_leave_(
				loop,
				script_executor::State::ArtifactCommit,
				script_executor::Action::Leave,
				retry_interval,
				artifact_script_path,
				rootfs_script_path),
			commit_error_(
				loop,
				script_executor::State::ArtifactCommit,
				script_executor::Action::Error,
				retry_interval,
				artifact_script_path,
				rootfs_script_path),
			commit_error_save_provides_(
				loop,
				script_executor::State::ArtifactCommit,
				script_executor::Action::Error,
				retry_interval,
				artifact_script_path,
				rootfs_script_path),
			failure_enter_(
				loop,
				script_executor::State::ArtifactFailure,
				script_executor::Action::Enter,
				retry_interval,
				artifact_script_path,
				rootfs_script_path,
				Context::kUpdateStateArtifactFailure,
				true), // IsFailureState
			failure_leave_update_save_provides_(
				loop,
				script_executor::State::ArtifactFailure,
				script_executor::Action::Leave,
				retry_interval,
				artifact_script_path,
				rootfs_script_path),
			failure_leave_state_loop_state_(
				loop,
				script_executor::State::ArtifactFailure,
				script_executor::Action::Leave,
				retry_interval,
				artifact_script_path,
				rootfs_script_path),
			rollback_reboot_enter_(
				loop,
				script_executor::State::ArtifactRollbackReboot,
				script_executor::Action::Enter,
				retry_interval,
				artifact_script_path,
				rootfs_script_path,
				Context::kUpdateStateArtifactRollbackReboot),
			rollback_reboot_leave_(
				loop,
				script_executor::State::ArtifactRollbackReboot,
				script_executor::Action::Leave,
				retry_interval,
				artifact_script_path,
				rootfs_script_path),
			rollback_reboot_error_(
				loop,
				script_executor::State::ArtifactRollbackReboot,
				script_executor::Action::Error,
				retry_interval,
				artifact_script_path,
				rootfs_script_path) {};

		StateScriptState idle_enter_;
		StateScriptState first_idle_enter_;
		StateScriptState idle_leave_deploy_;
		StateScriptState idle_leave_inv_;

		StateScriptState sync_enter_deployment_;
		StateScriptState sync_enter_inventory_;
		StateScriptState sync_leave_;
		StateScriptState sync_leave_download_;
		StateScriptState sync_error_;

		SaveStateScriptState download_enter_;
		StateScriptState download_leave_;
		StateScriptState download_leave_for_save_provides;
		StateScriptState download_error_;

		SaveStateScriptState install_enter_;
		StateScriptState install_leave_;
		StateScriptState install_error_;
		StateScriptState install_error_rollback_;

		SaveStateScriptState reboot_enter_;
		StateScriptState reboot_leave_;
		StateScriptState reboot_error_;

		StateScriptState rollback_enter_;
		StateScriptState rollback_leave_;
		StateScriptState rollback_leave_error_;

		StateScriptState commit_enter_;
		StateScriptState commit_leave_;
		StateScriptState commit_error_;
		StateScriptState commit_error_save_provides_;

		SaveStateScriptState failure_enter_;
		StateScriptState failure_leave_update_save_provides_;
		StateScriptState failure_leave_state_loop_state_;

		SaveStateScriptState rollback_reboot_enter_;
		StateScriptState rollback_reboot_leave_;
		StateScriptState rollback_reboot_error_;

	} state_scripts_;


	class DeploymentTracking {
	public:
		DeploymentTracking();

		EmptyState idle_state_;
		deployment_tracking::NoFailuresState no_failures_state_;
		deployment_tracking::FailureState failure_state_;
		deployment_tracking::RollbackAttemptedState rollback_attempted_state_;
		deployment_tracking::RollbackFailedState rollback_failed_state_;

		// Not used for actual deployment work (that's main states), but for tracking the failure
		// and rollback events. This is used to automatically update the running context so that the
		// correct database entries are saved at the end of the update. The alternative to this
		// state machine would be to update the context in every state that can fail, but this state
		// machine does it automatically based on the submitted events.
		sm::StateMachine<Context, StateEvent> states_;
	} deployment_tracking_;

	sm::StateMachineRunner<Context, StateEvent> runner_;
};

} // namespace daemon
} // namespace update
} // namespace mender

#endif // MENDER_UPDATE_STATE_MACHINE_HPP
