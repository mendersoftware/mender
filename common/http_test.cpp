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

#include <common/http.hpp>

#include <chrono>
#include <thread>

#include <gmock/gmock.h>
#include <gtest/gtest.h>

#include <common/events.hpp>
#include <common/events_io.hpp>
#include <common/http_test_helpers.hpp>
#include <common/testing.hpp>
#include <common/processes.hpp>

using namespace std;

namespace error = mender::common::error;
namespace events = mender::common::events;
namespace expected = mender::common::expected;
namespace http = mender::http;
namespace io = mender::common::io;
namespace mlog = mender::common::log;
namespace processes = mender::common::processes;
namespace mendertesting = mender::common::testing;

#define TEST_PORT "8001"

using TestEventLoop = mender::common::testing::TestEventLoop;

namespace mender {
namespace http {
class TestInspector {
public:
	static unordered_set<Server::StreamPtr> &GetStreams(Server &server) {
		return server.streams_;
	}
};

class TestServer : public Server {
public:
	using Server::Server;
	~TestServer() {
		// Check that no streams exist when we are destroyed. Streams can be a leak which is
		// hidden from the address sanitizer and valgrind, because it will actually be
		// cleaned up as part of the server destruction. However, the list should already be
		// empty before we get here, otherwise it's a sign that streams are
		// accumulating. The size should always be one, the listening socket.
		EXPECT_EQ(TestInspector::GetStreams(*this).size(), 1);
	}
};
} // namespace http
} // namespace mender

TEST(URLTest, URLEncode) {
	auto ret = http::URLEncode("all-supported_so~no~change.expected");
	EXPECT_EQ(ret, "all-supported_so~no~change.expected");

	ret = http::URLEncode("spaces are bad");
	EXPECT_EQ(ret, "spaces%20are%20bad");

	ret = http::URLEncode("so/are/slashes");
	EXPECT_EQ(ret, "so%2Fare%2Fslashes");
}

void TestBasicRequestAndResponse() {
	TestEventLoop loop;

	bool server_hit_header = false;
	bool server_hit_body = false;
	bool client_hit_header = false;
	bool client_hit_body = false;

	http::ServerConfig server_config;
	http::TestServer server(server_config, loop);
	auto err = server.AsyncServeUrl(
		"http://127.0.0.1:" TEST_PORT,
		[&server_hit_header](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			server_hit_header = true;

			EXPECT_EQ(exp_req.value()->GetPath(), "/endpoint");
		},
		[&server_hit_body](http::ExpectedIncomingRequestPtr exp_req) {
			server_hit_body = true;
			ASSERT_TRUE(exp_req) << exp_req.error().String();

			auto result = exp_req.value()->MakeResponse();
			ASSERT_TRUE(result);
			auto resp = result.value();

			resp->SetStatusCodeAndMessage(200, "Success");
			resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
		});
	ASSERT_EQ(error::NoError, err);

	http::ClientConfig client_config;
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("http://127.0.0.1:" TEST_PORT "/endpoint");
	err = client.AsyncCall(
		req,
		[&client_hit_header](http::ExpectedIncomingResponsePtr exp_resp) {
			client_hit_header = true;
		},
		[&client_hit_body, &loop](http::ExpectedIncomingResponsePtr exp_resp) {
			client_hit_body = true;
			loop.Stop();
		});
	ASSERT_EQ(error::NoError, err);

	loop.Run();

	EXPECT_TRUE(server_hit_header);
	EXPECT_TRUE(server_hit_body);
	EXPECT_TRUE(client_hit_header);
	EXPECT_TRUE(client_hit_body);
}

TEST(HttpTest, TestBasicRequestAndResponse) {
	TestBasicRequestAndResponse();
}

TEST(HttpTest, TestBasicRequestAndResponseWithDebugLogs) {
	auto level = mlog::Level();
	mlog::SetLevel(mlog::LogLevel::Debug);

	// We don't actually test the output. This is mainly about getting some coverage and making
	// sure we don't have any crash bugs in there.

	TestBasicRequestAndResponse();

	mlog::SetLevel(level);
}

TEST(HttpTest, TestMissingResponse) {
	TestEventLoop loop;

	bool server_hit_header = false;
	bool server_hit_body = false;
	bool client_hit_header = false;

	http::ServerConfig server_config;
	http::TestServer server(server_config, loop);
	server.AsyncServeUrl(
		"http://127.0.0.1:" TEST_PORT,
		[&server_hit_header, &server](http::ExpectedIncomingRequestPtr exp_req) {
			server_hit_header = true;

			// Should be two streams now, one active, and one listening.
			EXPECT_EQ(http::TestInspector::GetStreams(server).size(), 2);
		},
		[&server_hit_body, &server](http::ExpectedIncomingRequestPtr exp_req) {
			server_hit_body = true;
			ASSERT_TRUE(exp_req) << exp_req.error().String();

			// Should be two streams now, one active, and one listening.
			EXPECT_EQ(http::TestInspector::GetStreams(server).size(), 2);

			// Don't make a response.
		});

	http::ClientConfig client_config;
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("http://127.0.0.1:" TEST_PORT);
	client.AsyncCall(
		req,
		[&client_hit_header, &loop, &server](http::ExpectedIncomingResponsePtr exp_resp) {
			client_hit_header = true;
			loop.Stop();

			// Should get error here.
			ASSERT_FALSE(exp_resp);

			// Due to error, there should be exactly one stream, the listening one.
			EXPECT_EQ(http::TestInspector::GetStreams(server).size(), 1);
		},
		[](http::ExpectedIncomingResponsePtr exp_resp) {
			// Should never get here.
			FAIL();
		});

	loop.Run();

	EXPECT_TRUE(server_hit_header);
	EXPECT_TRUE(server_hit_body);
	EXPECT_TRUE(client_hit_header);

	// After the above, there should be exactly one stream, the listening one.
	EXPECT_EQ(http::TestInspector::GetStreams(server).size(), 1);
}

TEST(HttpTest, TestDestroyResponseBeforeUsingIt) {
	TestEventLoop loop;

	bool server_hit_header = false;
	bool server_hit_body = false;
	bool client_hit_header = false;

	http::ServerConfig server_config;
	http::TestServer server(server_config, loop);
	server.AsyncServeUrl(
		"http://127.0.0.1:" TEST_PORT,
		[&server_hit_header, &server](http::ExpectedIncomingRequestPtr exp_req) {
			server_hit_header = true;

			// Should be two streams now, one active, and one listening.
			EXPECT_EQ(http::TestInspector::GetStreams(server).size(), 2);
		},
		[&server_hit_body, &server](http::ExpectedIncomingRequestPtr exp_req) {
			server_hit_body = true;
			ASSERT_TRUE(exp_req) << exp_req.error().String();

			// Should be two streams now, one active, and one listening.
			EXPECT_EQ(http::TestInspector::GetStreams(server).size(), 2);

			auto result = exp_req.value()->MakeResponse();
			ASSERT_TRUE(result);
			// Let it go out of scope instead of using it.
		});

	http::ClientConfig client_config;
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("http://127.0.0.1:" TEST_PORT);
	client.AsyncCall(
		req,
		[&client_hit_header, &loop, &server](http::ExpectedIncomingResponsePtr exp_resp) {
			client_hit_header = true;
			loop.Stop();

			// Should get error here.
			ASSERT_FALSE(exp_resp);

			// Due to error, there should be exactly one stream, the listening one.
			EXPECT_EQ(http::TestInspector::GetStreams(server).size(), 1);
		},
		[](http::ExpectedIncomingResponsePtr exp_resp) {
			// Should never get here.
			FAIL();
		});

	loop.Run();

	EXPECT_TRUE(server_hit_header);
	EXPECT_TRUE(server_hit_body);
	EXPECT_TRUE(client_hit_header);

	// After the above, there should be exactly one stream, the listening one.
	EXPECT_EQ(http::TestInspector::GetStreams(server).size(), 1);
}

void TestHeaders() {
	TestEventLoop loop;

	bool server_hit_header = false;
	bool server_hit_body = false;
	bool client_hit_header = false;
	bool client_hit_body = false;

	http::ServerConfig server_config;
	http::TestServer server(server_config, loop);
	server.AsyncServeUrl(
		"http://127.0.0.1:" TEST_PORT,
		[&server_hit_header](http::ExpectedIncomingRequestPtr exp_req) {
			server_hit_header = true;
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			auto req = exp_req.value();

			EXPECT_FALSE(req->GetHeader("no-such-header"));

			ASSERT_TRUE(req->GetHeader("X-MyrequestHeader"));
			EXPECT_EQ(req->GetHeader("X-MyrequestHeader").value(), "header_value");
		},
		[&server_hit_body](http::ExpectedIncomingRequestPtr exp_req) {
			server_hit_body = true;
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			auto req = exp_req.value();

			EXPECT_FALSE(req->GetHeader("no-such-header"));

			ASSERT_TRUE(req->GetHeader("X-MyrequestHeader"));
			EXPECT_EQ(req->GetHeader("X-MyrequestHeader").value(), "header_value");

			auto exp_resp = req->MakeResponse();
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();
			auto resp = exp_resp.value();

			resp->SetStatusCodeAndMessage(200, "Success");
			resp->SetHeader("X-MyresponseHeader", "another_header_value");
			resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
		});

	http::ClientConfig client_config;
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("http://127.0.0.1:" TEST_PORT);
	// Note different case from above. This should work.
	req->SetHeader("x-myrequestheader", "header_value");
	client.AsyncCall(
		req,
		[&client_hit_header](http::ExpectedIncomingResponsePtr exp_resp) {
			client_hit_header = true;
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();
			auto resp = exp_resp.value();

			EXPECT_FALSE(resp->GetHeader("no-such-header"));

			ASSERT_TRUE(resp->GetHeader("x-myresponseheader"));
			EXPECT_EQ(resp->GetHeader("x-myresponseheader").value(), "another_header_value");
		},
		[&client_hit_body, &loop](http::ExpectedIncomingResponsePtr exp_resp) {
			client_hit_body = true;
			loop.Stop();
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();
			auto resp = exp_resp.value();

			EXPECT_FALSE(resp->GetHeader("no-such-header"));

			ASSERT_TRUE(resp->GetHeader("x-myresponseheader"));
			EXPECT_EQ(resp->GetHeader("x-myresponseheader").value(), "another_header_value");
		});

	loop.Run();

	EXPECT_TRUE(server_hit_header);
	EXPECT_TRUE(server_hit_body);
	EXPECT_TRUE(client_hit_header);
	EXPECT_TRUE(client_hit_body);
}

TEST(HttpTest, TestHeaders) {
	TestHeaders();
}

TEST(HttpTest, TestHeadersWithDebugLogs) {
	auto level = mlog::Level();
	mlog::SetLevel(mlog::LogLevel::Debug);

	// We don't actually test the output. This is mainly about getting some coverage and making
	// sure we don't have any crash bugs in there.

	TestHeaders();

	mlog::SetLevel(level);
}

TEST(HttpTest, TestMultipleSimultaneousConnections) {
	// Start one request, and when it has been received, start a second one and finish it
	// completely before completing the first one.

	TestEventLoop loop;

	http::ClientConfig client_config;

	http::Client client1(client_config, loop);
	auto req1 = make_shared<http::OutgoingRequest>();
	req1->SetMethod(http::Method::GET);
	req1->SetAddress("http://127.0.0.1:" TEST_PORT);
	req1->SetHeader("X-WhichRequest", "1");
	client1.AsyncCall(
		req1,
		[](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();
		},
		[&loop](http::ExpectedIncomingResponsePtr exp_resp) {
			loop.Stop();
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();
			auto resp = exp_resp.value();

			ASSERT_TRUE(resp->GetHeader("X-WhichResponse"));
			EXPECT_EQ(resp->GetHeader("X-WhichResponse").value(), "1");

			// Finished the first request is the last thing to happen, so stop loop now.
			loop.Stop();
		});
	http::OutgoingResponsePtr client1_response;

	http::Client client2(client_config, loop);
	auto req2 = make_shared<http::OutgoingRequest>();
	req2->SetMethod(http::Method::GET);
	req2->SetAddress("http://127.0.0.1:" TEST_PORT);
	req2->SetHeader("X-WhichRequest", "2");
	auto initiate_client2 = [&client1_response, &client2, &req2]() {
		client2.AsyncCall(
			req2,
			[](http::ExpectedIncomingResponsePtr exp_resp) {
				ASSERT_TRUE(exp_resp) << exp_resp.error().String();
			},
			[&client1_response](http::ExpectedIncomingResponsePtr exp_resp) {
				ASSERT_TRUE(exp_resp) << exp_resp.error().String();
				auto resp = exp_resp.value();

				ASSERT_TRUE(resp->GetHeader("X-WhichResponse"));
				EXPECT_EQ(resp->GetHeader("X-WhichResponse").value(), "2");

				// Finish the first request.
				ASSERT_TRUE(client1_response);
				client1_response->AsyncReply(
					[](error::Error err) { ASSERT_EQ(error::NoError, err); });
			});
	};

	http::ServerConfig server_config;
	http::Server server(server_config, loop);
	server.AsyncServeUrl(
		"http://127.0.0.1:" TEST_PORT,
		[](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
		},
		[&client1_response, &initiate_client2](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			auto req = exp_req.value();

			ASSERT_TRUE(req->GetHeader("X-WhichRequest"));
			if (req->GetHeader("X-WhichRequest").value() == "1") {
				// Start the response, but don't complete it now.
				auto exp_resp = req->MakeResponse();
				ASSERT_TRUE(exp_resp) << exp_resp.error().String();
				client1_response = exp_resp.value();
				client1_response->SetStatusCodeAndMessage(200, "Success");
				client1_response->SetHeader("X-WhichResponse", "1");

				initiate_client2();
			} else if (req->GetHeader("X-WhichRequest").value() == "2") {
				// Complete this response.
				auto exp_resp = req->MakeResponse();
				ASSERT_TRUE(exp_resp) << exp_resp.error().String();
				auto resp = exp_resp.value();

				resp->SetStatusCodeAndMessage(200, "Success");
				resp->SetHeader("X-WhichResponse", "2");
				resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
			} else {
				FAIL() << "Unexpected X-WhichRequest header";
			}
		});

	loop.Run();
}

TEST(HttpTest, TestRequestBody) {
	TestEventLoop loop;

	http::ServerConfig server_config;
	http::TestServer server(server_config, loop);
	vector<uint8_t> received_body;
	server.AsyncServeUrl(
		"http://127.0.0.1:" TEST_PORT,
		[&received_body](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			auto req = exp_req.value();

			auto content_length = req->GetHeader("Content-Length");
			ASSERT_TRUE(content_length);
			ASSERT_EQ(content_length.value(), to_string(BodyOfXes::TARGET_BODY_SIZE));

			auto body_writer = make_shared<io::ByteWriter>(received_body);
			body_writer->SetUnlimited(true);
			req->SetBodyWriter(body_writer);
		},
		[&received_body](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();

			vector<uint8_t> expected_body;
			io::ByteWriter expected_writer(expected_body);
			expected_writer.SetUnlimited(true);
			io::Copy(expected_writer, *make_shared<BodyOfXes>());

			ASSERT_EQ(received_body.size(), expected_body.size());
			EXPECT_EQ(received_body, expected_body)
				<< "Body not received correctly. Difference at index "
					   + to_string(
						   mismatch(
							   received_body.begin(), received_body.end(), expected_body.begin())
							   .first
						   - received_body.begin());

			auto result = exp_req.value()->MakeResponse();
			ASSERT_TRUE(result);
			auto resp = result.value();

			resp->SetStatusCodeAndMessage(200, "Success");
			resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
		});

	http::ClientConfig client_config;
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("http://127.0.0.1:" TEST_PORT);
	req->SetHeader("Content-Length", to_string(BodyOfXes::TARGET_BODY_SIZE));
	req->SetBodyGenerator([]() -> io::ExpectedReaderPtr { return make_shared<BodyOfXes>(); });
	client.AsyncCall(
		req,
		[](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();
		},
		[&loop](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();

			loop.Stop();
		});

	loop.Run();
}

TEST(HttpTest, TestMissingRequestBody) {
	TestEventLoop loop;

	http::ServerConfig server_config;
	http::TestServer server(server_config, loop);
	vector<uint8_t> received_body;
	server.AsyncServeUrl(
		"http://127.0.0.1:" TEST_PORT,
		[&received_body](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			auto req = exp_req.value();

			auto content_length = req->GetHeader("Content-Length");
			ASSERT_TRUE(content_length);
			ASSERT_EQ(content_length.value(), to_string(BodyOfXes::TARGET_BODY_SIZE));

			auto body_writer = make_shared<io::ByteWriter>(received_body);
			body_writer->SetUnlimited(true);
			req->SetBodyWriter(body_writer, http::BodyWriterErrorMode::KeepAlive);
		},
		[&loop](http::ExpectedIncomingRequestPtr exp_req) {
			// Should get error here because the stream as been terminated below.
			EXPECT_FALSE(exp_req);
			EXPECT_THAT(exp_req.error().String(), ::testing::HasSubstr("partial"));

			loop.Stop();
		});

	http::ClientConfig client_config;
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("http://127.0.0.1:" TEST_PORT);
	req->SetHeader("Content-Length", to_string(BodyOfXes::TARGET_BODY_SIZE));
	client.AsyncCall(
		req,
		[](http::ExpectedIncomingResponsePtr exp_resp) {
			EXPECT_FALSE(exp_resp);
			EXPECT_EQ(exp_resp.error().code, http::MakeError(http::BodyMissingError, "").code);
		},
		[](http::ExpectedIncomingResponsePtr exp_resp) { FAIL() << "Should not get here."; });

	loop.Run();
}

TEST(HttpTest, TestResponseBody) {
	TestEventLoop loop;

	http::ServerConfig server_config;
	http::TestServer server(server_config, loop);
	vector<uint8_t> received_body;
	server.AsyncServeUrl(
		"http://127.0.0.1:" TEST_PORT,
		[](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
		},
		[&loop](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();

			auto result = exp_req.value()->MakeResponse();
			ASSERT_TRUE(result);
			auto resp = result.value();

			resp->SetHeader("Content-Length", to_string(BodyOfXes::TARGET_BODY_SIZE));
			resp->SetBodyReader(make_shared<BodyOfXes>());
			resp->SetStatusCodeAndMessage(200, "Success");
			resp->AsyncReply([&loop](error::Error err) {
				ASSERT_EQ(error::NoError, err);
				loop.Stop();
			});
		});

	http::ClientConfig client_config;
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("http://127.0.0.1:" TEST_PORT);
	client.AsyncCall(
		req,
		[&received_body](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();
			auto resp = exp_resp.value();

			auto content_length = resp->GetHeader("Content-Length");
			ASSERT_TRUE(content_length);
			ASSERT_EQ(content_length.value(), to_string(BodyOfXes::TARGET_BODY_SIZE));

			auto body_writer = make_shared<io::ByteWriter>(received_body);
			body_writer->SetUnlimited(true);
			resp->SetBodyWriter(body_writer);
		},
		[&received_body](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();

			vector<uint8_t> expected_body;
			io::ByteWriter expected_writer(expected_body);
			expected_writer.SetUnlimited(true);
			io::Copy(expected_writer, *make_shared<BodyOfXes>());

			ASSERT_EQ(received_body.size(), expected_body.size());
			EXPECT_EQ(received_body, expected_body)
				<< "Body not received correctly. Difference at index "
					   + to_string(
						   mismatch(
							   received_body.begin(), received_body.end(), expected_body.begin())
							   .first
						   - received_body.begin());
		});

	loop.Run();
}

TEST(HttpTest, TestMissingResponseBody) {
	TestEventLoop loop;

	http::ServerConfig server_config;
	http::TestServer server(server_config, loop);
	vector<uint8_t> received_body;
	server.AsyncServeUrl(
		"http://127.0.0.1:" TEST_PORT,
		[](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
		},
		[](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();

			auto result = exp_req.value()->MakeResponse();
			ASSERT_TRUE(result);
			auto resp = result.value();

			resp->SetHeader("Content-Length", to_string(BodyOfXes::TARGET_BODY_SIZE));
			resp->SetStatusCodeAndMessage(200, "Success");
			resp->AsyncReply([](error::Error err) {
				EXPECT_NE(error::NoError, err);
				EXPECT_EQ(err.code, http::MakeError(http::BodyMissingError, "").code);
			});
		});

	http::ClientConfig client_config;
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("http://127.0.0.1:" TEST_PORT);
	client.AsyncCall(
		req,
		[&received_body](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();
			auto resp = exp_resp.value();

			auto content_length = resp->GetHeader("Content-Length");
			ASSERT_TRUE(content_length);
			ASSERT_EQ(content_length.value(), to_string(BodyOfXes::TARGET_BODY_SIZE));

			auto body_writer = make_shared<io::ByteWriter>(received_body);
			body_writer->SetUnlimited(true);
			resp->SetBodyWriter(body_writer);
		},
		[&loop](http::ExpectedIncomingResponsePtr exp_resp) {
			// Should be failure because we terminate the stream due to missing body above.
			ASSERT_FALSE(exp_resp);

			loop.Stop();
		});

	loop.Run();
}

TEST(HttpTest, TestShortResponseBody) {
	TestEventLoop loop;

	http::ServerConfig server_config;
	http::TestServer server(server_config, loop);
	vector<uint8_t> received_body;
	server.AsyncServeUrl(
		"http://127.0.0.1:" TEST_PORT,
		[](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
		},
		[](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();

			auto result = exp_req.value()->MakeResponse();
			ASSERT_TRUE(result);
			auto resp = result.value();

			// Note off-by-one content-length.
			resp->SetHeader("Content-Length", to_string(BodyOfXes::TARGET_BODY_SIZE + 1));
			resp->SetBodyReader(make_shared<BodyOfXes>());
			resp->SetStatusCodeAndMessage(200, "Success");
			resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
		});

	http::ClientConfig client_config;
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("http://127.0.0.1:" TEST_PORT);
	client.AsyncCall(
		req,
		[&received_body](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();
			auto resp = exp_resp.value();

			auto content_length = resp->GetHeader("Content-Length");
			ASSERT_TRUE(content_length);
			ASSERT_EQ(content_length.value(), to_string(BodyOfXes::TARGET_BODY_SIZE + 1));

			auto body_writer = make_shared<io::ByteWriter>(received_body);
			body_writer->SetUnlimited(true);
			resp->SetBodyWriter(body_writer, http::BodyWriterErrorMode::KeepAlive);
		},
		[&loop](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_FALSE(exp_resp);
			EXPECT_THAT(exp_resp.error().String(), testing::HasSubstr("partial message"));

			loop.Stop();
		});

	loop.Run();
}

TEST(HttpTest, TestHttpStatus) {
	TestEventLoop loop;

	http::ServerConfig server_config;
	http::TestServer server(server_config, loop);
	server.AsyncServeUrl(
		"http://127.0.0.1:" TEST_PORT,
		[](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
		},
		[](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();

			auto result = exp_req.value()->MakeResponse();
			ASSERT_TRUE(result);
			auto resp = result.value();

			resp->SetStatusCodeAndMessage(204, "No artifact for you, my friend");
			resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
		});

	http::ClientConfig client_config;
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("http://127.0.0.1:" TEST_PORT);
	client.AsyncCall(
		req,
		[](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();
			auto resp = exp_resp.value();

			EXPECT_EQ(resp->GetStatusCode(), 204);
			EXPECT_EQ(resp->GetStatusMessage(), "No artifact for you, my friend");
		},
		[&loop](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();
			auto resp = exp_resp.value();

			EXPECT_EQ(resp->GetStatusCode(), 204);
			EXPECT_EQ(resp->GetStatusMessage(), "No artifact for you, my friend");

			loop.Stop();
		});

	loop.Run();
}

TEST(HttpTest, TestUnsupportedRequestBody) {
	TestEventLoop loop;

	http::ServerConfig server_config;
	http::TestServer server(server_config, loop);
	server.AsyncServeUrl(
		"http://127.0.0.1:" TEST_PORT,
		[](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
		},
		[](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_FALSE(exp_req);

			EXPECT_EQ(exp_req.error().code, http::MakeError(http::UnsupportedBodyType, "").code);
		});

	http::ClientConfig client_config;
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("http://127.0.0.1:" TEST_PORT);
	req->SetHeader("Transfer-Encoding", "chunked");
	req->SetBodyGenerator([]() -> io::ExpectedReaderPtr { return make_shared<BodyOfXes>(); });
	client.AsyncCall(
		req,
		[&loop](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_FALSE(exp_resp);
			loop.Stop();
		},
		[](http::ExpectedIncomingResponsePtr exp_resp) { FAIL() << "Should not get here"; });

	loop.Run();
}

TEST(HttpTest, TestUnsupportedResponseBody) {
	TestEventLoop loop;

	http::ServerConfig server_config;
	http::TestServer server(server_config, loop);
	server.AsyncServeUrl(
		"http://127.0.0.1:" TEST_PORT,
		[](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
		},
		[](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();

			auto result = exp_req.value()->MakeResponse();
			ASSERT_TRUE(result);
			auto resp = result.value();

			resp->SetHeader("Transfer-Encoding", "chunked");
			resp->SetBodyReader(make_shared<BodyOfXes>());
			resp->SetStatusCodeAndMessage(200, "Success");
			resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
		});

	http::ClientConfig client_config;
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("http://127.0.0.1:" TEST_PORT);
	client.AsyncCall(
		req,
		[](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();
		},
		[&loop](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_FALSE(exp_resp);

			EXPECT_EQ(exp_resp.error().code, http::MakeError(http::UnsupportedBodyType, "").code);

			loop.Stop();
		});

	loop.Run();
}

TEST(HttpTest, TestServerUrlWithPath) {
	TestEventLoop loop;

	http::ServerConfig server_config;
	http::Server server(server_config, loop);
	auto err = server.AsyncServeUrl(
		"http://127.0.0.1:" TEST_PORT "/endpoint",
		[](http::ExpectedIncomingRequestPtr exp_req) {},
		[](http::ExpectedIncomingRequestPtr exp_req) {});

	ASSERT_NE(error::NoError, err);
	EXPECT_EQ(err.code, http::MakeError(http::InvalidUrlError, "").code);
}

TEST(HttpTest, TestClientCancelInHeaderHandler) {
	TestEventLoop loop;

	http::ServerConfig server_config;
	http::TestServer server(server_config, loop);
	server.AsyncServeUrl(
		"http://127.0.0.1:" TEST_PORT,
		[](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
		},
		[](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();

			auto result = exp_req.value()->MakeResponse();
			ASSERT_TRUE(result);
			auto resp = result.value();

			resp->SetHeader("Content-Length", to_string(BodyOfXes::TARGET_BODY_SIZE));
			resp->SetBodyReader(make_shared<BodyOfXes>());
			resp->SetStatusCodeAndMessage(200, "Success");
			resp->AsyncReply([](error::Error err) { ASSERT_NE(error::NoError, err); });
		});

	http::ClientConfig client_config;
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("http://127.0.0.1:" TEST_PORT "/endpoint");
	client.AsyncCall(
		req,
		[&client](http::ExpectedIncomingResponsePtr exp_resp) { client.Cancel(); },
		[](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_FALSE(exp_resp);
			EXPECT_EQ(exp_resp.error().code, make_error_condition(errc::operation_canceled));
		});

	events::Timer timer(loop);
	timer.AsyncWait(chrono::milliseconds(500), [&loop](error::Error err) { loop.Stop(); });

	loop.Run();
}

TEST(HttpTest, TestClientCancelInBodyHandler) {
	TestEventLoop loop;

	http::ServerConfig server_config;
	http::TestServer server(server_config, loop);
	server.AsyncServeUrl(
		"http://127.0.0.1:" TEST_PORT,
		[](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
		},
		[](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();

			auto result = exp_req.value()->MakeResponse();
			ASSERT_TRUE(result);
			auto resp = result.value();

			resp->SetStatusCodeAndMessage(200, "Success");
			resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
		});

	http::ClientConfig client_config;
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("http://127.0.0.1:" TEST_PORT "/endpoint");
	client.AsyncCall(
		req,
		[](http::ExpectedIncomingResponsePtr exp_resp) {},
		[&client](http::ExpectedIncomingResponsePtr exp_resp) { client.Cancel(); });

	events::Timer timer(loop);
	timer.AsyncWait(chrono::milliseconds(500), [&loop](error::Error err) { loop.Stop(); });

	loop.Run();
}

TEST(HttpTest, TestServerCancelInHeaderHandler) {
	TestEventLoop loop;

	http::ServerConfig server_config;
	http::TestServer server(server_config, loop);
	server.AsyncServeUrl(
		"http://127.0.0.1:" TEST_PORT,
		[](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			auto req = exp_req.value();
			req->Cancel();
		},
		[](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_FALSE(exp_req);
			EXPECT_EQ(exp_req.error().code, make_error_condition(errc::operation_canceled));
		});

	http::ClientConfig client_config;
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("http://127.0.0.1:" TEST_PORT "/endpoint");
	req->SetHeader("Content-Length", to_string(BodyOfXes::TARGET_BODY_SIZE));
	req->SetBodyGenerator([]() -> io::ExpectedReaderPtr { return make_shared<BodyOfXes>(); });
	client.AsyncCall(
		req,
		[](http::ExpectedIncomingResponsePtr exp_resp) {
			// Should be an error.
			ASSERT_FALSE(exp_resp);
		},
		[](http::ExpectedIncomingResponsePtr exp_resp) {
			FAIL() << "Should never get here since we cancelled.";
		});

	events::Timer timer(loop);
	timer.AsyncWait(chrono::milliseconds(500), [&loop](error::Error err) {
		// Should get here, without reaching the body handler first.

		loop.Stop();
	});

	loop.Run();
}

TEST(HttpTest, TestServerCancelInBodyHandler) {
	TestEventLoop loop;

	http::ServerConfig server_config;
	http::TestServer server(server_config, loop);
	server.AsyncServeUrl(
		"http://127.0.0.1:" TEST_PORT,
		[](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			exp_req.value()->SetBodyWriter(make_shared<io::Discard>());
		},
		[](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			auto req = exp_req.value();

			auto result = req->MakeResponse();
			ASSERT_TRUE(result);
			auto resp = result.value();

			resp->SetHeader("Content-Length", to_string(BodyOfXes::TARGET_BODY_SIZE));
			resp->SetBodyReader(make_shared<BodyOfXes>());
			resp->SetStatusCodeAndMessage(200, "Success");
			resp->AsyncReply([](error::Error err) {
				EXPECT_EQ(err.code, make_error_condition(errc::operation_canceled));
			});

			req->Cancel();
		});

	http::ClientConfig client_config;
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("http://127.0.0.1:" TEST_PORT "/endpoint");
	req->SetHeader("Content-Length", to_string(BodyOfXes::TARGET_BODY_SIZE));
	req->SetBodyGenerator([]() -> io::ExpectedReaderPtr { return make_shared<BodyOfXes>(); });
	bool got_error = false;
	client.AsyncCall(
		req,
		[&got_error](http::ExpectedIncomingResponsePtr exp_resp) {
			// It can fail in either the header or body handler, depending on how far it
			// got. Make sure that no handler is called after the error though.
			if (!exp_resp) {
				got_error = true;
			} else {
				exp_resp.value()->SetBodyWriter(make_shared<io::Discard>());
			}
		},
		[&got_error](http::ExpectedIncomingResponsePtr exp_resp) {
			// It can fail in either the header or body handler, depending on how far it
			// got. Make sure only one is called though.
			ASSERT_FALSE(got_error);
			// It should be an error
			if (!exp_resp) {
				got_error = true;
			} else {
				FAIL() << "Expected response to contain error.";
			}
		});

	events::Timer timer(loop);
	timer.AsyncWait(chrono::milliseconds(500), [&loop](error::Error err) {
		// Should get here, without reaching the body handler first.

		loop.Stop();
	});

	loop.Run();

	EXPECT_TRUE(got_error);
}

TEST(HttpTest, TestRequestNotReady) {
	TestEventLoop loop;

	http::ClientConfig client_config;
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	auto err = client.AsyncCall(
		req,
		[](http::ExpectedIncomingResponsePtr exp_resp) { FAIL() << "Should not get here."; },
		[](http::ExpectedIncomingResponsePtr exp_resp) { FAIL() << "Should not get here."; });

	EXPECT_NE(error::NoError, err);
}

TEST(HttpTest, TestRequestNoHandlers) {
	TestEventLoop loop;

	http::ClientConfig client_config;
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("http://127.0.0.1:" TEST_PORT "/endpoint");
	auto err = client.AsyncCall(
		req,
		function<void(http::ExpectedIncomingResponsePtr exp_resp)>(),
		function<void(http::ExpectedIncomingResponsePtr exp_resp)>());

	EXPECT_NE(error::NoError, err);
}

TEST(HttpTest, TestRequestInvalidProtocol) {
	TestEventLoop loop;

	http::ClientConfig client_config;
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	auto err = req->SetAddress("htt://127.0.0.1/endpoint");

	EXPECT_NE(error::NoError, err);
	EXPECT_EQ(err.code, make_error_condition(errc::protocol_not_supported));

	err = client.AsyncCall(
		req,
		[](http::ExpectedIncomingResponsePtr exp_resp) { FAIL() << "Should not get here."; },
		[](http::ExpectedIncomingResponsePtr exp_resp) { FAIL() << "Should not get here."; });

	EXPECT_NE(error::NoError, err);
	EXPECT_EQ(err.code, error::MakeError(error::ProgrammingError, "").code);
}

TEST(HttpTest, TestRequestInvalidProtocolWithPortNumber) {
	TestEventLoop loop;

	http::ClientConfig client_config;
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("htt://127.0.0.1:" TEST_PORT "/endpoint");
	auto err = client.AsyncCall(
		req,
		[](http::ExpectedIncomingResponsePtr exp_resp) { FAIL() << "Should not get here."; },
		[](http::ExpectedIncomingResponsePtr exp_resp) { FAIL() << "Should not get here."; });

	EXPECT_NE(error::NoError, err);
	EXPECT_EQ(err.code, make_error_condition(errc::protocol_not_supported));
}

TEST(HttpTest, TestTornDownStream) {
	TestEventLoop loop;

	http::OutgoingResponsePtr response;

	{
		http::ServerConfig server_config;
		http::Server server(server_config, loop);
		auto err = server.AsyncServeUrl(
			"http://127.0.0.1:" TEST_PORT,
			[](http::ExpectedIncomingRequestPtr exp_req) {
				ASSERT_TRUE(exp_req) << exp_req.error().String();
			},
			[&response](http::ExpectedIncomingRequestPtr exp_req) {
				ASSERT_TRUE(exp_req) << exp_req.error().String();

				auto result = exp_req.value()->MakeResponse();
				ASSERT_TRUE(result);
				response = result.value();

				response->SetStatusCodeAndMessage(200, "Success");
				// Do not call AsyncReply now, but later.
			});
		ASSERT_EQ(error::NoError, err);

		http::ClientConfig client_config;
		http::Client client(client_config, loop);
		auto req = make_shared<http::OutgoingRequest>();
		req->SetMethod(http::Method::GET);
		req->SetAddress("http://127.0.0.1:" TEST_PORT "/endpoint");
		err = client.AsyncCall(
			req,
			[](http::ExpectedIncomingResponsePtr exp_resp) {},
			[](http::ExpectedIncomingResponsePtr exp_resp) {});
		ASSERT_EQ(error::NoError, err);

		events::Timer timer(loop);
		timer.AsyncWait(chrono::milliseconds(500), [&loop](error::Error err) {
			// Quit the loop without finishing the response.

			loop.Stop();
		});

		loop.Run();
	}

	// Should be too late to use it now.
	auto err = response->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
	EXPECT_NE(error::NoError, err);
	EXPECT_EQ(err.code, http::MakeError(http::StreamCancelledError, "").code);
}

TEST(HttpTest, SerialRequestsWithSameObject) {
	TestEventLoop loop;

	int server_hit_header = 0;
	int server_hit_body = 0;
	bool client_hit1_header = false;
	bool client_hit1_body = false;
	bool client_hit2_header = false;
	bool client_hit2_body = false;

	http::ServerConfig server_config;
	http::TestServer server(server_config, loop);
	auto err = server.AsyncServeUrl(
		"http://127.0.0.1:" TEST_PORT,
		[&server_hit_header](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			server_hit_header++;

			EXPECT_EQ(exp_req.value()->GetPath(), "/endpoint");
		},
		[&server_hit_body](http::ExpectedIncomingRequestPtr exp_req) {
			server_hit_body++;
			ASSERT_TRUE(exp_req) << exp_req.error().String();

			auto result = exp_req.value()->MakeResponse();
			ASSERT_TRUE(result);
			auto resp = result.value();

			resp->SetStatusCodeAndMessage(200, "Success");
			resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
		});
	ASSERT_EQ(error::NoError, err);

	http::ClientConfig client_config;
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("http://127.0.0.1:" TEST_PORT "/endpoint");
	err = client.AsyncCall(
		req,
		[&](http::ExpectedIncomingResponsePtr exp_resp) { client_hit1_header = true; },
		[&](http::ExpectedIncomingResponsePtr exp_resp) {
			client_hit1_body = true;

			// Second request
			auto req = make_shared<http::OutgoingRequest>();
			req->SetMethod(http::Method::GET);
			req->SetAddress("http://127.0.0.1:" TEST_PORT "/endpoint");
			auto err = client.AsyncCall(
				req,
				[&](http::ExpectedIncomingResponsePtr exp_resp) { client_hit2_header = true; },
				[&](http::ExpectedIncomingResponsePtr exp_resp) {
					client_hit2_body = true;
					loop.Stop();
				});
			ASSERT_EQ(error::NoError, err);
		});
	ASSERT_EQ(error::NoError, err);

	loop.Run();

	EXPECT_EQ(server_hit_header, 2);
	EXPECT_EQ(server_hit_body, 2);
	EXPECT_TRUE(client_hit1_header);
	EXPECT_TRUE(client_hit1_body);
	EXPECT_TRUE(client_hit2_header);
	EXPECT_TRUE(client_hit2_body);
}

TEST(HttpTest, SerialRequestsWithSameObjectAfterCancel) {
	TestEventLoop loop;

	int server_hit_header = 0;
	int server_hit_body = 0;
	bool client_hit1_header = false;
	bool client_hit1_body = false;
	bool client_hit2_header = false;
	bool client_hit2_body = false;

	http::ServerConfig server_config;
	http::TestServer server(server_config, loop);
	auto err = server.AsyncServeUrl(
		"http://127.0.0.1:" TEST_PORT,
		[&server_hit_header](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			server_hit_header++;

			EXPECT_EQ(exp_req.value()->GetPath(), "/endpoint");
		},
		[&server_hit_body](http::ExpectedIncomingRequestPtr exp_req) {
			server_hit_body++;
			ASSERT_TRUE(exp_req) << exp_req.error().String();

			auto result = exp_req.value()->MakeResponse();
			ASSERT_TRUE(result);
			auto resp = result.value();

			resp->SetStatusCodeAndMessage(200, "Success");
			resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
		});
	ASSERT_EQ(error::NoError, err);

	http::ClientConfig client_config;
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("http://127.0.0.1:" TEST_PORT "/endpoint");
	err = client.AsyncCall(
		req,
		[&](http::ExpectedIncomingResponsePtr exp_resp) {
			client_hit1_header = true;

			client.Cancel();

			// Second request
			auto req = make_shared<http::OutgoingRequest>();
			req->SetMethod(http::Method::GET);
			req->SetAddress("http://127.0.0.1:" TEST_PORT "/endpoint");
			auto err = client.AsyncCall(
				req,
				[&](http::ExpectedIncomingResponsePtr exp_resp) { client_hit2_header = true; },
				[&](http::ExpectedIncomingResponsePtr exp_resp) {
					client_hit2_body = true;
					loop.Stop();
				});
			ASSERT_EQ(error::NoError, err);
		},
		[&](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_FALSE(exp_resp);
			EXPECT_EQ(exp_resp.error().code, make_error_condition(errc::operation_canceled));
			client_hit1_body = true;
		});
	ASSERT_EQ(error::NoError, err);

	loop.Run();

	EXPECT_EQ(server_hit_header, 2);
	EXPECT_EQ(server_hit_body, 2);
	EXPECT_TRUE(client_hit1_header);
	EXPECT_TRUE(client_hit1_body);
	EXPECT_TRUE(client_hit2_header);
	EXPECT_TRUE(client_hit2_body);
}

TEST(HttpTest, DestroyClientBeforeRequestComplete) {
	TestEventLoop loop;

	bool client_hit_header = false;
	bool client_hit_body = false;

	http::ClientConfig client_config;
	auto client = make_shared<http::Client>(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("http://google.com/");
	auto err = client->AsyncCall(
		req,
		[&client_hit_header](http::ExpectedIncomingResponsePtr exp_resp) {
			client_hit_header = true;
		},
		[&client_hit_body](http::ExpectedIncomingResponsePtr exp_resp) { client_hit_body = true; });
	ASSERT_EQ(error::NoError, err);

	client.reset();

	events::Timer timer(loop);
	timer.AsyncWait(chrono::milliseconds(500), [&loop](error::Error err) { loop.Stop(); });

	loop.Run();

	EXPECT_FALSE(client_hit_header);
	EXPECT_FALSE(client_hit_body);
}

TEST(HttpTest, TestAsyncBodyReaders) {
	TestEventLoop loop;

	http::ServerConfig server_config;
	http::TestServer server(server_config, loop);
	vector<uint8_t> received_body;
	vector<uint8_t> expected_body;
	io::ByteWriter expected_writer(expected_body);
	expected_writer.SetUnlimited(true);
	io::Copy(expected_writer, *make_shared<BodyOfXes>());
	server.AsyncServeUrl(
		"http://127.0.0.1:" TEST_PORT,
		[&received_body](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			auto req = exp_req.value();
			auto body_writer = make_shared<io::ByteWriter>(received_body);
			body_writer->SetUnlimited(true);
			req->SetBodyWriter(body_writer);
		},
		[&loop, &received_body, &expected_body](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();

			EXPECT_EQ(received_body, expected_body);
			// Reuse in response.
			received_body.clear();

			auto result = exp_req.value()->MakeResponse();
			ASSERT_TRUE(result);
			auto resp = result.value();

			resp->SetHeader("Content-Length", to_string(BodyOfXes::TARGET_BODY_SIZE));
			resp->SetAsyncBodyReader(
				make_shared<events::io::AsyncReaderFromReader>(loop, make_shared<BodyOfXes>()));
			resp->SetStatusCodeAndMessage(200, "Success");
			resp->AsyncReply([&loop](error::Error err) {
				ASSERT_EQ(error::NoError, err);
				loop.Stop();
			});
		});

	http::ClientConfig client_config;
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("http://127.0.0.1:" TEST_PORT);
	req->SetHeader("Content-Length", to_string(BodyOfXes::TARGET_BODY_SIZE));
	req->SetAsyncBodyGenerator([&loop]() {
		return make_shared<events::io::AsyncReaderFromReader>(loop, make_shared<BodyOfXes>());
	});
	client.AsyncCall(
		req,
		[&received_body](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();
			auto resp = exp_resp.value();

			auto content_length = resp->GetHeader("Content-Length");
			ASSERT_TRUE(content_length);
			ASSERT_EQ(content_length.value(), to_string(BodyOfXes::TARGET_BODY_SIZE));

			auto body_writer = make_shared<io::ByteWriter>(received_body);
			body_writer->SetUnlimited(true);
			resp->SetBodyWriter(body_writer);
		},
		[&received_body, &expected_body](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();

			ASSERT_EQ(received_body.size(), expected_body.size());
			EXPECT_EQ(received_body, expected_body)
				<< "Body not received correctly. Difference at index "
					   + to_string(
						   mismatch(
							   received_body.begin(), received_body.end(), expected_body.begin())
							   .first
						   - received_body.begin());
		});

	loop.Run();
}

TEST(HttpTest, TestResponseBodyReaderFailure) {
	TestEventLoop loop;

	http::ServerConfig server_config;
	http::TestServer server(server_config, loop);
	vector<uint8_t> received_body;
	server.AsyncServeUrl(
		"http://127.0.0.1:" TEST_PORT,
		[](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
		},
		[](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();

			auto result = exp_req.value()->MakeResponse();
			ASSERT_TRUE(result);
			auto resp = result.value();

			resp->SetHeader("Content-Length", to_string(BodyOfXes::TARGET_BODY_SIZE + 1));
			resp->SetBodyReader(make_shared<BodyOfXes>());
			resp->SetStatusCodeAndMessage(200, "Success");
			resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
		});

	http::ClientConfig client_config;
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("http://127.0.0.1:" TEST_PORT);
	vector<uint8_t> buf;
	// Use a weird buf size, just to iron out more corner cases.
	buf.resize(1235);
	bool got_read_success {false};
	bool got_read_error {false};
	client.AsyncCall(
		req,
		[&](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();
			auto resp = exp_resp.value();

			auto content_length = resp->GetHeader("Content-Length");
			ASSERT_TRUE(content_length);
			ASSERT_EQ(content_length.value(), to_string(BodyOfXes::TARGET_BODY_SIZE + 1));

			auto body_writer = make_shared<io::ByteWriter>(received_body);
			body_writer->SetUnlimited(true);
			auto reader = *client.MakeBodyAsyncReader(resp);
			// It should not be possible to make a second reader.
			EXPECT_FALSE(client.MakeBodyAsyncReader(resp));
			reader->RepeatedAsyncRead(
				buf.begin(),
				buf.end(),
				// Note in particular the capture of `reader`, to keep it alive.
				[&buf, reader, body_writer, &got_read_error, &got_read_success](
					io::ExpectedSize result) {
					if (!result) {
						EXPECT_THAT(result.error().String(), ::testing::HasSubstr("partial"));
						got_read_error = true;
						return io::Repeat::No;
					}
					if (result.value() == 0) {
						// Finished
						return io::Repeat::No;
					}
					got_read_success = true;
					body_writer->Write(buf.begin(), buf.begin() + result.value());
					return io::Repeat::Yes;
				});
		},
		[&loop](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_FALSE(exp_resp);
			ASSERT_THAT(exp_resp.error().String(), ::testing::HasSubstr("partial"));

			loop.Stop();
		});

	loop.Run();

	EXPECT_TRUE(got_read_success);
	EXPECT_TRUE(got_read_error);
}

TEST(HttpTest, TestRequestBodyReaderFailure) {
	TestEventLoop loop;

	http::ServerConfig server_config;
	http::TestServer server(server_config, loop);
	vector<uint8_t> received_body;
	vector<uint8_t> buf;
	// Use a weird buf size, just to iron out more corner cases.
	buf.resize(1235);
	bool got_read_success {false};
	bool got_read_error {false};
	server.AsyncServeUrl(
		"http://127.0.0.1:" TEST_PORT,
		[&](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			auto req = exp_req.value();
			auto body_writer = make_shared<io::ByteWriter>(received_body);
			body_writer->SetUnlimited(true);
			auto reader = *server.MakeBodyAsyncReader(req);
			// It should not be possible to make a second reader.
			EXPECT_FALSE(server.MakeBodyAsyncReader(req));
			reader->RepeatedAsyncRead(
				buf.begin(),
				buf.end(),
				// Note in particular the capture of `reader`, to keep it alive.
				[&buf, reader, body_writer, &got_read_error, &got_read_success](
					io::ExpectedSize result) {
					if (!result) {
						EXPECT_THAT(result.error().String(), ::testing::HasSubstr("partial"));
						got_read_error = true;
						return io::Repeat::No;
					}
					if (result.value() == 0) {
						// Finished
						return io::Repeat::No;
					}
					got_read_success = true;
					body_writer->Write(buf.begin(), buf.begin() + result.value());
					return io::Repeat::Yes;
				});
		},
		[&loop](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_FALSE(exp_req);
			loop.Stop();
		});

	http::ClientConfig client_config;
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("http://127.0.0.1:" TEST_PORT);
	req->SetHeader("Content-Length", to_string(BodyOfXes::TARGET_BODY_SIZE + 1));
	class ErrorAtEndReader : virtual public io::Reader {
	public:
		ErrorAtEndReader(io::ReaderPtr reader) :
			reader_ {reader} {
		}

		io::ExpectedSize Read(
			vector<uint8_t>::iterator start, vector<uint8_t>::iterator end) override {
			auto size = reader_->Read(start, end);
			// When reaching end, produce error instead.
			if (size && *size == 0) {
				return expected::unexpected(
					error::MakeError(error::GenericError, "Intential read error"));
			}

			return size;
		}

	private:
		io::ReaderPtr reader_;
	};
	req->SetBodyGenerator([]() { return make_shared<ErrorAtEndReader>(make_shared<BodyOfXes>()); });
	client.AsyncCall(
		req,
		[](http::ExpectedIncomingResponsePtr exp_resp) { ASSERT_FALSE(exp_resp); },
		[](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(false) << "Should never get here";
		});

	loop.Run();

	EXPECT_TRUE(got_read_success);
	EXPECT_TRUE(got_read_error);
}

TEST(HttpTest, TestRequestBodyIgnored) {
	TestEventLoop loop;

	http::ServerConfig server_config;
	http::TestServer server(server_config, loop);
	server.AsyncServeUrl(
		"http://127.0.0.1:" TEST_PORT,
		[](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
		},
		[&loop](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_FALSE(exp_req);
			EXPECT_EQ(exp_req.error().code, http::MakeError(http::BodyIgnoredError, "").code);
			loop.Stop();
		});

	http::ClientConfig client_config;
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("http://127.0.0.1:" TEST_PORT);
	req->SetHeader("Content-Length", to_string(BodyOfXes::TARGET_BODY_SIZE));
	req->SetBodyGenerator([]() { return make_shared<BodyOfXes>(); });
	client.AsyncCall(
		req,
		[](http::ExpectedIncomingResponsePtr exp_resp) { ASSERT_FALSE(exp_resp); },
		[](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(false) << "Should never get here";
		});

	loop.Run();
}

TEST(HttpTest, TestResponseBodyIgnored) {
	TestEventLoop loop;

	http::ServerConfig server_config;
	http::TestServer server(server_config, loop);
	server.AsyncServeUrl(
		"http://127.0.0.1:" TEST_PORT,
		[](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
		},
		[&loop](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();

			auto exp_resp = exp_req.value()->MakeResponse();
			ASSERT_TRUE(exp_resp);
			auto &resp = exp_resp.value();

			resp->SetHeader("Content-Length", to_string(BodyOfXes::TARGET_BODY_SIZE));
			resp->SetBodyReader(make_shared<BodyOfXes>());

			resp->AsyncReply([&loop](error::Error err) {
				EXPECT_NE(err, error::NoError);
				loop.Stop();
			});
		});

	http::ClientConfig client_config;
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("http://127.0.0.1:" TEST_PORT);
	client.AsyncCall(
		req,
		[](http::ExpectedIncomingResponsePtr exp_resp) { ASSERT_TRUE(exp_resp); },
		[](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_FALSE(exp_resp);
			EXPECT_EQ(exp_resp.error().code, http::MakeError(http::BodyIgnoredError, "").code);
		});

	loop.Run();
}

TEST(HttpsTest, CorrectSelfSignedCertificateSuccess) {
	TestEventLoop loop;

	bool client_hit_header {false};
	bool client_hit_body {false};

	mendertesting::TemporaryDirectory tmpdir;
	string script = R"(#! /bin/sh
	  exec openssl s_server -www )";
	script += " -key server.localhost.key";
	script += " -cert server.localhost.crt";
	script += " -accept " TEST_PORT;

	const string script_fname = tmpdir.Path() + "/test-script.sh";
	{
		std::ofstream os(script_fname.c_str(), std::ios::out);
		os << script;
	}
	int ret = chmod(script_fname.c_str(), S_IRUSR | S_IWUSR | S_IXUSR);
	ASSERT_EQ(ret, 0);
	processes::Process server({script_fname});
	auto err = server.Start();
	ASSERT_EQ(err, error::NoError);
	std::this_thread::sleep_for(std::chrono::seconds {1}); // Give the server a little time to setup

	http::ClientConfig client_config {"server.localhost.crt"};
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("https://localhost:" TEST_PORT "/index.html");
	err = client.AsyncCall(
		req,
		[&client_hit_header](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(exp_resp) << "Error message: " << exp_resp.error().String();
			auto resp = exp_resp.value();
			EXPECT_EQ(resp->GetStatusCode(), 200);
			EXPECT_EQ(resp->GetStatusMessage(), "ok");
			client_hit_header = true;
		},
		[&client_hit_body, &loop](http::ExpectedIncomingResponsePtr exp_resp) {
			client_hit_body = true;
			loop.Stop();
		});
	ASSERT_EQ(error::NoError, err);

	loop.Run();

	EXPECT_TRUE(client_hit_header);
	EXPECT_TRUE(client_hit_body);
}

TEST(HttpsTest, WrongSelfSignedCertificateError) {
	TestEventLoop loop;

	bool client_hit_header {false};
	bool client_hit_body {false};

	mendertesting::TemporaryDirectory tmpdir;
	string script = R"(#! /bin/sh
	  exec openssl s_server -www )";
	script += " -key server.localhost.key";
	script += " -cert server.localhost.crt";
	script += " -accept " TEST_PORT;

	const string script_fname = tmpdir.Path() + "/test-script.sh";
	{
		std::ofstream os(script_fname.c_str(), std::ios::out);
		os << script;
	}
	int ret = chmod(script_fname.c_str(), S_IRUSR | S_IWUSR | S_IXUSR);
	ASSERT_EQ(ret, 0);
	processes::Process server({script_fname});
	auto err = server.Start();
	ASSERT_EQ(err, error::NoError);
	std::this_thread::sleep_for(std::chrono::seconds {1}); // Give the server a little time to setup

	http::ClientConfig client_config {"server.wrong.crt"};
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("https://localhost:" TEST_PORT "/index.html");
	err = client.AsyncCall(
		req,
		[&client_hit_header, &loop](http::ExpectedIncomingResponsePtr exp_resp) {
			client_hit_header = true;
			ASSERT_FALSE(exp_resp);
			loop.Stop();
		},
		[&client_hit_body, &loop](http::ExpectedIncomingResponsePtr exp_resp) {
			client_hit_body = true; // This should never happen
			loop.Stop();
		});
	ASSERT_EQ(error::NoError, err);

	loop.Run();

	EXPECT_TRUE(client_hit_header);
	EXPECT_FALSE(client_hit_body);
}
TEST(HttpsTest, CorrectDefaultCertificateStoreVerification) {
	TestEventLoop loop;

	bool client_hit_header {false};
	bool client_hit_body {false};

	http::ClientConfig client_config {};
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("https://google.com");
	auto err = client.AsyncCall(
		req,
		[&client_hit_header](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(exp_resp) << "Error message: " << exp_resp.error().String();
			auto resp = exp_resp.value();
			EXPECT_EQ(resp->GetStatusCode(), 301);
			EXPECT_EQ(resp->GetStatusMessage(), "Moved Permanently");
			client_hit_header = true;
		},
		[&client_hit_body, &loop](http::ExpectedIncomingResponsePtr exp_resp) {
			client_hit_body = true;
			loop.Stop();
		});
	ASSERT_EQ(error::NoError, err);

	loop.Run();

	EXPECT_TRUE(client_hit_header);
	EXPECT_TRUE(client_hit_body);
}

TEST(HttpTest, ExponentialBackoff) {
	http::ExponentialBackoff::ExpectedInterval exp_interval;

	auto duration_fmt = [](chrono::milliseconds ms) { return to_string(ms.count()) + "ms"; };

	// Test with one minute maximum interval.
	{
		http::ExponentialBackoff backoff(chrono::minutes(1));
		exp_interval = backoff.NextInterval();
		ASSERT_TRUE(exp_interval) << exp_interval.error().String();
		EXPECT_EQ(exp_interval.value(), chrono::minutes(1)) << duration_fmt(exp_interval.value());

		exp_interval = backoff.NextInterval();
		ASSERT_TRUE(exp_interval) << exp_interval.error().String();
		EXPECT_EQ(exp_interval.value(), chrono::minutes(1)) << duration_fmt(exp_interval.value());

		exp_interval = backoff.NextInterval();
		ASSERT_TRUE(exp_interval) << exp_interval.error().String();
		EXPECT_EQ(exp_interval.value(), chrono::minutes(1)) << duration_fmt(exp_interval.value());

		exp_interval = backoff.NextInterval();
		ASSERT_FALSE(exp_interval);
		EXPECT_EQ(exp_interval.error().code, http::MakeError(http::MaxRetryError, "").code);

		exp_interval = backoff.NextInterval();
		ASSERT_FALSE(exp_interval);
		EXPECT_EQ(exp_interval.error().code, http::MakeError(http::MaxRetryError, "").code);
	}

	// Test with two minute maximum interval.
	{
		http::ExponentialBackoff backoff(chrono::minutes(2));
		backoff.SetIteration(5);
		exp_interval = backoff.NextInterval();
		ASSERT_TRUE(exp_interval) << exp_interval.error().String();
		EXPECT_EQ(exp_interval.value(), chrono::minutes(2)) << duration_fmt(exp_interval.value());

		exp_interval = backoff.NextInterval();
		ASSERT_FALSE(exp_interval);
		EXPECT_EQ(exp_interval.error().code, http::MakeError(http::MaxRetryError, "").code);
	}

	// Test with 10 minute maximum interval.
	{
		http::ExponentialBackoff backoff(chrono::minutes(10));
		backoff.SetIteration(11);
		exp_interval = backoff.NextInterval();
		ASSERT_TRUE(exp_interval) << exp_interval.error().String();
		EXPECT_EQ(exp_interval.value(), chrono::minutes(8)) << duration_fmt(exp_interval.value());

		exp_interval = backoff.NextInterval();
		ASSERT_TRUE(exp_interval) << exp_interval.error().String();
		EXPECT_EQ(exp_interval.value(), chrono::minutes(10)) << duration_fmt(exp_interval.value());

		backoff.SetIteration(14);
		exp_interval = backoff.NextInterval();
		ASSERT_TRUE(exp_interval) << exp_interval.error().String();
		EXPECT_EQ(exp_interval.value(), chrono::minutes(10)) << duration_fmt(exp_interval.value());

		exp_interval = backoff.NextInterval();
		ASSERT_FALSE(exp_interval);
		EXPECT_EQ(exp_interval.error().code, http::MakeError(http::MaxRetryError, "").code);
	}

	{
		// Test with one second maximum interval, which should revert to minutes (smallest
		// unit).
		http::ExponentialBackoff backoff(chrono::seconds(1));
		exp_interval = backoff.NextInterval();
		ASSERT_TRUE(exp_interval) << exp_interval.error().String();
		EXPECT_EQ(exp_interval.value(), chrono::minutes(1)) << duration_fmt(exp_interval.value());

		exp_interval = backoff.NextInterval();
		ASSERT_TRUE(exp_interval) << exp_interval.error().String();
		EXPECT_EQ(exp_interval.value(), chrono::minutes(1)) << duration_fmt(exp_interval.value());

		exp_interval = backoff.NextInterval();
		ASSERT_TRUE(exp_interval) << exp_interval.error().String();
		EXPECT_EQ(exp_interval.value(), chrono::minutes(1)) << duration_fmt(exp_interval.value());

		exp_interval = backoff.NextInterval();
		ASSERT_FALSE(exp_interval);
		EXPECT_EQ(exp_interval.error().code, http::MakeError(http::MaxRetryError, "").code);
	}

	{
		auto max_attempts = 8;
		auto expected_interval_minutes = chrono::minutes(1);
		http::ExponentialBackoff backoff(chrono::minutes(12));
		backoff.SetTryCount(max_attempts);
		for (auto attempt = 0; attempt < max_attempts; attempt++) {
			exp_interval = backoff.NextInterval();
			ASSERT_TRUE(exp_interval) << exp_interval.error().String();
			EXPECT_EQ(exp_interval.value(), expected_interval_minutes)
				<< duration_fmt(exp_interval.value());
			if (((attempt + 1) % 3) == 0) {
				expected_interval_minutes *= 2;
			}
		}
		exp_interval = backoff.NextInterval();
		ASSERT_FALSE(exp_interval);
		EXPECT_EQ(exp_interval.error().code, http::MakeError(http::MaxRetryError, "").code);
	}

	{
		auto max_attempts = 5;
		auto expected_interval_minutes = chrono::minutes(1);
		http::ExponentialBackoff backoff(chrono::minutes(4), max_attempts);
		for (auto attempt = 0; attempt < max_attempts; attempt++) {
			exp_interval = backoff.NextInterval();
			ASSERT_TRUE(exp_interval) << exp_interval.error().String();
			EXPECT_EQ(exp_interval.value(), expected_interval_minutes)
				<< duration_fmt(exp_interval.value());
			if (((attempt + 1) % 3) == 0) {
				expected_interval_minutes *= 2;
			}
		}
		exp_interval = backoff.NextInterval();
		ASSERT_FALSE(exp_interval);
		EXPECT_EQ(exp_interval.error().code, http::MakeError(http::MaxRetryError, "").code);
	}

	{
		auto max_attempts = 12;
		auto expected_interval_minutes = chrono::minutes(1);
		http::ExponentialBackoff backoff(chrono::minutes(2), max_attempts);
		for (auto attempt = 0; attempt < max_attempts; attempt++) {
			exp_interval = backoff.NextInterval();
			ASSERT_TRUE(exp_interval) << exp_interval.error().String();
			EXPECT_EQ(exp_interval.value(), expected_interval_minutes)
				<< duration_fmt(exp_interval.value());
			if (attempt + 1 == 3) {
				expected_interval_minutes *= 2;
			}
		}
		exp_interval = backoff.NextInterval();
		ASSERT_FALSE(exp_interval);
		EXPECT_EQ(exp_interval.error().code, http::MakeError(http::MaxRetryError, "").code);
	}
}

TEST(HttpsTest, MtlsFailureNoClientCertificate) {
	TestEventLoop loop;

	bool client_hit_header {false};

	mendertesting::TemporaryDirectory tmpdir;
	string script = R"(#! /bin/sh
	  exec openssl s_server -www )";
	script += " -key server.localhost.key";
	script += " -cert server.localhost.crt";
	script += " -accept " TEST_PORT;
	script += " -Verify 1"; // Force a client certificate check

	const string script_fname = tmpdir.Path() + "/test-script.sh";
	{
		std::ofstream os(script_fname.c_str(), std::ios::out);
		os << script;
	}
	int ret = chmod(script_fname.c_str(), S_IRUSR | S_IWUSR | S_IXUSR);
	ASSERT_EQ(ret, 0);
	processes::Process server({script_fname});
	auto err = server.Start();
	ASSERT_EQ(err, error::NoError);
	std::this_thread::sleep_for(std::chrono::seconds {1}); // Give the server a little time to setup

	http::ClientConfig client_config {"server.localhost.crt"};
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("https://localhost:" TEST_PORT "/index.html");
	err = client.AsyncCall(
		req,
		[&loop, &client_hit_header](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_FALSE(exp_resp);
			EXPECT_THAT(exp_resp.error().String(), testing::HasSubstr("certificate required"));
			client_hit_header = true;
			loop.Stop();
		},
		[](http::ExpectedIncomingResponsePtr exp_resp) {});
	ASSERT_EQ(error::NoError, err);

	loop.Run();
	EXPECT_TRUE(client_hit_header);
}

TEST(HttpsTest, MtlsSuccess) {
	TestEventLoop loop;

	bool client_hit_header {false};
	bool client_hit_body {false};

	mendertesting::TemporaryDirectory tmpdir;
	string script = R"(#! /bin/sh
	  exec openssl s_server -www )";
	script += " -key server.localhost.key";
	script += " -cert server.localhost.crt";
	script += " -accept " TEST_PORT;
	script += " -Verify 1"; // Force a client certificate check

	const string script_fname = tmpdir.Path() + "/test-script.sh";
	{
		std::ofstream os(script_fname.c_str(), std::ios::out);
		os << script;
	}
	int ret = chmod(script_fname.c_str(), S_IRUSR | S_IWUSR | S_IXUSR);
	ASSERT_EQ(ret, 0);
	processes::Process server({script_fname});
	auto err = server.Start();
	ASSERT_EQ(err, error::NoError);
	std::this_thread::sleep_for(std::chrono::seconds {1}); // Give the server a little time to setup

	http::ClientConfig client_config {
		"server.localhost.crt", "client.localhost.crt", "client.localhost.key"};
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("https://localhost:" TEST_PORT "/index.html");
	err = client.AsyncCall(
		req,
		[&client_hit_header](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(exp_resp) << "Error message: " << exp_resp.error().String();
			auto resp = exp_resp.value();
			EXPECT_EQ(resp->GetStatusCode(), 200);
			EXPECT_EQ(resp->GetStatusMessage(), "ok");
			client_hit_header = true;
		},
		[&client_hit_body, &loop](http::ExpectedIncomingResponsePtr exp_resp) {
			client_hit_body = true;
			loop.Stop();
		});
	ASSERT_EQ(error::NoError, err);

	loop.Run();

	EXPECT_TRUE(client_hit_header);
	EXPECT_TRUE(client_hit_body);
}
