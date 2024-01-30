// Copyright 2024 Northern.tech AS
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

#include <mender-auth/api/auth.hpp>

#include <string>
#include <iostream>

#include <gtest/gtest.h>
#include <gmock/gmock.h>

#include <common/error.hpp>
#include <common/http.hpp>
#include <common/io.hpp>
#include <common/log.hpp>
#include <common/path.hpp>
#include <common/testing.hpp>

using namespace std;

namespace error = mender::common::error;
namespace http = mender::common::http;
namespace io = mender::common::io;
namespace mlog = mender::common::log;
namespace path = mender::common::path;
namespace mtesting = mender::common::testing;

namespace auth = mender::auth::api::auth;

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
