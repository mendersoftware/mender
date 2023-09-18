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

// setenv() does not exist in <cstdlib>
#include <stdlib.h>

#include <gtest/gtest.h>
#include <gmock/gmock.h>

#include <common/error.hpp>
#include <common/events.hpp>
#include <common/expected.hpp>
#include <common/processes.hpp>
#include <common/testing.hpp>

namespace dbus = mender::common::dbus;
namespace error = mender::common::error;
namespace events = mender::common::events;
namespace expected = mender::common::expected;
namespace procs = mender::common::processes;
namespace mtesting = mender::common::testing;

using namespace std;

class DBusTests : public testing::Test {
public:
	// Have to use static setup/teardown/data because libdbus doesn't seem to
	// respect changing value of DBUS_SYSTEM_BUS_ADDRESS environment variable
	// and keeps connecting to the first address specified.
	static void SetUpTestSuite() {
		string dbus_sock_path = "unix:path=" + tmp_dir_.Path() + "/dbus.sock";
		dbus_daemon_proc_.reset(
			new procs::Process {{"dbus-daemon", "--session", "--address", dbus_sock_path}});
		dbus_daemon_proc_->Start();
		// give the DBus daemon time to start and initialize
		std::this_thread::sleep_for(chrono::seconds {1});

		setenv("DBUS_SYSTEM_BUS_ADDRESS", dbus_sock_path.c_str(), 1);
	};

	static void TearDownTestSuite() {
		dbus_daemon_proc_->EnsureTerminated();
		unsetenv("DBUS_SYSTEM_BUS_ADDRESS");
	};

protected:
	static mtesting::TemporaryDirectory tmp_dir_;
	static unique_ptr<procs::Process> dbus_daemon_proc_;
};

mtesting::TemporaryDirectory DBusTests::tmp_dir_;
unique_ptr<procs::Process> DBusTests::dbus_daemon_proc_;

class DBusClientTests : public DBusTests {};
class DBusServerTests : public DBusTests {};

TEST_F(DBusClientTests, DBusClientTrivialTest) {
	mtesting::TestEventLoop loop;

	bool reply_handler_called {false};
	bool signal_handler_called {false};
	dbus::DBusClient client {loop};

	// Fortunately, NameAcquired is always emitted and sent our way once we
	// connect.
	auto err = client.RegisterSignalHandler<expected::ExpectedString>(
		"org.freedesktop.DBus",
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

	client.UnregisterSignalHandler("org.freedesktop.DBus", "org.freedesktop.DBus", "NameAcquired");

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
		"org.freedesktop.DBus",
		"org.freedesktop.DBus",
		"NonExistingSignal",
		[&loop, &reply_handler_called](dbus::ExpectedStringPair ex_value) {
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
		"io.mender.Test", "io.mender.Test.TestIface", "TestMethod", [&method_handler_called]() {
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
		"io.mender.Test", "io.mender.Test.TestIface", "TestMethod", [&method_handler_called]() {
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
