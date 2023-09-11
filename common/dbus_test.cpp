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

#include <common/dbus.hpp>

#include <gtest/gtest.h>
#include <gmock/gmock.h>

#include <common/error.hpp>
#include <common/events.hpp>
#include <common/expected.hpp>
#include <common/testing.hpp>

namespace dbus = mender::common::dbus;
namespace error = mender::common::error;
namespace events = mender::common::events;
namespace expected = mender::common::expected;
namespace mtesting = mender::common::testing;

using namespace std;

TEST(DBusClientTests, DBusClientTrivialTest) {
	mtesting::TestEventLoop loop;

	bool reply_handler_called {false};
	bool signal_handler_called {false};
	dbus::DBusClient client {loop};

	// Fortunately, NameAcquired is always emitted and sent our way once we
	// connect.
	auto err = client.RegisterSignalHandler(
		"org.freedesktop.DBus",
		"org.freedesktop.DBus",
		"NameAcquired",
		[&loop, &reply_handler_called, &signal_handler_called](string value) {
			signal_handler_called = true;
			if (reply_handler_called) {
				loop.Stop();
			}
		});
	EXPECT_EQ(err, error::NoError);

	err = client.CallMethod(
		"org.freedesktop.DBus",
		"/",
		"org.freedesktop.DBus.Introspectable",
		"Introspect",
		[&loop, &reply_handler_called, &signal_handler_called](expected::ExpectedString reply) {
			EXPECT_TRUE(reply);
			reply_handler_called = true;
			// the signal should have arrived first, but let's be a bit more
			// careful
			if (signal_handler_called) {
				loop.Stop();
			}
		});
	EXPECT_EQ(err, error::NoError);
	loop.Run();
	EXPECT_TRUE(reply_handler_called);
	EXPECT_TRUE(signal_handler_called);
}

TEST(DBusClientTests, DBusClientSignalUnregisterTest) {
	mtesting::TestEventLoop loop;

	bool reply_handler_called {false};
	bool signal_handler_called {false};
	dbus::DBusClient client {loop};

	// Fortunately, NameAcquired is always emitted and sent our way once we
	// connect.
	auto err = client.RegisterSignalHandler(
		"org.freedesktop.DBus",
		"org.freedesktop.DBus",
		"NameAcquired",
		[&loop, &reply_handler_called, &signal_handler_called](string value) {
			signal_handler_called = true;
			if (reply_handler_called) {
				loop.Stop();
			}
		});
	EXPECT_EQ(err, error::NoError);

	client.UnregisterSignalHandler("org.freedesktop.DBus", "org.freedesktop.DBus", "NameAcquired");

	events::Timer timer {loop};
	err = client.CallMethod(
		"org.freedesktop.DBus",
		"/",
		"org.freedesktop.DBus.Introspectable",
		"Introspect",
		[&loop, &timer, &reply_handler_called](expected::ExpectedString reply) {
			EXPECT_TRUE(reply);
			reply_handler_called = true;
			// give the signal some extra time to be delivered (it should have
			// come already, but just in case)
			timer.AsyncWait(chrono::seconds {1}, [&loop](error::Error err) { loop.Stop(); });
		});
	EXPECT_EQ(err, error::NoError);
	loop.Run();
	EXPECT_TRUE(reply_handler_called);
	EXPECT_FALSE(signal_handler_called);
}
