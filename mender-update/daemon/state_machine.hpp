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

	error::Error Run();

private:
	events::EventLoop &event_loop_;

	///////////////////////////////////////////////////////////////////////////////////////////
	// Main states
	///////////////////////////////////////////////////////////////////////////////////////////

	IdleState idle_state_;
	SubmitInventoryState submit_inventory_state_;
	PollForDeploymentState poll_for_deployment_state_;
	UpdateDownloadState update_download_state_;
	UpdateInstallState update_install_state_;

	// Currently used same state code for checking NeedsReboot both before normal reboot, and
	// before rollback reboot, since currently they have the same behavior, only different state
	// transitions.
	UpdateCheckRebootState update_check_reboot_state_;
	UpdateCheckRebootState update_check_rollback_reboot_state_;

	UpdateRebootState update_reboot_state_;
	UpdateVerifyRebootState update_verify_reboot_state_;
	UpdateCommitState update_commit_state_;
	UpdateAfterCommitState update_after_commit_state_;
	UpdateCheckRollbackState update_check_rollback_state_;
	UpdateRollbackState update_rollback_state_;
	UpdateRollbackRebootState update_rollback_reboot_state_;
	UpdateVerifyRollbackRebootState update_verify_rollback_reboot_state_;
	UpdateFailureState update_failure_state_;
	UpdateSaveProvidesState update_save_provides_state_;
	UpdateCleanupState update_cleanup_state_;

	ClearArtifactDataState clear_artifact_data_state_;
	StateLoopState state_loop_state_;

	sm::StateMachine<Context, StateEvent> main_states_;

	///////////////////////////////////////////////////////////////////////////////////////////
	// Deployment tracking states
	///////////////////////////////////////////////////////////////////////////////////////////

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
