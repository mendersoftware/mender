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

#include <common/expected.hpp>
#include <common/testing.hpp>

namespace dbus = mender::common::dbus;
namespace expected = mender::common::expected;
namespace mtesting = mender::common::testing;

using namespace std;

TEST(DBusClientTests, DBusClientTrivialTest) {
	mtesting::TestEventLoop loop;

	bool handler_called {false};
	dbus::DBusClient client {loop};
	auto success = client.CallMethod(
		"org.freedesktop.DBus",
		"/",
		"org.freedesktop.DBus.Introspectable",
		"Introspect",
		[&loop, &handler_called](expected::ExpectedString reply) {
			EXPECT_TRUE(reply);
			handler_called = true;
			loop.Stop();
		});
	loop.Run();
	EXPECT_TRUE(handler_called);
}
