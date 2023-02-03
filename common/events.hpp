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

	friend class Timer;
#endif // MENDER_EVENTS_USE_BOOST
};

class Timer {
public:
	Timer(EventLoop &loop);

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

	void Cancel();

private:
#ifdef MENDER_EVENTS_USE_BOOST
	asio::steady_timer timer_;
#endif // MENDER_EVENTS_USE_BOOST
};

} // namespace events
} // namespace mender
