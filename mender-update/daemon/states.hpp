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

class EmptyState : virtual public StateType {
public:
	void OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) override;
};

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

class SaveState : virtual public StateType {
public:
	// Sub states should implement OnEnterSaveState instead, since we do state saving in
	// here. Note that not all states that participate in an update are SaveStates that get
	// their database key saved. Some states are not because it's good enough to rely on the
	// previously saved state.
	void OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) override final;

	virtual void OnEnterSaveState(Context &ctx, sm::EventPoster<StateEvent> &poster) = 0;
	virtual const string &DatabaseStateString() const = 0;
	virtual bool IsFailureState() const = 0;
};

class UpdateDownloadState : virtual public StateType {
public:
	void OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) override;

private:
	// `static` since it only needs the arguments, but is still strongly tied to
	// OnEnterSaveState.
	static void ParseArtifact(Context &ctx, sm::EventPoster<StateEvent> &poster);
};

class UpdateInstallState : virtual public SaveState {
public:
	void OnEnterSaveState(Context &ctx, sm::EventPoster<StateEvent> &poster) override;
	const string &DatabaseStateString() const override {
		return Context::kUpdateStateArtifactInstall;
	}
	bool IsFailureState() const override {
		return false;
	}
};

class UpdateCheckRebootState : virtual public StateType {
public:
	void OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) override;
};

class UpdateRebootState : virtual public SaveState {
public:
	void OnEnterSaveState(Context &ctx, sm::EventPoster<StateEvent> &poster) override;
	const string &DatabaseStateString() const override {
		return Context::kUpdateStateArtifactReboot;
	}
	bool IsFailureState() const override {
		return false;
	}
};

class UpdateVerifyRebootState : virtual public SaveState {
public:
	void OnEnterSaveState(Context &ctx, sm::EventPoster<StateEvent> &poster) override;
	const string &DatabaseStateString() const override {
		return Context::kUpdateStateArtifactVerifyReboot;
	}
	bool IsFailureState() const override {
		return false;
	}
};

class UpdateCommitState : virtual public SaveState {
public:
	void OnEnterSaveState(Context &ctx, sm::EventPoster<StateEvent> &poster) override;
	const string &DatabaseStateString() const override {
		return Context::kUpdateStateArtifactCommit;
	}
	bool IsFailureState() const override {
		return false;
	}
};

class UpdateAfterCommitState : virtual public SaveState {
public:
	void OnEnterSaveState(Context &ctx, sm::EventPoster<StateEvent> &poster) override;
	const string &DatabaseStateString() const override {
		return Context::kUpdateStateAfterArtifactCommit;
	}
	bool IsFailureState() const override {
		return false;
	}
};

class UpdateCheckRollbackState : virtual public StateType {
public:
	void OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) override;
};

class UpdateRollbackState : virtual public SaveState {
public:
	void OnEnterSaveState(Context &ctx, sm::EventPoster<StateEvent> &poster) override;
	const string &DatabaseStateString() const override {
		return Context::kUpdateStateArtifactRollback;
	}
	bool IsFailureState() const override {
		return true;
	}
};

class UpdateRollbackRebootState : virtual public SaveState {
public:
	void OnEnterSaveState(Context &ctx, sm::EventPoster<StateEvent> &poster) override;
	const string &DatabaseStateString() const override {
		return Context::kUpdateStateArtifactRollbackReboot;
	}
	bool IsFailureState() const override {
		return true;
	}
};

class UpdateVerifyRollbackRebootState : virtual public SaveState {
public:
	void OnEnterSaveState(Context &ctx, sm::EventPoster<StateEvent> &poster) override;
	const string &DatabaseStateString() const override {
		return Context::kUpdateStateArtifactVerifyRollbackReboot;
	}
	bool IsFailureState() const override {
		return true;
	}
};

class UpdateFailureState : virtual public SaveState {
public:
	void OnEnterSaveState(Context &ctx, sm::EventPoster<StateEvent> &poster) override;
	const string &DatabaseStateString() const override {
		return Context::kUpdateStateArtifactFailure;
	}
	bool IsFailureState() const override {
		return true;
	}
};

class UpdateSaveProvidesState : virtual public StateType {
public:
	void OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) override;
};

class UpdateCleanupState : virtual public SaveState {
public:
	void OnEnterSaveState(Context &ctx, sm::EventPoster<StateEvent> &poster) override;
	const string &DatabaseStateString() const override {
		return Context::kUpdateStateCleanup;
	}
	bool IsFailureState() const override {
		// This is actually both a failure and non-failure state, but it is executed in
		// every failure scenario, which is what is important here.
		return true;
	}
};

class ClearArtifactDataState : virtual public StateType {
public:
	void OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) override;
};

class StateLoopState : virtual public StateType {
public:
	void OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) override;
};

namespace deployment_tracking {

class NoFailuresState : virtual public StateType {
public:
	void OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) override;
};

class FailureState : virtual public StateType {
public:
	void OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) override;
};

class RollbackAttemptedState : virtual public StateType {
public:
	void OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) override;
};

class RollbackFailedState : virtual public StateType {
public:
	void OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) override;
};

} // namespace deployment_tracking

} // namespace daemon
} // namespace update
} // namespace mender

#endif // MENDER_UPDATE_STATES_HPP
