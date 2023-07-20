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

#include <common/log.hpp>
#include <mender-update/daemon/states.hpp>
#include <mender-update/daemon/state_machine.hpp>

namespace mender {
namespace update {
namespace daemon {

namespace log = mender::common::log;

StateMachine::StateMachine(Context &ctx, events::EventLoop &event_loop) :
	event_loop_(event_loop),
	submit_inventory_state_(event_loop),
	poll_for_deployment_state_(event_loop),
	main_states_(idle_state_),
	runner_(ctx) {
	runner_.AddStateMachine(main_states_);
	runner_.AddStateMachine(deployment_tracking_.states_);

	using se = StateEvent;
	using tf = sm::TransitionFlag;

	// clang-format off
	main_states_.AddTransition(idle_state_,                          se::DeploymentPollingTriggered, poll_for_deployment_state_,           tf::Deferred );
	main_states_.AddTransition(idle_state_,                          se::InventoryPollingTriggered,  submit_inventory_state_,              tf::Deferred );

	main_states_.AddTransition(submit_inventory_state_,              se::Success,                    idle_state_,                          tf::Immediate);
	main_states_.AddTransition(submit_inventory_state_,              se::Failure,                    idle_state_,                          tf::Immediate);

	main_states_.AddTransition(poll_for_deployment_state_,           se::Success,                    update_download_state_,               tf::Immediate);
	main_states_.AddTransition(poll_for_deployment_state_,           se::NothingToDo,                idle_state_,                          tf::Immediate);
	main_states_.AddTransition(poll_for_deployment_state_,           se::Failure,                    idle_state_,                          tf::Immediate);

	main_states_.AddTransition(update_download_state_,               se::Success,                    update_install_state_,                tf::Immediate);
	main_states_.AddTransition(update_download_state_,               se::Failure,                    update_cleanup_state_,                tf::Immediate);
	// Empty payload
	main_states_.AddTransition(update_download_state_,               se::NothingToDo,                update_save_provides_state_,          tf::Immediate);
	main_states_.AddTransition(update_download_state_,               se::StateLoopDetected,          state_loop_state_,                    tf::Immediate);

	main_states_.AddTransition(update_install_state_,                se::Success,                    update_check_reboot_state_,           tf::Immediate);
	main_states_.AddTransition(update_install_state_,                se::Failure,                    update_check_rollback_state_,         tf::Immediate);
	main_states_.AddTransition(update_install_state_,                se::StateLoopDetected,          state_loop_state_,                    tf::Immediate);

	main_states_.AddTransition(update_check_reboot_state_,           se::Success,                    update_reboot_state_,                 tf::Immediate);
	main_states_.AddTransition(update_check_reboot_state_,           se::NothingToDo,                update_commit_state_,                 tf::Immediate);
	main_states_.AddTransition(update_check_reboot_state_,           se::Failure,                    update_check_rollback_state_,         tf::Immediate);
	main_states_.AddTransition(update_check_reboot_state_,           se::StateLoopDetected,          state_loop_state_,                    tf::Immediate);

	main_states_.AddTransition(update_reboot_state_,                 se::Success,                    update_verify_reboot_state_,          tf::Immediate);
	main_states_.AddTransition(update_reboot_state_,                 se::Failure,                    update_check_rollback_state_,         tf::Immediate);
	main_states_.AddTransition(update_reboot_state_,                 se::StateLoopDetected,          state_loop_state_,                    tf::Immediate);

	main_states_.AddTransition(update_verify_reboot_state_,          se::Success,                    update_commit_state_,                 tf::Immediate);
	main_states_.AddTransition(update_verify_reboot_state_,          se::Failure,                    update_check_rollback_state_,         tf::Immediate);
	main_states_.AddTransition(update_verify_reboot_state_,          se::StateLoopDetected,          state_loop_state_,                    tf::Immediate);

	main_states_.AddTransition(update_commit_state_,                 se::Success,                    update_after_commit_state_,           tf::Immediate);
	main_states_.AddTransition(update_commit_state_,                 se::Failure,                    update_check_rollback_state_,         tf::Immediate);
	main_states_.AddTransition(update_commit_state_,                 se::StateLoopDetected,          state_loop_state_,                    tf::Immediate);

	main_states_.AddTransition(update_after_commit_state_,           se::Success,                    update_save_provides_state_,          tf::Immediate);
	main_states_.AddTransition(update_after_commit_state_,           se::Failure,                    update_save_provides_state_,          tf::Immediate);
	main_states_.AddTransition(update_after_commit_state_,           se::StateLoopDetected,          state_loop_state_,                    tf::Immediate);

	main_states_.AddTransition(update_check_rollback_state_,         se::Success,                    update_rollback_state_,               tf::Immediate);
	main_states_.AddTransition(update_check_rollback_state_,         se::NothingToDo,                update_failure_state_,                tf::Immediate);
	main_states_.AddTransition(update_check_rollback_state_,         se::Failure,                    update_failure_state_,                tf::Immediate);
	main_states_.AddTransition(update_check_rollback_state_,         se::StateLoopDetected,          state_loop_state_,                    tf::Immediate);

	main_states_.AddTransition(update_rollback_state_,               se::Success,                    update_check_rollback_reboot_state_,  tf::Immediate);
	main_states_.AddTransition(update_rollback_state_,               se::Failure,                    update_failure_state_,                tf::Immediate);
	main_states_.AddTransition(update_rollback_state_,               se::StateLoopDetected,          state_loop_state_,                    tf::Immediate);

	main_states_.AddTransition(update_check_rollback_reboot_state_,  se::Success,                    update_rollback_reboot_state_,        tf::Immediate);
	main_states_.AddTransition(update_check_rollback_reboot_state_,  se::NothingToDo,                update_failure_state_,                tf::Immediate);
	main_states_.AddTransition(update_check_rollback_reboot_state_,  se::Failure,                    update_failure_state_,                tf::Immediate);
	main_states_.AddTransition(update_check_rollback_reboot_state_,  se::StateLoopDetected,          state_loop_state_,                    tf::Immediate);

	main_states_.AddTransition(update_rollback_reboot_state_,        se::Success,                    update_verify_rollback_reboot_state_, tf::Immediate);
	main_states_.AddTransition(update_rollback_reboot_state_,        se::Failure,                    update_failure_state_,                tf::Immediate);
	main_states_.AddTransition(update_rollback_reboot_state_,        se::StateLoopDetected,          state_loop_state_,                    tf::Immediate);

	main_states_.AddTransition(update_verify_rollback_reboot_state_, se::Success,                    update_failure_state_,                tf::Immediate);
	main_states_.AddTransition(update_verify_rollback_reboot_state_, se::Failure,                    update_rollback_reboot_state_,        tf::Immediate);
	main_states_.AddTransition(update_verify_rollback_reboot_state_, se::StateLoopDetected,          state_loop_state_,                    tf::Immediate);

	main_states_.AddTransition(update_failure_state_,                se::Success,                    update_save_provides_state_,          tf::Immediate);
	main_states_.AddTransition(update_failure_state_,                se::Failure,                    update_save_provides_state_,          tf::Immediate);
	main_states_.AddTransition(update_failure_state_,                se::StateLoopDetected,          state_loop_state_,                    tf::Immediate);

	main_states_.AddTransition(update_save_provides_state_,          se::Success,                    update_cleanup_state_,                tf::Immediate);
	// Even if this fails, there is nothing we can do at this point.
	main_states_.AddTransition(update_save_provides_state_,          se::Failure,                    update_cleanup_state_,                tf::Immediate);
	main_states_.AddTransition(update_save_provides_state_,          se::StateLoopDetected,          state_loop_state_,                    tf::Immediate);

	main_states_.AddTransition(update_cleanup_state_,                se::Success,                    clear_artifact_data_state_,           tf::Immediate);
	main_states_.AddTransition(update_cleanup_state_,                se::Failure,                    clear_artifact_data_state_,           tf::Immediate);
	main_states_.AddTransition(update_cleanup_state_,                se::StateLoopDetected,          state_loop_state_,                    tf::Immediate);

	main_states_.AddTransition(clear_artifact_data_state_,           se::Success,                    idle_state_,                          tf::Immediate);
	main_states_.AddTransition(clear_artifact_data_state_,           se::Failure,                    idle_state_,                          tf::Immediate);

	main_states_.AddTransition(state_loop_state_,                    se::Success,                    idle_state_,                          tf::Immediate);
	main_states_.AddTransition(state_loop_state_,                    se::Failure,                    idle_state_,                          tf::Immediate);

	auto &dt = deployment_tracking_;

	dt.states_.AddTransition(dt.idle_state_,                         se::DeploymentStarted,          dt.no_failures_state_,                tf::Immediate);
	// May be posted repeatedly in main states' idle state.
	dt.states_.AddTransition(dt.idle_state_,                         se::DeploymentEnded,            dt.idle_state_,                       tf::Immediate);

	dt.states_.AddTransition(dt.no_failures_state_,                  se::Failure,                    dt.failure_state_,                    tf::Immediate);
	dt.states_.AddTransition(dt.no_failures_state_,                  se::DeploymentEnded,            dt.idle_state_,                       tf::Immediate);

	dt.states_.AddTransition(dt.failure_state_,                      se::RollbackStarted,            dt.rollback_attempted_state_,         tf::Immediate);
	dt.states_.AddTransition(dt.failure_state_,                      se::DeploymentEnded,            dt.idle_state_,                       tf::Immediate);

	dt.states_.AddTransition(dt.rollback_attempted_state_,           se::Failure,                    dt.rollback_failed_state_,            tf::Immediate);
	dt.states_.AddTransition(dt.rollback_attempted_state_,           se::DeploymentEnded,            dt.idle_state_,                       tf::Immediate);

	dt.states_.AddTransition(dt.rollback_failed_state_,              se::DeploymentEnded,            dt.idle_state_,                       tf::Immediate);
	// clang-format on
}

StateMachine::DeploymentTracking::DeploymentTracking() :
	states_(idle_state_) {
}

error::Error StateMachine::Run() {
	runner_.AttachToEventLoop(event_loop_);

	// Client is supposed to do one handling of each on startup.
	runner_.PostEvent(StateEvent::InventoryPollingTriggered);
	runner_.PostEvent(StateEvent::DeploymentPollingTriggered);

	event_loop_.Run();
	return error::NoError;
}

} // namespace daemon
} // namespace update
} // namespace mender
