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

#include <mender-update/inventory.hpp>

#include <string>
#include <vector>

#include <gmock/gmock.h>
#include <gtest/gtest.h>

#include <common/common.hpp>
#include <common/error.hpp>
#include <common/http.hpp>
#include <common/io.hpp>
#include <common/testing.hpp>

#define TEST_PORT "8002"

using namespace std;

namespace common = mender::common;
namespace error = mender::common::error;
namespace http = mender::http;
namespace io = mender::common::io;
namespace inv = mender::update::inventory;
namespace mtesting = mender::common::testing;

class InventoryAPITests : public ::testing::Test {
protected:
	mtesting::TemporaryDirectory test_scripts_dir;

	bool PrepareTestScript(const string &script_name, const string &script) {
		string test_script_path = test_scripts_dir.Path() + "/" + script_name;
		ofstream os(test_script_path);
		os << script;
		os.close();

		int ret = chmod(test_script_path.c_str(), S_IRUSR | S_IWUSR | S_IXUSR);
		return ret == 0;
	}
};

TEST_F(InventoryAPITests, PushInventoryDataTest) {
	string script = R"(#!/bin/sh
echo "key1=value1"
echo "key2=value2"
echo "key3=value3"
echo "key1=value11"
exit 0
)";
	auto ret = PrepareTestScript("mender-inventory-script1", script);
	ASSERT_TRUE(ret);

	mtesting::TestEventLoop loop;

	http::ServerConfig server_config;
	http::Server server(server_config, loop);

	http::ClientConfig client_config;
	http::Client client {client_config, loop};

	const string expected_request_data =
		R"([{"name":"key3","value":"value3"},{"name":"key2","value":"value2"},{"name":"key1","value":["value1","value11"]}])";

	vector<uint8_t> received_body;
	server.AsyncServeUrl(
		"http://127.0.0.1:" TEST_PORT,
		[&received_body, &expected_request_data](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			auto req = exp_req.value();

			auto content_length = req->GetHeader("Content-Length");
			ASSERT_TRUE(content_length);
			EXPECT_EQ(content_length.value(), to_string(expected_request_data.size()));
			auto ex_len = common::StringToLongLong(content_length.value());
			ASSERT_TRUE(ex_len);

			auto body_writer = make_shared<io::ByteWriter>(received_body);
			received_body.resize(ex_len.value());
			req->SetBodyWriter(body_writer);
		},
		[&received_body, &expected_request_data](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();

			auto req = exp_req.value();
			EXPECT_EQ(req->GetPath(), "/api/devices/v1/inventory/device/attributes");
			EXPECT_EQ(req->GetMethod(), http::Method::PUT);
			EXPECT_EQ(common::StringFromByteVector(received_body), expected_request_data);

			auto result = req->MakeResponse();
			ASSERT_TRUE(result);
			auto resp = result.value();

			resp->SetHeader("Content-Length", "0");
			resp->SetStatusCodeAndMessage(200, "Success");
			resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
		});

	bool handler_called = false;
	auto err = inv::PushInventoryData(
		test_scripts_dir.Path(),
		"http://127.0.0.1:" TEST_PORT,
		client,
		[&handler_called, &loop](error::Error err) {
			handler_called = true;
			ASSERT_EQ(err, error::NoError);
			loop.Stop();
		});
	EXPECT_EQ(err, error::NoError);

	loop.Run();
	EXPECT_TRUE(handler_called);
}

TEST_F(InventoryAPITests, PushInventoryDataFailTest) {
	string script = R"(#!/bin/sh
echo "key1=value1"
echo "key2=value2"
echo "key3=value3"
echo "key1=value11"
exit 0
)";
	auto ret = PrepareTestScript("mender-inventory-script1", script);
	ASSERT_TRUE(ret);

	mtesting::TestEventLoop loop;

	http::ServerConfig server_config;
	http::Server server(server_config, loop);

	http::ClientConfig client_config;
	http::Client client {client_config, loop};

	const string expected_request_data =
		R"([{"name":"key3","value":"value3"},{"name":"key2","value":"value2"},{"name":"key1","value":["value1","value11"]}])";
	const string response_data =
		R"({"error": "Some container failed to open so nowhere to put the goods", "request-id": "some id here"})";

	vector<uint8_t> received_body;
	server.AsyncServeUrl(
		"http://127.0.0.1:" TEST_PORT,
		[&received_body, &expected_request_data](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			auto req = exp_req.value();

			auto content_length = req->GetHeader("Content-Length");
			ASSERT_TRUE(content_length);
			EXPECT_EQ(content_length.value(), to_string(expected_request_data.size()));
			auto ex_len = common::StringToLongLong(content_length.value());
			ASSERT_TRUE(ex_len);

			auto body_writer = make_shared<io::ByteWriter>(received_body);
			received_body.resize(ex_len.value());
			req->SetBodyWriter(body_writer);
		},
		[&received_body, &expected_request_data, &response_data](
			http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();

			auto req = exp_req.value();
			EXPECT_EQ(req->GetPath(), "/api/devices/v1/inventory/device/attributes");
			EXPECT_EQ(req->GetMethod(), http::Method::PUT);
			EXPECT_EQ(common::StringFromByteVector(received_body), expected_request_data);

			auto result = req->MakeResponse();
			ASSERT_TRUE(result);
			auto resp = result.value();

			resp->SetHeader("Content-Length", to_string(response_data.size()));
			resp->SetStatusCodeAndMessage(500, "Internal server error");
			resp->SetBodyReader(make_shared<io::StringReader>(response_data));
			resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
		});

	bool handler_called = false;
	auto err = inv::PushInventoryData(
		test_scripts_dir.Path(),
		"http://127.0.0.1:" TEST_PORT,
		client,
		[&handler_called, &loop](error::Error err) {
			handler_called = true;
			ASSERT_NE(err, error::NoError);

			EXPECT_THAT(err.message, testing::HasSubstr("Got unexpected response"));
			EXPECT_THAT(err.message, testing::HasSubstr("500"));
			EXPECT_THAT(err.message, testing::HasSubstr("container failed to open"));
			loop.Stop();
		});
	EXPECT_EQ(err, error::NoError);

	loop.Run();
	EXPECT_TRUE(handler_called);
}
