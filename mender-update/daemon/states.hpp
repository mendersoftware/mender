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

#ifndef MENDER_UPDATE_STATES_HPP
#define MENDER_UPDATE_STATES_HPP

#include <common/io.hpp>
#include <common/state_machine.hpp>

#include <artifact/artifact.hpp>

#include <mender-update/daemon/context.hpp>
#include <mender-update/daemon/state_events.hpp>

namespace mender {
namespace update {
namespace daemon {

using namespace std;

namespace io = mender::common::io;
namespace sm = mender::common::state_machine;

namespace artifact = mender::artifact;

using StateType = sm::State<Context, StateEvent>;

class IdleState : virtual public StateType {
public:
	void OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) override;
};

class PollForDeploymentState : virtual public StateType {
public:
	PollForDeploymentState(events::EventLoop &loop) :
		poll_timer_(loop) {
	}
	void OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) override;

private:
	events::Timer poll_timer_;
};

class SubmitInventoryState : virtual public StateType {
public:
	SubmitInventoryState(events::EventLoop &loop) :
		poll_timer_(loop) {
	}
	void OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) override;

private:
	events::Timer poll_timer_;
};

class UpdateDownloadState : virtual public StateType {
public:
	void OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) override;

	static void ParseArtifact(Context &ctx, sm::EventPoster<StateEvent> &poster);
};

class UpdateInstallState : virtual public StateType {
public:
	void OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) override;
};

class UpdateCheckRebootState : virtual public StateType {
public:
	void OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) override;
};

class UpdateRebootState : virtual public StateType {
public:
	void OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) override;
};

class UpdateVerifyRebootState : virtual public StateType {
public:
	void OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) override;
};

class UpdateCommitState : virtual public StateType {
public:
	void OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) override;
};

class UpdateCheckRollbackState : virtual public StateType {
public:
	void OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) override;
};

class UpdateRollbackState : virtual public StateType {
public:
	void OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) override;
};

class UpdateRollbackRebootState : virtual public StateType {
public:
	void OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) override;
};

class UpdateVerifyRollbackRebootState : virtual public StateType {
public:
	void OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) override;
};

class UpdateFailureState : virtual public StateType {
public:
	void OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) override;
};

class UpdateSaveArtifactDataState : virtual public StateType {
public:
	void OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) override;
};

class UpdateCleanupState : virtual public StateType {
public:
	void OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) override;
};

} // namespace daemon
} // namespace update
} // namespace mender

#endif // MENDER_UPDATE_STATES_HPP
