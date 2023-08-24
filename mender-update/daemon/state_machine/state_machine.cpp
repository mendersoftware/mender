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

#include <common/conf.hpp>
#include <common/log.hpp>

#include <mender-update/daemon/states.hpp>
#include <mender-update/daemon/state_machine.hpp>

namespace mender {
namespace update {
namespace daemon {

namespace conf = mender::common::conf;
namespace log = mender::common::log;

StateMachine::StateMachine(Context &ctx, events::EventLoop &event_loop) :
	ctx_(ctx),
	event_loop_(event_loop),
	check_update_handler_(event_loop),
	inventory_update_handler_(event_loop),
	termination_handler_(event_loop),
	submit_inventory_state_(event_loop),
	poll_for_deployment_state_(event_loop),
	send_download_status_state_(deployments::DeploymentStatus::Downloading),
	send_install_status_state_(deployments::DeploymentStatus::Installing),
	send_reboot_status_state_(deployments::DeploymentStatus::Rebooting),
	send_commit_status_state_(
		deployments::DeploymentStatus::Installing,
		event_loop,
		ctx.mender_context.GetConfig().retry_poll_interval_seconds),
	// nullopt means: Fetch success/failure status from deployment context
	send_final_status_state_(
		optional::nullopt, event_loop, ctx.mender_context.GetConfig().retry_poll_interval_seconds),
	exit_state_(event_loop),
	main_states_(idle_state_),
	state_scripts_(
		event_loop,
		chrono::seconds {ctx.mender_context.GetConfig().state_script_timeout_seconds},
		ctx_.mender_context.GetConfig().paths.GetArtScriptsPath(),
		ctx_.mender_context.GetConfig().paths.GetRootfsScriptsPath()),
	runner_(ctx) {
	runner_.AddStateMachine(main_states_);
	runner_.AddStateMachine(deployment_tracking_.states_);

	runner_.AttachToEventLoop(event_loop_);

	using se = StateEvent;
	using tf = sm::TransitionFlag;

	// When updating the table below, make sure that the initial states are in sync as well, in
	// LoadStateFromDb().

	// clang-format off
	main_states_.AddTransition(idle_state_,                          se::DeploymentPollingTriggered, poll_for_deployment_state_,           tf::Deferred );
	main_states_.AddTransition(idle_state_,                          se::InventoryPollingTriggered,  submit_inventory_state_,              tf::Deferred );

	main_states_.AddTransition(submit_inventory_state_,              se::Success,                    idle_state_,                          tf::Immediate);
	main_states_.AddTransition(submit_inventory_state_,              se::Failure,                    idle_state_,                          tf::Immediate);

	main_states_.AddTransition(poll_for_deployment_state_,           se::Success,                    send_download_status_state_,          tf::Immediate);
	main_states_.AddTransition(poll_for_deployment_state_,           se::NothingToDo,                idle_state_,                          tf::Immediate);
	main_states_.AddTransition(poll_for_deployment_state_,           se::Failure,                    idle_state_,                          tf::Immediate);

	// Cannot fail due to FailureMode::Ignore.
	main_states_.AddTransition(send_download_status_state_,          se::Success,                    update_download_state_,               tf::Immediate);

	main_states_.AddTransition(update_download_state_,               se::Success,                    state_scripts_.install_enter_,           tf::Immediate);
	main_states_.AddTransition(update_download_state_,               se::Failure,                    update_rollback_not_needed_state_,    tf::Immediate);
	// Empty payload
	main_states_.AddTransition(update_download_state_,               se::NothingToDo,                update_save_provides_state_,          tf::Immediate);
	main_states_.AddTransition(update_download_state_,               se::StateLoopDetected,          state_loop_state_,                    tf::Immediate);

   	main_states_.AddTransition(state_scripts_.install_enter_,             se::Success,              send_install_status_state_,                tf::Immediate);
   	main_states_.AddTransition(state_scripts_.install_enter_,             se::Failure,              state_scripts_.install_error_,             tf::Immediate);
   	main_states_.AddTransition(state_scripts_.install_error_,             se::Success,              state_scripts_.failure_enter_,                     tf::Immediate);
   	main_states_.AddTransition(state_scripts_.install_error_,             se::Failure,              state_scripts_.failure_enter_,                     tf::Immediate);
   	//                                                                    Cannot                    fail                                       due    to    FailureMode::Ignore.
   	main_states_.AddTransition(send_install_status_state_,                se::Success,              update_install_state_,                     tf::Immediate);
   	main_states_.AddTransition(update_install_state_,                     se::Success,              state_scripts_.install_leave_,             tf::Immediate);
   	main_states_.AddTransition(update_install_state_,                     se::Failure,              state_scripts_.install_error_rollback_,    tf::Immediate);
   	main_states_.AddTransition(update_install_state_,                     se::StateLoopDetected,    state_loop_state_,                         tf::Immediate);

   	main_states_.AddTransition(state_scripts_.install_leave_,             se::Success,              update_check_reboot_state_,                tf::Immediate);
   	main_states_.AddTransition(state_scripts_.install_leave_,             se::Failure,              state_scripts_.install_error_rollback_,    tf::Immediate);
   	main_states_.AddTransition(state_scripts_.install_error_rollback_,    se::Success,              update_check_rollback_state_,              tf::Immediate);
   	main_states_.AddTransition(state_scripts_.install_error_rollback_,    se::Failure,              update_check_rollback_state_,              tf::Immediate);


   	main_states_.AddTransition(state_scripts_.failure_enter_,             se::Success,              update_failure_state_,                     tf::Immediate);
   	main_states_.AddTransition(state_scripts_.failure_enter_,             se::Failure,              update_failure_state_,                     tf::Immediate);


	main_states_.AddTransition(update_check_reboot_state_,           se::Success,                    send_reboot_status_state_,            tf::Immediate);
	main_states_.AddTransition(update_check_reboot_state_,           se::NothingToDo,                update_before_commit_state_,          tf::Immediate);
	main_states_.AddTransition(update_check_reboot_state_,           se::Failure,                    update_check_rollback_state_,         tf::Immediate);
	main_states_.AddTransition(update_check_reboot_state_,           se::StateLoopDetected,          state_loop_state_,                    tf::Immediate);

	// Cannot fail due to FailureMode::Ignore.
   	main_states_.AddTransition(send_reboot_status_state_,       se::Success,              state_scripts_.reboot_enter_,    tf::Immediate);
   	main_states_.AddTransition(state_scripts_.reboot_enter_,    se::Success,              update_reboot_state_,            tf::Immediate);
   	main_states_.AddTransition(state_scripts_.reboot_enter_,    se::Failure,              state_scripts_.reboot_error_,    tf::Immediate);

   	main_states_.AddTransition(update_reboot_state_,            se::Success,              update_verify_reboot_state_,     tf::Immediate);
   	main_states_.AddTransition(update_reboot_state_,            se::Failure,              state_scripts_.reboot_error_,    tf::Immediate);
   	main_states_.AddTransition(state_scripts_.reboot_error_,    se::Success,              update_check_rollback_state_,    tf::Immediate);
   	main_states_.AddTransition(state_scripts_.reboot_error_,    se::Failure,              update_check_rollback_state_,    tf::Immediate);
   	main_states_.AddTransition(update_reboot_state_,            se::StateLoopDetected,    state_loop_state_,               tf::Immediate);

   	main_states_.AddTransition(update_verify_reboot_state_,     se::Success,              state_scripts_.reboot_leave_,    tf::Immediate);
   	main_states_.AddTransition(state_scripts_.reboot_leave_,    se::Success,              update_before_commit_state_,     tf::Immediate);
   	main_states_.AddTransition(state_scripts_.reboot_leave_,    se::Failure,              state_scripts_.reboot_error_,    tf::Immediate);

   	main_states_.AddTransition(update_verify_reboot_state_,     se::Failure,              state_scripts_.reboot_error_,    tf::Immediate);
   	main_states_.AddTransition(update_verify_reboot_state_,     se::StateLoopDetected,    state_loop_state_,               tf::Immediate);

	// Cannot fail.
   	main_states_.AddTransition(update_before_commit_state_,                   se::Success,              send_commit_status_state_,                     tf::Immediate);

   	main_states_.AddTransition(send_commit_status_state_,                     se::Success,              state_scripts_.commit_enter_,                  tf::Immediate);
   	main_states_.AddTransition(send_commit_status_state_,                     se::Failure,              update_check_rollback_state_,                  tf::Immediate);

   	main_states_.AddTransition(state_scripts_.commit_enter_,                  se::Success,              update_commit_state_,                          tf::Immediate);
   	main_states_.AddTransition(state_scripts_.commit_enter_,                  se::Failure,              state_scripts_.commit_error_,                  tf::Immediate);

   	main_states_.AddTransition(state_scripts_.commit_error_,                  se::Success,              update_check_rollback_state_,                  tf::Immediate);
   	main_states_.AddTransition(state_scripts_.commit_error_,                  se::Failure,              update_check_rollback_state_,                  tf::Immediate);

   	main_states_.AddTransition(update_commit_state_,                          se::Success,              update_after_commit_state_,                    tf::Immediate);
   	main_states_.AddTransition(update_commit_state_,                          se::Failure,              state_scripts_.commit_error_,                  tf::Immediate);
   	main_states_.AddTransition(update_commit_state_,                          se::StateLoopDetected,    state_loop_state_,                             tf::Immediate);

   	main_states_.AddTransition(state_scripts_.commit_leave_,                  se::Success,              update_check_rollback_state_,                  tf::Immediate);
   	main_states_.AddTransition(state_scripts_.commit_leave_,                  se::Failure,              state_scripts_.commit_error_,                  tf::Immediate);

   	main_states_.AddTransition(update_after_commit_state_,                    se::Success,              state_scripts_.commit_leave_,                  tf::Immediate);
   	main_states_.AddTransition(update_after_commit_state_,                    se::Failure,              state_scripts_.commit_error_save_provides_,    tf::Immediate);
   	main_states_.AddTransition(update_after_commit_state_,                    se::StateLoopDetected,    state_loop_state_,                             tf::Immediate);

   	main_states_.AddTransition(state_scripts_.commit_leave_,                  se::Success,              update_save_provides_state_,                   tf::Immediate);
   	main_states_.AddTransition(state_scripts_.commit_leave_,                  se::Failure,              update_save_provides_state_,                   tf::Immediate);

   	main_states_.AddTransition(state_scripts_.commit_error_save_provides_,    se::Success,              update_save_provides_state_,                   tf::Immediate);
   	main_states_.AddTransition(state_scripts_.commit_error_save_provides_,    se::Failure,              update_save_provides_state_,                   tf::Immediate);

    main_states_.AddTransition(update_check_rollback_state_,      se::Success,              state_scripts_.rollback_enter_,         tf::Immediate);
    main_states_.AddTransition(state_scripts_.rollback_enter_,    se::Success,              update_rollback_state_,                 tf::Immediate);
    main_states_.AddTransition(state_scripts_.rollback_enter_,    se::Failure,              state_scripts_.failure_enter_,          tf::Immediate);

    main_states_.AddTransition(update_check_rollback_state_,      se::NothingToDo,          state_scripts_.failure_enter_,          tf::Immediate);
    main_states_.AddTransition(update_check_rollback_state_,      se::Failure,              state_scripts_.failure_enter_,          tf::Immediate);
    main_states_.AddTransition(update_check_rollback_state_,      se::StateLoopDetected,    state_loop_state_,                      tf::Immediate);

    main_states_.AddTransition(update_rollback_state_,            se::Success,              state_scripts_.rollback_leave_,         tf::Immediate);
   	main_states_.AddTransition(state_scripts_.rollback_leave_,    se::Success,              update_check_rollback_reboot_state_,    tf::Immediate);
   	main_states_.AddTransition(state_scripts_.rollback_leave_,    se::Failure,              state_scripts_.failure_enter_,          tf::Immediate);

   	main_states_.AddTransition(update_rollback_state_,            se::Failure,              state_scripts_.rollback_error_,         tf::Immediate);
   	main_states_.AddTransition(state_scripts_.rollback_error_,    se::Success,              state_scripts_.failure_enter_,          tf::Immediate);
   	main_states_.AddTransition(state_scripts_.rollback_error_,    se::Failure,              state_scripts_.failure_enter_,          tf::Immediate);

	main_states_.AddTransition(update_rollback_state_,               se::StateLoopDetected,          state_loop_state_,                    tf::Immediate);

	main_states_.AddTransition(update_check_rollback_reboot_state_,  se::Success,                    update_rollback_reboot_state_,        tf::Immediate);
	main_states_.AddTransition(update_check_rollback_reboot_state_,  se::NothingToDo,                update_rollback_successful_state_,    tf::Immediate);
	main_states_.AddTransition(update_check_rollback_reboot_state_,  se::Failure,                    state_scripts_.failure_enter_,                tf::Immediate);
	main_states_.AddTransition(update_check_rollback_reboot_state_,  se::StateLoopDetected,          state_loop_state_,                    tf::Immediate);

	// No Failure transition for this state, see comments in handler.
	main_states_.AddTransition(update_rollback_reboot_state_,        se::Success,                    update_verify_rollback_reboot_state_, tf::Immediate);
	main_states_.AddTransition(update_rollback_reboot_state_,        se::StateLoopDetected,          state_loop_state_,                    tf::Immediate);

	main_states_.AddTransition(update_verify_rollback_reboot_state_, se::Success,                    update_rollback_successful_state_,    tf::Immediate);
	main_states_.AddTransition(update_verify_rollback_reboot_state_, se::Retry,                      update_rollback_reboot_state_,        tf::Immediate);
	main_states_.AddTransition(update_verify_rollback_reboot_state_, se::StateLoopDetected,          state_loop_state_,                    tf::Immediate);

   	main_states_.AddTransition(update_rollback_successful_state_,                     se::Success,              state_scripts_.failure_enter_,                         tf::Immediate);

   	main_states_.AddTransition(update_failure_state_,                                 se::Success,              state_scripts_.failure_leave_update_save_provides_,    tf::Immediate);
   	main_states_.AddTransition(update_failure_state_,                                 se::Failure,              state_scripts_.failure_leave_update_save_provides_,    tf::Immediate);
   	main_states_.AddTransition(update_failure_state_,                                 se::StateLoopDetected,    state_scripts_.failure_leave_state_loop_state_,        tf::Immediate);

   	main_states_.AddTransition(state_scripts_.failure_leave_update_save_provides_,    se::Success,              update_save_provides_state_,                           tf::Immediate);
   	main_states_.AddTransition(state_scripts_.failure_leave_update_save_provides_,    se::Failure,              update_save_provides_state_,                           tf::Immediate);

   	main_states_.AddTransition(state_scripts_.failure_leave_state_loop_state_,        se::Success,              state_loop_state_,                                     tf::Immediate);
   	main_states_.AddTransition(state_scripts_.failure_leave_state_loop_state_,        se::Failure,              state_loop_state_,                                     tf::Immediate);

	main_states_.AddTransition(update_save_provides_state_,          se::Success,                    update_cleanup_state_,                tf::Immediate);
	// Even if this fails, there is nothing we can do at this point.
	main_states_.AddTransition(update_save_provides_state_,          se::Failure,                    update_cleanup_state_,                tf::Immediate);
	main_states_.AddTransition(update_save_provides_state_,          se::StateLoopDetected,          state_loop_state_,                    tf::Immediate);

	main_states_.AddTransition(update_rollback_not_needed_state_,    se::Success,                    update_cleanup_state_,                tf::Immediate);

	main_states_.AddTransition(update_cleanup_state_,                se::Success,                    send_final_status_state_,             tf::Immediate);
	main_states_.AddTransition(update_cleanup_state_,                se::Failure,                    send_final_status_state_,             tf::Immediate);
	main_states_.AddTransition(update_cleanup_state_,                se::StateLoopDetected,          state_loop_state_,                    tf::Immediate);

	main_states_.AddTransition(state_loop_state_,                    se::Success,                    send_final_status_state_,             tf::Immediate);
	main_states_.AddTransition(state_loop_state_,                    se::Failure,                    send_final_status_state_,             tf::Immediate);

	main_states_.AddTransition(send_final_status_state_,             se::Success,                    clear_artifact_data_state_,           tf::Immediate);
	main_states_.AddTransition(send_final_status_state_,             se::Failure,                    clear_artifact_data_state_,           tf::Immediate);

	main_states_.AddTransition(clear_artifact_data_state_,           se::Success,                    end_of_deployment_state_,             tf::Immediate);
	main_states_.AddTransition(clear_artifact_data_state_,           se::Failure,                    end_of_deployment_state_,             tf::Immediate);

	main_states_.AddTransition(end_of_deployment_state_,             se::Success,                    submit_inventory_state_,              tf::Immediate);

	auto &dt = deployment_tracking_;

	dt.states_.AddTransition(dt.idle_state_,                         se::DeploymentStarted,          dt.no_failures_state_,                tf::Immediate);

	dt.states_.AddTransition(dt.no_failures_state_,                  se::Failure,                    dt.failure_state_,                    tf::Immediate);
	dt.states_.AddTransition(dt.no_failures_state_,                  se::DeploymentEnded,            dt.idle_state_,                       tf::Immediate);

	dt.states_.AddTransition(dt.failure_state_,                      se::RollbackStarted,            dt.rollback_attempted_state_,         tf::Immediate);
	dt.states_.AddTransition(dt.failure_state_,                      se::DeploymentEnded,            dt.idle_state_,                       tf::Immediate);

	dt.states_.AddTransition(dt.rollback_attempted_state_,           se::Failure,                    dt.rollback_failed_state_,            tf::Immediate);
	dt.states_.AddTransition(dt.rollback_attempted_state_,           se::DeploymentEnded,            dt.idle_state_,                       tf::Immediate);

	dt.states_.AddTransition(dt.rollback_failed_state_,              se::DeploymentEnded,            dt.idle_state_,                       tf::Immediate);
	// clang-format on
}

StateMachine::StateMachine(
	Context &ctx, events::EventLoop &event_loop, chrono::milliseconds minimum_wait_time) :
	StateMachine(ctx, event_loop) {
	send_commit_status_state_.SetSmallestWaitInterval(minimum_wait_time);
	send_final_status_state_.SetSmallestWaitInterval(minimum_wait_time);
}

StateMachine::DeploymentTracking::DeploymentTracking() :
	states_(idle_state_) {
}

void StateMachine::LoadStateFromDb() {
	unique_ptr<StateData> state_data(new StateData);
	auto exp_loaded = ctx_.LoadDeploymentStateData(*state_data);
	if (!exp_loaded) {
		if (exp_loaded.error().code
			== context::MakeError(context::StateDataStoreCountExceededError, "").code) {
			log::Error("State loop detected. Forcefully aborting update.");

			// This particular error code also fills in state_data.
			ctx_.deployment.state_data = std::move(state_data);

			ctx_.BeginDeploymentLogging();

			main_states_.SetState(state_loop_state_);
			deployment_tracking_.states_.SetState(deployment_tracking_.rollback_failed_state_);
		} else {
			log::Error(
				"Unable to load deployment data from database: " + exp_loaded.error().String());
			log::Error("Starting from initial state");
		}
		return;
	}

	if (!exp_loaded.value()) {
		log::Debug("No existing deployment data, starting from initial state");
		return;
	}

	// We have state data, move it to the context.
	ctx_.deployment.state_data = std::move(state_data);

	ctx_.BeginDeploymentLogging();

	auto &state = ctx_.deployment.state_data->state;

	if (state == ctx_.kUpdateStateDownload) {
		main_states_.SetState(update_cleanup_state_);
		// "rollback_attempted_state" because Download in its nature makes no system
		// changes, so a rollback is a no-op.
		deployment_tracking_.states_.SetState(deployment_tracking_.rollback_attempted_state_);

	} else if (state == ctx_.kUpdateStateArtifactReboot) {
		// Normal update path with a reboot.
		main_states_.SetState(update_verify_reboot_state_);
		deployment_tracking_.states_.SetState(deployment_tracking_.no_failures_state_);

	} else if (state == ctx_.kUpdateStateArtifactRollback) {
		// Installation failed, but rollback could still succeed.
		main_states_.SetState(update_rollback_state_);
		deployment_tracking_.states_.SetState(deployment_tracking_.rollback_attempted_state_);

	} else if (
		state == ctx_.kUpdateStateArtifactRollbackReboot
		|| state == ctx_.kUpdateStateArtifactVerifyRollbackReboot
		|| state == ctx_.kUpdateStateVerifyRollbackReboot) {
		// Normal flow for a rebooting rollback.
		main_states_.SetState(update_verify_rollback_reboot_state_);
		deployment_tracking_.states_.SetState(deployment_tracking_.rollback_attempted_state_);

	} else if (
		state == ctx_.kUpdateStateAfterArtifactCommit
		|| state == ctx_.kUpdateStateUpdateAfterFirstCommit) {
		// Re-run commit Leave scripts if spontaneously rebooted after commit.
		main_states_.SetState(update_after_commit_state_);
		deployment_tracking_.states_.SetState(deployment_tracking_.no_failures_state_);

	} else if (state == ctx_.kUpdateStateArtifactFailure) {
		// Re-run ArtifactFailure if spontaneously rebooted before finishing.
		main_states_.SetState(update_failure_state_);
		if (ctx_.deployment.state_data->update_info.all_rollbacks_successful) {
			deployment_tracking_.states_.SetState(deployment_tracking_.rollback_attempted_state_);
		} else {
			deployment_tracking_.states_.SetState(deployment_tracking_.failure_state_);
		}

	} else if (state == ctx_.kUpdateStateCleanup) {
		// Re-run Cleanup if spontaneously rebooted before finishing.
		main_states_.SetState(update_cleanup_state_);
		if (ctx_.deployment.state_data->update_info.all_rollbacks_successful) {
			deployment_tracking_.states_.SetState(deployment_tracking_.rollback_attempted_state_);
		} else {
			deployment_tracking_.states_.SetState(deployment_tracking_.failure_state_);
		}

	} else {
		// All other states trigger a rollback.
		main_states_.SetState(update_check_rollback_state_);
		deployment_tracking_.states_.SetState(deployment_tracking_.failure_state_);
	}

	auto &payload_types = ctx_.deployment.state_data->update_info.artifact.payload_types;
	assert(payload_types.size() == 1);
	ctx_.deployment.update_module.reset(
		new update_module::UpdateModule(ctx_.mender_context, payload_types[0]));
}

error::Error StateMachine::Run() {
	// Client is supposed to do one handling of each on startup.
	runner_.PostEvent(StateEvent::InventoryPollingTriggered);
	runner_.PostEvent(StateEvent::DeploymentPollingTriggered);

	auto err = RegisterSignalHandlers();
	if (err != error::NoError) {
		return err;
	}

	log::Info("Running Mender client " + conf::kMenderVersion);

	event_loop_.Run();
	log::Debug("State machine finished running");
	std::this_thread::sleep_for(std::chrono::seconds {3}); // TODO - Remove
	return exit_state_.exit_error;
}

void StateMachine::StopAfterDeployment() {
	main_states_.AddTransition(
		end_of_deployment_state_,
		StateEvent::DeploymentEnded,
		exit_state_,
		sm::TransitionFlag::Immediate);
}

} // namespace daemon
} // namespace update
} // namespace mender
