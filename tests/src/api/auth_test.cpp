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

#include <api/auth.hpp>

#include <string>
#include <iostream>

#include <gtest/gtest.h>
#include <gmock/gmock.h>

#include <common/error.hpp>
#include <common/events.hpp>
#include <common/expected.hpp>
#include <common/dbus.hpp>
#include <common/testing.hpp>
#include <common/testing_dbus.hpp>

using namespace std;

namespace auth = mender::api::auth;
namespace dbus = mender::common::dbus;
namespace error = mender::common::error;
namespace events = mender::common::events;
namespace expected = mender::common::expected;
namespace mtesting = mender::common::testing;
namespace testing_dbus = mender::common::testing::dbus;

using TestEventLoop = mender::common::testing::TestEventLoop;

class AuthDBusTests : public testing_dbus::DBusTests {};

TEST_F(AuthDBusTests, AuthenticatorBasicTest) {
	const string JWT_TOKEN = "FOOBARJWTTOKEN";
	const string SERVER_URL = "some.server";

	TestEventLoop loop;

	// Setup fake mender-auth simply returning auth data
	dbus::DBusServer dbus_server {loop, "io.mender.AuthenticationManager"};
	auto dbus_obj = make_shared<dbus::DBusObject>("/io/mender/AuthenticationManager");
	dbus_obj->AddMethodHandler<dbus::ExpectedStringPair>(
		"io.mender.Authentication1", "GetJwtToken", [JWT_TOKEN, SERVER_URL]() {
			return dbus::StringPair {JWT_TOKEN, SERVER_URL};
		});
	dbus_server.AdvertiseObject(dbus_obj);

	auth::Authenticator authenticator {loop};

	bool action_called = false;
	auto err = authenticator.WithToken(
		[JWT_TOKEN, SERVER_URL, &action_called, &loop](auth::ExpectedAuthData ex_auth_data) {
			action_called = true;
			ASSERT_TRUE(ex_auth_data);

			EXPECT_EQ(ex_auth_data.value().token, JWT_TOKEN);
			EXPECT_EQ(ex_auth_data.value().server_url, SERVER_URL);
			loop.Stop();
		});
	EXPECT_EQ(err, error::NoError) << "Unexpected error: " << err.message;

	loop.Run();
	EXPECT_TRUE(action_called);
}

TEST_F(AuthDBusTests, AuthenticatorTwoActionsTest) {
	const string JWT_TOKEN = "FOOBARJWTTOKEN";
	const string SERVER_URL = "some.server";

	TestEventLoop loop;

	// Setup fake mender-auth simply returning auth data
	dbus::DBusServer dbus_server {loop, "io.mender.AuthenticationManager"};
	auto dbus_obj = make_shared<dbus::DBusObject>("/io/mender/AuthenticationManager");
	dbus_obj->AddMethodHandler<dbus::ExpectedStringPair>(
		"io.mender.Authentication1", "GetJwtToken", [JWT_TOKEN, SERVER_URL]() {
			return dbus::StringPair {JWT_TOKEN, SERVER_URL};
		});
	dbus_server.AdvertiseObject(dbus_obj);

	auth::Authenticator authenticator {loop};

	bool action1_called = false;
	bool action2_called = false;
	auto err =
		authenticator.WithToken([JWT_TOKEN, SERVER_URL, &action1_called, &action2_called, &loop](
									auth::ExpectedAuthData ex_auth_data) {
			action1_called = true;
			ASSERT_TRUE(ex_auth_data);

			EXPECT_EQ(ex_auth_data.value().token, JWT_TOKEN);
			EXPECT_EQ(ex_auth_data.value().server_url, SERVER_URL);
			if (action1_called && action2_called) {
				loop.Stop();
			}
		});
	EXPECT_EQ(err, error::NoError) << "Unexpected error: " << err.message;

	err = authenticator.WithToken([JWT_TOKEN, SERVER_URL, &action1_called, &action2_called, &loop](
									  auth::ExpectedAuthData ex_auth_data) {
		action2_called = true;
		ASSERT_TRUE(ex_auth_data);

		EXPECT_EQ(ex_auth_data.value().token, JWT_TOKEN);
		EXPECT_EQ(ex_auth_data.value().server_url, SERVER_URL);
		if (action1_called && action2_called) {
			loop.Stop();
		}
	});
	EXPECT_EQ(err, error::NoError) << "Unexpected error: " << err.message;

	loop.Run();
	EXPECT_TRUE(action1_called);
	EXPECT_TRUE(action2_called);
}

TEST_F(AuthDBusTests, AuthenticatorTwoActionsWithTokenClearTest) {
	const string JWT_TOKEN = "FOOBARJWTTOKEN";
	const string SERVER_URL = "some.server";

	TestEventLoop loop;

	// Setup fake mender-auth simply returning auth data
	int n_replies = 0;
	dbus::DBusServer dbus_server {loop, "io.mender.AuthenticationManager"};
	auto dbus_obj = make_shared<dbus::DBusObject>("/io/mender/AuthenticationManager");
	dbus_obj->AddMethodHandler<dbus::ExpectedStringPair>(
		"io.mender.Authentication1", "GetJwtToken", [JWT_TOKEN, SERVER_URL, &n_replies]() {
			n_replies++;
			return dbus::StringPair {JWT_TOKEN, SERVER_URL};
		});
	dbus_obj->AddMethodHandler<expected::ExpectedBool>(
		"io.mender.Authentication1",
		"FetchJwtToken",
		[&n_replies, &dbus_server, JWT_TOKEN, SERVER_URL]() {
			n_replies++;
			dbus_server.EmitSignal<dbus::StringPair>(
				"/io/mender/AuthenticationManager",
				"io.mender.Authentication1",
				"JwtTokenStateChange",
				dbus::StringPair {JWT_TOKEN + "2", SERVER_URL + "2"});

			return true;
		});
	dbus_server.AdvertiseObject(dbus_obj);

	auth::Authenticator authenticator {loop, chrono::seconds {2}};

	bool action1_called = false;
	bool action2_called = false;
	auto err = authenticator.WithToken(
		[JWT_TOKEN, SERVER_URL, &action1_called, &action2_called, &loop, &authenticator](
			auth::ExpectedAuthData ex_auth_data) {
			action1_called = true;
			ASSERT_TRUE(ex_auth_data);

			EXPECT_EQ(ex_auth_data.value().token, JWT_TOKEN);
			EXPECT_EQ(ex_auth_data.value().server_url, SERVER_URL);

			authenticator.ExpireToken();

			auto err = authenticator.WithToken([JWT_TOKEN, SERVER_URL, &action2_called, &loop](
												   auth::ExpectedAuthData ex_auth_data) {
				action2_called = true;
				ASSERT_TRUE(ex_auth_data);

				EXPECT_EQ(ex_auth_data.value().token, JWT_TOKEN + "2");
				EXPECT_EQ(ex_auth_data.value().server_url, SERVER_URL + "2");

				loop.Stop();
			});
			EXPECT_EQ(err, error::NoError) << "Unexpected error: " << err.message;
		});
	EXPECT_EQ(err, error::NoError) << "Unexpected error: " << err.message;
	loop.Run();

	EXPECT_EQ(n_replies, 2);
	EXPECT_TRUE(action1_called);
	EXPECT_TRUE(action2_called);
}

TEST_F(AuthDBusTests, AuthenticatorTwoActionsWithTokenClearAndTimeoutTest) {
	const string JWT_TOKEN = "FOOBARJWTTOKEN";
	const string SERVER_URL = "some.server";

	TestEventLoop loop;

	// Setup fake mender-auth simply returning auth data, but never announcing a
	// new token with a signal
	int n_replies = 0;
	dbus::DBusServer dbus_server {loop, "io.mender.AuthenticationManager"};
	auto dbus_obj = make_shared<dbus::DBusObject>("/io/mender/AuthenticationManager");
	dbus_obj->AddMethodHandler<dbus::ExpectedStringPair>(
		"io.mender.Authentication1", "GetJwtToken", [JWT_TOKEN, SERVER_URL, &n_replies]() {
			n_replies++;
			return dbus::StringPair {JWT_TOKEN, SERVER_URL};
		});
	dbus_obj->AddMethodHandler<expected::ExpectedBool>(
		"io.mender.Authentication1", "FetchJwtToken", [&n_replies]() {
			n_replies++;
			// no JwtTokenStateChange signal emitted here
			return true;
		});
	dbus_server.AdvertiseObject(dbus_obj);

	auth::Authenticator authenticator {loop, chrono::seconds {2}};

	bool action1_called = false;
	bool action2_called = false;
	auto err = authenticator.WithToken(
		[JWT_TOKEN, SERVER_URL, &action1_called, &action2_called, &loop, &authenticator](
			auth::ExpectedAuthData ex_auth_data) {
			action1_called = true;
			ASSERT_TRUE(ex_auth_data);

			EXPECT_EQ(ex_auth_data.value().token, JWT_TOKEN);
			EXPECT_EQ(ex_auth_data.value().server_url, SERVER_URL);

			authenticator.ExpireToken();

			auto err = authenticator.WithToken([JWT_TOKEN, SERVER_URL, &action2_called, &loop](
												   auth::ExpectedAuthData ex_auth_data) {
				action2_called = true;
				ASSERT_FALSE(ex_auth_data);

				loop.Stop();
			});
			EXPECT_EQ(err, error::NoError) << "Unexpected error: " << err.message;
		});
	EXPECT_EQ(err, error::NoError) << "Unexpected error: " << err.message;
	loop.Run();

	EXPECT_EQ(n_replies, 2);
	EXPECT_TRUE(action1_called);
	EXPECT_TRUE(action2_called);
}

TEST_F(AuthDBusTests, AuthenticatorBasicRealLifeTest) {
	const string JWT_TOKEN = "FOOBARJWTTOKEN";
	const string SERVER_URL = "some.server";

	TestEventLoop loop;

	// Setup fake mender-auth first returning empty data
	dbus::DBusServer dbus_server {loop, "io.mender.AuthenticationManager"};
	auto dbus_obj = make_shared<dbus::DBusObject>("/io/mender/AuthenticationManager");
	dbus_obj->AddMethodHandler<dbus::ExpectedStringPair>(
		"io.mender.Authentication1", "GetJwtToken", []() {
			// no token initially
			return dbus::StringPair {"", ""};
		});
	dbus_obj->AddMethodHandler<expected::ExpectedBool>(
		"io.mender.Authentication1", "FetchJwtToken", [&dbus_server, JWT_TOKEN, SERVER_URL]() {
			dbus_server.EmitSignal<dbus::StringPair>(
				"/io/mender/AuthenticationManager",
				"io.mender.Authentication1",
				"JwtTokenStateChange",
				dbus::StringPair {JWT_TOKEN, SERVER_URL});

			return true;
		});
	dbus_server.AdvertiseObject(dbus_obj);

	auth::Authenticator authenticator {loop, chrono::seconds {2}};

	bool action_called = false;
	auto err = authenticator.WithToken(
		[JWT_TOKEN, SERVER_URL, &action_called, &loop](auth::ExpectedAuthData ex_auth_data) {
			action_called = true;
			ASSERT_TRUE(ex_auth_data);

			EXPECT_EQ(ex_auth_data.value().token, JWT_TOKEN);
			EXPECT_EQ(ex_auth_data.value().server_url, SERVER_URL);
			loop.Stop();
		});
	EXPECT_EQ(err, error::NoError) << "Unexpected error: " << err.message;

	loop.Run();
	EXPECT_TRUE(action_called);
}

TEST(AuthNoDBusTests, AuthenticatorAttemptNoDBus) {
	setenv("DBUS_SYSTEM_BUS_ADDRESS", "dummy-address", 1);

	TestEventLoop loop;
	auth::Authenticator authenticator {loop};

	int action_called = false;
	auto err = authenticator.WithToken(
		[&action_called](auth::ExpectedAuthData ex_auth_data) { action_called = true; });
	EXPECT_NE(error::NoError, err);

	events::Timer timer(loop);
	timer.AsyncWait(chrono::milliseconds(500), [&loop](error::Error err) {
		ASSERT_EQ(err, error::NoError);
		loop.Stop();
	});

	loop.Run();
	EXPECT_FALSE(action_called);

	unsetenv("DBUS_SYSTEM_BUS_ADDRESS");
}
