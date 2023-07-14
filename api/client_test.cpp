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

#include <gmock/gmock.h>
#include <gtest/gtest.h>

#include <api/auth.hpp>
#include <common/common.hpp>
#include <common/error.hpp>
#include <common/events.hpp>
#include <common/http.hpp>
#include <common/io.hpp>
#include <common/path.hpp>
#include <common/testing.hpp>

using namespace std;

namespace api = mender::api;
namespace auth = mender::api::auth;
namespace common = mender::common;
namespace error = mender::common::error;
namespace events = mender::common::events;
namespace http = mender::http;
namespace io = mender::common::io;
namespace path = mender::common::path;
namespace mtesting = mender::common::testing;

using TestEventLoop = mender::common::testing::TestEventLoop;

const string TEST_PORT = "8088";

class APIClientTests : public testing::Test {
protected:
	mtesting::TemporaryDirectory tmpdir;
	const string test_device_identity_script = path::Join(tmpdir.Path(), "mender-device-identity");
	const string auth_uri = "/api/devices/v1/authentication/auth_requests";

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

TEST_F(APIClientTests, ClientBasicTest) {
	const string JWT_TOKEN = "FOOBARJWTTOKEN";
	const string test_data = "some testing data";
	const string test_uri = "/test/uri";

	TestEventLoop loop;

	// Setup a test server
	const string server_url {"http://127.0.0.1:" + TEST_PORT};
	http::ServerConfig server_config;
	http::Server server(server_config, loop);
	bool auth_data_sent = false;
	server.AsyncServeUrl(
		server_url,
		[JWT_TOKEN, test_uri, &auth_data_sent, this](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			auto req = exp_req.value();
			if (auth_data_sent) {
				EXPECT_EQ(req->GetPath(), test_uri);
				auto ex_auth = req->GetHeader("Authorization");
				ASSERT_TRUE(ex_auth);
				EXPECT_EQ(ex_auth.value(), "Bearer " + JWT_TOKEN);
			} else {
				EXPECT_EQ(req->GetPath(), auth_uri);
			}
		},
		[JWT_TOKEN, test_data, &auth_data_sent, this](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();

			auto result = exp_req.value()->MakeResponse();
			ASSERT_TRUE(result);
			auto resp = result.value();

			auto req = exp_req.value();
			if (req->GetPath() == auth_uri) {
				resp->SetStatusCodeAndMessage(200, "OK");
				resp->SetBodyReader(make_shared<io::StringReader>(JWT_TOKEN));
				resp->SetHeader("Content-Length", to_string(JWT_TOKEN.size()));
				resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
				auth_data_sent = true;
			} else {
				resp->SetStatusCodeAndMessage(200, "OK");
				resp->SetBodyReader(make_shared<io::StringReader>(test_data));
				resp->SetHeader("Content-Length", to_string(test_data.size()));
				resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
			}
		});

	string private_key_path = "./private_key.pem";
	string server_certificate_path {};
	http::ClientConfig client_config = http::ClientConfig(server_certificate_path);
	auth::Authenticator authenticator {
		loop, client_config, server_url, private_key_path, test_device_identity_script};

	api::Client client {client_config, loop, authenticator};

	auto req = make_shared<http::OutgoingRequest>();
	req->SetAddress(server_url + test_uri);
	req->SetMethod(http::Method::GET);

	auto received_body = make_shared<vector<uint8_t>>();
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
}

TEST_F(APIClientTests, TwoClientsTest) {
	const string JWT_TOKEN = "FOOBARJWTTOKEN";
	const string test_data1 = "some testing data 1";
	const string test_data2 = "some testing data 2";
	const string test_uri1 = "/test/uri/1";
	const string test_uri2 = "/test/uri/2";

	TestEventLoop loop;

	// Setup a test server
	const string server_url {"http://127.0.0.1:" + TEST_PORT};
	http::ServerConfig server_config;
	http::Server server(server_config, loop);
	bool auth_data_sent = false;
	server.AsyncServeUrl(
		server_url,
		[JWT_TOKEN, test_uri1, test_uri2, &auth_data_sent, this](
			http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			auto req = exp_req.value();
			if (auth_data_sent) {
				EXPECT_TRUE((req->GetPath() == test_uri1) || (req->GetPath() == test_uri2));
				auto ex_auth = req->GetHeader("Authorization");
				ASSERT_TRUE(ex_auth);
				EXPECT_EQ(ex_auth.value(), "Bearer " + JWT_TOKEN);
			} else {
				EXPECT_EQ(req->GetPath(), auth_uri);
			}
		},
		[JWT_TOKEN, test_data1, test_data2, test_uri1, test_uri2, &auth_data_sent, this](
			http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();

			auto result = exp_req.value()->MakeResponse();
			ASSERT_TRUE(result);
			auto resp = result.value();

			auto req = exp_req.value();
			if (req->GetPath() == auth_uri) {
				resp->SetStatusCodeAndMessage(200, "OK");
				resp->SetBodyReader(make_shared<io::StringReader>(JWT_TOKEN));
				resp->SetHeader("Content-Length", to_string(JWT_TOKEN.size()));
				resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
				auth_data_sent = true;
			} else if (req->GetPath() == test_uri1) {
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

	string private_key_path = "./private_key.pem";
	string server_certificate_path {};
	http::ClientConfig client_config = http::ClientConfig(server_certificate_path);
	auth::Authenticator authenticator {
		loop, client_config, server_url, private_key_path, test_device_identity_script};

	api::Client client1 {client_config, loop, authenticator};

	auto req1 = make_shared<http::OutgoingRequest>();
	req1->SetAddress(server_url + test_uri1);
	req1->SetMethod(http::Method::GET);

	auto received_body1 = make_shared<vector<uint8_t>>();
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
		[&body_handler_called1, test_data1, received_body1](
			http::ExpectedIncomingResponsePtr exp_resp) {
			EXPECT_FALSE(body_handler_called1);
			body_handler_called1 = true;

			EXPECT_EQ(common::StringFromByteVector(*received_body1), test_data1);
		});

	EXPECT_EQ(err, error::NoError) << "Unexpected error: " << err.message;

	api::Client client2 {client_config, loop, authenticator};

	auto req2 = make_shared<http::OutgoingRequest>();
	req2->SetAddress(server_url + test_uri2);
	req2->SetMethod(http::Method::GET);

	auto received_body2 = make_shared<vector<uint8_t>>();
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

	EXPECT_TRUE(header_handler_called1);
	EXPECT_TRUE(body_handler_called1);
	EXPECT_TRUE(header_handler_called2);
	EXPECT_TRUE(body_handler_called2);
}

TEST_F(APIClientTests, ClientReauthenticationTest) {
	const string JWT_TOKEN1 = "FOOBARJWTTOKEN1";
	const string JWT_TOKEN2 = "FOOBARJWTTOKEN2";
	const string test_data1 = "some testing data 1";
	const string test_data2 = "some testing data 2";
	const string test_uri1 = "/test/uri/1";
	const string test_uri2 = "/test/uri/2";

	TestEventLoop loop;

	// Setup a test server
	const string server_url {"http://127.0.0.1:" + TEST_PORT};
	http::ServerConfig server_config;
	http::Server server(server_config, loop);
	bool test_data1_sent = false;
	bool auth_data_sent_once = false;
	bool test_data2_requested = false;
	bool auth_data_sent_twice = false;
	size_t n_reqs_handled = 0;
	server.AsyncServeUrl(
		server_url,
		[JWT_TOKEN1,
		 JWT_TOKEN2,
		 test_uri1,
		 test_uri2,
		 &auth_data_sent_once,
		 &auth_data_sent_twice,
		 &test_data2_requested,
		 &test_data1_sent,
		 this](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			auto req = exp_req.value();
			if (!auth_data_sent_once) {
				EXPECT_EQ(req->GetPath(), auth_uri);
			} else if (auth_data_sent_once && !test_data1_sent) {
				EXPECT_EQ(req->GetPath(), test_uri1);
				auto ex_auth = req->GetHeader("Authorization");
				ASSERT_TRUE(ex_auth);
				EXPECT_EQ(ex_auth.value(), "Bearer " + JWT_TOKEN1);
			} else if (!auth_data_sent_twice && !test_data2_requested) {
				EXPECT_EQ(req->GetPath(), test_uri2);
				auto ex_auth = req->GetHeader("Authorization");
				ASSERT_TRUE(ex_auth);
				EXPECT_EQ(ex_auth.value(), "Bearer " + JWT_TOKEN1);
			} else if (!auth_data_sent_twice && test_data2_requested) {
				EXPECT_EQ(req->GetPath(), auth_uri);
			} else if (auth_data_sent_twice) {
				EXPECT_EQ(req->GetPath(), test_uri2);
				auto ex_auth = req->GetHeader("Authorization");
				ASSERT_TRUE(ex_auth);
				EXPECT_EQ(ex_auth.value(), "Bearer " + JWT_TOKEN2);
			} else {
				// all situations should be covered above
				ASSERT_TRUE(false);
			}
		},
		[JWT_TOKEN1,
		 JWT_TOKEN2,
		 test_uri1,
		 test_uri2,
		 test_data1,
		 test_data2,
		 &auth_data_sent_once,
		 &auth_data_sent_twice,
		 &test_data1_sent,
		 &test_data2_requested,
		 &n_reqs_handled,
		 this](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();

			auto result = exp_req.value()->MakeResponse();
			ASSERT_TRUE(result);
			auto resp = result.value();

			auto req = exp_req.value();
			if (req->GetPath() == auth_uri) {
				resp->SetStatusCodeAndMessage(200, "OK");
				if (!auth_data_sent_once) {
					resp->SetBodyReader(make_shared<io::StringReader>(JWT_TOKEN1));
					resp->SetHeader("Content-Length", to_string(JWT_TOKEN1.size()));
					auth_data_sent_once = true;
				} else {
					resp->SetBodyReader(make_shared<io::StringReader>(JWT_TOKEN2));
					resp->SetHeader("Content-Length", to_string(JWT_TOKEN2.size()));
					auth_data_sent_twice = true;
				}
			} else if (auth_data_sent_once && !test_data1_sent) {
				resp->SetStatusCodeAndMessage(200, "OK");
				resp->SetBodyReader(make_shared<io::StringReader>(test_data1));
				resp->SetHeader("Content-Length", to_string(test_data1.size()));
				test_data1_sent = true;
			} else if (auth_data_sent_once && test_data1_sent && !auth_data_sent_twice) {
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
			n_reqs_handled++;
			resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
		});

	string private_key_path = "./private_key.pem";
	string server_certificate_path {};
	http::ClientConfig client_config = http::ClientConfig(server_certificate_path);
	auth::Authenticator authenticator {
		loop, client_config, server_url, private_key_path, test_device_identity_script};

	api::Client client {client_config, loop, authenticator};

	auto req1 = make_shared<http::OutgoingRequest>();
	req1->SetAddress(server_url + test_uri1);
	req1->SetMethod(http::Method::GET);

	auto received_body1 = make_shared<vector<uint8_t>>();
	bool header_handler_called1 = false;
	bool body_handler_called1 = false;

	auto req2 = make_shared<http::OutgoingRequest>();
	req2->SetAddress(server_url + test_uri2);
	req2->SetMethod(http::Method::GET);

	auto received_body2 = make_shared<vector<uint8_t>>();
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

	// The client should:
	// 1. request a new token because it has none
	// 2. request test_data1 at test_uri1
	// 3. request test_data2 at test_uri2 but get 401
	// 4. request a new token
	// 5. request test_data2 at test_uri2
	EXPECT_EQ(n_reqs_handled, 5);

	EXPECT_TRUE(header_handler_called1);
	EXPECT_TRUE(body_handler_called1);
	EXPECT_TRUE(header_handler_called2);
	EXPECT_TRUE(body_handler_called2);
}

TEST_F(APIClientTests, ClientEarlyAuthErrorTest) {
	const string test_uri = "/test/uri";

	TestEventLoop loop;

	// Setup a test server
	const string server_url {"http://127.0.0.1:" + TEST_PORT};
	http::ServerConfig server_config;
	http::Server server(server_config, loop);
	bool auth_error_sent = false;
	const string error_response_data =
		R"({"error": "Ran out of memory", "response-id": "some id here"})";
	server.AsyncServeUrl(
		server_url,
		[this](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			auto req = exp_req.value();
			EXPECT_EQ(req->GetPath(), auth_uri);
		},
		[&auth_error_sent, error_response_data, this](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			EXPECT_FALSE(auth_error_sent);

			auto result = exp_req.value()->MakeResponse();
			ASSERT_TRUE(result);
			auto resp = result.value();

			auto req = exp_req.value();
			EXPECT_EQ(req->GetPath(), auth_uri);
			resp->SetStatusCodeAndMessage(501, "Internal server error");
			resp->SetBodyReader(make_shared<io::StringReader>(error_response_data));
			resp->SetHeader("Content-Length", to_string(error_response_data.size()));
			resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
			auth_error_sent = true;
		});

	string private_key_path = "./private_key.pem";
	string server_certificate_path {};
	http::ClientConfig client_config = http::ClientConfig(server_certificate_path);
	auth::Authenticator authenticator {
		loop, client_config, server_url, private_key_path, test_device_identity_script};

	api::Client client {client_config, loop, authenticator};

	auto req = make_shared<http::OutgoingRequest>();
	req->SetAddress(server_url + test_uri);
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
}

TEST_F(APIClientTests, ClientReauthenticationFailureTest) {
	const string JWT_TOKEN1 = "FOOBARJWTTOKEN1";
	const string test_data1 = "some testing data 1";
	const string test_uri1 = "/test/uri/1";
	const string test_uri2 = "/test/uri/2";

	TestEventLoop loop;

	// Setup a test server
	const string server_url {"http://127.0.0.1:" + TEST_PORT};
	http::ServerConfig server_config;
	http::Server server(server_config, loop);
	const string error_response_data =
		R"({"error": "Ran out of memory", "response-id": "some id here"})";
	bool test_data1_sent = false;
	bool auth_data_sent = false;
	bool auth_error_sent = false;
	bool test_data2_requested = false;
	size_t n_reqs_handled = 0;
	server.AsyncServeUrl(
		server_url,
		[JWT_TOKEN1,
		 test_uri1,
		 test_uri2,
		 &auth_data_sent,
		 &auth_error_sent,
		 &test_data2_requested,
		 &test_data1_sent,
		 this](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			auto req = exp_req.value();
			if (!auth_data_sent) {
				EXPECT_EQ(req->GetPath(), auth_uri);
			} else if (auth_data_sent && !test_data1_sent) {
				EXPECT_EQ(req->GetPath(), test_uri1);
				auto ex_auth = req->GetHeader("Authorization");
				ASSERT_TRUE(ex_auth);
				EXPECT_EQ(ex_auth.value(), "Bearer " + JWT_TOKEN1);
			} else if (!auth_error_sent && !test_data2_requested) {
				EXPECT_EQ(req->GetPath(), test_uri2);
				auto ex_auth = req->GetHeader("Authorization");
				ASSERT_TRUE(ex_auth);
				EXPECT_EQ(ex_auth.value(), "Bearer " + JWT_TOKEN1);
			} else if (!auth_error_sent && test_data2_requested) {
				EXPECT_EQ(req->GetPath(), auth_uri);
			} else {
				// all situations should be covered above
				ASSERT_TRUE(false);
			}
		},
		[JWT_TOKEN1,
		 test_uri1,
		 test_uri2,
		 test_data1,
		 error_response_data,
		 &auth_data_sent,
		 &auth_error_sent,
		 &test_data1_sent,
		 &test_data2_requested,
		 &n_reqs_handled,
		 this](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();

			auto result = exp_req.value()->MakeResponse();
			ASSERT_TRUE(result);
			auto resp = result.value();

			auto req = exp_req.value();
			if (req->GetPath() == auth_uri) {
				if (!auth_data_sent) {
					resp->SetStatusCodeAndMessage(200, "OK");
					resp->SetBodyReader(make_shared<io::StringReader>(JWT_TOKEN1));
					resp->SetHeader("Content-Length", to_string(JWT_TOKEN1.size()));
					auth_data_sent = true;
				} else {
					resp->SetStatusCodeAndMessage(501, "Internal server error");
					resp->SetBodyReader(make_shared<io::StringReader>(error_response_data));
					resp->SetHeader("Content-Length", to_string(error_response_data.size()));
					auth_error_sent = true;
				}
			} else if (auth_data_sent && !test_data1_sent) {
				resp->SetStatusCodeAndMessage(200, "OK");
				resp->SetBodyReader(make_shared<io::StringReader>(test_data1));
				resp->SetHeader("Content-Length", to_string(test_data1.size()));
				test_data1_sent = true;
			} else if (auth_data_sent && test_data1_sent && !auth_error_sent) {
				// simulating expired token when requested data the second time
				EXPECT_EQ(req->GetPath(), test_uri2);
				resp->SetStatusCodeAndMessage(401, "Unauthorized");
				test_data2_requested = true;
			} else {
				// all cases should be covered above
				ASSERT_TRUE(false);
			}
			n_reqs_handled++;
			resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
		});

	string private_key_path = "./private_key.pem";
	string server_certificate_path {};
	http::ClientConfig client_config = http::ClientConfig(server_certificate_path);
	auth::Authenticator authenticator {
		loop, client_config, server_url, private_key_path, test_device_identity_script};

	api::Client client {client_config, loop, authenticator};

	auto req1 = make_shared<http::OutgoingRequest>();
	req1->SetAddress(server_url + test_uri1);
	req1->SetMethod(http::Method::GET);

	auto received_body1 = make_shared<vector<uint8_t>>();
	bool header_handler_called1 = false;
	bool body_handler_called1 = false;

	auto req2 = make_shared<http::OutgoingRequest>();
	req2->SetAddress(server_url + test_uri2);
	req2->SetMethod(http::Method::GET);

	bool header_handler_called2 = false;
	bool body_handler_called2 = false;

	events::Timer timer {loop};
	http::ResponseHandler header_handler2 =
		[&header_handler_called2, &timer, &loop](http::ExpectedIncomingResponsePtr exp_resp) {
			EXPECT_FALSE(header_handler_called2);
			header_handler_called2 = true;

			EXPECT_FALSE(exp_resp);

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

	// The client should:
	// 1. request a new token because it has none
	// 2. request test_data1 at test_uri1
	// 3. request test_data2 at test_uri2 but get 401
	// 4. request a new token and handle the failure
	EXPECT_EQ(n_reqs_handled, 4);

	EXPECT_TRUE(header_handler_called1);
	EXPECT_TRUE(body_handler_called1);
	EXPECT_TRUE(header_handler_called2);
	EXPECT_FALSE(body_handler_called2);
}
