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

#include <gtest/gtest.h>
#include <gmock/gmock.h>

#include <common/error.hpp>
#include <common/events.hpp>
#include <common/http.hpp>
#include <common/http_test_helpers.hpp>
#include <common/testing.hpp>

#include <mender-auth/http_forwarder.hpp>

#define TEST_PORT "8001"

using namespace std;

namespace error = mender::common::error;
namespace events = mender::common::events;
namespace http = mender::http;

namespace mtesting = mender::common::testing;

namespace hf = mender::auth::http_forwarder;

namespace mender {
namespace auth {
namespace http_forwarder {

class TestServer : public hf::Server {
public:
	TestServer(
		const http::ServerConfig &server_config,
		const http::ClientConfig &client_config,
		events::EventLoop &loop) :
		Server(server_config, client_config, loop),
		event_loop_(loop) {
	}
	~TestServer() {
		if (connections_.size() != 0) {
			// Give the forwarder a little bit of time to finish its own internal
			// connection. The internal connection is not exposed to the caller, so we
			// cannot use the caller's handler as a signal that all connections have
			// finished. Either the caller's connection may finish first, or the
			// connection we make on their behalf, it depends. However, after a "finite"
			// time, both should finish, hence this small timer.
			//
			// Starting the event loop in a destructor is a bit evil, but it's only for
			// test scenarios. The problem will not occur in production because the loop
			// is continously running there.
			events::Timer timer(event_loop_);
			timer.AsyncWait(
				chrono::milliseconds(100), [this](error::Error) { event_loop_.Stop(); });
			event_loop_.Run();
		}

		// There should be no connections left at the end of the tests.
		EXPECT_EQ(connections_.size(), 0);
	}

private:
	events::EventLoop &event_loop_;
};

} // namespace http_forwarder
} // namespace auth
} // namespace mender

class TerminatingWriter : virtual public io::Writer {
public:
	TerminatingWriter(io::WriterPtr writer, size_t stop_after) :
		writer_(writer),
		stop_after_(stop_after) {
	}

	io::ExpectedSize Write(
		vector<uint8_t>::const_iterator start, vector<uint8_t>::const_iterator end) override {
		written_ += end - start;
		if (written_ > stop_after_) {
			return expected::unexpected(
				error::MakeError(error::GenericError, "Stopping deliberately"));
		}
		return writer_->Write(start, end);
	}


private:
	io::WriterPtr writer_;
	size_t stop_after_;
	size_t written_ {0};
};

TEST(HttpForwarderTests, BasicRequest) {
	mtesting::TestEventLoop loop;

	bool hit_endpoint_correctly = false;

	http::ServerConfig server_config;
	http::Server server(server_config, loop);
	server.AsyncServeUrl(
		"http://127.0.0.1:" TEST_PORT,
		[](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
		},
		[&hit_endpoint_correctly](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			auto req = exp_req.value();

			EXPECT_EQ(req->GetMethod(), http::Method::PUT);
			EXPECT_EQ(req->GetPath(), "/test-endpoint");

			auto exp_resp = exp_req.value()->MakeResponse();
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();
			auto resp = exp_resp.value();

			resp->SetStatusCodeAndMessage(200, "OK");
			auto err = resp->AsyncReply([&hit_endpoint_correctly](error::Error err) {
				hit_endpoint_correctly = true;
				ASSERT_EQ(err, error::NoError);
			});
		});

	http::ClientConfig client_config;

	hf::TestServer forwarder(server_config, client_config, loop);
	auto err = forwarder.AsyncForward("http://127.0.0.1:0", "http://127.0.0.1:" TEST_PORT "/");
	ASSERT_EQ(err, error::NoError);

	// Should not be possible to call again without cancelling first.
	err = forwarder.AsyncForward("http://127.0.0.1:0", "http://127.0.0.1:" TEST_PORT "/");
	ASSERT_NE(err, error::NoError);
	forwarder.Cancel();
	err = forwarder.AsyncForward("http://127.0.0.1:0", "http://127.0.0.1:" TEST_PORT "/");
	ASSERT_EQ(err, error::NoError);

	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::PUT);
	req->SetAddress(http::JoinUrl(forwarder.GetUrl(), "/test-endpoint"));
	err = client.AsyncCall(
		req,
		[](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();
		},
		[&loop](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();

			loop.Stop();
		});
	ASSERT_EQ(err, error::NoError);

	loop.Run();

	EXPECT_TRUE(hit_endpoint_correctly);
}

TEST(HttpForwarderTests, RequestAndResponseWithBody) {
	mtesting::TestEventLoop loop;

	bool hit_endpoint_correctly = false;

	http::ServerConfig server_config;
	http::Server server(server_config, loop);
	vector<uint8_t> req_body;
	vector<uint8_t> resp_body;
	server.AsyncServeUrl(
		"http://127.0.0.1:" TEST_PORT,
		[&req_body](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			auto writer = make_shared<io::ByteWriter>(req_body);
			writer->SetUnlimited(true);
			exp_req.value()->SetBodyWriter(writer);
		},
		[&hit_endpoint_correctly](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			auto req = exp_req.value();

			EXPECT_EQ(req->GetMethod(), http::Method::PUT);
			EXPECT_EQ(req->GetPath(), "/test-endpoint");

			auto exp_resp = exp_req.value()->MakeResponse();
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();
			auto resp = exp_resp.value();

			resp->SetHeader("Content-Length", to_string(BodyOfXes::TARGET_BODY_SIZE));
			resp->SetBodyReader(make_shared<BodyOfXes>());
			resp->SetStatusCodeAndMessage(200, "OK");
			auto err = resp->AsyncReply([&hit_endpoint_correctly](error::Error err) {
				hit_endpoint_correctly = true;
				ASSERT_EQ(err, error::NoError);
			});
		});

	http::ClientConfig client_config;

	hf::TestServer forwarder(server_config, client_config, loop);
	auto err = forwarder.AsyncForward("http://127.0.0.1:0", "http://127.0.0.1:" TEST_PORT "/");
	ASSERT_EQ(err, error::NoError);

	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::PUT);
	req->SetAddress(http::JoinUrl(forwarder.GetUrl(), "/test-endpoint"));
	req->SetHeader("Content-Length", to_string(BodyOfXes::TARGET_BODY_SIZE));
	req->SetBodyGenerator([]() { return make_shared<BodyOfXes>(); });
	err = client.AsyncCall(
		req,
		[&resp_body](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();
			auto writer = make_shared<io::ByteWriter>(resp_body);
			writer->SetUnlimited(true);
			exp_resp.value()->SetBodyWriter(writer);
		},
		[&loop](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();

			loop.Stop();
		});
	ASSERT_EQ(err, error::NoError);

	loop.Run();

	EXPECT_TRUE(hit_endpoint_correctly);
	EXPECT_EQ(req_body.size(), BodyOfXes::TARGET_BODY_SIZE);
	EXPECT_EQ(resp_body.size(), BodyOfXes::TARGET_BODY_SIZE);

	vector<uint8_t> expected_body;
	auto writer = make_shared<io::ByteWriter>(expected_body);
	writer->SetUnlimited(true);
	ASSERT_EQ(io::Copy(*writer, *make_shared<BodyOfXes>()), error::NoError);
	EXPECT_EQ(req_body, expected_body);
	EXPECT_EQ(resp_body, expected_body);
}

TEST(HttpForwarderTests, ConnectionFailure) {
	mtesting::TestEventLoop loop;

	http::ServerConfig server_config;
	http::ClientConfig client_config;

	hf::TestServer forwarder(server_config, client_config, loop);
	auto err = forwarder.AsyncForward("http://127.0.0.1:0", "http://127.0.0.1:" TEST_PORT "/");
	ASSERT_EQ(err, error::NoError);

	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::PUT);
	req->SetAddress(http::JoinUrl(forwarder.GetUrl(), "/test-endpoint"));
	err = client.AsyncCall(
		req,
		[&loop](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_FALSE(exp_resp);
			// If we connected directly, this would have been "connection refused", but it's
			// forwarded and already open, so we just close it with no request served.
			EXPECT_THAT(exp_resp.error().String(), ::testing::HasSubstr("end of stream"));

			loop.Stop();
		},
		[](http::ExpectedIncomingResponsePtr exp_resp) {
			// Should never get here.
			ASSERT_TRUE(false);
		});
	ASSERT_EQ(err, error::NoError);

	loop.Run();
}

TEST(HttpForwarderTests, ClientTerminatesDownload) {
	mtesting::TestEventLoop loop;

	bool hit_endpoint_correctly = false;

	http::ServerConfig server_config;
	http::Server server(server_config, loop);
	vector<uint8_t> req_body;
	vector<uint8_t> resp_body;
	server.AsyncServeUrl(
		"http://127.0.0.1:" TEST_PORT,
		[&req_body](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			auto writer = make_shared<io::ByteWriter>(req_body);
			writer->SetUnlimited(true);
			exp_req.value()->SetBodyWriter(writer);
		},
		[&hit_endpoint_correctly, &loop](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			auto req = exp_req.value();

			EXPECT_EQ(req->GetMethod(), http::Method::PUT);
			EXPECT_EQ(req->GetPath(), "/test-endpoint");

			auto exp_resp = exp_req.value()->MakeResponse();
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();
			auto resp = exp_resp.value();

			resp->SetHeader("Content-Length", to_string(BodyOfXes::TARGET_BODY_SIZE));
			resp->SetBodyReader(make_shared<BodyOfXes>());
			resp->SetStatusCodeAndMessage(200, "OK");
			auto err = resp->AsyncReply([&hit_endpoint_correctly, &loop](error::Error err) {
				hit_endpoint_correctly = true;
				ASSERT_NE(err, error::NoError);

				loop.Stop();
			});
		});

	http::ClientConfig client_config;

	hf::TestServer forwarder(server_config, client_config, loop);
	auto err = forwarder.AsyncForward("http://127.0.0.1:0", "http://127.0.0.1:" TEST_PORT "/");
	ASSERT_EQ(err, error::NoError);

	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::PUT);
	req->SetAddress(http::JoinUrl(forwarder.GetUrl(), "/test-endpoint"));
	req->SetHeader("Content-Length", to_string(BodyOfXes::TARGET_BODY_SIZE));
	req->SetBodyGenerator([]() { return make_shared<BodyOfXes>(); });
	err = client.AsyncCall(
		req,
		[&resp_body](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();
			auto byte_writer = make_shared<io::ByteWriter>(resp_body);
			byte_writer->SetUnlimited(true);
			auto writer =
				make_shared<TerminatingWriter>(byte_writer, BodyOfXes::TARGET_BODY_SIZE / 2);
			exp_resp.value()->SetBodyWriter(writer);
		},
		[](http::ExpectedIncomingResponsePtr exp_resp) { ASSERT_FALSE(exp_resp); });
	ASSERT_EQ(err, error::NoError);

	loop.Run();

	EXPECT_TRUE(hit_endpoint_correctly);
}

TEST(HttpForwarderTests, TargetTerminatesUpload) {
	mtesting::TestEventLoop loop;

	http::ServerConfig server_config;
	http::Server server(server_config, loop);
	vector<uint8_t> req_body;
	server.AsyncServeUrl(
		"http://127.0.0.1:" TEST_PORT,
		[&req_body](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			auto byte_writer = make_shared<io::ByteWriter>(req_body);
			byte_writer->SetUnlimited(true);
			auto writer =
				make_shared<TerminatingWriter>(byte_writer, BodyOfXes::TARGET_BODY_SIZE / 2);
			exp_req.value()->SetBodyWriter(writer);
		},
		[](http::ExpectedIncomingRequestPtr exp_req) { ASSERT_FALSE(exp_req); });

	http::ClientConfig client_config;

	hf::TestServer forwarder(server_config, client_config, loop);
	auto err = forwarder.AsyncForward("http://127.0.0.1:0", "http://127.0.0.1:" TEST_PORT "/");
	ASSERT_EQ(err, error::NoError);

	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::PUT);
	req->SetAddress(http::JoinUrl(forwarder.GetUrl(), "/test-endpoint"));
	req->SetHeader("Content-Length", to_string(BodyOfXes::TARGET_BODY_SIZE));
	req->SetBodyGenerator([]() { return make_shared<BodyOfXes>(); });
	err = client.AsyncCall(
		req,
		[&loop](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_FALSE(exp_resp);
			EXPECT_THAT(
				exp_resp.error().String(), ::testing::HasSubstr("Connection reset by peer"));
			loop.Stop();
		},
		[](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(false) << "Should not get here";
		});
	ASSERT_EQ(err, error::NoError);

	loop.Run();
}

TEST(HttpForwarderTests, ClientTerminatesUpload) {
	mtesting::TestEventLoop loop;

	http::ServerConfig server_config;
	http::Server server(server_config, loop);
	vector<uint8_t> req_body;
	bool hit_server_header_handler {false};
	bool hit_server_body_handler {false};
	server.AsyncServeUrl(
		"http://127.0.0.1:" TEST_PORT,
		[&req_body, &hit_server_header_handler](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			auto writer = make_shared<io::ByteWriter>(req_body);
			writer->SetUnlimited(true);
			exp_req.value()->SetBodyWriter(writer);
			hit_server_header_handler = true;
		},
		[&hit_server_body_handler, &loop](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_FALSE(exp_req);
			hit_server_body_handler = true;
			loop.Stop();
		});

	http::ClientConfig client_config;

	hf::TestServer forwarder(server_config, client_config, loop);
	auto err = forwarder.AsyncForward("http://127.0.0.1:0", "http://127.0.0.1:" TEST_PORT "/");
	ASSERT_EQ(err, error::NoError);

	class DisconnectAtEndReader : virtual public io::Reader {
	public:
		http::Client &client;
		io::ReaderPtr reader;

		DisconnectAtEndReader(http::Client &client, io::ReaderPtr reader) :
			client {client},
			reader {reader} {
		}

		io::ExpectedSize Read(
			vector<uint8_t>::iterator start, vector<uint8_t>::iterator end) override {
			auto result = reader->Read(start, end);
			if (result && result.value() == 0) {
				client.Cancel();
			}
			return result;
		}
	};

	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::PUT);
	req->SetAddress(http::JoinUrl(forwarder.GetUrl(), "/test-endpoint"));
	// Too big, same as termination.
	req->SetHeader("Content-Length", to_string(BodyOfXes::TARGET_BODY_SIZE * 2));
	req->SetBodyGenerator([&client]() {
		return make_shared<DisconnectAtEndReader>(client, make_shared<BodyOfXes>());
	});
	err = client.AsyncCall(
		req,
		[](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_FALSE(exp_resp);
			EXPECT_EQ(exp_resp.error().code, make_error_condition(errc::operation_canceled));
		},
		[](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(false) << "Should not get here";
		});
	ASSERT_EQ(err, error::NoError);

	loop.Run();

	EXPECT_TRUE(hit_server_header_handler);
	EXPECT_TRUE(hit_server_body_handler);
}

TEST(HttpForwarderTests, TargetTerminatesDownload) {
	mtesting::TestEventLoop loop;

	http::ServerConfig server_config;
	http::Server server(server_config, loop);
	vector<uint8_t> req_body;
	vector<uint8_t> resp_body;
	server.AsyncServeUrl(
		"http://127.0.0.1:" TEST_PORT,
		[&req_body](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			auto writer = make_shared<io::ByteWriter>(req_body);
			writer->SetUnlimited(true);
			exp_req.value()->SetBodyWriter(writer);
		},
		[](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			auto req = exp_req.value();

			EXPECT_EQ(req->GetMethod(), http::Method::PUT);
			EXPECT_EQ(req->GetPath(), "/test-endpoint");

			auto exp_resp = exp_req.value()->MakeResponse();
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();
			auto resp = exp_resp.value();

			// Too big, same as termination.
			resp->SetHeader("Content-Length", to_string(BodyOfXes::TARGET_BODY_SIZE * 2));
			resp->SetBodyReader(make_shared<BodyOfXes>());
			resp->SetStatusCodeAndMessage(200, "OK");
			auto err = resp->AsyncReply([](error::Error err) { ASSERT_EQ(err, error::NoError); });
		});

	http::ClientConfig client_config;

	hf::TestServer forwarder(server_config, client_config, loop);
	auto err = forwarder.AsyncForward("http://127.0.0.1:0", "http://127.0.0.1:" TEST_PORT "/");
	ASSERT_EQ(err, error::NoError);

	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::PUT);
	req->SetAddress(http::JoinUrl(forwarder.GetUrl(), "/test-endpoint"));
	req->SetHeader("Content-Length", to_string(BodyOfXes::TARGET_BODY_SIZE));
	req->SetBodyGenerator([]() { return make_shared<BodyOfXes>(); });
	err = client.AsyncCall(
		req,
		[&resp_body](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();
			auto writer = make_shared<io::ByteWriter>(resp_body);
			writer->SetUnlimited(true);
			exp_resp.value()->SetBodyWriter(writer);
		},
		[&loop](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_FALSE(exp_resp);
			loop.Stop();
		});
	ASSERT_EQ(err, error::NoError);

	loop.Run();
}
