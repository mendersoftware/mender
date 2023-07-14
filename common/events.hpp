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

#ifndef MENDER_COMMON_EVENTS_HPP
#define MENDER_COMMON_EVENTS_HPP

#include <config.h>

#include <functional>
#include <system_error>

#include <common/error.hpp>

#ifdef MENDER_USE_BOOST_ASIO
#include <boost/asio.hpp>
#endif // MENDER_USE_BOOST_ASIO

namespace mender {
namespace common {
namespace events {

using EventHandler = std::function<void(mender::common::error::Error err)>;

#ifdef MENDER_USE_BOOST_ASIO
namespace asio = boost::asio;
#endif // MENDER_USE_BOOST_ASIO

class EventLoop {
public:
	void Run();
	void Stop();

	// Runs the function on the event loop. Note that there is no way to cancel a registered
	// function before running it. If you need cancellation, make sure the function tests for a
	// cancellation condition before doing its work.
	//
	// Thread-safe.
	void Post(std::function<void()> func);

private:
#ifdef MENDER_USE_BOOST_ASIO
	asio::io_context ctx_;
#endif // MENDER_USE_BOOST_ASIO

	friend class EventLoopObject;
};

class EventLoopObject {
#ifdef MENDER_USE_BOOST_ASIO
protected:
	static asio::io_context &GetAsioIoContext(EventLoop &loop) {
		return loop.ctx_;
	}
#endif // MENDER_USE_BOOST_ASIO
};

class Timer : public EventLoopObject {
public:
	Timer(EventLoop &loop);
	~Timer() {
		Cancel();
	}

#ifdef MENDER_USE_BOOST_ASIO
	template <typename Duration>
	void Wait(Duration duration) {
		timer_.expires_after(duration);
		timer_.wait();
	}

	template <typename Duration>
	void AsyncWait(Duration duration, EventHandler handler) {
		timer_.expires_after(duration);
		timer_.async_wait([handler](std::error_code ec) {
			if (ec) {
				auto err = ec.default_error_condition();
				if (err != make_error_condition(boost::system::errc::operation_canceled)) {
					handler(error::Error(err, "Timer error"));
				}
			} else {
				handler(error::NoError);
			}
		});
	}
#endif // MENDER_USE_BOOST_ASIO

	void Cancel();

private:
#ifdef MENDER_USE_BOOST_ASIO
	asio::steady_timer timer_;
#endif // MENDER_USE_BOOST_ASIO
};

} // namespace events
} // namespace common
} // namespace mender

#endif // MENDER_COMMON_EVENTS_HPP
