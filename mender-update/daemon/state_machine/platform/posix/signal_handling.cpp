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

#include <csignal>

#include <mender-update/daemon/state_machine.hpp>

#include <common/error.hpp>
#include <common/log.hpp>

namespace mender {
namespace update {
namespace daemon {

namespace error = mender::common::error;
namespace log = mender::common::log;

error::Error StateMachine::RegisterSignalHandlers() {
	auto err =
		check_update_handler_.RegisterHandler({SIGUSR1}, [this](events::SignalNumber signum) {
			log::Info("SIGUSR1 received, triggering deployments check");
			runner_.PostEvent(StateEvent::DeploymentPollingTriggered);
		});
	if (err != error::NoError) {
		return err;
	}

	err = inventory_update_handler_.RegisterHandler({SIGUSR2}, [this](events::SignalNumber signum) {
		log::Info("SIGUSR2 received, triggering inventory update");
		runner_.PostEvent(StateEvent::InventoryPollingTriggered);
	});
	if (err != error::NoError) {
		return err;
	}

	err = termination_handler_.RegisterHandler(
		{SIGTERM, SIGINT, SIGQUIT}, [this](events::SignalNumber signum) {
			log::Info("Termination signal received, shutting down gracefully");
			event_loop_.Stop();
		});
	if (err != error::NoError) {
		return err;
	}

	return error::NoError;
}

} // namespace daemon
} // namespace update
} // namespace mender
