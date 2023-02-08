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

#include <functional>
#include <system_error>

typedef std::function<void(std::error_code)> EventHandler;

#ifdef MENDER_EVENTS_USE_BOOST
#include <boost/asio.hpp>
#endif // MENDER_EVENTS_USE_BOOST

namespace mender {
namespace events {

#ifdef MENDER_EVENTS_USE_BOOST
namespace asio = boost::asio;
#endif // MENDER_EVENTS_USE_BOOST

class EventLoop {
public:
	void Run();
	void Stop();

private:
#ifdef MENDER_EVENTS_USE_BOOST
	asio::io_context ctx_;
#endif // MENDER_EVENTS_USE_BOOST

	friend class EventLoopObject;
};

class EventLoopObject {
#ifdef MENDER_EVENTS_USE_BOOST
protected:
	static asio::io_context &GetAsioIoContext(EventLoop &loop) {
		return loop.ctx_;
	}
#endif // MENDER_EVENTS_USE_BOOST
};

class Timer : public EventLoopObject {
public:
	Timer(EventLoop &loop);

#ifdef MENDER_EVENTS_USE_BOOST
	template <typename Duration>
	void Wait(Duration duration) {
		timer_.expires_after(duration);
		timer_.wait();
	}

	template <typename Duration>
	void AsyncWait(Duration duration, EventHandler handler) {
		timer_.expires_after(duration);
		timer_.async_wait(handler);
	}
#endif // MENDER_EVENTS_USE_BOOST

	void Cancel();

private:
#ifdef MENDER_EVENTS_USE_BOOST
	asio::steady_timer timer_;
#endif // MENDER_EVENTS_USE_BOOST
};

} // namespace events
} // namespace mender

#endif // MENDER_COMMON_EVENTS_HPP
