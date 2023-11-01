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
#include <common/http.hpp>
#include <common/io.hpp>
#include <common/log.hpp>
#include <common/path.hpp>
#include <common/testing.hpp>
#include <common/testing_dbus.hpp>

using namespace std;

namespace auth = mender::api::auth;
namespace dbus = mender::common::dbus;
namespace error = mender::common::error;
namespace events = mender::common::events;
namespace expected = mender::common::expected;
namespace http = mender::http;
namespace io = mender::common::io;
namespace mlog = mender::common::log;
namespace path = mender::common::path;
namespace mtesting = mender::common::testing;
namespace testing_dbus = mender::common::testing::dbus;

using TestEventLoop = mender::common::testing::TestEventLoop;

const string TEST_PORT = "8088";
const string TEST_PORT2 = "8089";
const string TEST_PORT3 = "8090";

class AuthTests : public testing::Test {
protected:
	mtesting::TemporaryDirectory tmpdir;
	const string test_device_identity_script = path::Join(tmpdir.Path(), "mender-device-identity");

	void SetUp() override {
		// silence Debug and Trace noise from HTTP and stuff
		mlog::SetLevel(mlog::LogLevel::Info);

		// Create the device-identity script
		string script = R"(#!/bin/sh
echo "key1=value1"
echo "key2=value2"
echo "key3=value3"
echo "key1=value11"
exit 0
)";

		ofstream os(test_device_identity_script);
		os << script;
		os.close();

		int ret = chmod(test_device_identity_script.c_str(), S_IRUSR | S_IWUSR | S_IXUSR);
		ASSERT_EQ(ret, 0);
	}
};

class AuthDBusTests : public testing_dbus::DBusTests {};

TEST_F(AuthTests, FetchJWTTokenBasicTest) {
	const string JWT_TOKEN = "FOOBARJWTTOKEN";

	TestEventLoop loop;

	// Setup a test server
	const string server_url {"http://127.0.0.1:" + TEST_PORT};
	http::ServerConfig server_config;
	http::Server server(server_config, loop);
	server.AsyncServeUrl(
		server_url,
		[](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			exp_req.value()->SetBodyWriter(make_shared<io::Discard>());
		},
		[JWT_TOKEN](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();

			auto result = exp_req.value()->MakeResponse();
			ASSERT_TRUE(result);
			auto resp = result.value();

			resp->SetStatusCodeAndMessage(200, "OK");
			resp->SetBodyReader(make_shared<io::StringReader>(JWT_TOKEN));
			resp->SetHeader("Content-Length", to_string(JWT_TOKEN.size()));
			resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
		});

	string private_key_path = "./private_key.pem";

	string server_certificate_path {};
	http::ClientConfig client_config {server_certificate_path};
	http::Client client {client_config, loop};

	vector<string> servers {server_url};
	auth::APIResponseHandler handle_jwt_token_callback =
		[&loop, JWT_TOKEN, &servers](auth::APIResponse resp) {
			ASSERT_TRUE(resp);
			EXPECT_EQ(resp.value().token, JWT_TOKEN);
			EXPECT_EQ(resp.value().server_url, servers[0]);
			loop.Stop();
		};
	auto err = auth::FetchJWTToken(
		client,
		servers,
		{private_key_path},
		test_device_identity_script,
		handle_jwt_token_callback);

	loop.Run();

	ASSERT_EQ(err, error::NoError) << "Unexpected error: " << err.message;
}

TEST_F(AuthTests, FetchJWTTokenFailoverTest) {
	const string JWT_TOKEN = "FOOBARJWTTOKEN";

	TestEventLoop loop;

	// Setup test servers (a working one and a failing one)
	const string working_server_url {"http://127.0.0.1:" + TEST_PORT};
	http::ServerConfig server_config;
	http::Server working_server(server_config, loop);
	working_server.AsyncServeUrl(
		working_server_url,
		[](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			exp_req.value()->SetBodyWriter(make_shared<io::Discard>());
		},
		[JWT_TOKEN](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();

			auto result = exp_req.value()->MakeResponse();
			ASSERT_TRUE(result);
			auto resp = result.value();

			resp->SetStatusCodeAndMessage(200, "OK");
			resp->SetBodyReader(make_shared<io::StringReader>(JWT_TOKEN));
			resp->SetHeader("Content-Length", to_string(JWT_TOKEN.size()));
			resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
		});

	const string failing_server_url {"http://127.0.0.1:" + TEST_PORT3};
	http::Server failing_server(server_config, loop);
	const string err_response_data =
		R"({"error": "Bad weather in the clouds", "response-id": "some id here"})";
	failing_server.AsyncServeUrl(
		failing_server_url,
		[](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			exp_req.value()->SetBodyWriter(make_shared<io::Discard>());
		},
		[err_response_data](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();

			auto result = exp_req.value()->MakeResponse();
			ASSERT_TRUE(result);
			auto resp = result.value();

			resp->SetStatusCodeAndMessage(500, "Internal server error");
			resp->SetBodyReader(make_shared<io::StringReader>(err_response_data));
			resp->SetHeader("Content-Length", to_string(err_response_data.size()));
			resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
		});

	string private_key_path = "./private_key.pem";

	string server_certificate_path {};
	http::ClientConfig client_config {server_certificate_path};
	http::Client client {client_config, loop};

	const string no_server_url {"http://127.0.0.1:" + TEST_PORT2};
	vector<string> servers {no_server_url, failing_server_url, working_server_url};
	auth::APIResponseHandler handle_jwt_token_callback =
		[&loop, JWT_TOKEN, working_server_url](auth::APIResponse resp) {
			ASSERT_TRUE(resp);
			EXPECT_EQ(resp.value().token, JWT_TOKEN);
			EXPECT_EQ(resp.value().server_url, working_server_url);
			loop.Stop();
		};
	auto err = auth::FetchJWTToken(
		client,
		servers,
		{private_key_path},
		test_device_identity_script,
		handle_jwt_token_callback);

	loop.Run();

	ASSERT_EQ(err, error::NoError) << "Unexpected error: " << err.message;
}

TEST_F(AuthTests, FetchJWTTokenFailTest) {
	TestEventLoop loop;

	http::ServerConfig server_config;
	const string failing_server_url {"http://127.0.0.1:" + TEST_PORT3};
	http::Server failing_server(server_config, loop);
	const string err_response_data =
		R"({"error": "Bad weather in the clouds", "response-id": "some id here"})";
	failing_server.AsyncServeUrl(
		failing_server_url,
		[](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			exp_req.value()->SetBodyWriter(make_shared<io::Discard>());
		},
		[err_response_data](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();

			auto result = exp_req.value()->MakeResponse();
			ASSERT_TRUE(result);
			auto resp = result.value();

			resp->SetStatusCodeAndMessage(500, "Internal server error");
			resp->SetBodyReader(make_shared<io::StringReader>(err_response_data));
			resp->SetHeader("Content-Length", to_string(err_response_data.size()));
			resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
		});

	string private_key_path = "./private_key.pem";

	string server_certificate_path {};
	http::ClientConfig client_config {server_certificate_path};
	http::Client client {client_config, loop};

	const string no_server_url {"http://127.0.0.1:" + TEST_PORT2};
	vector<string> servers {no_server_url, failing_server_url};
	auth::APIResponseHandler handle_jwt_token_callback = [&loop](auth::APIResponse resp) {
		loop.Stop();
		ASSERT_FALSE(resp);
		EXPECT_THAT(resp.error().String(), ::testing::HasSubstr("Authentication error"));
		EXPECT_THAT(resp.error().String(), ::testing::HasSubstr("No more servers"));
	};
	auto err = auth::FetchJWTToken(
		client,
		servers,
		{private_key_path},
		test_device_identity_script,
		handle_jwt_token_callback);

	loop.Run();

	ASSERT_EQ(err, error::NoError) << "Unexpected error: " << err.message;
}

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

TEST_F(AuthDBusTests, AuthenticatorExternalTokenUpdateTest) {
	const string JWT_TOKEN = "FOOBARJWTTOKEN";
	const string SERVER_URL = "some.server";

	TestEventLoop loop;

	// Setup fake mender-auth returning auth data
	int n_replies = 0;
	dbus::DBusServer dbus_server {loop, "io.mender.AuthenticationManager"};
	auto dbus_obj = make_shared<dbus::DBusObject>("/io/mender/AuthenticationManager");
	dbus_obj->AddMethodHandler<dbus::ExpectedStringPair>(
		"io.mender.Authentication1", "GetJwtToken", [JWT_TOKEN, SERVER_URL, &n_replies]() {
			n_replies++;
			return dbus::StringPair {JWT_TOKEN, SERVER_URL};
		});
	dbus_obj->AddMethodHandler<expected::ExpectedBool>(
		"io.mender.Authentication1", "FetchJwtToken", [&dbus_server, JWT_TOKEN, SERVER_URL]() {
			dbus_server.EmitSignal<dbus::StringPair>(
				"/io/mender/AuthenticationManager",
				"io.mender.Authentication1",
				"JwtTokenStateChange",
				dbus::StringPair {JWT_TOKEN + "2", SERVER_URL + "2"});

			return true;
		});
	dbus_server.AdvertiseObject(dbus_obj);

	dbus::DBusClient dbus_client {loop};
	auth::Authenticator authenticator {loop, chrono::seconds {2}};

	events::Timer ext_token_fetch_timer {loop};
	events::Timer second_with_token_timer {loop};
	bool action1_called = false;
	bool action2_called = false;
	auto err = authenticator.WithToken(
		[JWT_TOKEN, SERVER_URL, &action1_called](auth::ExpectedAuthData ex_auth_data) {
			action1_called = true;
			ASSERT_TRUE(ex_auth_data);

			EXPECT_EQ(ex_auth_data.value().token, JWT_TOKEN);
			EXPECT_EQ(ex_auth_data.value().server_url, SERVER_URL);
		});
	EXPECT_EQ(err, error::NoError) << "Unexpected error: " << err.message;
	ext_token_fetch_timer.AsyncWait(chrono::seconds {1}, [&dbus_client](error::Error err) {
		dbus_client.CallMethod<expected::ExpectedBool>(
			"io.mender.AuthenticationManager",
			"/io/mender/AuthenticationManager",
			"io.mender.Authentication1",
			"FetchJwtToken",
			[](expected::ExpectedBool ex_value) {
				ASSERT_TRUE(ex_value);
				ASSERT_TRUE(ex_value.value());
			});
	});
	second_with_token_timer.AsyncWait(
		chrono::seconds {2},
		[JWT_TOKEN, SERVER_URL, &authenticator, &action2_called, &loop](error::Error err) {
			auto lerr = authenticator.WithToken([JWT_TOKEN, SERVER_URL, &action2_called, &loop](
													auth::ExpectedAuthData ex_auth_data) {
				action2_called = true;
				ASSERT_TRUE(ex_auth_data);

				EXPECT_EQ(ex_auth_data.value().token, JWT_TOKEN + "2");
				EXPECT_EQ(ex_auth_data.value().server_url, SERVER_URL + "2");

				loop.Stop();
			});
			EXPECT_EQ(lerr, error::NoError) << "Unexpected error: " << lerr.message;
		});
	loop.Run();
	EXPECT_TRUE(action1_called);
	EXPECT_TRUE(action2_called);

	// GetJwtToken() should have only been called once, by the first
	// WithToken(), the second WithToken() should use the token delivered by the
	// DBus signal.
	EXPECT_EQ(n_replies, 1);
}
