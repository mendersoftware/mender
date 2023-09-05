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

#include <common/conf.hpp>
#include <common/http.hpp>
#include <common/io.hpp>
#include <common/path.hpp>
#include <common/testing.hpp>

#define TEST_PORT "8001"
#define TEST_PORT_2 "8002"

using namespace std;

namespace conf = mender::common::conf;
namespace error = mender::common::error;
namespace http = mender::http;
namespace io = mender::common::io;
namespace path = mender::common::path;
namespace mtesting = mender::common::testing;

namespace ipc = mender::auth::ipc;

using TestEventLoop = mender::common::testing::TestEventLoop;

class ListenClientTests : public testing::Test {
protected:
	mtesting::TemporaryDirectory tmpdir {};
	string test_device_identity_script {path::Join(tmpdir.Path(), "mender-device-identity")};

	void SetUp() override {
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
};

TEST_F(ListenClientTests, TestListenGetJWTToken) {
	TestEventLoop loop;

	// Set up the test client (Emulating mender-update)
	http::ClientConfig test_client_config {};
	http::Client update_client(test_client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	const string url {"http://127.0.0.1:" TEST_PORT "/getjwttoken"};
	req->SetAddress(url);
	update_client.AsyncCall(
		req,
		[&loop](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(exp_resp) << exp_resp.error().message;
			EXPECT_EQ(exp_resp.value()->GetHeader("X-MEN-JWTTOKEN").value(), "foobar");
			EXPECT_EQ(
				exp_resp.value()->GetHeader("X-MEN-SERVERURL").value(), "http://127.0.0.1:8001");
			loop.Stop();
		},
		[](http::ExpectedIncomingResponsePtr exp_resp) {});

	conf::MenderConfig config {};

	ipc::type::webserver::Caching server {loop, config};
	server.Cache("foobar", "http://127.0.0.1:" TEST_PORT);

	const string server_url {"http://127.0.0.1:" TEST_PORT};

	auto err = server.Listen(server_url, "./private-key.rsa.pem", test_device_identity_script);
	ASSERT_EQ(err, error::NoError);

	loop.Run();
}

TEST_F(ListenClientTests, TestListenFetchJWTToken) {
	TestEventLoop loop;

	string expected_jwt_token {"foobarbazbatz"};
	string expected_server_url {"http://127.0.0.1:" TEST_PORT_2};

	// Set up the test server (Emulating hosted mender)
	http::ServerConfig test_server_config {};
	http::Server http_server(test_server_config, loop);
	auto err = http_server.AsyncServeUrl(
		"http://127.0.0.1:" TEST_PORT_2,
		[](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();

			EXPECT_EQ(exp_req.value()->GetPath(), "/api/devices/v1/authentication/auth_requests");
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
	config.server_url = "http://127.0.0.1:" TEST_PORT_2;
	config.tenant_token = "dummytenanttoken";

	ipc::type::webserver::Caching server {loop, config};
	server.Cache("NotYourJWTTokenBitch", "http://127.0.0.1:" TEST_PORT_2);

	const string server_url {"http://127.0.0.1:" TEST_PORT};

	err = server.Listen(server_url, "./private-key.rsa.pem", test_device_identity_script);
	ASSERT_EQ(err, error::NoError);

	// Set up the test client (Emulating mender-update)
	http::ClientConfig test_client_config {};
	http::Client update_client(test_client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	const string url {"http://127.0.0.1:" TEST_PORT "/fetchjwttoken"};
	req->SetAddress(url);
	update_client.AsyncCall(
		req,
		[](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(exp_resp) << exp_resp.error().message;
		},
		[](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(exp_resp) << exp_resp.error().message;
			ASSERT_EQ(exp_resp.value()->GetStatusCode(), 200);
		});

	auto t1 = std::thread([&loop]() {
		std::this_thread::sleep_for(std::chrono::seconds {1});
		loop.Stop();
	});

	loop.Run();

	t1.join();

	EXPECT_EQ(expected_jwt_token, server.GetJWTToken());
	EXPECT_EQ(expected_server_url, server.GetServerURL());
}
