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

#ifndef MENDER_COMMON_STATE_MACHINE_HPP
#define MENDER_COMMON_STATE_MACHINE_HPP

#include <queue>
#include <unordered_map>
#include <unordered_set>

#include <common/events.hpp>
#include <common/log.hpp>

namespace mender {
namespace common {
namespace state_machine {

using namespace std;

namespace events = mender::common::events;
namespace log = mender::common::log;

template <typename ContextType, typename EventType>
class StateMachineRunner;

template <typename EventType>
class EventPoster {
public:
	virtual ~EventPoster() {
	}

	virtual void PostEvent(EventType event) = 0;
};

template <typename ContextType, typename EventType>
class State {
public:
	virtual ~State() {
	}

	virtual void OnEnter(ContextType &ctx, EventPoster<EventType> &poster) = 0;
};

enum class TransitionFlag {
	Immediate,
	Deferred,
};

template <typename ContextType, typename EventType>
class StateMachine {
public:
	StateMachine(State<ContextType, EventType> &start_state) :
		current_state_(&start_state) {
	}
	StateMachine(StateMachine &) = delete;

private:
	struct TransitionCondition {
		// Note: Comparing address-of states. We don't want to rely on comparison operators
		// in the states themselves, and we just want to know if they are different
		// instances.
		State<ContextType, EventType> *state;
		EventType event;

		bool operator==(const TransitionCondition &t) const {
			return state == t.state && event == t.event;
		}
	};

	class Hasher {
	public:
		size_t operator()(const TransitionCondition &obj) const {
			return std::hash<State<ContextType, EventType> *>()(obj.state)
				   ^ std::hash<int>()(static_cast<int>(obj.event));
		}
	};

	State<ContextType, EventType> *current_state_;
	unordered_map<TransitionCondition, State<ContextType, EventType> *, Hasher> transitions_;
	unordered_set<EventType> deferred_events_;

	friend class StateMachineRunner<ContextType, EventType>;

public:
	void AddTransition(
		State<ContextType, EventType> &source_state,
		EventType event,
		State<ContextType, EventType> &target_state,
		TransitionFlag flag) {
		transitions_[TransitionCondition {&source_state, event}] = &target_state;
		if (flag == TransitionFlag::Deferred) {
			// Event is involved in at least one deferred transition, so mark that.
			deferred_events_.insert(event);
		}
	}
};

template <typename ContextType, typename EventType>
class StateMachineRunner : virtual public EventPoster<EventType> {
public:
	StateMachineRunner(ContextType &ctx) :
		ctx_(ctx) {
	}
	StateMachineRunner(StateMachineRunner &) = delete;

	void PostEvent(EventType event) override {
		event_queue_.push(event);
		PostToEventLoop();
	}

	// Continously run state machinery on event loop.
	void AttachToEventLoop(events::EventLoop &event_loop) {
		DetachFromEventLoop();

		cancelled_ = make_shared<bool>(false);

		// We don't actually own the object, we are just keeping a pointer to it. Use null
		// deleter.
		event_loop_.reset(&event_loop, [](events::EventLoop *loop) {});
	}

	void DetachFromEventLoop() {
		if (cancelled_) {
			*cancelled_ = true;
			cancelled_.reset();
		}
		event_loop_.reset();
	}

	void AddStateMachine(StateMachine<ContextType, EventType> &machine) {
		machines_.push_back(&machine);
	}

private:
	void RunOne() {
		if (event_queue_.empty()) {
			return;
		}

		const size_t size = event_queue_.size();
		vector<State<ContextType, EventType> *> to_run;

		for (size_t count = 0; count < size; count++) {
			bool deferred = false;
			auto event = event_queue_.front();
			event_queue_.pop();
			to_run.clear();

			for (auto machine : machines_) {
				typename StateMachine<ContextType, EventType>::TransitionCondition cond {
					machine->current_state_, event};
				if (machine->deferred_events_.find(event) != machine->deferred_events_.end()) {
					deferred = true;
				}

				auto match = machine->transitions_.find(cond);
				if (match == machine->transitions_.end()) {
					// No match in this machine, continue.
					continue;
				}

				auto &target = match->second;
				to_run.push_back(target);
				machine->current_state_ = target;
			}

			if (to_run.empty()) {
				if (deferred) {
					// Put back in the queue to try later. This won't be tried
					// again during this run, due to only making `size`
					// attempts in the for loop.
					event_queue_.push(event);
				} else {
					log::Warning(
						"State machine event " + to_string(static_cast<int>(event))
						+ " was not handled by any state. This is a bug and could hang the state machine.");
					assert(!to_run.empty());
				}
			} else {
				for (auto &state : to_run) {
					state->OnEnter(ctx_, *this);
				}
				// Since we ran something, there may be more events waiting to
				// execute. OTOH, if we didn't, it means that all objects currently
				// in the queue are deferred, and not actionable until at least one
				// state machine reaches a different state.
				if (!event_queue_.empty()) {
					PostToEventLoop();
				}
				break;
			}
		}
	}

	void PostToEventLoop() {
		if (!event_loop_ || event_queue_.empty()) {
			return;
		}

		auto cancelled = cancelled_;
		event_loop_->Post([cancelled, this]() {
			if (!*cancelled_) {
				RunOne();
			}
		});
	}

	ContextType &ctx_;

	shared_ptr<bool> cancelled_;
	vector<StateMachine<ContextType, EventType> *> machines_;

	queue<EventType> event_queue_;

	// Would be nice with optional<EventLoop &> reference here, but optional doesn't support
	// references. Use a pointer with a null deleter instead.
	shared_ptr<events::EventLoop> event_loop_;
};

} // namespace state_machine
} // namespace common
} // namespace mender

#endif // MENDER_COMMON_STATE_MACHINE_HPP
