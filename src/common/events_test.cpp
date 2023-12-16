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

#include <gtest/gtest.h>

#include <unistd.h> // getpid()
#include <csignal>
#include <thread>
#include <array>

#include <common/error.hpp>

using namespace std;

namespace error = mender::common::error;
namespace events = mender::common::events;

TEST(Events, Timers) {
	using std::chrono::seconds;
	using std::chrono::steady_clock;

	auto short_wait = seconds(1);
	auto medium_wait = seconds(5);
	auto long_wait = seconds(10);

	// Since we'll be waiting quite a bit, run test cases in parallel.
	std::thread test_threads[] = {
		// Synchronous wait.
		std::thread([&]() {
			auto check_point = steady_clock::now();
			events::EventLoop loop;
			events::Timer timer(loop);

			timer.Wait(short_wait);
			EXPECT_GE(steady_clock::now(), check_point + short_wait);
		}),

		// Asynchronous wait.
		std::thread([&]() {
			auto check_point = steady_clock::now();
			events::EventLoop loop;
			events::Timer timer(loop);

			timer.AsyncWait(short_wait, [](error::Error err) {});
			loop.Run();
			EXPECT_GE(steady_clock::now(), check_point + short_wait);
		}),

		// Asynchronous wait with cancel.
		std::thread([&]() {
			auto check_point = steady_clock::now();
			events::EventLoop loop;
			events::Timer timer(loop);

			timer.AsyncWait(long_wait, [](error::Error err) {
				EXPECT_EQ(err.code, make_error_condition(errc::operation_canceled));
			});
			timer.Cancel();
			loop.Run();
			EXPECT_LT(steady_clock::now(), check_point + long_wait);
		}),

		// Two asynchronous waits.
		std::thread([&]() {
			auto check_point = steady_clock::now();
			events::EventLoop loop;
			events::Timer timer(loop);
			events::Timer timer2(loop);

			timer.AsyncWait(short_wait, [](error::Error err) {});
			timer2.AsyncWait(medium_wait, [](error::Error err) {});
			loop.Run();
			EXPECT_GE(steady_clock::now(), check_point + medium_wait);
		}),

		// Two asynchronous waits with cancel.
		std::thread([&]() {
			auto check_point = steady_clock::now();
			events::EventLoop loop;
			events::Timer timer(loop);
			events::Timer timer2(loop);

			timer.AsyncWait(short_wait, [&timer2](error::Error err) { timer2.Cancel(); });
			timer2.AsyncWait(long_wait, [](error::Error err) {});
			loop.Run();
			EXPECT_GE(steady_clock::now(), check_point + short_wait);
			EXPECT_LT(steady_clock::now(), check_point + long_wait);
		}),

		// Stop event loop.
		std::thread([&]() {
			auto check_point = steady_clock::now();
			events::EventLoop loop;
			events::Timer timer(loop);
			events::Timer timer2(loop);

			timer.AsyncWait(short_wait, [&loop](error::Error err) { loop.Stop(); });
			timer2.AsyncWait(long_wait, [](error::Error err) {});
			loop.Run();
			EXPECT_GE(steady_clock::now(), check_point + short_wait);
			EXPECT_LT(steady_clock::now(), check_point + long_wait);
		}),
	};
	for (auto &thr : test_threads) {
		thr.join();
	}
}

TEST(Events, SignalHandlerTest) {
	events::EventLoop loop;
	events::Timer signal_timer_1 {loop};
	events::Timer signal_timer_2 {loop};
	events::Timer stop_timer {loop};

	size_t n_sigs_handled = 0;
	events::SignalHandlerFn handler_fn = [&n_sigs_handled, &loop](events::SignalNumber sig_num) {
		if (n_sigs_handled == 0) {
			EXPECT_EQ(sig_num, SIGUSR1);
		} else if (n_sigs_handled == 1) {
			EXPECT_EQ(sig_num, SIGUSR2);
		} else {
			EXPECT_EQ(n_sigs_handled, 2);
			EXPECT_EQ(sig_num, SIGUSR1);
			loop.Stop();
		}
		n_sigs_handled++;
	};

	{
		// Things should get properly cancelled when this handler goes of out
		// scope and so they should have no effect.
		events::SignalHandler ephemeral_sig_handler {loop};
		ephemeral_sig_handler.RegisterHandler({SIGUSR1, SIGUSR2}, handler_fn);
	}

	events::SignalHandler sig_handler {loop};
	sig_handler.RegisterHandler({SIGUSR1, SIGUSR2}, handler_fn);

	loop.Post([]() { kill(getpid(), SIGUSR1); });
	signal_timer_1.AsyncWait(
		std::chrono::seconds {1}, [](error::Error err) { kill(getpid(), SIGUSR2); });
	signal_timer_2.AsyncWait(
		std::chrono::seconds {2}, [](error::Error err) { kill(getpid(), SIGUSR1); });
	stop_timer.AsyncWait(std::chrono::seconds {3}, [&loop](error::Error err) { loop.Stop(); });

	loop.Run();

	EXPECT_EQ(n_sigs_handled, 3);
}
