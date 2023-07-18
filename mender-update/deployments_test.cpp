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

#include <chrono>
#include <thread>

#include <gmock/gmock.h>
#include <gtest/gtest.h>

#include <common/common.hpp>
#include <common/conf.hpp>
#include <common/events.hpp>
#include <common/http.hpp>
#include <common/testing.hpp>

#include <mender-update/deployments.hpp>

using namespace std;

namespace common = mender::common;
namespace conf = mender::common::conf;
namespace context = mender::update::context;
namespace error = mender::common::error;
namespace events = mender::common::events;
namespace expected = mender::common::expected;
namespace http = mender::http;
namespace io = mender::common::io;
namespace optional = mender::common::optional;
namespace mtesting = mender::common::testing;
namespace deps = mender::update::deployments;

#define TEST_PORT "8001"

using TestEventLoop = mtesting::TestEventLoop;

class DeploymentsTests : public testing::Test {
protected:
	mtesting::TemporaryDirectory test_state_dir;
};

TEST_F(DeploymentsTests, TestV2APIWithNextDeployment) {
	conf::MenderConfig cfg;
	cfg.data_store_dir = test_state_dir.Path();

	context::MenderContext ctx(cfg);
	auto err = ctx.Initialize();
	ASSERT_EQ(err, error::NoError);

	auto &db = ctx.GetMenderStoreDB();
	err = db.Write("artifact-name", common::ByteVectorFromString("artifact-name value"));
	ASSERT_EQ(err, error::NoError);
	err = db.Write("artifact-group", common::ByteVectorFromString("artifact-group value"));
	ASSERT_EQ(err, error::NoError);

	const string input_provides_data_str = R"({
  "something_else": "something_else value"
})";
	err = db.Write("artifact-provides", common::ByteVectorFromString(input_provides_data_str));
	ASSERT_EQ(err, error::NoError);

	ofstream os(cfg.data_store_dir + "/device_type");
	ASSERT_TRUE(os);
	os << "device_type=Some device type" << endl;
	os.close();

	const string expected_request_data =
		R"({"update_control_map":false,"device_provides":{"device_type":"Some device type","something_else":"something_else value","artifact_group":"artifact-group value","artifact_name":"artifact-name value"}})";
	const string response_data = R"({
  "some": "data here"
})";

	TestEventLoop loop;

	http::ServerConfig server_config;
	http::Server server(server_config, loop);

	http::ClientConfig client_config;
	http::Client client {client_config, loop};

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
			EXPECT_EQ(req->GetPath(), "/api/devices/v2/deployments/device/deployments/next");
			EXPECT_EQ(req->GetMethod(), http::Method::POST);
			EXPECT_EQ(common::StringFromByteVector(received_body), expected_request_data);

			auto result = req->MakeResponse();
			ASSERT_TRUE(result);
			auto resp = result.value();

			resp->SetHeader("Content-Length", to_string(response_data.size()));
			resp->SetBodyReader(make_shared<io::StringReader>(response_data));
			resp->SetStatusCodeAndMessage(200, "Success");
			resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
		});

	bool handler_called = false;
	err = deps::CheckNewDeployments(
		ctx,
		"http://127.0.0.1:" TEST_PORT,
		client,
		[&response_data, &handler_called, &loop](deps::CheckUpdatesAPIResponse resp) {
			handler_called = true;
			ASSERT_TRUE(resp);

			auto o_js = resp.value();
			ASSERT_NE(o_js, optional::nullopt);

			EXPECT_EQ(o_js->Dump(), response_data);
			loop.Stop();
		});
	EXPECT_EQ(err, error::NoError);

	loop.Run();
	EXPECT_TRUE(handler_called);
}

TEST_F(DeploymentsTests, TestV2APIWithNoNextDeployment) {
	conf::MenderConfig cfg;
	cfg.data_store_dir = test_state_dir.Path();

	context::MenderContext ctx(cfg);
	auto err = ctx.Initialize();
	ASSERT_EQ(err, error::NoError);

	auto &db = ctx.GetMenderStoreDB();
	err = db.Write("artifact-name", common::ByteVectorFromString("artifact-name value"));
	ASSERT_EQ(err, error::NoError);

	ofstream os(cfg.data_store_dir + "/device_type");
	ASSERT_TRUE(os);
	os << "device_type=Some device type" << endl;
	os.close();

	const string expected_request_data =
		R"({"update_control_map":false,"device_provides":{"device_type":"Some device type","artifact_name":"artifact-name value"}})";
	const string response_data = "";

	TestEventLoop loop;

	http::ServerConfig server_config;
	http::Server server(server_config, loop);

	http::ClientConfig client_config;
	http::Client client {client_config, loop};

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
			EXPECT_EQ(req->GetPath(), "/api/devices/v2/deployments/device/deployments/next");
			EXPECT_EQ(req->GetMethod(), http::Method::POST);
			EXPECT_EQ(common::StringFromByteVector(received_body), expected_request_data);

			auto result = req->MakeResponse();
			ASSERT_TRUE(result);
			auto resp = result.value();

			resp->SetHeader("Content-Length", to_string(response_data.size()));
			resp->SetBodyReader(make_shared<io::StringReader>(response_data));
			resp->SetStatusCodeAndMessage(http::StatusNoContent, "No content");
			resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
		});

	bool handler_called = false;
	err = deps::CheckNewDeployments(
		ctx,
		"http://127.0.0.1:" TEST_PORT,
		client,
		[&handler_called, &loop](deps::CheckUpdatesAPIResponse resp) {
			handler_called = true;
			ASSERT_TRUE(resp);

			auto o_js = resp.value();
			EXPECT_EQ(o_js, optional::nullopt);

			loop.Stop();
		});
	EXPECT_EQ(err, error::NoError);

	loop.Run();
	EXPECT_TRUE(handler_called);
}

TEST_F(DeploymentsTests, TestV2APIError) {
	conf::MenderConfig cfg;
	cfg.data_store_dir = test_state_dir.Path();

	context::MenderContext ctx(cfg);
	auto err = ctx.Initialize();
	ASSERT_EQ(err, error::NoError);

	auto &db = ctx.GetMenderStoreDB();
	err = db.Write("artifact-name", common::ByteVectorFromString("artifact-name value"));
	ASSERT_EQ(err, error::NoError);

	ofstream os(cfg.data_store_dir + "/device_type");
	ASSERT_TRUE(os);
	os << "device_type=Some device type" << endl;
	os.close();

	const string expected_request_data =
		R"({"update_control_map":false,"device_provides":{"device_type":"Some device type","artifact_name":"artifact-name value"}})";
	const string response_data = R"({"error": "JWT token expired", "response-id": "some id here"})";

	TestEventLoop loop;

	http::ServerConfig server_config;
	http::Server server(server_config, loop);

	http::ClientConfig client_config;
	http::Client client {client_config, loop};

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
			EXPECT_EQ(req->GetPath(), "/api/devices/v2/deployments/device/deployments/next");
			EXPECT_EQ(req->GetMethod(), http::Method::POST);
			EXPECT_EQ(common::StringFromByteVector(received_body), expected_request_data);

			auto result = req->MakeResponse();
			ASSERT_TRUE(result);
			auto resp = result.value();

			resp->SetHeader("Content-Length", to_string(response_data.size()));
			resp->SetBodyReader(make_shared<io::StringReader>(response_data));
			resp->SetStatusCodeAndMessage(403, "Forbidden");
			resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
		});

	bool handler_called = false;
	err = deps::CheckNewDeployments(
		ctx,
		"http://127.0.0.1:" TEST_PORT,
		client,
		[&handler_called, &loop](deps::CheckUpdatesAPIResponse resp) {
			handler_called = true;
			ASSERT_FALSE(resp);

			EXPECT_THAT(resp.error().message, testing::HasSubstr("Got unexpected response"));
			EXPECT_THAT(resp.error().message, testing::HasSubstr("403"));
			EXPECT_THAT(resp.error().message, testing::HasSubstr("JWT token expired"));

			loop.Stop();
		});
	EXPECT_EQ(err, error::NoError);

	loop.Run();
	EXPECT_TRUE(handler_called);
}

TEST_F(DeploymentsTests, TestV1APIFallbackWithNextDeployment) {
	conf::MenderConfig cfg;
	cfg.data_store_dir = test_state_dir.Path();

	context::MenderContext ctx(cfg);
	auto err = ctx.Initialize();
	ASSERT_EQ(err, error::NoError);

	auto &db = ctx.GetMenderStoreDB();
	err = db.Write("artifact-name", common::ByteVectorFromString("artifact-name value"));
	ASSERT_EQ(err, error::NoError);

	ofstream os(cfg.data_store_dir + "/device_type");
	ASSERT_TRUE(os);
	os << "device_type=Some device type" << endl;
	os.close();

	const string expected_request_data =
		R"({"update_control_map":false,"device_provides":{"device_type":"Some device type","artifact_name":"artifact-name value"}})";
	const string response_data = R"({
  "some": "data here"
})";

	TestEventLoop loop;

	http::ServerConfig server_config;
	http::Server server(server_config, loop);

	http::ClientConfig client_config;
	http::Client client {client_config, loop};

	vector<uint8_t> received_body;
	bool v2_requested = false;
	server.AsyncServeUrl(
		"http://127.0.0.1:" TEST_PORT,
		[&received_body, &expected_request_data, &v2_requested](
			http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			auto req = exp_req.value();

			if (!v2_requested) {
				auto content_length = req->GetHeader("Content-Length");
				ASSERT_TRUE(content_length);
				EXPECT_EQ(content_length.value(), to_string(expected_request_data.size()));
				auto ex_len = common::StringToLongLong(content_length.value());
				ASSERT_TRUE(ex_len);

				auto body_writer = make_shared<io::ByteWriter>(received_body);
				received_body.resize(ex_len.value());
				req->SetBodyWriter(body_writer);
			} else {
				auto content_length = req->GetHeader("Content-Length");
				EXPECT_FALSE(content_length);
				received_body.clear();
			}
		},
		[&received_body, &expected_request_data, &response_data, &v2_requested](
			http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();

			if (!v2_requested) {
				auto req = exp_req.value();
				EXPECT_EQ(req->GetPath(), "/api/devices/v2/deployments/device/deployments/next");
				EXPECT_EQ(req->GetMethod(), http::Method::POST);
				EXPECT_EQ(common::StringFromByteVector(received_body), expected_request_data);

				auto result = req->MakeResponse();
				ASSERT_TRUE(result);
				auto resp = result.value();

				resp->SetHeader("Content-Length", "0");
				resp->SetBodyReader(make_shared<io::StringReader>(""));
				resp->SetStatusCodeAndMessage(404, "Not found");
				resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
				v2_requested = true;
			} else {
				auto req = exp_req.value();
				EXPECT_EQ(
					req->GetPath(),
					"/api/devices/v1/deployments/device/deployments/next?artifact_name=artifact-name%20value&device_type=Some%20device%20type");
				EXPECT_EQ(req->GetMethod(), http::Method::GET);

				auto result = req->MakeResponse();
				ASSERT_TRUE(result);
				auto resp = result.value();

				resp->SetHeader("Content-Length", to_string(response_data.size()));
				resp->SetBodyReader(make_shared<io::StringReader>(response_data));
				resp->SetStatusCodeAndMessage(http::StatusOK, "Success");
				resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
			}
		});

	bool handler_called = false;
	err = deps::CheckNewDeployments(
		ctx,
		"http://127.0.0.1:" TEST_PORT,
		client,
		[&response_data, &handler_called, &loop](deps::CheckUpdatesAPIResponse resp) {
			handler_called = true;
			ASSERT_TRUE(resp);

			auto o_js = resp.value();
			ASSERT_NE(o_js, optional::nullopt);

			EXPECT_EQ(o_js->Dump(), response_data);
			loop.Stop();
		});
	EXPECT_EQ(err, error::NoError);

	loop.Run();
	EXPECT_TRUE(handler_called);
}

TEST_F(DeploymentsTests, TestV1APIFallbackWithNoNextDeployment) {
	conf::MenderConfig cfg;
	cfg.data_store_dir = test_state_dir.Path();

	context::MenderContext ctx(cfg);
	auto err = ctx.Initialize();
	ASSERT_EQ(err, error::NoError);

	auto &db = ctx.GetMenderStoreDB();
	err = db.Write("artifact-name", common::ByteVectorFromString("artifact-name value"));
	ASSERT_EQ(err, error::NoError);

	ofstream os(cfg.data_store_dir + "/device_type");
	ASSERT_TRUE(os);
	os << "device_type=Some device type" << endl;
	os.close();

	const string expected_request_data =
		R"({"update_control_map":false,"device_provides":{"device_type":"Some device type","artifact_name":"artifact-name value"}})";
	const string response_data = "";

	TestEventLoop loop(chrono::seconds(3600));

	http::ServerConfig server_config;
	http::Server server(server_config, loop);

	http::ClientConfig client_config;
	http::Client client {client_config, loop};

	vector<uint8_t> received_body;
	bool v2_requested = false;
	server.AsyncServeUrl(
		"http://127.0.0.1:" TEST_PORT,
		[&received_body, &expected_request_data, &v2_requested](
			http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			auto req = exp_req.value();

			if (!v2_requested) {
				auto content_length = req->GetHeader("Content-Length");
				ASSERT_TRUE(content_length);
				EXPECT_EQ(content_length.value(), to_string(expected_request_data.size()));
				auto ex_len = common::StringToLongLong(content_length.value());
				ASSERT_TRUE(ex_len);

				auto body_writer = make_shared<io::ByteWriter>(received_body);
				received_body.resize(ex_len.value());
				req->SetBodyWriter(body_writer);
			} else {
				auto content_length = req->GetHeader("Content-Length");
				EXPECT_FALSE(content_length);
				received_body.clear();
			}
		},
		[&received_body, &expected_request_data, &response_data, &v2_requested](
			http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();

			if (!v2_requested) {
				auto req = exp_req.value();
				EXPECT_EQ(req->GetPath(), "/api/devices/v2/deployments/device/deployments/next");
				EXPECT_EQ(req->GetMethod(), http::Method::POST);
				EXPECT_EQ(common::StringFromByteVector(received_body), expected_request_data);

				auto result = req->MakeResponse();
				ASSERT_TRUE(result);
				auto resp = result.value();

				resp->SetHeader("Content-Length", "0");
				resp->SetBodyReader(make_shared<io::StringReader>(""));
				resp->SetStatusCodeAndMessage(404, "Not found");
				resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
				v2_requested = true;
			} else {
				auto req = exp_req.value();
				EXPECT_EQ(
					req->GetPath(),
					"/api/devices/v1/deployments/device/deployments/next?artifact_name=artifact-name%20value&device_type=Some%20device%20type");
				EXPECT_EQ(req->GetMethod(), http::Method::GET);

				auto result = req->MakeResponse();
				ASSERT_TRUE(result);
				auto resp = result.value();

				resp->SetHeader("Content-Length", to_string(response_data.size()));
				resp->SetBodyReader(make_shared<io::StringReader>(response_data));
				resp->SetStatusCodeAndMessage(http::StatusNoContent, "No content");
				resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
			}
		});

	bool handler_called = false;
	err = deps::CheckNewDeployments(
		ctx,
		"http://127.0.0.1:" TEST_PORT,
		client,
		[&handler_called, &loop](deps::CheckUpdatesAPIResponse resp) {
			handler_called = true;
			ASSERT_TRUE(resp);

			auto o_js = resp.value();
			EXPECT_EQ(o_js, optional::nullopt);

			loop.Stop();
		});
	EXPECT_EQ(err, error::NoError);

	loop.Run();
	EXPECT_TRUE(handler_called);
}

TEST_F(DeploymentsTests, TestV1APIFallbackWithError) {
	conf::MenderConfig cfg;
	cfg.data_store_dir = test_state_dir.Path();

	context::MenderContext ctx(cfg);
	auto err = ctx.Initialize();
	ASSERT_EQ(err, error::NoError);

	auto &db = ctx.GetMenderStoreDB();
	err = db.Write("artifact-name", common::ByteVectorFromString("artifact-name value"));
	ASSERT_EQ(err, error::NoError);

	ofstream os(cfg.data_store_dir + "/device_type");
	ASSERT_TRUE(os);
	os << "device_type=Some device type" << endl;
	os.close();

	const string expected_request_data =
		R"({"update_control_map":false,"device_provides":{"device_type":"Some device type","artifact_name":"artifact-name value"}})";
	const string response_data = "";

	TestEventLoop loop(chrono::seconds(3600));

	http::ServerConfig server_config;
	http::Server server(server_config, loop);

	http::ClientConfig client_config;
	http::Client client {client_config, loop};

	vector<uint8_t> received_body;
	bool v2_requested = false;
	server.AsyncServeUrl(
		"http://127.0.0.1:" TEST_PORT,
		[&received_body, &expected_request_data, &v2_requested](
			http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			auto req = exp_req.value();

			if (!v2_requested) {
				auto content_length = req->GetHeader("Content-Length");
				ASSERT_TRUE(content_length);
				EXPECT_EQ(content_length.value(), to_string(expected_request_data.size()));
				auto ex_len = common::StringToLongLong(content_length.value());
				ASSERT_TRUE(ex_len);

				auto body_writer = make_shared<io::ByteWriter>(received_body);
				received_body.resize(ex_len.value());
				req->SetBodyWriter(body_writer);
			} else {
				auto content_length = req->GetHeader("Content-Length");
				EXPECT_FALSE(content_length);
				received_body.clear();
			}
		},
		[&received_body, &expected_request_data, &response_data, &v2_requested](
			http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();

			if (!v2_requested) {
				auto req = exp_req.value();
				EXPECT_EQ(req->GetPath(), "/api/devices/v2/deployments/device/deployments/next");
				EXPECT_EQ(req->GetMethod(), http::Method::POST);
				EXPECT_EQ(common::StringFromByteVector(received_body), expected_request_data);

				auto result = req->MakeResponse();
				ASSERT_TRUE(result);
				auto resp = result.value();

				resp->SetHeader("Content-Length", "0");
				resp->SetBodyReader(make_shared<io::StringReader>(""));
				resp->SetStatusCodeAndMessage(404, "Not found");
				resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
				v2_requested = true;
			} else {
				auto req = exp_req.value();
				EXPECT_EQ(
					req->GetPath(),
					"/api/devices/v1/deployments/device/deployments/next?artifact_name=artifact-name%20value&device_type=Some%20device%20type");
				EXPECT_EQ(req->GetMethod(), http::Method::GET);

				auto result = req->MakeResponse();
				ASSERT_TRUE(result);
				auto resp = result.value();

				resp->SetHeader("Content-Length", to_string(response_data.size()));
				resp->SetBodyReader(make_shared<io::StringReader>(response_data));
				resp->SetStatusCodeAndMessage(403, "Forbidden");
				resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
			}
		});

	bool handler_called = false;
	err = deps::CheckNewDeployments(
		ctx,
		"http://127.0.0.1:" TEST_PORT,
		client,
		[&handler_called, &loop](deps::CheckUpdatesAPIResponse resp) {
			handler_called = true;

			ASSERT_FALSE(resp);

			EXPECT_THAT(resp.error().message, testing::HasSubstr("Got unexpected response"));
			EXPECT_THAT(resp.error().message, testing::HasSubstr("403"));
			EXPECT_THAT(resp.error().message, testing::HasSubstr("Forbidden"));

			loop.Stop();
		});
	EXPECT_EQ(err, error::NoError);

	loop.Run();
	EXPECT_TRUE(handler_called);
}

TEST_F(DeploymentsTests, PushStatusTest) {
	TestEventLoop loop;

	http::ServerConfig server_config;
	http::Server server(server_config, loop);

	http::ClientConfig client_config;
	http::Client client {client_config, loop};

	auto status {deps::DeploymentStatus::Rebooting};
	string deployment_id = "2";
	string substatus = "Rebooting now";
	string expected_request_data = R"({"status":"rebooting","substate":")" + substatus + "\"}";

	const string response_data = "";

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
		[&received_body, &expected_request_data, &response_data, deployment_id](
			http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();

			auto req = exp_req.value();
			EXPECT_EQ(
				req->GetPath(),
				"/api/devices/v1/deployments/device/deployments/" + deployment_id + "/status");
			EXPECT_EQ(req->GetMethod(), http::Method::PUT);
			EXPECT_EQ(common::StringFromByteVector(received_body), expected_request_data);

			auto result = req->MakeResponse();
			ASSERT_TRUE(result);
			auto resp = result.value();

			resp->SetHeader("Content-Length", to_string(response_data.size()));
			resp->SetBodyReader(make_shared<io::StringReader>(response_data));
			resp->SetStatusCodeAndMessage(200, "Success");
			resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
		});

	bool handler_called = false;
	auto err = deps::PushStatus(
		deployment_id,
		status,
		substatus,
		"http://127.0.0.1:" TEST_PORT,
		client,
		[&handler_called, &loop](deps::StatusAPIResponse resp) {
			handler_called = true;
			EXPECT_EQ(resp, error::NoError);
			loop.Stop();
		});
	EXPECT_EQ(err, error::NoError);

	loop.Run();
	EXPECT_TRUE(handler_called);
}

TEST_F(DeploymentsTests, PushStatusNoSubstatusTest) {
	TestEventLoop loop;

	http::ServerConfig server_config;
	http::Server server(server_config, loop);

	http::ClientConfig client_config;
	http::Client client {client_config, loop};

	auto status {deps::DeploymentStatus::AlreadyInstalled};
	string deployment_id = "2";
	string substatus = "";
	string expected_request_data = R"({"status":"already-installed"})";

	const string response_data = "";

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
		[&received_body, &expected_request_data, &response_data, deployment_id](
			http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();

			auto req = exp_req.value();
			EXPECT_EQ(
				req->GetPath(),
				"/api/devices/v1/deployments/device/deployments/" + deployment_id + "/status");
			EXPECT_EQ(req->GetMethod(), http::Method::PUT);
			EXPECT_EQ(common::StringFromByteVector(received_body), expected_request_data);

			auto result = req->MakeResponse();
			ASSERT_TRUE(result);
			auto resp = result.value();

			resp->SetHeader("Content-Length", to_string(response_data.size()));
			resp->SetBodyReader(make_shared<io::StringReader>(response_data));
			resp->SetStatusCodeAndMessage(200, "Success");
			resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
		});

	bool handler_called = false;
	auto err = deps::PushStatus(
		deployment_id,
		status,
		substatus,
		"http://127.0.0.1:" TEST_PORT,
		client,
		[&handler_called, &loop](deps::StatusAPIResponse resp) {
			handler_called = true;
			EXPECT_EQ(resp, error::NoError);
			loop.Stop();
		});
	EXPECT_EQ(err, error::NoError);

	loop.Run();
	EXPECT_TRUE(handler_called);
}

TEST_F(DeploymentsTests, PushStatusFailureTest) {
	TestEventLoop loop;

	http::ServerConfig server_config;
	http::Server server(server_config, loop);

	http::ClientConfig client_config;
	http::Client client {client_config, loop};

	auto status {deps::DeploymentStatus::Installing};
	string deployment_id = "2";
	string substatus = "Installing now";
	string expected_request_data = R"({"status":"installing","substate":")" + substatus + "\"}";

	const string response_data = R"({"error": "Access denied", "response-id": "some id here"})";

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
		[&received_body, &expected_request_data, &response_data, deployment_id](
			http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();

			auto req = exp_req.value();
			EXPECT_EQ(
				req->GetPath(),
				"/api/devices/v1/deployments/device/deployments/" + deployment_id + "/status");
			EXPECT_EQ(req->GetMethod(), http::Method::PUT);
			EXPECT_EQ(common::StringFromByteVector(received_body), expected_request_data);

			auto result = req->MakeResponse();
			ASSERT_TRUE(result);
			auto resp = result.value();

			resp->SetHeader("Content-Length", to_string(response_data.size()));
			resp->SetBodyReader(make_shared<io::StringReader>(response_data));
			resp->SetStatusCodeAndMessage(403, "Forbidden");
			resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
		});

	bool handler_called = false;
	auto err = deps::PushStatus(
		deployment_id,
		status,
		substatus,
		"http://127.0.0.1:" TEST_PORT,
		client,
		[&handler_called, &loop](deps::StatusAPIResponse resp) {
			handler_called = true;
			EXPECT_NE(resp, error::NoError);
			EXPECT_THAT(resp.message, testing::HasSubstr("Got unexpected response"));
			EXPECT_THAT(resp.message, testing::HasSubstr("403"));
			EXPECT_THAT(resp.message, testing::HasSubstr("Access denied"));
			loop.Stop();
		});
	EXPECT_EQ(err, error::NoError);

	loop.Run();
	EXPECT_TRUE(handler_called);
}
