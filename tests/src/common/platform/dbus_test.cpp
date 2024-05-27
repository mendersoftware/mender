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

#include <common/platform/dbus.hpp>

// setenv() does not exist in <cstdlib>
#include <stdlib.h>

#include <gtest/gtest.h>
#include <gmock/gmock.h>

#include <common/error.hpp>
#include <common/events.hpp>
#include <common/expected.hpp>
#include <common/processes.hpp>
#include <common/testing.hpp>
#include <common/platform/testing_dbus.hpp>

namespace dbus = mender::common::dbus;
namespace error = mender::common::error;
namespace events = mender::common::events;
namespace expected = mender::common::expected;
namespace procs = mender::common::processes;
namespace mtesting = mender::common::testing;
namespace testing_dbus = mender::common::testing::dbus;

using namespace std;

class DBusClientTests : public testing_dbus::DBusTests {};
class DBusServerTests : public testing_dbus::DBusTests {};

TEST_F(DBusClientTests, DBusClientTrivialTest) {
	mtesting::TestEventLoop loop;

	bool reply_handler_called {false};
	bool signal_handler_called {false};
	dbus::DBusClient client {loop};

	// Fortunately, NameAcquired is always emitted and sent our way once we
	// connect.
	auto err = client.RegisterSignalHandler<expected::ExpectedString>(
		"org.freedesktop.DBus",
		"NameAcquired",
		[&loop, &reply_handler_called, &signal_handler_called](expected::ExpectedString ex_value) {
			EXPECT_TRUE(ex_value);
			signal_handler_called = true;
			if (reply_handler_called) {
				loop.Stop();
			}
		});
	EXPECT_EQ(err, error::NoError);

	err = client.CallMethod<expected::ExpectedString>(
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

TEST_F(DBusClientTests, DBusClientSignalUnregisterTest) {
	mtesting::TestEventLoop loop;

	bool reply_handler_called {false};
	bool signal_handler_called {false};
	dbus::DBusClient client {loop};

	// Fortunately, NameAcquired is always emitted and sent our way once we
	// connect.
	auto err = client.RegisterSignalHandler<expected::ExpectedString>(
		"org.freedesktop.DBus",
		"NameAcquired",
		[&loop, &reply_handler_called, &signal_handler_called](expected::ExpectedString ex_value) {
			EXPECT_TRUE(ex_value);
			signal_handler_called = true;
			if (reply_handler_called) {
				loop.Stop();
			}
		});
	EXPECT_EQ(err, error::NoError);

	client.UnregisterSignalHandler("org.freedesktop.DBus", "NameAcquired");

	events::Timer timer {loop};
	err = client.CallMethod<expected::ExpectedString>(
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

TEST_F(DBusClientTests, DBusClientRegisterStringPairSignalTest) {
	mtesting::TestEventLoop loop;

	bool reply_handler_called {false};
	dbus::DBusClient client {loop};

	// just check we can do this, we cannot easily trigger a signal with such
	// signature
	auto err = client.RegisterSignalHandler<dbus::ExpectedStringPair>(
		"org.freedesktop.DBus", "NonExistingSignal", [](dbus::ExpectedStringPair ex_value) {
			EXPECT_TRUE(ex_value);
		});
	EXPECT_EQ(err, error::NoError);

	err = client.CallMethod<expected::ExpectedString>(
		"org.freedesktop.DBus",
		"/",
		"org.freedesktop.DBus.Introspectable",
		"Introspect",
		[&loop, &reply_handler_called](expected::ExpectedString reply) {
			EXPECT_TRUE(reply);
			reply_handler_called = true;
			loop.Stop();
		});
	EXPECT_EQ(err, error::NoError);
	loop.Run();
	EXPECT_TRUE(reply_handler_called);
}

TEST_F(DBusServerTests, DBusServerBasicMethodHandlingTest) {
	mtesting::TestEventLoop loop;

	bool method_handler_called {false};
	dbus::DBusObject obj {"/io/mender/Test/Obj"};
	obj.AddMethodHandler<expected::ExpectedString>(
		"io.mender.Test.TestIface", "TestMethod", [&method_handler_called]() {
			method_handler_called = true;
			return "test return value";
		});

	dbus::DBusServer server {loop, "io.mender.Test"};
	auto err = server.AdvertiseObject(obj);
	EXPECT_EQ(err, error::NoError);

	bool reply_handler_called {false};
	dbus::DBusClient client {loop};
	err = client.CallMethod<expected::ExpectedString>(
		"io.mender.Test",
		"/io/mender/Test/Obj",
		"io.mender.Test.TestIface",
		"TestMethod",
		[&loop, &reply_handler_called](expected::ExpectedString reply) {
			ASSERT_TRUE(reply);
			EXPECT_EQ(reply.value(), "test return value");
			reply_handler_called = true;
			loop.Stop();
		});
	EXPECT_EQ(err, error::NoError);

	loop.Run();

	EXPECT_TRUE(method_handler_called);
	EXPECT_TRUE(reply_handler_called);
}

TEST_F(DBusServerTests, DBusServerErrorMethodHandlingTest) {
	mtesting::TestEventLoop loop;

	bool method_handler_called {false};
	dbus::DBusObject obj {"/io/mender/Test/Obj"};
	obj.AddMethodHandler<expected::ExpectedString>(
		"io.mender.Test.TestIface", "TestMethod", [&method_handler_called]() {
			method_handler_called = true;
			return expected::unexpected(
				error::MakeError(error::GenericError, "testing error handling"));
		});

	dbus::DBusServer server {loop, "io.mender.Test"};
	auto err = server.AdvertiseObject(obj);
	EXPECT_EQ(err, error::NoError);

	bool reply_handler_called {false};
	dbus::DBusClient client {loop};
	err = client.CallMethod<expected::ExpectedString>(
		"io.mender.Test",
		"/io/mender/Test/Obj",
		"io.mender.Test.TestIface",
		"TestMethod",
		[&loop, &reply_handler_called](expected::ExpectedString reply) {
			ASSERT_FALSE(reply);
			EXPECT_THAT(reply.error().String(), ::testing::HasSubstr("testing error handling"));
			reply_handler_called = true;
			loop.Stop();
		});
	EXPECT_EQ(err, error::NoError);

	loop.Run();

	EXPECT_TRUE(method_handler_called);
	EXPECT_TRUE(reply_handler_called);
}

TEST_F(DBusServerTests, DBusServerBoolMethodHandlingTest) {
	mtesting::TestEventLoop loop;

	bool method_handler_called {false};
	dbus::DBusObject obj {"/io/mender/Test/Obj"};
	obj.AddMethodHandler<expected::ExpectedBool>(
		"io.mender.Test.TestIface", "TestMethod", [&method_handler_called]() {
			method_handler_called = true;
			return true;
		});

	dbus::DBusServer server {loop, "io.mender.Test"};
	auto err = server.AdvertiseObject(obj);
	EXPECT_EQ(err, error::NoError);

	bool reply_handler_called {false};
	dbus::DBusClient client {loop};
	err = client.CallMethod<expected::ExpectedBool>(
		"io.mender.Test",
		"/io/mender/Test/Obj",
		"io.mender.Test.TestIface",
		"TestMethod",
		[&loop, &reply_handler_called](expected::ExpectedBool reply) {
			ASSERT_TRUE(reply);
			EXPECT_TRUE(reply.value());
			reply_handler_called = true;
			loop.Stop();
		});
	EXPECT_EQ(err, error::NoError);

	loop.Run();

	EXPECT_TRUE(method_handler_called);
	EXPECT_TRUE(reply_handler_called);
}

TEST_F(DBusServerTests, DBusServerBasicSignalTest) {
	mtesting::TestEventLoop loop;

	dbus::DBusObject obj {"/io/mender/Test/Obj"};
	dbus::DBusServer server {loop, "io.mender.Test"};
	auto err = server.AdvertiseObject(obj);
	EXPECT_EQ(err, error::NoError);

	bool signal_handler_called {false};
	dbus::DBusClient client {loop};
	err = client.RegisterSignalHandler<expected::ExpectedString>(
		"io.mender.Test.TestIface",
		"TestSignal",
		[&signal_handler_called, &loop](expected::ExpectedString ex_value) {
			signal_handler_called = true;
			ASSERT_TRUE(ex_value);
			EXPECT_EQ(ex_value.value(), "test signal value");
			loop.Stop();
		});
	EXPECT_EQ(err, error::NoError);

	err = server.EmitSignal<string>(
		"/io/mender/Test/Obj", "io.mender.Test.TestIface", "TestSignal", "test signal value");
	EXPECT_EQ(err, error::NoError);

	loop.Run();
	EXPECT_TRUE(signal_handler_called);
}
