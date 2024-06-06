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

#include <api/client.hpp>

#include <string>
#include <iostream>

#include <gtest/gtest.h>

#include <api/auth.hpp>
#include <common/common.hpp>
#include <common/error.hpp>
#include <common/events.hpp>
#include <common/expected.hpp>
#include <common/http.hpp>
#include <common/io.hpp>
#include <common/path.hpp>
#include <common/testing.hpp>

#ifdef MENDER_USE_DBUS
#include <common/platform/dbus.hpp>
#include <common/platform/testing_dbus.hpp>
#endif

using namespace std;

namespace api = mender::api;
namespace auth = mender::api::auth;
namespace common = mender::common;
namespace error = mender::common::error;
namespace events = mender::common::events;
namespace expected = mender::common::expected;
namespace http = mender::common::http;
namespace io = mender::common::io;
namespace path = mender::common::path;
namespace mtesting = mender::common::testing;

#ifdef MENDER_USE_DBUS
namespace dbus = mender::common::dbus;
namespace testing_dbus = mender::common::testing::dbus;
#endif

using TestEventLoop = mender::common::testing::TestEventLoop;

const string TEST_PORT = "8088";

#ifdef MENDER_USE_DBUS
class APIClientTests : public testing_dbus::DBusTests {
protected:
	mtesting::TemporaryDirectory tmpdir;
	const string test_device_identity_script = path::Join(tmpdir.Path(), "mender-device-identity");
	const string auth_uri = "/api/devices/v1/authentication/auth_requests";

	void SetUp() override {
		testing_dbus::DBusTests::SetUp();

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
#else
class APIClientTests : public testing::Test {};
#endif // MENDER_USE_DBUS

TEST_F(APIClientTests, ClientBasicTest) {
#ifndef MENDER_USE_DBUS
	GTEST_SKIP();
#else
	const string JWT_TOKEN = "FOOBARJWTTOKEN";
	const string SERVER_URL = "http://127.0.0.1:" + TEST_PORT;
	const string test_data = "some testing data";
	const string test_uri = "/test/uri";

	TestEventLoop loop;

	// Setup a test server
	http::ServerConfig server_config;
	http::Server server(server_config, loop);
	server.AsyncServeUrl(
		SERVER_URL,
		[JWT_TOKEN, test_uri](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			auto req = exp_req.value();

			EXPECT_EQ(req->GetPath(), test_uri);
			auto ex_auth = req->GetHeader("Authorization");
			ASSERT_TRUE(ex_auth);
			EXPECT_EQ(ex_auth.value(), "Bearer " + JWT_TOKEN);

			req->SetBodyWriter(make_shared<io::Discard>());
		},
		[test_data](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();

			auto result = exp_req.value()->MakeResponse();
			ASSERT_TRUE(result);
			auto resp = result.value();

			resp->SetStatusCodeAndMessage(200, "OK");
			resp->SetBodyReader(make_shared<io::StringReader>(test_data));
			resp->SetHeader("Content-Length", to_string(test_data.size()));
			resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
		});

	// Setup fake mender-auth simply returning auth data
	dbus::DBusServer dbus_server {loop, "io.mender.AuthenticationManager"};
	auto dbus_obj = make_shared<dbus::DBusObject>("/io/mender/AuthenticationManager");
	dbus_obj->AddMethodHandler<dbus::ExpectedStringPair>(
		"io.mender.Authentication1", "GetJwtToken", [JWT_TOKEN, SERVER_URL]() {
			return dbus::StringPair {JWT_TOKEN, SERVER_URL};
		});
	dbus_server.AdvertiseObject(dbus_obj);

	auth::AuthenticatorDBus authenticator {loop, chrono::seconds {2}};

	http::ClientConfig client_config {""};
	api::HTTPClient client {client_config, loop, authenticator};

	auto req = make_shared<api::APIRequest>();
	req->SetMethod(http::Method::GET);
	req->SetPath(test_uri);

	auto received_body = make_shared<vector<uint8_t> >();
	bool header_handler_called = false;
	bool body_handler_called = false;
	auto err = client.AsyncCall(
		req,
		[&header_handler_called, received_body](http::ExpectedIncomingResponsePtr exp_resp) {
			EXPECT_FALSE(header_handler_called);
			header_handler_called = true;

			auto body_writer = make_shared<io::ByteWriter>(received_body);
			ASSERT_TRUE(exp_resp);
			auto resp = exp_resp.value();
			auto content_length = resp->GetHeader("Content-Length");
			auto ex_len = common::StringToLongLong(content_length.value());
			ASSERT_TRUE(ex_len);
			received_body->resize(ex_len.value());
			resp->SetBodyWriter(body_writer);
		},
		[&body_handler_called, test_data, received_body, &loop](
			http::ExpectedIncomingResponsePtr exp_resp) {
			EXPECT_FALSE(body_handler_called);
			body_handler_called = true;

			EXPECT_EQ(common::StringFromByteVector(*received_body), test_data);
			loop.Stop();
		});

	EXPECT_EQ(err, error::NoError) << "Unexpected error: " << err.message;
	loop.Run();

	EXPECT_TRUE(header_handler_called);
	EXPECT_TRUE(body_handler_called);
#endif // MENDER_USE_DBUS
}

TEST_F(APIClientTests, TwoClientsTest) {
#ifndef MENDER_USE_DBUS
	GTEST_SKIP();
#else
	const string JWT_TOKEN = "FOOBARJWTTOKEN";
	const string SERVER_URL = "http://127.0.0.1:" + TEST_PORT;
	const string test_data1 = "some testing data 1";
	const string test_data2 = "some testing data 2";
	const string test_uri1 = "/test/uri/1";
	const string test_uri2 = "/test/uri/2";

	TestEventLoop loop;

	// Setup a test server
	http::ServerConfig server_config;
	http::Server server(server_config, loop);
	server.AsyncServeUrl(
		SERVER_URL,
		[JWT_TOKEN, test_uri1, test_uri2](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			auto req = exp_req.value();
			EXPECT_TRUE((req->GetPath() == test_uri1) || (req->GetPath() == test_uri2));
			auto ex_auth = req->GetHeader("Authorization");
			ASSERT_TRUE(ex_auth);
			EXPECT_EQ(ex_auth.value(), "Bearer " + JWT_TOKEN);
			req->SetBodyWriter(make_shared<io::Discard>());
		},
		[JWT_TOKEN, test_data1, test_data2, test_uri1, test_uri2](
			http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();

			auto result = exp_req.value()->MakeResponse();
			ASSERT_TRUE(result);
			auto resp = result.value();

			auto req = exp_req.value();
			if (req->GetPath() == test_uri1) {
				resp->SetStatusCodeAndMessage(200, "OK");
				resp->SetBodyReader(make_shared<io::StringReader>(test_data1));
				resp->SetHeader("Content-Length", to_string(test_data1.size()));
				resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
			} else {
				EXPECT_EQ(req->GetPath(), test_uri2);
				resp->SetStatusCodeAndMessage(200, "OK");
				resp->SetBodyReader(make_shared<io::StringReader>(test_data2));
				resp->SetHeader("Content-Length", to_string(test_data2.size()));
				resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
			}
		});

	// Setup fake mender-auth simply returning auth data
	int n_replies = 0;
	dbus::DBusServer dbus_server {loop, "io.mender.AuthenticationManager"};
	auto dbus_obj = make_shared<dbus::DBusObject>("/io/mender/AuthenticationManager");
	dbus_obj->AddMethodHandler<dbus::ExpectedStringPair>(
		"io.mender.Authentication1", "GetJwtToken", [JWT_TOKEN, SERVER_URL, &n_replies]() {
			// auth data should be requested by both clients, no caching
			n_replies++;
			EXPECT_LE(n_replies, 2);
			return dbus::StringPair {JWT_TOKEN, SERVER_URL};
		});
	dbus_server.AdvertiseObject(dbus_obj);

	auth::AuthenticatorDBus authenticator {loop, chrono::seconds {2}};

	http::ClientConfig client_config {""};
	api::HTTPClient client1 {client_config, loop, authenticator};

	auto req1 = make_shared<api::APIRequest>();
	req1->SetPath(test_uri1);
	req1->SetMethod(http::Method::GET);

	auto received_body1 = make_shared<vector<uint8_t> >();
	bool header_handler_called1 = false;
	bool body_handler_called1 = false;
	auto err = client1.AsyncCall(
		req1,
		[&header_handler_called1, received_body1](http::ExpectedIncomingResponsePtr exp_resp) {
			EXPECT_FALSE(header_handler_called1);
			header_handler_called1 = true;

			auto body_writer = make_shared<io::ByteWriter>(received_body1);
			ASSERT_TRUE(exp_resp);
			auto resp = exp_resp.value();
			auto content_length = resp->GetHeader("Content-Length");
			auto ex_len = common::StringToLongLong(content_length.value());
			ASSERT_TRUE(ex_len);
			received_body1->resize(ex_len.value());
			resp->SetBodyWriter(body_writer);
		},
		[&body_handler_called1, test_data1, received_body1, &loop](
			http::ExpectedIncomingResponsePtr exp_resp) {
			EXPECT_FALSE(body_handler_called1);
			body_handler_called1 = true;

			EXPECT_EQ(common::StringFromByteVector(*received_body1), test_data1);

			loop.Stop();
		});

	EXPECT_EQ(err, error::NoError) << "Unexpected error: " << err.message;

	loop.Run();

	api::HTTPClient client2 {client_config, loop, authenticator};

	auto req2 = make_shared<api::APIRequest>();
	req2->SetPath(test_uri2);
	req2->SetMethod(http::Method::GET);

	auto received_body2 = make_shared<vector<uint8_t> >();
	bool header_handler_called2 = false;
	bool body_handler_called2 = false;
	err = client2.AsyncCall(
		req2,
		[&header_handler_called2, received_body2](http::ExpectedIncomingResponsePtr exp_resp) {
			EXPECT_FALSE(header_handler_called2);
			header_handler_called2 = true;

			auto body_writer = make_shared<io::ByteWriter>(received_body2);
			ASSERT_TRUE(exp_resp);
			auto resp = exp_resp.value();
			auto content_length = resp->GetHeader("Content-Length");
			auto ex_len = common::StringToLongLong(content_length.value());
			ASSERT_TRUE(ex_len);
			received_body2->resize(ex_len.value());
			resp->SetBodyWriter(body_writer);
		},
		[&body_handler_called2, test_data2, received_body2, &loop](
			http::ExpectedIncomingResponsePtr exp_resp) {
			EXPECT_FALSE(body_handler_called2);
			body_handler_called2 = true;

			EXPECT_EQ(common::StringFromByteVector(*received_body2), test_data2);
			loop.Stop();
		});
	EXPECT_EQ(err, error::NoError) << "Unexpected error: " << err.message;

	loop.Run();

	EXPECT_EQ(n_replies, 2);

	EXPECT_TRUE(header_handler_called1);
	EXPECT_TRUE(body_handler_called1);
	EXPECT_TRUE(header_handler_called2);
	EXPECT_TRUE(body_handler_called2);
#endif // MENDER_USE_DBUS
}

TEST_F(APIClientTests, ClientReauthenticationTest) {
#ifndef MENDER_USE_DBUS
	GTEST_SKIP();
#else
	const string JWT_TOKEN1 = "FOOBARJWTTOKEN1";
	const string JWT_TOKEN2 = "FOOBARJWTTOKEN2";
	const string SERVER_URL = "http://127.0.0.1:" + TEST_PORT;
	const string test_data1 = "some testing data 1";
	const string test_data2 = "some testing data 2";
	const string test_uri1 = "/test/uri/1";
	const string test_uri2 = "/test/uri/2";

	TestEventLoop loop;

	// Setup a test server
	http::ServerConfig server_config;
	http::Server server(server_config, loop);
	bool test_data1_sent = false;
	bool test_data2_requested = false;
	server.AsyncServeUrl(
		SERVER_URL,
		[JWT_TOKEN1, JWT_TOKEN2, test_uri1, test_uri2, &test_data2_requested, &test_data1_sent](
			http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			auto req = exp_req.value();
			if (!test_data1_sent) {
				EXPECT_EQ(req->GetPath(), test_uri1);
				auto ex_auth = req->GetHeader("Authorization");
				ASSERT_TRUE(ex_auth);
				EXPECT_EQ(ex_auth.value(), "Bearer " + JWT_TOKEN1);
			} else if (!test_data2_requested) {
				EXPECT_EQ(req->GetPath(), test_uri2);
				auto ex_auth = req->GetHeader("Authorization");
				ASSERT_TRUE(ex_auth);
				EXPECT_EQ(ex_auth.value(), "Bearer " + JWT_TOKEN1);
			} else {
				EXPECT_EQ(req->GetPath(), test_uri2);
				auto ex_auth = req->GetHeader("Authorization");
				ASSERT_TRUE(ex_auth);
				EXPECT_EQ(ex_auth.value(), "Bearer " + JWT_TOKEN2);
			}
			req->SetBodyWriter(make_shared<io::Discard>());
		},
		[JWT_TOKEN1,
		 JWT_TOKEN2,
		 test_uri1,
		 test_uri2,
		 test_data1,
		 test_data2,
		 &test_data1_sent,
		 &test_data2_requested](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();

			auto result = exp_req.value()->MakeResponse();
			ASSERT_TRUE(result);
			auto resp = result.value();

			auto req = exp_req.value();
			if (!test_data1_sent) {
				resp->SetStatusCodeAndMessage(200, "OK");
				resp->SetBodyReader(make_shared<io::StringReader>(test_data1));
				resp->SetHeader("Content-Length", to_string(test_data1.size()));
				test_data1_sent = true;
			} else if (test_data1_sent && !test_data2_requested) {
				// simulating expired token when requested data the second time
				EXPECT_EQ(req->GetPath(), test_uri2);
				resp->SetStatusCodeAndMessage(401, "Unauthorized");
				test_data2_requested = true;
			} else {
				EXPECT_EQ(req->GetPath(), test_uri2);
				resp->SetStatusCodeAndMessage(200, "OK");
				resp->SetBodyReader(make_shared<io::StringReader>(test_data2));
				resp->SetHeader("Content-Length", to_string(test_data2.size()));
			}
			resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
		});

	// Setup fake mender-auth simply returning auth data
	dbus::DBusServer dbus_server {loop, "io.mender.AuthenticationManager"};
	auto dbus_obj = make_shared<dbus::DBusObject>("/io/mender/AuthenticationManager");
	dbus_obj->AddMethodHandler<dbus::ExpectedStringPair>(
		"io.mender.Authentication1", "GetJwtToken", [JWT_TOKEN1, SERVER_URL]() {
			return dbus::StringPair {JWT_TOKEN1, SERVER_URL};
		});
	dbus_obj->AddMethodHandler<expected::ExpectedBool>(
		"io.mender.Authentication1", "FetchJwtToken", [&dbus_server, JWT_TOKEN2, SERVER_URL]() {
			dbus_server.EmitSignal<dbus::StringPair>(
				"/io/mender/AuthenticationManager",
				"io.mender.Authentication1",
				"JwtTokenStateChange",
				dbus::StringPair {JWT_TOKEN2, SERVER_URL});

			return true;
		});
	dbus_server.AdvertiseObject(dbus_obj);

	auth::AuthenticatorDBus authenticator {loop, chrono::seconds {2}};

	http::ClientConfig client_config {""};
	api::HTTPClient client {client_config, loop, authenticator};

	auto req1 = make_shared<api::APIRequest>();
	req1->SetPath(test_uri1);
	req1->SetMethod(http::Method::GET);

	auto received_body1 = make_shared<vector<uint8_t> >();
	bool header_handler_called1 = false;
	bool body_handler_called1 = false;

	auto req2 = make_shared<api::APIRequest>();
	req2->SetPath(test_uri2);
	req2->SetMethod(http::Method::GET);

	auto received_body2 = make_shared<vector<uint8_t> >();
	bool header_handler_called2 = false;
	bool body_handler_called2 = false;

	http::ResponseHandler header_handler2 =
		[&header_handler_called2, received_body2](http::ExpectedIncomingResponsePtr exp_resp) {
			EXPECT_FALSE(header_handler_called2);
			header_handler_called2 = true;

			auto body_writer = make_shared<io::ByteWriter>(received_body2);
			ASSERT_TRUE(exp_resp);
			auto resp = exp_resp.value();
			auto content_length = resp->GetHeader("Content-Length");
			ASSERT_TRUE(content_length);
			auto ex_len = common::StringToLongLong(content_length.value());
			ASSERT_TRUE(ex_len);
			received_body2->resize(ex_len.value());
			resp->SetBodyWriter(body_writer);
		};

	http::ResponseHandler body_handler2 = [&body_handler_called2,
										   test_data2,
										   received_body2,
										   &loop](http::ExpectedIncomingResponsePtr exp_resp) {
		EXPECT_FALSE(body_handler_called2);
		body_handler_called2 = true;

		EXPECT_EQ(common::StringFromByteVector(*received_body2), test_data2);
		loop.Stop();
	};

	http::ResponseHandler header_handler1 =
		[&header_handler_called1, received_body1](http::ExpectedIncomingResponsePtr exp_resp) {
			EXPECT_FALSE(header_handler_called1);
			header_handler_called1 = true;

			auto body_writer = make_shared<io::ByteWriter>(received_body1);
			ASSERT_TRUE(exp_resp);
			auto resp = exp_resp.value();
			auto content_length = resp->GetHeader("Content-Length");
			auto ex_len = common::StringToLongLong(content_length.value());
			ASSERT_TRUE(ex_len);
			received_body1->resize(ex_len.value());
			resp->SetBodyWriter(body_writer);
		};

	http::ResponseHandler body_handler1 = [&client,
										   &body_handler_called1,
										   test_data1,
										   received_body1,
										   req2,
										   header_handler2,
										   body_handler2,
										   &loop](http::ExpectedIncomingResponsePtr exp_resp) {
		EXPECT_FALSE(body_handler_called1);
		body_handler_called1 = true;

		EXPECT_EQ(common::StringFromByteVector(*received_body1), test_data1);
		loop.Post([&client, req2, header_handler2, body_handler2]() {
			auto err = client.AsyncCall(req2, header_handler2, body_handler2);
			EXPECT_EQ(err, error::NoError) << "Unexpected error: " << err.message;
		});
	};

	auto err = client.AsyncCall(req1, header_handler1, body_handler1);

	EXPECT_EQ(err, error::NoError) << "Unexpected error: " << err.message;
	loop.Run();

	EXPECT_TRUE(header_handler_called1);
	EXPECT_TRUE(body_handler_called1);
	EXPECT_TRUE(header_handler_called2);
	EXPECT_TRUE(body_handler_called2);
#endif // MENDER_USE_DBUS
}

TEST_F(APIClientTests, ClientEarlyAuthErrorTest) {
#ifndef MENDER_USE_DBUS
	GTEST_SKIP();
#else
	const string test_uri = "/test/uri";
	const string SERVER_URL {"http://127.0.0.1:" + TEST_PORT};

	TestEventLoop loop;

	// no DBus server to handle auth here

	auth::AuthenticatorDBus authenticator {loop, chrono::seconds {2}};

	http::ClientConfig client_config {""};
	api::HTTPClient client {client_config, loop, authenticator};

	auto req = make_shared<api::APIRequest>();
	req->SetPath(test_uri);
	req->SetMethod(http::Method::GET);

	bool header_handler_called = false;
	bool body_handler_called = false;
	events::Timer timer {loop};
	auto err = client.AsyncCall(
		req,
		[&header_handler_called, &timer, &loop](http::ExpectedIncomingResponsePtr exp_resp) {
			EXPECT_FALSE(header_handler_called);
			header_handler_called = true;

			EXPECT_FALSE(exp_resp);

			// give the body handler a chance to run (it shouldn't, but if we do
			// loop.Stop() here, it definitely won't)
			timer.AsyncWait(chrono::seconds(1), [&loop](error::Error err) { loop.Stop(); });
		},
		[&body_handler_called, &loop](http::ExpectedIncomingResponsePtr exp_resp) {
			// this shouldn't be called at all
			EXPECT_FALSE(body_handler_called);
			body_handler_called = true;
			loop.Stop();
		});

	EXPECT_EQ(err, error::NoError) << "Unexpected error: " << err.message;
	loop.Run();

	EXPECT_TRUE(header_handler_called);
	EXPECT_FALSE(body_handler_called);
#endif // MENDER_USE_DBUS
}

TEST_F(APIClientTests, ClientAuthenticationTimeoutFailureTest) {
#ifndef MENDER_USE_DBUS
	GTEST_SKIP();
#else
	const string JWT_TOKEN1 = "FOOBARJWTTOKEN1";
	const string SERVER_URL = "http://127.0.0.1:" + TEST_PORT;
	const string test_data1 = "some testing data 1";
	const string test_uri1 = "/test/uri/1";
	const string test_uri2 = "/test/uri/2";

	TestEventLoop loop;

	// Setup a test server
	http::ServerConfig server_config;
	http::Server server(server_config, loop);
	bool test_data1_sent = false;
	bool test_data2_requested = false;
	server.AsyncServeUrl(
		SERVER_URL,
		[JWT_TOKEN1, test_uri1, test_uri2, &test_data2_requested, &test_data1_sent](
			http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			auto req = exp_req.value();
			if (!test_data1_sent) {
				EXPECT_EQ(req->GetPath(), test_uri1);
				auto ex_auth = req->GetHeader("Authorization");
				ASSERT_TRUE(ex_auth);
				EXPECT_EQ(ex_auth.value(), "Bearer " + JWT_TOKEN1);
			} else if (!test_data2_requested) {
				EXPECT_EQ(req->GetPath(), test_uri2);
				auto ex_auth = req->GetHeader("Authorization");
				ASSERT_TRUE(ex_auth);
				EXPECT_EQ(ex_auth.value(), "Bearer " + JWT_TOKEN1);
			}
			req->SetBodyWriter(make_shared<io::Discard>());
		},
		[JWT_TOKEN1, test_uri1, test_uri2, test_data1, &test_data1_sent, &test_data2_requested](
			http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();

			auto result = exp_req.value()->MakeResponse();
			ASSERT_TRUE(result);
			auto resp = result.value();

			auto req = exp_req.value();
			if (!test_data1_sent) {
				resp->SetStatusCodeAndMessage(200, "OK");
				resp->SetBodyReader(make_shared<io::StringReader>(test_data1));
				resp->SetHeader("Content-Length", to_string(test_data1.size()));
				test_data1_sent = true;
			} else if (test_data1_sent && !test_data2_requested) {
				// simulating expired token when requested data the second time
				EXPECT_EQ(req->GetPath(), test_uri2);
				resp->SetStatusCodeAndMessage(401, "Unauthorized");
				test_data2_requested = true;
			}
			resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
		});

	// Setup fake mender-auth simply returning auth data
	dbus::DBusServer dbus_server {loop, "io.mender.AuthenticationManager"};
	auto dbus_obj = make_shared<dbus::DBusObject>("/io/mender/AuthenticationManager");
	dbus_obj->AddMethodHandler<dbus::ExpectedStringPair>(
		"io.mender.Authentication1", "GetJwtToken", [JWT_TOKEN1, SERVER_URL]() {
			return dbus::StringPair {JWT_TOKEN1, SERVER_URL};
		});
	dbus_obj->AddMethodHandler<expected::ExpectedBool>(
		"io.mender.Authentication1", "FetchJwtToken", []() {
			// no signal emitted here
			return true;
		});
	dbus_server.AdvertiseObject(dbus_obj);

	auth::AuthenticatorDBus authenticator {loop, chrono::seconds {2}};

	http::ClientConfig client_config {""};
	api::HTTPClient client {client_config, loop, authenticator};

	auto req1 = make_shared<api::APIRequest>();
	req1->SetPath(test_uri1);
	req1->SetMethod(http::Method::GET);

	auto received_body1 = make_shared<vector<uint8_t> >();
	bool header_handler_called1 = false;
	bool body_handler_called1 = false;

	auto req2 = make_shared<api::APIRequest>();
	req2->SetPath(test_uri2);
	req2->SetMethod(http::Method::GET);

	bool header_handler_called2 = false;
	bool body_handler_called2 = false;

	events::Timer timer {loop};
	http::ResponseHandler header_handler2 =
		[&header_handler_called2, &timer, &loop](http::ExpectedIncomingResponsePtr exp_resp) {
			EXPECT_FALSE(header_handler_called2);
			header_handler_called2 = true;

			EXPECT_FALSE(exp_resp);
			EXPECT_EQ(exp_resp.error().code, auth::MakeError(auth::AuthenticationError, "").code);

			// give the body handler a chance to run (it shouldn't, but if we do
			// loop.Stop() here, it definitely won't)
			timer.AsyncWait(chrono::seconds(1), [&loop](error::Error err) { loop.Stop(); });
		};

	http::ResponseHandler body_handler2 = [&body_handler_called2,
										   &loop](http::ExpectedIncomingResponsePtr exp_resp) {
		// should never be called
		EXPECT_FALSE(body_handler_called2);
		body_handler_called2 = true;
		EXPECT_FALSE(exp_resp);
		loop.Stop();
	};

	http::ResponseHandler header_handler1 =
		[&header_handler_called1, received_body1](http::ExpectedIncomingResponsePtr exp_resp) {
			EXPECT_FALSE(header_handler_called1);
			header_handler_called1 = true;

			auto body_writer = make_shared<io::ByteWriter>(received_body1);
			ASSERT_TRUE(exp_resp);
			auto resp = exp_resp.value();
			auto content_length = resp->GetHeader("Content-Length");
			auto ex_len = common::StringToLongLong(content_length.value());
			ASSERT_TRUE(ex_len);
			received_body1->resize(ex_len.value());
			resp->SetBodyWriter(body_writer);
		};

	http::ResponseHandler body_handler1 = [&client,
										   &body_handler_called1,
										   test_data1,
										   received_body1,
										   req2,
										   header_handler2,
										   body_handler2,
										   &loop](http::ExpectedIncomingResponsePtr exp_resp) {
		EXPECT_FALSE(body_handler_called1);
		body_handler_called1 = true;

		EXPECT_EQ(common::StringFromByteVector(*received_body1), test_data1);
		loop.Post([&client, req2, header_handler2, body_handler2]() {
			auto err = client.AsyncCall(req2, header_handler2, body_handler2);
			EXPECT_EQ(err, error::NoError) << "Unexpected error: " << err.message;
		});
	};

	auto err = client.AsyncCall(req1, header_handler1, body_handler1);

	EXPECT_EQ(err, error::NoError) << "Unexpected error: " << err.message;
	loop.Run();

	EXPECT_TRUE(header_handler_called1);
	EXPECT_TRUE(body_handler_called1);
	EXPECT_TRUE(header_handler_called2);
	EXPECT_FALSE(body_handler_called2);
#endif // MENDER_USE_DBUS
}

TEST_F(APIClientTests, ClientReauthenticationFailureTest) {
#ifndef MENDER_USE_DBUS
	GTEST_SKIP();
#else
	const string JWT_TOKEN1 = "FOOBARJWTTOKEN1";
	const string SERVER_URL = "http://127.0.0.1:" + TEST_PORT;
	const string test_data1 = "some testing data 1";
	const string test_uri1 = "/test/uri/1";
	const string test_uri2 = "/test/uri/2";

	TestEventLoop loop;

	// Setup a test server
	http::ServerConfig server_config;
	http::Server server(server_config, loop);
	bool test_data1_sent = false;
	bool test_data2_requested = false;
	server.AsyncServeUrl(
		SERVER_URL,
		[JWT_TOKEN1, test_uri1, test_uri2, &test_data2_requested, &test_data1_sent](
			http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			auto req = exp_req.value();
			if (!test_data1_sent) {
				EXPECT_EQ(req->GetPath(), test_uri1);
				auto ex_auth = req->GetHeader("Authorization");
				ASSERT_TRUE(ex_auth);
				EXPECT_EQ(ex_auth.value(), "Bearer " + JWT_TOKEN1);
			} else if (!test_data2_requested) {
				EXPECT_EQ(req->GetPath(), test_uri2);
				auto ex_auth = req->GetHeader("Authorization");
				ASSERT_TRUE(ex_auth);
				EXPECT_EQ(ex_auth.value(), "Bearer " + JWT_TOKEN1);
			}
			req->SetBodyWriter(make_shared<io::Discard>());
		},
		[JWT_TOKEN1, test_uri1, test_uri2, test_data1, &test_data1_sent, &test_data2_requested](
			http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();

			auto result = exp_req.value()->MakeResponse();
			ASSERT_TRUE(result);
			auto resp = result.value();

			auto req = exp_req.value();
			if (!test_data1_sent) {
				resp->SetStatusCodeAndMessage(200, "OK");
				resp->SetBodyReader(make_shared<io::StringReader>(test_data1));
				resp->SetHeader("Content-Length", to_string(test_data1.size()));
				test_data1_sent = true;
			} else if (test_data1_sent && !test_data2_requested) {
				// simulating expired token when requested data the second time
				EXPECT_EQ(req->GetPath(), test_uri2);
				resp->SetStatusCodeAndMessage(401, "Unauthorized");
				test_data2_requested = true;
			}
			resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
		});

	// Setup fake mender-auth simply returning auth data
	dbus::DBusServer dbus_server {loop, "io.mender.AuthenticationManager"};
	auto dbus_obj = make_shared<dbus::DBusObject>("/io/mender/AuthenticationManager");
	dbus_obj->AddMethodHandler<dbus::ExpectedStringPair>(
		"io.mender.Authentication1", "GetJwtToken", [JWT_TOKEN1, SERVER_URL]() {
			return dbus::StringPair {JWT_TOKEN1, SERVER_URL};
		});
	dbus_obj->AddMethodHandler<expected::ExpectedBool>(
		"io.mender.Authentication1", "FetchJwtToken", [&dbus_server]() {
			auto err = dbus_server.EmitSignal(
				"/io/mender/AuthenticationManager",
				"io.mender.Authentication1",
				"JwtTokenStateChange",
				dbus::StringPair {"", ""});
			EXPECT_EQ(err, error::NoError);
			return true;
		});
	dbus_server.AdvertiseObject(dbus_obj);

	auth::AuthenticatorDBus authenticator {loop, chrono::seconds {2}};

	http::ClientConfig client_config {""};
	api::HTTPClient client {client_config, loop, authenticator};

	auto req1 = make_shared<api::APIRequest>();
	req1->SetPath(test_uri1);
	req1->SetMethod(http::Method::GET);

	auto received_body1 = make_shared<vector<uint8_t> >();
	bool header_handler_called1 = false;
	bool body_handler_called1 = false;

	auto req2 = make_shared<api::APIRequest>();
	req2->SetPath(test_uri2);
	req2->SetMethod(http::Method::GET);

	bool header_handler_called2 = false;
	bool body_handler_called2 = false;

	events::Timer timer {loop};
	http::ResponseHandler header_handler2 =
		[&header_handler_called2, &timer, &loop](http::ExpectedIncomingResponsePtr exp_resp) {
			EXPECT_FALSE(header_handler_called2);
			header_handler_called2 = true;

			EXPECT_FALSE(exp_resp);
			EXPECT_EQ(exp_resp.error().code, auth::MakeError(auth::UnauthorizedError, "").code);

			// give the body handler a chance to run (it shouldn't, but if we do
			// loop.Stop() here, it definitely won't)
			timer.AsyncWait(chrono::seconds(1), [&loop](error::Error err) { loop.Stop(); });
		};

	http::ResponseHandler body_handler2 = [&body_handler_called2,
										   &loop](http::ExpectedIncomingResponsePtr exp_resp) {
		// should never be called
		EXPECT_FALSE(body_handler_called2);
		body_handler_called2 = true;
		EXPECT_FALSE(exp_resp);
		loop.Stop();
	};

	http::ResponseHandler header_handler1 =
		[&header_handler_called1, received_body1](http::ExpectedIncomingResponsePtr exp_resp) {
			EXPECT_FALSE(header_handler_called1);
			header_handler_called1 = true;

			auto body_writer = make_shared<io::ByteWriter>(received_body1);
			ASSERT_TRUE(exp_resp);
			auto resp = exp_resp.value();
			auto content_length = resp->GetHeader("Content-Length");
			auto ex_len = common::StringToLongLong(content_length.value());
			ASSERT_TRUE(ex_len);
			received_body1->resize(ex_len.value());
			resp->SetBodyWriter(body_writer);
		};

	http::ResponseHandler body_handler1 = [&client,
										   &body_handler_called1,
										   test_data1,
										   received_body1,
										   req2,
										   header_handler2,
										   body_handler2,
										   &loop](http::ExpectedIncomingResponsePtr exp_resp) {
		EXPECT_FALSE(body_handler_called1);
		body_handler_called1 = true;

		EXPECT_EQ(common::StringFromByteVector(*received_body1), test_data1);
		loop.Post([&client, req2, header_handler2, body_handler2]() {
			auto err = client.AsyncCall(req2, header_handler2, body_handler2);
			EXPECT_EQ(err, error::NoError) << "Unexpected error: " << err.message;
		});
	};

	auto err = client.AsyncCall(req1, header_handler1, body_handler1);

	EXPECT_EQ(err, error::NoError) << "Unexpected error: " << err.message;
	loop.Run();

	EXPECT_TRUE(header_handler_called1);
	EXPECT_TRUE(body_handler_called1);
	EXPECT_TRUE(header_handler_called2);
	EXPECT_FALSE(body_handler_called2);
#endif // MENDER_USE_DBUS
}
