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

#include <mender-auth/ipc/server.hpp>

#include <iostream>
#include <string>
#include <thread>
#include <vector>

#include <gtest/gtest.h>

#include <client_shared/conf.hpp>
#include <common/platform/dbus.hpp>
#include <common/error.hpp>
#include <common/events.hpp>
#include <common/expected.hpp>
#include <common/http.hpp>
#include <common/io.hpp>
#include <common/log.hpp>
#include <common/path.hpp>
#include <common/processes.hpp>
#include <common/testing.hpp>
#include <common/platform/testing_dbus.hpp>

#define TEST_PORT "8001"

using namespace std;

namespace conf = mender::client_shared::conf;
namespace dbus = mender::common::dbus;
namespace error = mender::common::error;
namespace events = mender::common::events;
namespace expected = mender::common::expected;
namespace http = mender::common::http;
namespace io = mender::common::io;
namespace mlog = mender::common::log;
namespace path = mender::common::path;
namespace procs = mender::common::processes;
namespace mtesting = mender::common::testing;
namespace testing_dbus = mender::common::testing::dbus;

namespace ipc = mender::auth::ipc;

using TestEventLoop = mender::common::testing::TestEventLoop;

class ListenClientTests : public testing_dbus::DBusTests {
protected:
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

		{
			ofstream os(test_device_identity_script);
			os << script;
		}

		int ret = chmod(test_device_identity_script.c_str(), S_IRUSR | S_IXUSR);
		ASSERT_EQ(ret, 0);
	}

	string test_device_identity_script {path::Join(tmp_dir_.Path(), "mender-device-identity")};
};

TEST_F(ListenClientTests, TestListenGetJWTToken) {
	TestEventLoop loop;

	conf::MenderConfig config {};
	config.servers.push_back("http://127.0.0.1:" TEST_PORT);
	ipc::Server server {loop, config};
	server.Cache("foobar", "http://127.0.0.1:" TEST_PORT);
	auto err = server.Listen({"./private-key.rsa.pem"}, test_device_identity_script);
	ASSERT_EQ(err, error::NoError);

	// Set up the test client (Emulating mender-update)
	dbus::DBusClient client {loop};
	err = client.CallMethod<dbus::ExpectedStringPair>(
		"io.mender.AuthenticationManager",
		"/io/mender/AuthenticationManager",
		"io.mender.Authentication1",
		"GetJwtToken",
		[&loop](dbus::ExpectedStringPair ex_values) {
			ASSERT_TRUE(ex_values) << ex_values.error().message;
			EXPECT_EQ(ex_values.value().first, "foobar");
			EXPECT_EQ(ex_values.value().second, "http://127.0.0.1:" TEST_PORT);
			loop.Stop();
		});

	loop.Run();
}

TEST_F(ListenClientTests, TestListenFetchJWTToken) {
	TestEventLoop loop;

	string expected_jwt_token {"foobarbazbatz"};
	string expected_hosted_url {"http://127.0.0.1:" TEST_PORT};

	// Set up the test server (Emulating hosted mender)
	http::ServerConfig test_server_config {};
	http::Server http_server(test_server_config, loop);
	auto err = http_server.AsyncServeUrl(
		"http://127.0.0.1:" TEST_PORT,
		[](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			auto &req = exp_req.value();

			EXPECT_EQ(req->GetPath(), "/api/devices/v1/authentication/auth_requests");
			req->SetBodyWriter(make_shared<io::Discard>());
		},
		[&expected_jwt_token](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();

			auto result = exp_req.value()->MakeResponse();
			ASSERT_TRUE(result);
			auto resp = result.value();

			resp->SetStatusCodeAndMessage(200, "Success");
			resp->SetBodyReader(make_shared<io::StringReader>(expected_jwt_token));
			resp->SetHeader("Content-Length", to_string(expected_jwt_token.size()));
			resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
		});
	ASSERT_EQ(error::NoError, err);

	conf::MenderConfig config {};
	config.servers.push_back("http://127.0.0.1:" TEST_PORT);
	config.tenant_token = "dummytenanttoken";

	ipc::Server server {loop, config};
	server.Cache("NotYourJWTTokenBitch", "http://127.1.1.1:" TEST_PORT);
	err = server.Listen({"./private-key.rsa.pem"}, test_device_identity_script);
	ASSERT_EQ(err, error::NoError);

	dbus::DBusClient client {loop};
	err = client.RegisterSignalHandler<dbus::ExpectedStringPair>(
		"io.mender.Authentication1",
		"JwtTokenStateChange",
		[&loop, expected_jwt_token, &server](dbus::ExpectedStringPair ex_value) {
			ASSERT_TRUE(ex_value);
			EXPECT_EQ(ex_value.value().first, expected_jwt_token);
			EXPECT_EQ(ex_value.value().second, server.GetServerURL());
			loop.Stop();
		});
	ASSERT_EQ(err, error::NoError);

	err = client.CallMethod<expected::ExpectedBool>(
		"io.mender.AuthenticationManager",
		"/io/mender/AuthenticationManager",
		"io.mender.Authentication1",
		"FetchJwtToken",
		[](expected::ExpectedBool ex_value) {
			ASSERT_TRUE(ex_value) << ex_value.error().message;
			EXPECT_TRUE(ex_value.value());
		});
	ASSERT_EQ(err, error::NoError);

	loop.Run();

	EXPECT_EQ(expected_jwt_token, server.GetJWTToken());
	EXPECT_EQ(expected_hosted_url, server.GetForwarder().GetTargetUrl());
	EXPECT_EQ(server.GetServerURL(), server.GetForwarder().GetUrl());
	EXPECT_NE(expected_hosted_url, server.GetServerURL());
}

TEST_F(ListenClientTests, TestUseForwarder) {
	TestEventLoop loop;

	string expected_jwt_token {"foobarbazbatz"};
	string expected_hosted_url {"http://127.0.0.1:" TEST_PORT};

	int stop_counter = 0;

	// Set up the test server (Emulating hosted mender)
	http::ServerConfig test_server_config {};
	http::Server http_server(test_server_config, loop);
	auto err = http_server.AsyncServeUrl(
		"http://127.0.0.1:" TEST_PORT,
		[](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			auto &req = exp_req.value();

			if (req->GetPath() == "/api/devices/v1/authentication/auth_requests") {
				req->SetBodyWriter(make_shared<io::Discard>());
			} else {
				EXPECT_EQ(req->GetPath(), "/payload-endpoint");
			}
		},
		[&expected_jwt_token, &stop_counter, &loop](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			auto &req = exp_req.value();

			auto result = exp_req.value()->MakeResponse();
			ASSERT_TRUE(result);
			auto resp = result.value();

			resp->SetStatusCodeAndMessage(200, "Success");
			if (req->GetPath() == "/api/devices/v1/authentication/auth_requests") {
				resp->SetBodyReader(make_shared<io::StringReader>(expected_jwt_token));
				resp->SetHeader("Content-Length", to_string(expected_jwt_token.size()));
			}
			resp->AsyncReply([&stop_counter, &loop](error::Error err) {
				ASSERT_EQ(error::NoError, err);
				if (++stop_counter >= 2) {
					loop.Stop();
				}
			});
		});
	ASSERT_EQ(error::NoError, err);

	conf::MenderConfig config {};
	config.servers.push_back("http://127.0.0.1:" TEST_PORT);
	config.tenant_token = "dummytenanttoken";

	http::Client http_client {http::ClientConfig {}, loop};

	ipc::Server server {loop, config};
	server.Cache("NotYourJWTTokenBitch", "http://127.1.1.1:" TEST_PORT);
	err = server.Listen({"./private-key.rsa.pem"}, test_device_identity_script);
	ASSERT_EQ(err, error::NoError);

	dbus::DBusClient client {loop};
	err = client.RegisterSignalHandler<dbus::ExpectedStringPair>(
		"io.mender.Authentication1",
		"JwtTokenStateChange",
		[&http_client, expected_jwt_token, &server, &stop_counter, &loop](
			dbus::ExpectedStringPair ex_value) {
			ASSERT_TRUE(ex_value);
			EXPECT_EQ(ex_value.value().first, expected_jwt_token);
			EXPECT_EQ(ex_value.value().second, server.GetServerURL());

			auto req = make_shared<http::OutgoingRequest>();
			ASSERT_EQ(
				req->SetAddress(http::JoinUrl(ex_value.value().second, "payload-endpoint")),
				error::NoError);
			req->SetMethod(http::Method::GET);
			auto err = http_client.AsyncCall(
				req,
				[](http::ExpectedIncomingResponsePtr exp_resp) { ASSERT_TRUE(exp_resp); },
				[&stop_counter, &loop](http::ExpectedIncomingResponsePtr exp_resp) {
					ASSERT_TRUE(exp_resp);
					if (++stop_counter >= 2) {
						loop.Stop();
					}
				});
			ASSERT_EQ(err, error::NoError);
		});
	ASSERT_EQ(err, error::NoError);

	err = client.CallMethod<expected::ExpectedBool>(
		"io.mender.AuthenticationManager",
		"/io/mender/AuthenticationManager",
		"io.mender.Authentication1",
		"FetchJwtToken",
		[](expected::ExpectedBool ex_value) {
			ASSERT_TRUE(ex_value) << ex_value.error().message;
			EXPECT_TRUE(ex_value.value());
		});
	ASSERT_EQ(err, error::NoError);

	ASSERT_EQ(err, error::NoError);

	loop.Run();

	EXPECT_EQ(expected_jwt_token, server.GetJWTToken());
	EXPECT_EQ(expected_hosted_url, server.GetForwarder().GetTargetUrl());
	EXPECT_EQ(server.GetServerURL(), server.GetForwarder().GetUrl());
	EXPECT_NE(expected_hosted_url, server.GetServerURL());
}
