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
#include <common/testing.hpp>

#ifdef MENDER_USE_DBUS
#include <common/platform/dbus.hpp>
#include <common/platform/testing_dbus.hpp>
#endif

using namespace std;

namespace auth = mender::api::auth;
namespace error = mender::common::error;
namespace events = mender::common::events;
namespace expected = mender::common::expected;
namespace mtesting = mender::common::testing;

#ifdef MENDER_USE_DBUS
namespace dbus = mender::common::dbus;
namespace testing_dbus = mender::common::testing::dbus;
#endif

using TestEventLoop = mender::common::testing::TestEventLoop;

#ifdef MENDER_USE_DBUS
class AuthDBusTests : public testing_dbus::DBusTests {};
#else
// Dummy.
class AuthDBusTests : public testing::Test {};
#endif

TEST_F(AuthDBusTests, AuthenticatorBasicTest) {
#ifndef MENDER_USE_DBUS
	GTEST_SKIP();
#else
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

	auth::AuthenticatorDBus authenticator {loop};

	bool action_called = false;
	authenticator.WithToken(
		[JWT_TOKEN, SERVER_URL, &action_called, &loop](auth::ExpectedAuthData ex_auth_data) {
			action_called = true;
			ASSERT_TRUE(ex_auth_data);

			EXPECT_EQ(ex_auth_data.value().token, JWT_TOKEN);
			EXPECT_EQ(ex_auth_data.value().server_url, SERVER_URL);
			loop.Stop();
		});

	loop.Run();
	EXPECT_TRUE(action_called);
#endif // MENDER_USE_DBUS
}

TEST_F(AuthDBusTests, AuthenticatorTwoActionsTest) {
#ifndef MENDER_USE_DBUS
	GTEST_SKIP();
#else
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

	auth::AuthenticatorDBus authenticator {loop};

	bool action1_called = false;
	bool action2_called = false;
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

	authenticator.WithToken([JWT_TOKEN, SERVER_URL, &action1_called, &action2_called, &loop](
								auth::ExpectedAuthData ex_auth_data) {
		action2_called = true;
		ASSERT_TRUE(ex_auth_data);

		EXPECT_EQ(ex_auth_data.value().token, JWT_TOKEN);
		EXPECT_EQ(ex_auth_data.value().server_url, SERVER_URL);
		if (action1_called && action2_called) {
			loop.Stop();
		}
	});

	loop.Run();
	EXPECT_TRUE(action1_called);
	EXPECT_TRUE(action2_called);
#endif // MENDER_USE_DBUS
}

TEST_F(AuthDBusTests, AuthenticatorTwoActionsWithTokenClearTest) {
#ifndef MENDER_USE_DBUS
	GTEST_SKIP();
#else
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

	auth::AuthenticatorDBus authenticator {loop, chrono::seconds {2}};

	bool action1_called = false;
	bool action2_called = false;
	authenticator.WithToken(
		[JWT_TOKEN, SERVER_URL, &action1_called, &action2_called, &loop, &authenticator](
			auth::ExpectedAuthData ex_auth_data) {
			action1_called = true;
			ASSERT_TRUE(ex_auth_data);

			EXPECT_EQ(ex_auth_data.value().token, JWT_TOKEN);
			EXPECT_EQ(ex_auth_data.value().server_url, SERVER_URL);

			authenticator.ExpireToken();

			authenticator.WithToken([JWT_TOKEN, SERVER_URL, &action2_called, &loop](
										auth::ExpectedAuthData ex_auth_data) {
				action2_called = true;
				ASSERT_TRUE(ex_auth_data);

				EXPECT_EQ(ex_auth_data.value().token, JWT_TOKEN + "2");
				EXPECT_EQ(ex_auth_data.value().server_url, SERVER_URL + "2");

				loop.Stop();
			});
		});
	loop.Run();

	EXPECT_EQ(n_replies, 2);
	EXPECT_TRUE(action1_called);
	EXPECT_TRUE(action2_called);
#endif // MENDER_USE_DBUS
}

TEST_F(AuthDBusTests, AuthenticatorTwoActionsWithTokenClearAndTimeoutTest) {
#ifndef MENDER_USE_DBUS
	GTEST_SKIP();
#else
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

	auth::AuthenticatorDBus authenticator {loop, chrono::seconds {2}};

	bool action1_called = false;
	bool action2_called = false;
	authenticator.WithToken(
		[JWT_TOKEN, SERVER_URL, &action1_called, &action2_called, &loop, &authenticator](
			auth::ExpectedAuthData ex_auth_data) {
			action1_called = true;
			ASSERT_TRUE(ex_auth_data);

			EXPECT_EQ(ex_auth_data.value().token, JWT_TOKEN);
			EXPECT_EQ(ex_auth_data.value().server_url, SERVER_URL);

			authenticator.ExpireToken();

			authenticator.WithToken([JWT_TOKEN, SERVER_URL, &action2_called, &loop](
										auth::ExpectedAuthData ex_auth_data) {
				action2_called = true;
				ASSERT_FALSE(ex_auth_data);

				loop.Stop();
			});
		});
	loop.Run();

	EXPECT_EQ(n_replies, 2);
	EXPECT_TRUE(action1_called);
	EXPECT_TRUE(action2_called);
#endif // MENDER_USE_DBUS
}

TEST_F(AuthDBusTests, AuthenticatorBasicRealLifeTest) {
#ifndef MENDER_USE_DBUS
	GTEST_SKIP();
#else
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

	auth::AuthenticatorDBus authenticator {loop, chrono::seconds {2}};

	bool action_called = false;
	authenticator.WithToken(
		[JWT_TOKEN, SERVER_URL, &action_called, &loop](auth::ExpectedAuthData ex_auth_data) {
			action_called = true;
			ASSERT_TRUE(ex_auth_data);

			EXPECT_EQ(ex_auth_data.value().token, JWT_TOKEN);
			EXPECT_EQ(ex_auth_data.value().server_url, SERVER_URL);
			loop.Stop();
		});

	loop.Run();
	EXPECT_TRUE(action_called);
#endif // MENDER_USE_DBUS
}

TEST(AuthNoDBusTests, AuthenticatorAttemptNoDBus) {
#ifndef MENDER_USE_DBUS
	GTEST_SKIP();
#else
	setenv("DBUS_SYSTEM_BUS_ADDRESS", "dummy-address", 1);

	TestEventLoop loop;
	auth::AuthenticatorDBus authenticator {loop};

	bool action_called = false;
	authenticator.WithToken([&action_called](auth::ExpectedAuthData ex_auth_data) {
		action_called = true;
		// With no D-Bus available the token cannot be obtained. WithToken no longer
		// returns an error; instead the failure is delivered to the action itself with error set
		// in ex_auth_data.
		EXPECT_FALSE(ex_auth_data);
	});

	events::Timer timer(loop);
	timer.AsyncWait(chrono::milliseconds(500), [&loop](error::Error err) {
		ASSERT_EQ(err, error::NoError);
		loop.Stop();
	});

	loop.Run();
	EXPECT_TRUE(action_called);

	unsetenv("DBUS_SYSTEM_BUS_ADDRESS");
#endif // MENDER_USE_DBUS
}

// Deterministic authenticator for the MEN-9246 StartWatchingTokenSignal regression
// test, without a real D-Bus: StartWatchingTokenSignal() can be made to fail, and
// the internal queue / in-progress state is exposed so the test can assert on it
// directly.
class TestAuthenticator : public auth::Authenticator {
public:
	using auth::Authenticator::Authenticator;

	bool start_watching_fails = false;
	bool get_jwt_token_fails = false;

	// Expose the real internal state so tests can assert on it directly, rather
	// than only on whether an action callback happened to run.
	size_t PendingActionsCount() const {
		return pending_actions_.size();
	}
	bool TokenFetchInProgress() const {
		return token_fetch_in_progress_;
	}

protected:
	error::Error StartWatchingTokenSignal() override {
		if (start_watching_fails) {
			return auth::MakeError(auth::AuthenticationError, "simulated signal watch failure");
		}
		return error::NoError;
	}
	error::Error GetJwtToken() override {
		if (get_jwt_token_fails) {
			return auth::MakeError(auth::AuthenticationError, "simulated GetJwtToken failure");
		}
		// Fetch is "initiated" but never resolves in this test (no token delivered),
		// which keeps it in progress with the action queued.
		return error::NoError;
	}
	error::Error FetchJwtToken() override {
		return error::NoError;
	}
};

// Regression test for MEN-9246, StartWatchingTokenSignal branch.
//
// This branch runs BEFORE the token_fetch_in_progress_ gate, so it can be
// reached while a fetch is already in flight with other actions queued behind
// it. A failure here must be delivered to THIS action only and must NOT leave
// the action in pending_actions_: the original defect stranded it in the shared
// queue, so a later token resolution re-delivered it as a surplus terminal event
// (this is what aborted the state machine on D-Bus restore). It must also not
// disturb the in-flight fetch. We assert on pending_actions_ / the in-progress
// flag directly, because on the buggy code the failing branch never invokes the
// action, so an action-callback counter alone would not observe the defect.
TEST(AuthRegressionTests, StartWatchingFailureDoesNotStrandTheAction) {
	TestEventLoop loop;
	TestAuthenticator authenticator {loop};

	// A fetch is in progress, with one action queued behind it.
	authenticator.WithToken([](auth::ExpectedAuthData) {});
	ASSERT_EQ(authenticator.PendingActionsCount(), 1u);
	ASSERT_TRUE(authenticator.TokenFetchInProgress());

	// A second call whose StartWatchingTokenSignal fails.
	int failed_calls = 0;
	authenticator.start_watching_fails = true;
	authenticator.WithToken([&failed_calls](auth::ExpectedAuthData ex_auth_data) {
		failed_calls++;
		EXPECT_FALSE(ex_auth_data);
	});
	authenticator.start_watching_fails = false;

	// The failing action must NOT have been left in the shared queue, and the
	// in-flight fetch (and its queued action) must be untouched. On the buggy code
	// the action is stranded here, so the count is 2.
	EXPECT_EQ(authenticator.PendingActionsCount(), 1u);
	EXPECT_TRUE(authenticator.TokenFetchInProgress());

	events::Timer timer {loop};
	timer.AsyncWait(chrono::milliseconds {200}, [&loop](error::Error) { loop.Stop(); });
	loop.Run();

	// The failing action is delivered its error exactly once (not stranded, not
	// dropped).
	EXPECT_EQ(failed_calls, 1);
}

// Coverage test for MEN-9246, GetJwtToken branch.
//
// This branch is reached only when NO fetch is in progress (it starts one), so
// it cannot strand an action behind an in-flight fetch the way the
// StartWatchingTokenSignal branch could. On master this branch was already
// auth-correct (it delivered the error once and cleared the queue); the surplus
// terminal event that aborted the state machine came from the caller layer
// (WithToken both delivered the error AND returned it), which is not observable
// inside Authenticator and is now structurally prevented by the void return.
TEST(AuthRegressionTests, GetJwtTokenFailureDeliversErrorWithoutQueueing) {
	TestEventLoop loop;
	TestAuthenticator authenticator {loop};

	// No fetch in progress and nothing queued: this call is the one that would
	// start a fetch, but GetJwtToken fails.
	ASSERT_EQ(authenticator.PendingActionsCount(), 0u);
	ASSERT_FALSE(authenticator.TokenFetchInProgress());

	int failed_calls = 0;
	authenticator.get_jwt_token_fails = true;
	authenticator.WithToken([&failed_calls](auth::ExpectedAuthData ex_auth_data) {
		failed_calls++;
		EXPECT_FALSE(ex_auth_data);
	});
	authenticator.get_jwt_token_fails = false;

	// The failing action must not be queued, and no fetch must be marked in
	// progress: the failure fully resolves this call and leaves no state behind.
	EXPECT_EQ(authenticator.PendingActionsCount(), 0u);
	EXPECT_FALSE(authenticator.TokenFetchInProgress());

	events::Timer timer {loop};
	timer.AsyncWait(chrono::milliseconds {200}, [&loop](error::Error) { loop.Stop(); });
	loop.Run();

	// The error is delivered to the action exactly once.
	EXPECT_EQ(failed_calls, 1);
}
