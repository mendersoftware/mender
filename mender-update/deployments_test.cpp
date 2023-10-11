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
#include <fstream>
#include <thread>

#include <gmock/gmock.h>
#include <gtest/gtest.h>

#include <common/common.hpp>
#include <common/conf.hpp>
#include <common/events.hpp>
#include <common/http.hpp>
#include <common/io.hpp>
#include <common/json.hpp>
#include <common/log.hpp>
#include <common/path.hpp>
#include <common/testing.hpp>

#include <mender-update/deployments.hpp>

using namespace std;
using mender::nullopt;
using mender::optional;

namespace common = mender::common;
namespace conf = mender::common::conf;
namespace context = mender::update::context;
namespace deps = mender::update::deployments;
namespace error = mender::common::error;
namespace events = mender::common::events;
namespace expected = mender::common::expected;
namespace http = mender::http;
namespace io = mender::common::io;
namespace json = mender::common::json;
namespace mlog = mender::common::log;
namespace mtesting = mender::common::testing;
namespace path = mender::common::path;

#define TEST_SERVER "http://127.0.0.1:8001"

using TestEventLoop = mtesting::TestEventLoop;

class NoAuthHTTPClient : public http::Client {
public:
	NoAuthHTTPClient(const http::ClientConfig &config, events::EventLoop &event_loop) :
		http::Client(config, event_loop) {};

	error::Error AsyncCall(
		http::OutgoingRequestPtr req,
		http::ResponseHandler header_handler,
		http::ResponseHandler body_handler) override {
		req->SetAddress(http::JoinUrl(TEST_SERVER, req->GetPath()));
		return http::Client::AsyncCall(req, header_handler, body_handler);
	}
};

class DeploymentsTests : public testing::Test {
protected:
	mtesting::TemporaryDirectory test_state_dir;
};

TEST_F(DeploymentsTests, TestV2APIWithNextDeployment) {
	conf::MenderConfig cfg;
	cfg.paths.SetDataStore(test_state_dir.Path());

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

	ofstream os(cfg.paths.GetDataStore() + "/device_type");
	ASSERT_TRUE(os);
	os << "device_type=Some device type" << endl;
	os.close();

	const string expected_request_data =
		R"({"device_provides":{"device_type":"Some device type","something_else":"something_else value","artifact_group":"artifact-group value","artifact_name":"artifact-name value"}})";
	const string response_data = R"({
  "some": "data here"
})";

	TestEventLoop loop;

	http::ServerConfig server_config;
	http::Server server(server_config, loop);

	http::ClientConfig client_config;
	NoAuthHTTPClient client {client_config, loop};

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
	err = deps::DeploymentClient().CheckNewDeployments(
		ctx, client, [&response_data, &handler_called, &loop](deps::CheckUpdatesAPIResponse resp) {
			handler_called = true;
			ASSERT_TRUE(resp);

			auto o_js = resp.value();
			ASSERT_NE(o_js, nullopt);

			EXPECT_EQ(o_js->Dump(), response_data);
			loop.Stop();
		});
	EXPECT_EQ(err, error::NoError);

	loop.Run();
	EXPECT_TRUE(handler_called);
}

TEST_F(DeploymentsTests, TestV2APIWithNoNextDeployment) {
	conf::MenderConfig cfg;
	cfg.paths.SetDataStore(test_state_dir.Path());

	context::MenderContext ctx(cfg);
	auto err = ctx.Initialize();
	ASSERT_EQ(err, error::NoError);

	auto &db = ctx.GetMenderStoreDB();
	err = db.Write("artifact-name", common::ByteVectorFromString("artifact-name value"));
	ASSERT_EQ(err, error::NoError);

	ofstream os(cfg.paths.GetDataStore() + "/device_type");
	ASSERT_TRUE(os);
	os << "device_type=Some device type" << endl;
	os.close();

	const string expected_request_data =
		R"({"device_provides":{"device_type":"Some device type","artifact_name":"artifact-name value"}})";
	const string response_data = "";

	TestEventLoop loop;

	http::ServerConfig server_config;
	http::Server server(server_config, loop);

	http::ClientConfig client_config;
	NoAuthHTTPClient client {client_config, loop};

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
	err = deps::DeploymentClient().CheckNewDeployments(
		ctx, client, [&handler_called, &loop](deps::CheckUpdatesAPIResponse resp) {
			handler_called = true;
			ASSERT_TRUE(resp);

			auto o_js = resp.value();
			EXPECT_EQ(o_js, nullopt);

			loop.Stop();
		});
	EXPECT_EQ(err, error::NoError);

	loop.Run();
	EXPECT_TRUE(handler_called);
}

TEST_F(DeploymentsTests, TestV2APIError) {
	conf::MenderConfig cfg;
	cfg.paths.SetDataStore(test_state_dir.Path());

	context::MenderContext ctx(cfg);
	auto err = ctx.Initialize();
	ASSERT_EQ(err, error::NoError);

	auto &db = ctx.GetMenderStoreDB();
	err = db.Write("artifact-name", common::ByteVectorFromString("artifact-name value"));
	ASSERT_EQ(err, error::NoError);

	ofstream os(cfg.paths.GetDataStore() + "/device_type");
	ASSERT_TRUE(os);
	os << "device_type=Some device type" << endl;
	os.close();

	const string expected_request_data =
		R"({"device_provides":{"device_type":"Some device type","artifact_name":"artifact-name value"}})";
	const string response_data = R"({"error": "JWT token expired", "response-id": "some id here"})";

	TestEventLoop loop;

	http::ServerConfig server_config;
	http::Server server(server_config, loop);

	http::ClientConfig client_config;
	NoAuthHTTPClient client {client_config, loop};

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
	err = deps::DeploymentClient().CheckNewDeployments(
		ctx, client, [&handler_called, &loop](deps::CheckUpdatesAPIResponse resp) {
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
	cfg.paths.SetDataStore(test_state_dir.Path());

	context::MenderContext ctx(cfg);
	auto err = ctx.Initialize();
	ASSERT_EQ(err, error::NoError);

	auto &db = ctx.GetMenderStoreDB();
	err = db.Write("artifact-name", common::ByteVectorFromString("artifact-name value"));
	ASSERT_EQ(err, error::NoError);

	ofstream os(cfg.paths.GetDataStore() + "/device_type");
	ASSERT_TRUE(os);
	os << "device_type=Some device type" << endl;
	os.close();

	const string expected_request_data =
		R"({"device_provides":{"device_type":"Some device type","artifact_name":"artifact-name value"}})";
	const string response_data = R"({
  "some": "data here"
})";

	TestEventLoop loop;

	http::ServerConfig server_config;
	http::Server server(server_config, loop);

	http::ClientConfig client_config;
	NoAuthHTTPClient client {client_config, loop};

	vector<uint8_t> received_body;
	bool v2_requested = false;
	server.AsyncServeUrl(
		TEST_SERVER,
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
	err = deps::DeploymentClient().CheckNewDeployments(
		ctx, client, [&response_data, &handler_called, &loop](deps::CheckUpdatesAPIResponse resp) {
			handler_called = true;
			ASSERT_TRUE(resp);

			auto o_js = resp.value();
			ASSERT_NE(o_js, nullopt);

			EXPECT_EQ(o_js->Dump(), response_data);
			loop.Stop();
		});
	EXPECT_EQ(err, error::NoError);

	loop.Run();
	EXPECT_TRUE(handler_called);
}

TEST_F(DeploymentsTests, TestV1APIFallbackWithNoNextDeployment) {
	conf::MenderConfig cfg;
	cfg.paths.SetDataStore(test_state_dir.Path());

	context::MenderContext ctx(cfg);
	auto err = ctx.Initialize();
	ASSERT_EQ(err, error::NoError);

	auto &db = ctx.GetMenderStoreDB();
	err = db.Write("artifact-name", common::ByteVectorFromString("artifact-name value"));
	ASSERT_EQ(err, error::NoError);

	ofstream os(cfg.paths.GetDataStore() + "/device_type");
	ASSERT_TRUE(os);
	os << "device_type=Some device type" << endl;
	os.close();

	const string expected_request_data =
		R"({"device_provides":{"device_type":"Some device type","artifact_name":"artifact-name value"}})";
	const string response_data = "";

	TestEventLoop loop(chrono::seconds(3600));

	http::ServerConfig server_config;
	http::Server server(server_config, loop);

	http::ClientConfig client_config;
	NoAuthHTTPClient client {client_config, loop};

	vector<uint8_t> received_body;
	bool v2_requested = false;
	server.AsyncServeUrl(
		TEST_SERVER,
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
	err = deps::DeploymentClient().CheckNewDeployments(
		ctx, client, [&handler_called, &loop](deps::CheckUpdatesAPIResponse resp) {
			handler_called = true;
			ASSERT_TRUE(resp);

			auto o_js = resp.value();
			EXPECT_EQ(o_js, nullopt);

			loop.Stop();
		});
	EXPECT_EQ(err, error::NoError);

	loop.Run();
	EXPECT_TRUE(handler_called);
}

TEST_F(DeploymentsTests, TestV1APIFallbackWithError) {
	conf::MenderConfig cfg;
	cfg.paths.SetDataStore(test_state_dir.Path());

	context::MenderContext ctx(cfg);
	auto err = ctx.Initialize();
	ASSERT_EQ(err, error::NoError);

	auto &db = ctx.GetMenderStoreDB();
	err = db.Write("artifact-name", common::ByteVectorFromString("artifact-name value"));
	ASSERT_EQ(err, error::NoError);

	ofstream os(cfg.paths.GetDataStore() + "/device_type");
	ASSERT_TRUE(os);
	os << "device_type=Some device type" << endl;
	os.close();

	const string expected_request_data =
		R"({"device_provides":{"device_type":"Some device type","artifact_name":"artifact-name value"}})";
	const string response_data = "";

	TestEventLoop loop(chrono::seconds(3600));

	http::ServerConfig server_config;
	http::Server server(server_config, loop);

	http::ClientConfig client_config;
	NoAuthHTTPClient client {client_config, loop};

	vector<uint8_t> received_body;
	bool v2_requested = false;
	server.AsyncServeUrl(
		TEST_SERVER,
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
	err = deps::DeploymentClient().CheckNewDeployments(
		ctx, client, [&handler_called, &loop](deps::CheckUpdatesAPIResponse resp) {
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
	NoAuthHTTPClient client {client_config, loop};

	auto status {deps::DeploymentStatus::Rebooting};
	string deployment_id = "2";
	string substatus = "Rebooting now";
	string expected_request_data = R"({"status":"rebooting","substate":")" + substatus + "\"}";

	const string response_data = "";

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
			resp->SetStatusCodeAndMessage(204, "No content");
			resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
		});

	bool handler_called = false;
	auto err = deps::DeploymentClient().PushStatus(
		deployment_id,
		status,
		substatus,
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
	NoAuthHTTPClient client {client_config, loop};

	auto status {deps::DeploymentStatus::AlreadyInstalled};
	string deployment_id = "2";
	string substatus = "";
	string expected_request_data = R"({"status":"already-installed"})";

	const string response_data = "";

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
			resp->SetStatusCodeAndMessage(204, "No content");
			resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
		});

	bool handler_called = false;
	auto err = deps::DeploymentClient().PushStatus(
		deployment_id,
		status,
		substatus,
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
	NoAuthHTTPClient client {client_config, loop};

	auto status {deps::DeploymentStatus::Installing};
	string deployment_id = "2";
	string substatus = "Installing now";
	string expected_request_data = R"({"status":"installing","substate":")" + substatus + "\"}";

	const string response_data = R"({"error": "Access denied", "response-id": "some id here"})";

	bool aborted_failure = false;

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
		[&received_body, &expected_request_data, &response_data, deployment_id, &aborted_failure](
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
			if (aborted_failure) {
				resp->SetStatusCodeAndMessage(409, "Conflict");
			} else {
				resp->SetStatusCodeAndMessage(403, "Forbidden");
			}
			resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
		});

	bool handler_called = false;
	auto err = deps::DeploymentClient().PushStatus(
		deployment_id,
		status,
		substatus,
		client,
		[&handler_called, &loop](deps::StatusAPIResponse resp) {
			handler_called = true;
			EXPECT_NE(resp, error::NoError);
			EXPECT_THAT(resp.message, testing::HasSubstr("Got unexpected response"));
			EXPECT_THAT(resp.message, testing::HasSubstr("403"));
			EXPECT_THAT(resp.message, testing::HasSubstr("Access denied"));
			EXPECT_NE(resp.code, deps::MakeError(deps::DeploymentAbortedError, "").code);
			loop.Stop();
		});
	EXPECT_EQ(err, error::NoError);

	loop.Run();
	EXPECT_TRUE(handler_called);

	// Redo with 409 Conflict response (deployment aborted).
	handler_called = false;
	aborted_failure = true;

	err = deps::DeploymentClient().PushStatus(
		deployment_id,
		status,
		substatus,
		client,
		[&handler_called, &loop](deps::StatusAPIResponse resp) {
			handler_called = true;
			EXPECT_NE(resp, error::NoError);
			EXPECT_EQ(resp.code, deps::MakeError(deps::DeploymentAbortedError, "").code);
			loop.Stop();
		});
	EXPECT_EQ(err, error::NoError);

	loop.Run();
	EXPECT_TRUE(handler_called);
}

TEST_F(DeploymentsTests, JsonLogMessageReaderTest) {
	const string messages =
		R"({"timestamp": "2016-03-11T13:03:17.063493443Z", "level": "INFO", "message": "OK"}
{"timestamp": "2020-03-11T13:03:17.063493443Z", "level": "WARNING", "message": "Warnings appeared"}
{"timestamp": "2021-03-11T13:03:17.063493443Z", "level": "DEBUG", "message": "Just some noise"}
)";
	const string test_log_file_path = test_state_dir.Path() + "/test.log";
	ofstream os {test_log_file_path};
	auto err = io::WriteStringIntoOfstream(os, messages);
	ASSERT_EQ(err, error::NoError);
	os.close();

	string header = R"({"messages":[)";
	string closing = "]}";
	string expected_data =
		R"({"messages":[{"timestamp": "2016-03-11T13:03:17.063493443Z", "level": "INFO", "message": "OK"},{"timestamp": "2020-03-11T13:03:17.063493443Z", "level": "WARNING", "message": "Warnings appeared"},{"timestamp": "2021-03-11T13:03:17.063493443Z", "level": "DEBUG", "message": "Just some noise"}]})";

	// the reader takes size of the data without a trailing newline
	auto expected_total_size = header.size() + messages.size() - 1 + closing.size();
	EXPECT_EQ(deps::JsonLogMessagesReader::TotalDataSize(messages.size() - 1), expected_total_size);

	auto file_reader = make_shared<io::FileReader>(test_log_file_path);
	deps::JsonLogMessagesReader logs_reader {file_reader, messages.size() - 1};

	stringstream ss;
	vector<uint8_t> buf(1024);
	size_t n_read = 0;
	do {
		auto ex_n_read = logs_reader.Read(buf.begin(), buf.end());
		ASSERT_TRUE(ex_n_read);
		n_read = ex_n_read.value();
		EXPECT_LE(n_read, buf.size());
		for (auto it = buf.begin(); it < buf.begin() + n_read; it++) {
			ss << static_cast<char>(*it);
		}
	} while (n_read > 0);
	EXPECT_EQ(ss.str(), expected_data);
}

TEST_F(DeploymentsTests, JsonLogMessageReaderSmallBufferTest) {
	const string messages =
		R"({"timestamp": "2016-03-11T13:03:17.063493443Z", "level": "INFO", "message": "OK"}
{"timestamp": "2020-03-11T13:03:17.063493443Z", "level": "WARNING", "message": "Warnings appeared"}
{"timestamp": "2021-03-11T13:03:17.063493443Z", "level": "DEBUG", "message": "Just some noise"}
)";
	const string test_log_file_path = test_state_dir.Path() + "/test.log";
	ofstream os {test_log_file_path};
	auto err = io::WriteStringIntoOfstream(os, messages);
	ASSERT_EQ(err, error::NoError);
	os.close();

	string header = R"({"messages":[)";
	string closing = "]}";
	string expected_data =
		R"({"messages":[{"timestamp": "2016-03-11T13:03:17.063493443Z", "level": "INFO", "message": "OK"},{"timestamp": "2020-03-11T13:03:17.063493443Z", "level": "WARNING", "message": "Warnings appeared"},{"timestamp": "2021-03-11T13:03:17.063493443Z", "level": "DEBUG", "message": "Just some noise"}]})";

	// the reader takes size of the data without a trailing newline
	auto expected_total_size = header.size() + messages.size() - 1 + closing.size();
	EXPECT_EQ(deps::JsonLogMessagesReader::TotalDataSize(messages.size() - 1), expected_total_size);

	auto file_reader = make_shared<io::FileReader>(test_log_file_path);
	deps::JsonLogMessagesReader logs_reader {file_reader, messages.size() - 1};

	stringstream ss;
	vector<uint8_t> buf(16);
	size_t n_read = 0;
	do {
		auto ex_n_read = logs_reader.Read(buf.begin(), buf.end());
		ASSERT_TRUE(ex_n_read);
		n_read = ex_n_read.value();
		EXPECT_LE(n_read, buf.size());
		for (auto it = buf.begin(); it < buf.begin() + n_read; it++) {
			ss << static_cast<char>(*it);
		}
	} while (n_read > 0);
	EXPECT_EQ(ss.str(), expected_data);
}

TEST_F(DeploymentsTests, JsonLogMessageReaderSmallEvenBufferTest) {
	const string messages =
		R"({"timestamp": "2016-03-11T13:03:17.063493443Z", "level": "INFO", "message": "OK"}
{"timestamp": "2020-03-11T13:03:17.063493443Z", "level": "WARNING", "message": "Warnings appeared"}
{"timestamp": "2021-03-11T13:03:17.063493443Z", "level": "DEBUG", "message": "Just some noise"}
)";
	const string test_log_file_path = test_state_dir.Path() + "/test.log";
	ofstream os {test_log_file_path};
	auto err = io::WriteStringIntoOfstream(os, messages);
	ASSERT_EQ(err, error::NoError);
	os.close();

	string header = R"({"messages":[)";
	string closing = "]}";
	string expected_data =
		R"({"messages":[{"timestamp": "2016-03-11T13:03:17.063493443Z", "level": "INFO", "message": "OK"},{"timestamp": "2020-03-11T13:03:17.063493443Z", "level": "WARNING", "message": "Warnings appeared"},{"timestamp": "2021-03-11T13:03:17.063493443Z", "level": "DEBUG", "message": "Just some noise"}]})";

	// the reader takes size of the data without a trailing newline
	auto expected_total_size = header.size() + messages.size() - 1 + closing.size();
	EXPECT_EQ(deps::JsonLogMessagesReader::TotalDataSize(messages.size() - 1), expected_total_size);

	auto file_reader = make_shared<io::FileReader>(test_log_file_path);
	deps::JsonLogMessagesReader logs_reader {file_reader, messages.size() - 1};

	stringstream ss;
	vector<uint8_t> buf(7);
	size_t n_read = 0;
	do {
		auto ex_n_read = logs_reader.Read(buf.begin(), buf.end());
		ASSERT_TRUE(ex_n_read);
		n_read = ex_n_read.value();
		EXPECT_LE(n_read, buf.size());
		for (auto it = buf.begin(); it < buf.begin() + n_read; it++) {
			ss << static_cast<char>(*it);
		}
	} while (n_read > 0);
	EXPECT_EQ(ss.str(), expected_data);
}

TEST_F(DeploymentsTests, JsonLogMessageReaderRewindTest) {
	const string messages =
		R"({"timestamp": "2016-03-11T13:03:17.063493443Z", "level": "INFO", "message": "OK"}
{"timestamp": "2020-03-11T13:03:17.063493443Z", "level": "WARNING", "message": "Warnings appeared"}
{"timestamp": "2021-03-11T13:03:17.063493443Z", "level": "DEBUG", "message": "Just some noise"}
)";
	const string test_log_file_path = test_state_dir.Path() + "/test.log";
	ofstream os {test_log_file_path};
	auto err = io::WriteStringIntoOfstream(os, messages);
	ASSERT_EQ(err, error::NoError);
	os.close();

	string header = R"({"messages":[)";
	string closing = "]}";
	string expected_data =
		R"({"messages":[{"timestamp": "2016-03-11T13:03:17.063493443Z", "level": "INFO", "message": "OK"},{"timestamp": "2020-03-11T13:03:17.063493443Z", "level": "WARNING", "message": "Warnings appeared"},{"timestamp": "2021-03-11T13:03:17.063493443Z", "level": "DEBUG", "message": "Just some noise"}]})";

	// the reader takes size of the data without a trailing newline
	auto expected_total_size = header.size() + messages.size() - 1 + closing.size();
	EXPECT_EQ(deps::JsonLogMessagesReader::TotalDataSize(messages.size() - 1), expected_total_size);

	auto file_reader = make_shared<io::FileReader>(test_log_file_path);
	deps::JsonLogMessagesReader logs_reader {file_reader, messages.size() - 1};

	stringstream ss;
	vector<uint8_t> buf(1024);
	size_t n_read = 0;
	do {
		auto ex_n_read = logs_reader.Read(buf.begin(), buf.end());
		ASSERT_TRUE(ex_n_read);
		n_read = ex_n_read.value();
		EXPECT_LE(n_read, buf.size());
		for (auto it = buf.begin(); it < buf.begin() + n_read; it++) {
			ss << static_cast<char>(*it);
		}
	} while (n_read > 0);
	EXPECT_EQ(ss.str(), expected_data);

	stringstream ss2;
	logs_reader.Rewind();
	do {
		auto ex_n_read = logs_reader.Read(buf.begin(), buf.end());
		ASSERT_TRUE(ex_n_read);
		n_read = ex_n_read.value();
		EXPECT_LE(n_read, buf.size());
		for (auto it = buf.begin(); it < buf.begin() + n_read; it++) {
			ss2 << static_cast<char>(*it);
		}
	} while (n_read > 0);
	EXPECT_EQ(ss2.str(), expected_data);
}

TEST_F(DeploymentsTests, PushLogsTest) {
	TestEventLoop loop;

	http::ServerConfig server_config;
	http::Server server(server_config, loop);

	http::ClientConfig client_config;
	NoAuthHTTPClient client {client_config, loop};

	const string messages =
		R"({"timestamp": "2016-03-11T13:03:17.063493443Z", "level": "INFO", "message": "OK"}
{"timestamp": "2020-03-11T13:03:17.063493443Z", "level": "WARNING", "message": "Warnings appeared"}
{"timestamp": "2021-03-11T13:03:17.063493443Z", "level": "DEBUG", "message": "Just some noise"}
)";
	const string test_log_file_path = test_state_dir.Path() + "/test.log";
	ofstream os {test_log_file_path};
	auto err = io::WriteStringIntoOfstream(os, messages);
	ASSERT_EQ(err, error::NoError);
	os.close();

	string deployment_id = "2";
	string expected_request_data =
		R"({"messages":[{"timestamp": "2016-03-11T13:03:17.063493443Z", "level": "INFO", "message": "OK"},{"timestamp": "2020-03-11T13:03:17.063493443Z", "level": "WARNING", "message": "Warnings appeared"},{"timestamp": "2021-03-11T13:03:17.063493443Z", "level": "DEBUG", "message": "Just some noise"}]})";

	const string response_data = "";

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
		[&received_body, &expected_request_data, &response_data, deployment_id](
			http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();

			auto req = exp_req.value();
			EXPECT_EQ(
				req->GetPath(),
				"/api/devices/v1/deployments/device/deployments/" + deployment_id + "/log");
			EXPECT_EQ(req->GetMethod(), http::Method::PUT);
			EXPECT_EQ(common::StringFromByteVector(received_body), expected_request_data);

			auto result = req->MakeResponse();
			ASSERT_TRUE(result);
			auto resp = result.value();

			resp->SetHeader("Content-Length", to_string(response_data.size()));
			resp->SetBodyReader(make_shared<io::StringReader>(response_data));
			resp->SetStatusCodeAndMessage(204, "No content");
			resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
		});

	bool handler_called = false;
	err = deps::DeploymentClient().PushLogs(
		deployment_id,
		test_log_file_path,
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

TEST_F(DeploymentsTests, PushLogsOneMessageTest) {
	TestEventLoop loop;

	http::ServerConfig server_config;
	http::Server server(server_config, loop);

	http::ClientConfig client_config;
	NoAuthHTTPClient client {client_config, loop};

	const string messages =
		R"({"timestamp": "2021-03-11T13:03:17.063493443Z", "level": "DEBUG", "message": "Just some noise"}
)";
	const string test_log_file_path = test_state_dir.Path() + "/test.log";
	ofstream os {test_log_file_path};
	auto err = io::WriteStringIntoOfstream(os, messages);
	ASSERT_EQ(err, error::NoError);
	os.close();

	string deployment_id = "2";
	string expected_request_data =
		R"({"messages":[{"timestamp": "2021-03-11T13:03:17.063493443Z", "level": "DEBUG", "message": "Just some noise"}]})";

	const string response_data = "";

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
		[&received_body, &expected_request_data, &response_data, deployment_id](
			http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();

			auto req = exp_req.value();
			EXPECT_EQ(
				req->GetPath(),
				"/api/devices/v1/deployments/device/deployments/" + deployment_id + "/log");
			EXPECT_EQ(req->GetMethod(), http::Method::PUT);
			EXPECT_EQ(common::StringFromByteVector(received_body), expected_request_data);

			auto result = req->MakeResponse();
			ASSERT_TRUE(result);
			auto resp = result.value();

			resp->SetHeader("Content-Length", to_string(response_data.size()));
			resp->SetBodyReader(make_shared<io::StringReader>(response_data));
			resp->SetStatusCodeAndMessage(204, "No content");
			resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
		});

	bool handler_called = false;
	err = deps::DeploymentClient().PushLogs(
		deployment_id,
		test_log_file_path,
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

TEST_F(DeploymentsTests, PushLogsFailureTest) {
	TestEventLoop loop;

	http::ServerConfig server_config;
	http::Server server(server_config, loop);

	http::ClientConfig client_config;
	NoAuthHTTPClient client {client_config, loop};

	const string messages =
		R"({"timestamp": "2021-03-11T13:03:17.063493443Z", "level": "DEBUG", "message": "Just some noise"}
)";
	const string test_log_file_path = test_state_dir.Path() + "/test.log";
	ofstream os {test_log_file_path};
	auto err = io::WriteStringIntoOfstream(os, messages);
	ASSERT_EQ(err, error::NoError);
	os.close();

	string deployment_id = "2";
	string expected_request_data =
		R"({"messages":[{"timestamp": "2021-03-11T13:03:17.063493443Z", "level": "DEBUG", "message": "Just some noise"}]})";

	const string response_data = R"({"error": "Access denied", "response-id": "some id here"})";

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
		[&received_body, &expected_request_data, &response_data, deployment_id](
			http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();

			auto req = exp_req.value();
			EXPECT_EQ(
				req->GetPath(),
				"/api/devices/v1/deployments/device/deployments/" + deployment_id + "/log");
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
	err = deps::DeploymentClient().PushLogs(
		deployment_id,
		test_log_file_path,
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

TEST_F(DeploymentsTests, DeploymentLogTest) {
	deps::DeploymentLog dlog {test_state_dir.Path(), "1"};
	dlog.BeginLogging();
	mlog::Info("Testing info deployment logging");
	mlog::Error("Testing error deployment logging");
	dlog.FinishLogging();
	mlog::Warning("Shouldn't appear in the deployment log");

	ifstream is {path::Join(test_state_dir.Path(), "deployments.0000.1.log")};
	string line;
	size_t line_idx = 0;
	for (; getline(is, line); line_idx++) {
		EXPECT_THAT(line, Not(testing::HasSubstr("Shouldn't appear")));

		auto ex_j = json::Load(line);
		ASSERT_TRUE(ex_j);
		auto j = ex_j.value();
		EXPECT_TRUE(j.IsObject());

		auto ex_ts = j.Get("timestamp");
		ASSERT_TRUE(ex_ts);
		EXPECT_TRUE(ex_ts.value().IsString());

		auto ex_level = j.Get("level").and_then(json::ToString);
		ASSERT_TRUE(ex_level);
		EXPECT_THAT(
			ex_level.value(),
			testing::Conditional(line_idx == 0, testing::Eq("info"), testing::Eq("error")));

		auto ex_msg = j.Get("message").and_then(json::ToString);
		ASSERT_TRUE(ex_msg);
		EXPECT_THAT(
			ex_msg.value(),
			testing::Conditional(
				line_idx == 0,
				testing::Eq("Testing info deployment logging"),
				testing::Eq("Testing error deployment logging")));
	}
}

TEST_F(DeploymentsTests, DeploymentLogScopedTest) {
	{
		deps::DeploymentLog dlog {test_state_dir.Path(), "1"};
		dlog.BeginLogging();
		mlog::Info("Testing info deployment logging");
		mlog::Error("Testing error deployment logging");
	}
	mlog::Warning("Shouldn't appear in the deployment log");

	ifstream is {path::Join(test_state_dir.Path(), "deployments.0000.1.log")};
	string line;
	size_t line_idx = 0;
	for (; getline(is, line); line_idx++) {
		EXPECT_THAT(line, Not(testing::HasSubstr("Shouldn't appear")));

		auto ex_j = json::Load(line);
		ASSERT_TRUE(ex_j);
		auto j = ex_j.value();
		EXPECT_TRUE(j.IsObject());

		auto ex_ts = j.Get("timestamp");
		ASSERT_TRUE(ex_ts);
		EXPECT_TRUE(ex_ts.value().IsString());

		auto ex_level = j.Get("level").and_then(json::ToString);
		ASSERT_TRUE(ex_level);
		EXPECT_THAT(
			ex_level.value(),
			testing::Conditional(line_idx == 0, testing::Eq("info"), testing::Eq("error")));

		auto ex_msg = j.Get("message").and_then(json::ToString);
		ASSERT_TRUE(ex_msg);
		EXPECT_THAT(
			ex_msg.value(),
			testing::Conditional(
				line_idx == 0,
				testing::Eq("Testing info deployment logging"),
				testing::Eq("Testing error deployment logging")));
	}
}

TEST_F(DeploymentsTests, DeploymentLogFileAppendTest) {
	{
		deps::DeploymentLog dlog {test_state_dir.Path(), "1"};
		dlog.BeginLogging();
		mlog::Info("Testing info deployment logging");
	}
	{
		// should just append to the same file
		deps::DeploymentLog dlog {test_state_dir.Path(), "1"};
		dlog.BeginLogging();
		mlog::Error("Testing error deployment logging");
	}

	ifstream is {path::Join(test_state_dir.Path(), "deployments.0000.1.log")};
	string line;
	size_t line_idx = 0;
	for (; getline(is, line); line_idx++) {
		EXPECT_THAT(line, Not(testing::HasSubstr("Shouldn't appear")));

		auto ex_j = json::Load(line);
		ASSERT_TRUE(ex_j);
		auto j = ex_j.value();
		EXPECT_TRUE(j.IsObject());

		auto ex_ts = j.Get("timestamp");
		ASSERT_TRUE(ex_ts);
		EXPECT_TRUE(ex_ts.value().IsString());

		auto ex_level = j.Get("level").and_then(json::ToString);
		ASSERT_TRUE(ex_level);
		EXPECT_THAT(
			ex_level.value(),
			testing::Conditional(line_idx == 0, testing::Eq("info"), testing::Eq("error")));

		auto ex_msg = j.Get("message").and_then(json::ToString);
		ASSERT_TRUE(ex_msg);
		EXPECT_THAT(
			ex_msg.value(),
			testing::Conditional(
				line_idx == 0,
				testing::Eq("Testing info deployment logging"),
				testing::Eq("Testing error deployment logging")));
	}
}

string GetFileContent(const string &path) {
	ifstream is {path};
	if (!is) {
		return "";
	}
	// taken from https://stackoverflow.com/questions/2912520/read-file-contents-into-a-string-in-c
	string content {istreambuf_iterator<char>(is), istreambuf_iterator<char>()};

	return content;
}

TEST_F(DeploymentsTests, DeploymentLogRenameAndCleanPreviousLogsTest) {
	ofstream os;
	for (int i : {0, 1, 2, 3, 4}) {
		string file_name = "deployments.000" + to_string(i) + ".1" + to_string(i) + ".log";
		os.open(path::Join(test_state_dir.Path(), file_name));
		os << "Test content " + to_string(i) + " here\n";
		os.close();
	}
	os.open(path::Join(test_state_dir.Path(), "deployments.log"));
	os << "Test content in malformed file name\n";
	os.close();

	os.open(path::Join(test_state_dir.Path(), "deployments.00000.1.log"));
	os << "Test content in malformed file name 1\n";
	os.close();

	os.open(path::Join(test_state_dir.Path(), "deployments.000.2.log"));
	os << "Test content in malformed file name 2\n";
	os.close();

	os.open(path::Join(test_state_dir.Path(), "deployments.3.log"));
	os << "Test content in malformed file name 3\n";
	os.close();

	deps::DeploymentLog dlog {test_state_dir.Path(), "21"};
	dlog.BeginLogging();
	mlog::Info("Testing info deployment logging");
	mlog::Error("Testing error deployment logging");
	dlog.FinishLogging();
	mlog::Warning("Shouldn't appear in the deployment log");

	ifstream is {path::Join(test_state_dir.Path(), "deployments.0000.21.log")};
	string line;
	size_t line_idx = 0;
	for (; getline(is, line); line_idx++) {
		EXPECT_THAT(line, Not(testing::HasSubstr("Shouldn't appear")));

		auto ex_j = json::Load(line);
		ASSERT_TRUE(ex_j);
		auto j = ex_j.value();
		EXPECT_TRUE(j.IsObject());

		auto ex_ts = j.Get("timestamp");
		ASSERT_TRUE(ex_ts);
		EXPECT_TRUE(ex_ts.value().IsString());

		auto ex_level = j.Get("level").and_then(json::ToString);
		ASSERT_TRUE(ex_level);
		EXPECT_THAT(
			ex_level.value(),
			testing::Conditional(line_idx == 0, testing::Eq("info"), testing::Eq("error")));

		auto ex_msg = j.Get("message").and_then(json::ToString);
		ASSERT_TRUE(ex_msg);
		EXPECT_THAT(
			ex_msg.value(),
			testing::Conditional(
				line_idx == 0,
				testing::Eq("Testing info deployment logging"),
				testing::Eq("Testing error deployment logging")));
	}

	for (int i : {0, 1, 2, 3}) {
		string file_name = "deployments.000" + to_string(i + 1) + ".1" + to_string(i) + ".log";
		EXPECT_EQ(
			GetFileContent(path::Join(test_state_dir.Path(), file_name)),
			"Test content " + to_string(i) + " here\n");
	}
	// the past log with the highest index shouldn't exist anymore under any name
	EXPECT_EQ(GetFileContent(path::Join(test_state_dir.Path(), "deployments.0004.14.log")), "");
	EXPECT_EQ(GetFileContent(path::Join(test_state_dir.Path(), "deployments.0005.14.log")), "");

	// malformed log files intact
	EXPECT_EQ(
		GetFileContent(path::Join(test_state_dir.Path(), "deployments.log")),
		"Test content in malformed file name\n");
	EXPECT_EQ(
		GetFileContent(path::Join(test_state_dir.Path(), "deployments.00000.1.log")),
		"Test content in malformed file name 1\n");
	EXPECT_EQ(
		GetFileContent(path::Join(test_state_dir.Path(), "deployments.000.2.log")),
		"Test content in malformed file name 2\n");
	EXPECT_EQ(
		GetFileContent(path::Join(test_state_dir.Path(), "deployments.3.log")),
		"Test content in malformed file name 3\n");
}
