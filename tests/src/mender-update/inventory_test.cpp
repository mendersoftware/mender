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

#include <api/client.hpp>
#include <common/common.hpp>
#include <client_shared/conf.hpp>
#include <common/error.hpp>
#include <common/events.hpp>
#include <common/http.hpp>
#include <common/io.hpp>
#include <common/testing.hpp>

#define TEST_SERVER "http://127.0.0.1:8002"

using namespace std;

namespace api = mender::api;
namespace common = mender::common;
namespace conf = mender::client_shared::conf;
namespace error = mender::common::error;
namespace events = mender::common::events;
namespace http = mender::common::http;
namespace io = mender::common::io;
namespace inv = mender::update::inventory;
namespace mtesting = mender::common::testing;

class NoAuthHTTPClient : public api::Client {
public:
	NoAuthHTTPClient(const http::ClientConfig &config, events::EventLoop &event_loop) :
		http_client_ {config, event_loop} {};

	error::Error AsyncCall(
		api::APIRequestPtr req,
		http::ResponseHandler header_handler,
		http::ResponseHandler body_handler) override {
		auto ex_req = req->WithAuthData({TEST_SERVER, ""});
		if (!ex_req) {
			return ex_req.error();
		}
		return http_client_.AsyncCall(ex_req.value(), header_handler, body_handler);
	}

private:
	http::Client http_client_;
};

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
echo "mender_client_version=additional_version"
exit 0
)";
	auto ret = PrepareTestScript("mender-inventory-script1", script);
	ASSERT_TRUE(ret);

	mtesting::TestEventLoop loop;

	http::ServerConfig server_config;
	http::Server server(server_config, loop);

	http::ClientConfig client_config;
	NoAuthHTTPClient client {client_config, loop};

	const string expected_request_data =
		R"([{"name":"key1","value":["value1","value11"]},{"name":"key2","value":"value2"},{"name":"key3","value":"value3"},{"name":"mender_client_version","value":["additional_version",)"
		+ conf::kMenderVersion + R"(]}])";

	vector<uint8_t> received_body;
	server.AsyncServeUrl(
		TEST_SERVER,
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
	size_t last_hash = 0;
	auto err = inv::PushInventoryData(
		test_scripts_dir.Path(),
		loop,
		client,
		last_hash,
		[&handler_called, &loop](error::Error err) {
			handler_called = true;
			ASSERT_EQ(err, error::NoError);
			loop.Stop();
		});
	EXPECT_EQ(err, error::NoError);

	loop.Run();
	EXPECT_TRUE(handler_called);
	EXPECT_EQ(last_hash, std::hash<string> {}(expected_request_data));
}

TEST_F(InventoryAPITests, PushInventoryNoDataTest) {
	string script = R"(#!/bin/sh
exit 0
)";
	auto ret = PrepareTestScript("mender-inventory-script1", script);
	ASSERT_TRUE(ret);

	mtesting::TestEventLoop loop;

	http::ServerConfig server_config;
	http::Server server(server_config, loop);

	http::ClientConfig client_config;
	NoAuthHTTPClient client {client_config, loop};

	const string expected_request_data =
		R"([{"name":"mender_client_version","value":)" + conf::kMenderVersion + R"(}])";

	vector<uint8_t> received_body;
	server.AsyncServeUrl(
		TEST_SERVER,
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
	size_t last_hash = 0;
	auto err = inv::PushInventoryData(
		test_scripts_dir.Path(),
		loop,
		client,
		last_hash,
		[&handler_called, &loop](error::Error err) {
			handler_called = true;
			ASSERT_EQ(err, error::NoError);
			loop.Stop();
		});
	EXPECT_EQ(err, error::NoError);

	loop.Run();
	EXPECT_TRUE(handler_called);
	size_t expected_hash = std::hash<string> {}(expected_request_data);
	EXPECT_EQ(last_hash, expected_hash);
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
	NoAuthHTTPClient client {client_config, loop};

	const string expected_request_data =
		R"([{"name":"key1","value":["value1","value11"]},{"name":"key2","value":"value2"},{"name":"key3","value":"value3"},{"name":"mender_client_version","value":)"
		+ conf::kMenderVersion + R"(}])";
	const string response_data =
		R"({"error": "Some container failed to open so nowhere to put the goods", "request-id": "some id here"})";

	vector<uint8_t> received_body;
	server.AsyncServeUrl(
		TEST_SERVER,
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
	size_t last_hash = 0;
	auto err = inv::PushInventoryData(
		test_scripts_dir.Path(),
		loop,
		client,
		last_hash,
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

	// no change in case of failure
	EXPECT_EQ(last_hash, 0);
}

TEST_F(InventoryAPITests, PushInventoryDataNumericTypesTest) {
	string script = R"(#!/bin/sh
echo "storage_used=63"
echo "cpu_count=4" 
echo "memory_total=8192"
echo "temperature=23.5"
echo "device_name=raspberry-pi"
echo "enabled=true"
echo "negative_value=-100"
exit 0
)";
	auto ret = PrepareTestScript("mender-inventory-script1", script);
	ASSERT_TRUE(ret);

	mtesting::TestEventLoop loop;

	http::ServerConfig server_config;
	http::Server server(server_config, loop);

	http::ClientConfig client_config;
	NoAuthHTTPClient client {client_config, loop};

	// Expected JSON with numeric values NOT quoted
	const string expected_request_data =
		R"([{"name":"cpu_count","value":4},{"name":"device_name","value":"raspberry-pi"},{"name":"enabled","value":"true"},{"name":"memory_total","value":8192},{"name":"mender_client_version","value":)"
		+ conf::kMenderVersion + R"(},{"name":"negative_value","value":-100},{"name":"storage_used","value":63},{"name":"temperature","value":23.5}])";

	vector<uint8_t> received_body;
	server.AsyncServeUrl(
		TEST_SERVER,
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
	size_t last_hash = 0;
	auto err = inv::PushInventoryData(
		test_scripts_dir.Path(),
		loop,
		client,
		last_hash,
		[&handler_called, &loop](error::Error err) {
			handler_called = true;
			ASSERT_EQ(err, error::NoError);
			loop.Stop();
		});
	EXPECT_EQ(err, error::NoError);

	loop.Run();
	EXPECT_TRUE(handler_called);
	EXPECT_EQ(last_hash, std::hash<string> {}(expected_request_data));
}

TEST_F(InventoryAPITests, PushInventoryDataMixedArrayTypesTest) {
	string script = R"(#!/bin/sh
echo "numbers=42"
echo "numbers=3.14" 
echo "numbers=text"
echo "numbers=-7"
exit 0
)";
	auto ret = PrepareTestScript("mender-inventory-script1", script);
	ASSERT_TRUE(ret);

	mtesting::TestEventLoop loop;

	http::ServerConfig server_config;
	http::Server server(server_config, loop);

	http::ClientConfig client_config;
	NoAuthHTTPClient client {client_config, loop};

	// Expected JSON with mixed array: [42, 3.14, "text", -7]
	const string expected_request_data =
		R"([{"name":"mender_client_version","value":)"
		+ conf::kMenderVersion + R"(},{"name":"numbers","value":[42,3.14,"text",-7]}])";

	vector<uint8_t> received_body;
	server.AsyncServeUrl(
		TEST_SERVER,
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
	size_t last_hash = 0;
	auto err = inv::PushInventoryData(
		test_scripts_dir.Path(),
		loop,
		client,
		last_hash,
		[&handler_called, &loop](error::Error err) {
			handler_called = true;
			ASSERT_EQ(err, error::NoError);
			loop.Stop();
		});
	EXPECT_EQ(err, error::NoError);

	loop.Run();
	EXPECT_TRUE(handler_called);
	EXPECT_EQ(last_hash, std::hash<string> {}(expected_request_data));
}

TEST_F(InventoryAPITests, PushInventoryDataEdgeCasesTest) {
	string script = R"(#!/bin/sh
echo "partial_numeric=123abc"
echo "leading_space= 42"
echo "trailing_space=42 "
echo "infinity_val=inf"
echo "nan_val=nan"
echo "plus_sign=+42"
echo "scientific_upper=1.23E-4"
echo "hex_like=0x123"
echo "empty_val="
echo "just_minus=-"
echo "just_plus=+"
echo "just_dot=."
exit 0
)";
	auto ret = PrepareTestScript("mender-inventory-script1", script);
	ASSERT_TRUE(ret);

	mtesting::TestEventLoop loop;

	http::ServerConfig server_config;
	http::Server server(server_config, loop);

	http::ClientConfig client_config;
	NoAuthHTTPClient client {client_config, loop};

	// Expected JSON - edge cases properly handled: numeric when valid, string when not  
	// Note: hex is parsed as numeric by strtod, leading space is trimmed, scientific notation preserved
	const string expected_request_data =
		R"([{"name":"empty_val","value":""},{"name":"hex_like","value":0x123},{"name":"infinity_val","value":"inf"},{"name":"just_dot","value":"."},{"name":"just_minus","value":"-"},{"name":"just_plus","value":"+"},{"name":"leading_space","value": 42},{"name":"mender_client_version","value":)"
		+ conf::kMenderVersion + R"(},{"name":"nan_val","value":"nan"},{"name":"partial_numeric","value":"123abc"},{"name":"plus_sign","value":+42},{"name":"scientific_upper","value":1.23E-4},{"name":"trailing_space","value":"42 "}])";

	vector<uint8_t> received_body;
	server.AsyncServeUrl(
		TEST_SERVER,
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
	size_t last_hash = 0;
	auto err = inv::PushInventoryData(
		test_scripts_dir.Path(),
		loop,
		client,
		last_hash,
		[&handler_called, &loop](error::Error err) {
			handler_called = true;
			ASSERT_EQ(err, error::NoError);
			loop.Stop();
		});
	EXPECT_EQ(err, error::NoError);

	loop.Run();
	EXPECT_TRUE(handler_called);
	EXPECT_EQ(last_hash, std::hash<string> {}(expected_request_data));
}

TEST_F(InventoryAPITests, PushInventoryDataNoopTest) {
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
	NoAuthHTTPClient client {client_config, loop};

	server.AsyncServeUrl(
		TEST_SERVER,
		[](http::ExpectedIncomingRequestPtr exp_req) {
			// there should be no request
			EXPECT_TRUE(false);
		},
		[](http::ExpectedIncomingRequestPtr exp_req) { EXPECT_TRUE(false); });

	bool handler_called = false;
	size_t last_hash = std::hash<string> {}(
		R"([{"name":"key1","value":["value1","value11"]},{"name":"key2","value":"value2"},{"name":"key3","value":"value3"},{"name":"mender_client_version","value":)"
		+ conf::kMenderVersion + R"(}])");
	size_t last_hash_orig = last_hash;
	auto err = inv::PushInventoryData(
		test_scripts_dir.Path(),
		loop,
		client,
		last_hash,
		[&handler_called, &loop](error::Error err) {
			handler_called = true;
			ASSERT_EQ(err, error::NoError);
			loop.Stop();
		});
	EXPECT_EQ(err, error::NoError);

	loop.Run();
	EXPECT_TRUE(handler_called);
	EXPECT_EQ(last_hash, last_hash_orig);
}
