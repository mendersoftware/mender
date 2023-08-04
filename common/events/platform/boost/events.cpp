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

#include <common/events.hpp>

#include <boost/asio.hpp>

#include <common/error.hpp>
#include <common/log.hpp>

namespace mender {
namespace common {
namespace events {

namespace asio = boost::asio;
namespace error = mender::common::error;
namespace log = mender::common::log;

void EventLoop::Run() {
	bool stopped = ctx_.stopped();
	if (stopped) {
		ctx_.restart();
	}
	ctx_.run();
	if (!stopped) {
		// For recursive invocations. If we were originally running, but we stopped and
		// exited this level, then keep the running state of the previous recursive level.
		ctx_.restart();
	}
}

void EventLoop::Stop() {
	ctx_.stop();
}

void EventLoop::Post(std::function<void()> func) {
	ctx_.post(func);
}

Timer::Timer(EventLoop &loop) :
	timer_(GetAsioIoContext(loop)) {
}

void Timer::Cancel() {
	timer_.cancel();
}

SignalHandler::SignalHandler(EventLoop &loop) :
	signal_set_ {GetAsioIoContext(loop)} {};

void SignalHandler::Cancel() {
	signal_set_.cancel();
}

error::Error SignalHandler::RegisterHandler(const SignalSet &set, SignalHandlerFn handler_fn) {
	signal_set_.clear();
	for (auto sig_num : set) {
		boost::system::error_code ec;
		signal_set_.add(sig_num, ec);
		if (ec) {
			return error::Error(
				ec.default_error_condition(),
				"Could not add signal " + std::to_string(sig_num) + " to signal set");
		}
	}

	class SignalHandlerFunctor {
	public:
		asio::signal_set &sig_set;
		SignalHandlerFn handler_fn;

		void operator()(const boost::system::error_code &ec, int signal_number) {
			if (ec) {
				if (ec == boost::asio::error::operation_aborted) {
					// All handlers are called with this error when the signal set
					// is cancelled.
					return;
				}
				log::Error("Failure in signal handler: " + ec.message());
			} else {
				handler_fn(signal_number);
			}

			// in either case, register ourselves again because
			// asio::signal_set::async_wait() is a one-off thing
			sig_set.async_wait(*this);
		}
	};

	SignalHandlerFunctor fctor {signal_set_, handler_fn};
	signal_set_.async_wait(fctor);

	// Unfortunately, asio::signal_set::async_wait() doesn't return anything so
	// we have no chance to propagate any error here (wonder what they do if
	// they get an error from sigaction() or whatever they use
	// internally). Other implementations may have better options, though.
	return error::NoError;
}

} // namespace events
} // namespace common
} // namespace mender
