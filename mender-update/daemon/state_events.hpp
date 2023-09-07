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

#ifndef MENDER_UPDATE_STATE_EVENTS_HPP
#define MENDER_UPDATE_STATE_EVENTS_HPP

namespace mender {
namespace update {
namespace daemon {

enum class StateEvent {
	Started,
	AlwaysSuccess,
	Success,
	Failure,
	NothingToDo,
	Retry,
	InventoryPollingTriggered,
	DeploymentPollingTriggered,
	StateLoopDetected,
	DeploymentStarted,
	DeploymentEnded,
	RollbackStarted,
};

inline std::string StateEventToString(const StateEvent &event) {
	switch (event) {
	case StateEvent::Started:
		return "Started";
	case StateEvent::AlwaysSuccess:
		return "AlwaysSuccess";
	case StateEvent::Success:
		return "Success";
	case StateEvent::Failure:
		return "Failure";
	case StateEvent::NothingToDo:
		return "NothingToDo";
	case StateEvent::Retry:
		return "Retry";
	case StateEvent::InventoryPollingTriggered:
		return "InventoryPollingTriggered";
	case StateEvent::DeploymentPollingTriggered:
		return "DeploymentPollingTriggered";
	case StateEvent::StateLoopDetected:
		return "StateLoopDetected";
	case StateEvent::DeploymentStarted:
		return "DeploymentStarted";
	case StateEvent::DeploymentEnded:
		return "DeploymentEnded";
	case StateEvent::RollbackStarted:
		return "RollbackStarted";
	}
	assert(false);
	return "MissingStateInSwitchStatement";
}

} // namespace daemon
} // namespace update
} // namespace mender

#endif // MENDER_UPDATE_STATE_EVENTS_HPP
