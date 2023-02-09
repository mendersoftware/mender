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

#include <thread>
#include <array>

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

			timer.AsyncWait(short_wait, [](std::error_code ec) {});
			loop.Run();
			EXPECT_GE(steady_clock::now(), check_point + short_wait);
		}),

		// Asynchronous wait with cancel.
		std::thread([&]() {
			auto check_point = steady_clock::now();
			events::EventLoop loop;
			events::Timer timer(loop);

			timer.AsyncWait(long_wait, [](std::error_code ec) {});
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

			timer.AsyncWait(short_wait, [](std::error_code ec) {});
			timer2.AsyncWait(medium_wait, [](std::error_code ec) {});
			loop.Run();
			EXPECT_GE(steady_clock::now(), check_point + medium_wait);
		}),

		// Two asynchronous waits with cancel.
		std::thread([&]() {
			auto check_point = steady_clock::now();
			events::EventLoop loop;
			events::Timer timer(loop);
			events::Timer timer2(loop);

			timer.AsyncWait(short_wait, [&timer2](std::error_code ec) { timer2.Cancel(); });
			timer2.AsyncWait(long_wait, [](std::error_code ec) {});
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

			timer.AsyncWait(short_wait, [&loop](std::error_code ec) { loop.Stop(); });
			timer2.AsyncWait(long_wait, [](std::error_code ec) {});
			loop.Run();
			EXPECT_GE(steady_clock::now(), check_point + short_wait);
			EXPECT_LT(steady_clock::now(), check_point + long_wait);
		}),
	};
	for (auto &thr : test_threads) {
		thr.join();
	}
}
