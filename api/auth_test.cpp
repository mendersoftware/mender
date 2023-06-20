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

#include <gmock/gmock.h>
#include <gtest/gtest.h>

#include <common/http.hpp>
#include <common/io.hpp>
#include <common/path.hpp>
#include <common/testing.hpp>

using namespace std;

namespace auth = mender::api::auth;
namespace error = mender::common::error;
namespace http = mender::http;
namespace io = mender::common::io;
namespace path = mender::common::path;
namespace mtesting = mender::common::testing;

using TestEventLoop = mender::common::testing::TestEventLoop;

const string TEST_PORT = "8088";

class AuthClientTests : public testing::Test {
protected:
	mtesting::TemporaryDirectory tmpdir;
	const string test_device_identity_script = path::Join(tmpdir.Path(), "/mender-device-identity");

	void SetUp() override {
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

TEST_F(AuthClientTests, AuthDaemonSuccessTest) {
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

	auth::APIResponseHandler handle_jwt_token_callback = [&loop,
														  JWT_TOKEN](auth::APIResponse resp) {
		ASSERT_TRUE(resp);
		EXPECT_EQ(resp.value(), JWT_TOKEN);
		loop.Stop();
	};


	string server_certificate_path {};
	http::ClientConfig client_config = http::ClientConfig(server_certificate_path);
	http::Client client {client_config, loop};

	auto err = auth::GetJWTToken(
		client,
		server_url,
		private_key_path,
		test_device_identity_script,
		loop,
		handle_jwt_token_callback);

	loop.Run();

	ASSERT_EQ(err, error::NoError) << "Unexpected error: " << err.message;
}
