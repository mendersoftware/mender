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
#include <mender-update/http_resumer.hpp>

#include <chrono>
#include <thread>

#include <gmock/gmock.h>
#include <gtest/gtest.h>

#include <common/common.hpp>
#include <common/events.hpp>
#include <common/testing.hpp>

using namespace std;

namespace error = mender::common::error;
namespace expected = mender::common::expected;
namespace http = mender::http;
namespace http_resumer = mender::update::http_resumer;
namespace io = mender::common::io;
namespace common = mender::common;
namespace events = mender::common::events;

using testing::StartsWith;

#define TEST_PORT "8001"

using TestEventLoop = mender::common::testing::TestEventLoop;

namespace mender {
namespace http {

class BackupServer : public Server {
public:
	BackupServer(const ServerConfig &server, events::EventLoop &event_loop) :
		Server(server, event_loop) {};
	~BackupServer() = default;

	void Setup(const string &url, RequestHandler header_handler, RequestHandler body_handler) {
		url_ = url;
		header_handler_ = header_handler;
		body_handler_ = body_handler;
	};

	error::Error Start() {
		return AsyncServeUrl(url_, header_handler_, body_handler_);
	};

private:
	string url_;
	RequestHandler header_handler_;
	RequestHandler body_handler_;
};

} // namespace http
} // namespace mender

class RangeBodyOfXes : virtual public io::Reader {
public:
	expected::ExpectedSize Read(
		vector<uint8_t>::iterator start, vector<uint8_t>::iterator end) override {
		auto iter_count = end - start;
		size_t read;
		if (iter_count + count_ > GetMaxRead()) {
			read = GetMaxRead() - count_;
		} else {
			read = iter_count;
		}

		for (size_t i = 0; i < read; i++) {
			start[i] = TransferFunction(range_.start + count_ + i);
		}
		count_ += read;
		return read;
	}

	void SetRanges(size_t start, size_t end) {
		range_.start = start;
		range_.end = end;
	};

	size_t GetMaxRead() const {
		return range_.end - range_.start + 1;
	}

	string GetContentLengthHeader() const {
		return to_string(range_.end - range_.start + 1);
	}

	static uint8_t TransferFunction(size_t index) {
		// Fill in a specific pattern to try to catch offset/block errors: Raise the input
		// number to the power of 1.1 and round to the nearest integer. If it's odd, return
		// 'X', if it's even, return 'x'. Due to the exponent, this pattern will change
		// slightly throughout the sequence, giving us a chance to catch offset errors.
		// (Note: Try printing it, the pattern is mesmerizing to watch!)
		auto num = size_t(round(pow(index, 1.1)));
		if (num & 1) {
			return 'X';
		} else {
			return 'x';
		}
	}

	// Just some random size, but preferably big, and not falling on a block boundary.
	static const size_t TARGET_BODY_SIZE {1234567};

private:
	struct {
		size_t start = 0;
		size_t end = TARGET_BODY_SIZE - 1;
	} range_;

	size_t count_;
};
const size_t RangeBodyOfXes::TARGET_BODY_SIZE;

struct DownloadResumerTestCase {
	string case_name;

	bool broken_content_length;
	bool missing_content_length;
	bool unknown_content_length;
	bool early_range_start;
	bool late_range_start;
	bool no_partial_content_support;
	string custom_content_range;
	bool missing_content_range;
	bool garbled_content_start;
	bool garbled_bytes;
	chrono::milliseconds server_down_after;
	chrono::milliseconds server_up_again_after;

	bool success;
};

vector<DownloadResumerTestCase> GenerateDownloadResumerTestCases() {
	return {
		DownloadResumerTestCase {.case_name = "BasicDownloadAndResume", .success = true},
		// NOTE: The following use case would pass in the old client, where special handling
		// was done for this specific case. Due to implementation details it is much harder
		// to support with our async IO design and we decided to drop it.
		// This also applies to related test breakAfterShortRange from the old client (not ported)
		DownloadResumerTestCase {
			.case_name = "EarlyRangeStart", .early_range_start = true, .success = false},
		DownloadResumerTestCase {
			.case_name = "LateRangeStart", .late_range_start = true, .success = false},
		DownloadResumerTestCase {
			.case_name = "BrokenContentLength", .broken_content_length = true, .success = false},
		DownloadResumerTestCase {
			.case_name = "MissingContentLength", .missing_content_length = true, .success = true},
		DownloadResumerTestCase {
			.case_name = "NoPartialContentSupport",
			.no_partial_content_support = true,
			.success = false},
		DownloadResumerTestCase {
			.case_name = "EmptyContentRange", .custom_content_range = "bytes ", .success = false},
		DownloadResumerTestCase {
			.case_name = "FormattedButInvalidContentRange",
			.custom_content_range = "bytes abc-def/deadbeef",
			.success = false},
		DownloadResumerTestCase {
			.case_name = "ImproperlyFormattedContentRange",
			.custom_content_range = "bytes 5",
			.success = false},
		DownloadResumerTestCase {
			.case_name = "MissingBytesContentRange",
			.custom_content_range = "5-6/2",
			.success = false},
		DownloadResumerTestCase {
			.case_name = "MissingContentRange", .missing_content_range = true, .success = false},
		DownloadResumerTestCase {
			.case_name = "TooManyContentRanges",
			.custom_content_range = "bytes 5-6/20 7-8/20",
			.success = false},
		DownloadResumerTestCase {
			.case_name = "InvalidContentRange",
			.custom_content_range = "bytes 100-200-300/400",
			.success = false},
		DownloadResumerTestCase {
			.case_name = "ChangeContentLength",
			.custom_content_range = "bytes 10000-20000/30000",
			.success = false},
		DownloadResumerTestCase {
			.case_name = "NegativeRange",
			.custom_content_range =
				"bytes 20000-10000/" + to_string(RangeBodyOfXes::TARGET_BODY_SIZE),
			.success = false},
		DownloadResumerTestCase {
			.case_name = "GarbledContentStart", .garbled_content_start = true, .success = false},
		DownloadResumerTestCase {
			.case_name = "GarbledBytes", .garbled_bytes = true, .success = false},
		DownloadResumerTestCase {
			.case_name = "ServerDownAndUp",
			.server_down_after = chrono::milliseconds(150),
			.server_up_again_after = chrono::milliseconds(250),
			.success = true},
		DownloadResumerTestCase {
			.case_name = "ServerDown",
			.server_down_after = chrono::milliseconds(150),
			.success = false},
		// NOTE: The following use case would fail in the old client, where the total
		// size either had to be correct or missing, but couldn't be '*', which is allowed
		// according to the HTTP specification.
		DownloadResumerTestCase {
			.case_name = "UnknownContentLength", .unknown_content_length = true, .success = true},
		// NOTE: The following use case would pass in the old client, where the range end
		// was unparsed and ignored. The new client verifies that it is exactly what we asked for.
		DownloadResumerTestCase {
			.case_name = "InvalidEndRange",
			.custom_content_range =
				"bytes 1000-20o0/" + to_string(RangeBodyOfXes::TARGET_BODY_SIZE),
			.success = false},
	};
}

class DownloadResumerTest : public testing::TestWithParam<DownloadResumerTestCase> {
public:
};

INSTANTIATE_TEST_SUITE_P(
	,
	DownloadResumerTest,
	::testing::ValuesIn(GenerateDownloadResumerTestCases()),
	[](const testing::TestParamInfo<DownloadResumerTestCase> &test_case) {
		return test_case.param.case_name;
	});


TEST_P(DownloadResumerTest, Cases) {
	TestEventLoop loop(chrono::seconds(65));

	// Servers
	http::ServerConfig server_config;
	http::Server server(server_config, loop);
	http::BackupServer backup_server(server_config, loop);

	auto &test_case = GetParam();
	int server_num_requests = 0;
	bool server_down_done = false;

	events::Timer timer(loop);

	backup_server.Setup(
		"http://127.0.0.1:" TEST_PORT,
		[](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
		},
		[&server_num_requests](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			auto req = exp_req.value();

			auto result = exp_req.value()->MakeResponse();
			ASSERT_TRUE(result);
			auto resp = result.value();

			server_num_requests++;

			auto size = RangeBodyOfXes::TARGET_BODY_SIZE;
			long long pos = 0;

			auto exp_range_header = req->GetHeader("Range");
			ASSERT_TRUE(exp_range_header);
			resp->SetStatusCodeAndMessage(206, "Partial Content");
			ASSERT_THAT(exp_range_header.value(), StartsWith("bytes="));
			auto range_string = exp_range_header.value().substr(
				string("bytes=").length(), exp_range_header.value().length());
			auto range_parts = common::SplitString(range_string, "-");
			ASSERT_EQ(range_parts.size(), 2);
			auto exp_pos = common::StringToLongLong(range_parts[0]);
			ASSERT_TRUE(exp_pos);
			pos = exp_pos.value();
			resp->SetHeader(
				"Content-Range",
				"bytes " + to_string(pos) + "-" + to_string(size - 1) + "/" + to_string(size));

			resp->SetHeader("Content-Length", to_string(size - pos));

			// Only give some, not all, then terminate connection.
			auto to_copy = size / 5;
			if (to_copy > size - pos) {
				to_copy = size - pos;
			}

			auto partial_body = make_shared<RangeBodyOfXes>();
			partial_body->SetRanges(pos, pos + to_copy - 1);
			resp->SetBodyReader(partial_body);

			resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
		});

	server.AsyncServeUrl(
		"http://127.0.0.1:" TEST_PORT,
		[](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
		},
		[&backup_server, &server, &timer, &server_num_requests, &test_case, &server_down_done](
			http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			auto req = exp_req.value();

			auto result = exp_req.value()->MakeResponse();
			ASSERT_TRUE(result);
			auto resp = result.value();

			server_num_requests++;

			auto size = RangeBodyOfXes::TARGET_BODY_SIZE;
			long long pos = 0;

			auto exp_range_header = req->GetHeader("Range");
			if (exp_range_header && !test_case.no_partial_content_support) {
				resp->SetStatusCodeAndMessage(206, "Partial Content");
				ASSERT_THAT(exp_range_header.value(), StartsWith("bytes="));
				auto range_string = exp_range_header.value().substr(
					string("bytes=").length(), exp_range_header.value().length());
				auto range_parts = common::SplitString(range_string, "-");
				ASSERT_EQ(range_parts.size(), 2);
				auto exp_pos = common::StringToLongLong(range_parts[0]);
				ASSERT_TRUE(exp_pos);
				pos = exp_pos.value();

				if (test_case.early_range_start) {
					pos -= 5;
				} else if (test_case.late_range_start) {
					pos += 5;
				}
				if (test_case.missing_content_range) {
					resp->SetHeader("Content-Range", "");
				} else if (test_case.custom_content_range != "") {
					resp->SetHeader("Content-Range", test_case.custom_content_range);
				} else if (test_case.missing_content_length) {
					resp->SetHeader(
						"Content-Range", "bytes " + to_string(pos) + "-" + to_string(size - 1));
				} else if (test_case.unknown_content_length) {
					resp->SetHeader(
						"Content-Range",
						"bytes " + to_string(pos) + "-" + to_string(size - 1) + "/*");
				} else if (test_case.garbled_content_start) {
					resp->SetHeader(
						"Content-Range",
						"bytes abc-" + to_string(size - 1) + "/" + to_string(size));
				} else if (test_case.garbled_bytes) {
					resp->SetHeader(
						"Content-Range",
						"abcde " + to_string(pos) + "-" + to_string(size - 1) + "/"
							+ to_string(size));
				} else {
					if (test_case.broken_content_length) {
						size -= 1;
					}
					resp->SetHeader(
						"Content-Range",
						"bytes " + to_string(pos) + "-" + to_string(size - 1) + "/"
							+ to_string(size));
				}
			} else {
				resp->SetStatusCodeAndMessage(200, "Success");
			}

			resp->SetHeader("Content-Length", to_string(size - pos));

			// Only give some, not all, then terminate connection.
			auto to_copy = size / 5;
			if (to_copy > size - pos) {
				to_copy = size - pos;
			}

			auto partial_body = make_shared<RangeBodyOfXes>();
			partial_body->SetRanges(pos, pos + to_copy - 1);
			resp->SetBodyReader(partial_body);

			resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });

			if (test_case.server_down_after > chrono::seconds(0) && !server_down_done) {
				server_down_done = true;
				auto again_after = test_case.server_up_again_after;
				timer.AsyncWait(
					test_case.server_down_after,
					[&server, &backup_server, &timer, again_after](error::Error err) {
						server.Cancel();
						if (again_after > chrono::seconds(0)) {
							timer.AsyncWait(again_after, [&backup_server](error::Error err) {
								backup_server.Start();
							});
						}
					});
			}
		});

	// Request
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("http://127.0.0.1:" TEST_PORT);

	// Client
	http::ClientConfig client_config;
	shared_ptr<http_resumer::DownloadResumerClient> client =
		make_shared<http_resumer::DownloadResumerClient>(client_config, loop);
	client->SetSmallestWaitInterval(chrono::milliseconds(100));

	vector<uint8_t> received_body;
	int user_num_callbacks = 0;

	http::ResponseHandler user_header_handler =
		[&received_body, &user_num_callbacks](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();
			auto resp = exp_resp.value();

			user_num_callbacks++;

			auto content_length = resp->GetHeader("Content-Length");
			ASSERT_TRUE(content_length);
			ASSERT_EQ(content_length.value(), to_string(RangeBodyOfXes::TARGET_BODY_SIZE));

			auto body_writer = make_shared<io::ByteWriter>(received_body);
			body_writer->SetUnlimited(true);
			resp->SetBodyWriter(body_writer);
		};

	http::ResponseHandler user_body_handler = [&test_case,
											   &loop](http::ExpectedIncomingResponsePtr exp_resp) {
		EXPECT_EQ((bool) exp_resp, test_case.success);
		loop.Stop();
	};

	auto err = client->AsyncCall(req, user_header_handler, user_body_handler);
	EXPECT_EQ(err, error::NoError) << "Unexpected error: " << err.message;

	loop.Run();

	EXPECT_EQ(user_num_callbacks, 1);

	if (test_case.success) {
		EXPECT_TRUE(server_num_requests > 5) << "num of requests: " << server_num_requests;
		EXPECT_EQ(received_body.size(), RangeBodyOfXes::TARGET_BODY_SIZE);

		// Check data integrity
		vector<uint8_t> expected_body;
		io::ByteWriter expected_writer(expected_body);
		expected_writer.SetUnlimited(true);
		io::Copy(expected_writer, *make_shared<RangeBodyOfXes>());

		ASSERT_EQ(received_body.size(), expected_body.size());
		EXPECT_EQ(received_body, expected_body)
			<< "Body not received correctly. Difference at index "
				   + to_string(
					   mismatch(received_body.begin(), received_body.end(), expected_body.begin())
						   .first
					   - received_body.begin());
	}
}

TEST_F(DownloadResumerTest, FullResponseNoResume) {
	TestEventLoop loop;

	// Server
	http::ServerConfig server_config;
	http::Server server(server_config, loop);

	int server_num_requests = 0;

	server.AsyncServeUrl(
		"http://127.0.0.1:" TEST_PORT,
		[](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
		},
		[&server_num_requests](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			auto req = exp_req.value();

			auto result = exp_req.value()->MakeResponse();
			ASSERT_TRUE(result);
			auto resp = result.value();

			server_num_requests++;
			// Pretend to send a full response but truncate the data
			resp->SetHeader("Content-Length", to_string(RangeBodyOfXes::TARGET_BODY_SIZE));
			auto full_body = make_shared<RangeBodyOfXes>();
			// full_body->SetRanges(0, RangeBodyOfXes::TARGET_BODY_SIZE - 1);
			resp->SetBodyReader(full_body);
			resp->SetStatusCodeAndMessage(200, "Success");
			resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
		});

	// Request
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("http://127.0.0.1:" TEST_PORT);

	// Client
	http::ClientConfig client_config;
	shared_ptr<http_resumer::DownloadResumerClient> client =
		make_shared<http_resumer::DownloadResumerClient>(client_config, loop);

	vector<uint8_t> received_body;
	int user_num_callbacks = 0;

	http::ResponseHandler user_header_handler =
		[&received_body, &user_num_callbacks](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();
			auto resp = exp_resp.value();

			user_num_callbacks++;

			ASSERT_EQ(resp->GetStatusCode(), http::StatusOK);
			auto content_length = resp->GetHeader("Content-Length");
			ASSERT_TRUE(content_length);
			ASSERT_EQ(content_length.value(), to_string(RangeBodyOfXes::TARGET_BODY_SIZE));

			auto body_writer = make_shared<io::ByteWriter>(received_body);
			body_writer->SetUnlimited(true);
			resp->SetBodyWriter(body_writer);
		};

	http::ResponseHandler user_body_handler = [&loop](http::ExpectedIncomingResponsePtr exp_resp) {
		EXPECT_TRUE(exp_resp);
		// There was no resuming, the incoming response shall be 200 OK
		ASSERT_EQ(exp_resp.value()->GetStatusCode(), http::StatusOK);
		loop.Stop();
	};

	auto err = client->AsyncCall(req, user_header_handler, user_body_handler);
	EXPECT_EQ(err, error::NoError) << "Unexpected error: " << err.message;

	loop.Run();

	EXPECT_EQ(server_num_requests, 1);
	EXPECT_EQ(user_num_callbacks, 1);
	EXPECT_EQ(received_body.size(), RangeBodyOfXes::TARGET_BODY_SIZE);

	// Check data integrity
	vector<uint8_t> expected_body;
	io::ByteWriter expected_writer(expected_body);
	expected_writer.SetUnlimited(true);
	io::Copy(expected_writer, *make_shared<RangeBodyOfXes>());
	ASSERT_EQ(received_body.size(), expected_body.size());
	EXPECT_EQ(received_body, expected_body)
		<< "Body not received correctly. Difference at index "
			   + to_string(
				   mismatch(received_body.begin(), received_body.end(), expected_body.begin()).first
				   - received_body.begin());
}

TEST_F(DownloadResumerTest, ResponseBodyReaderSmallBuffer) {
	TestEventLoop loop;

	http::ServerConfig server_config;
	http::Server server(server_config, loop);

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

			resp->SetHeader("Content-Length", to_string(RangeBodyOfXes::TARGET_BODY_SIZE));
			resp->SetBodyReader(make_shared<RangeBodyOfXes>());
			resp->SetStatusCodeAndMessage(200, "Success");
			resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
		});

	http::ClientConfig client_config;
	shared_ptr<http_resumer::DownloadResumerClient> client =
		make_shared<http_resumer::DownloadResumerClient>(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("http://127.0.0.1:" TEST_PORT);
	vector<uint8_t> buf;
	// Use a weird buf size, just to iron out more corner cases.
	buf.resize(1235);
	bool got_read_success {false};
	bool got_read_error {false};
	client->AsyncCall(
		req,
		[&](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();
			auto resp = exp_resp.value();

			auto content_length = resp->GetHeader("Content-Length");
			ASSERT_TRUE(content_length);
			ASSERT_EQ(content_length.value(), to_string(RangeBodyOfXes::TARGET_BODY_SIZE));

			auto body_writer = make_shared<io::ByteWriter>(received_body);
			body_writer->SetUnlimited(true);
			auto reader = *client->MakeBodyAsyncReader(resp);
			// It should not be possible to make a second reader.
			EXPECT_FALSE(client->MakeBodyAsyncReader(resp));
			reader->RepeatedAsyncRead(
				buf.begin(),
				buf.end(),
				// Note in particular the capture of `reader`, to keep it alive.
				[&buf, reader, body_writer, &got_read_error, &got_read_success](
					io::ExpectedSize result) {
					if (!result) {
						EXPECT_TRUE(false) << "Unexpected error: " << result.error().String();
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
			ASSERT_TRUE(exp_resp);
			loop.Stop();
		});

	loop.Run();

	EXPECT_TRUE(got_read_success);
	EXPECT_FALSE(got_read_error);
}

TEST_F(DownloadResumerTest, TwoRangesClientReuse) {
	TestEventLoop loop(chrono::seconds(10));

	// Server
	http::ServerConfig server_config;
	http::Server server(server_config, loop);

	int server_num_requests = 0;

	server.AsyncServeUrl(
		"http://127.0.0.1:" TEST_PORT,
		[](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
		},
		[&server_num_requests](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			auto req = exp_req.value();

			auto result = exp_req.value()->MakeResponse();
			ASSERT_TRUE(result);
			auto resp = result.value();

			server_num_requests++;
			if ((server_num_requests == 1) || (server_num_requests == 3)) {
				// Pretend to send a full response but truncate the data
				resp->SetHeader("Content-Length", to_string(RangeBodyOfXes::TARGET_BODY_SIZE));
				auto partial_body = make_shared<RangeBodyOfXes>();
				partial_body->SetRanges(0, 654321 - 1);
				resp->SetBodyReader(partial_body);
				resp->SetStatusCodeAndMessage(200, "Success");
			} else if ((server_num_requests == 2) || (server_num_requests == 4)) {
				auto exp_header = req->GetHeader("Range");
				EXPECT_TRUE(exp_header);
				EXPECT_EQ(
					exp_header.value(),
					"bytes=654321-" + to_string(RangeBodyOfXes::TARGET_BODY_SIZE - 1));

				// Send now an actual range response
				auto partial_body = make_shared<RangeBodyOfXes>();
				partial_body->SetRanges(654321, RangeBodyOfXes::TARGET_BODY_SIZE - 1);
				resp->SetBodyReader(partial_body);
				resp->SetHeader(
					"Content-Range",
					"bytes 654321-" + to_string(RangeBodyOfXes::TARGET_BODY_SIZE - 1) + "/"
						+ to_string(RangeBodyOfXes::TARGET_BODY_SIZE));
				resp->SetHeader("Content-Length", partial_body->GetContentLengthHeader());
				resp->SetStatusCodeAndMessage(206, "Partial Content");
			} else {
				FAIL() << "Should not get here";
			}
			resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
		});

	// Request
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("http://127.0.0.1:" TEST_PORT);

	// Client
	http::ClientConfig client_config;
	shared_ptr<http_resumer::DownloadResumerClient> client =
		make_shared<http_resumer::DownloadResumerClient>(client_config, loop);
	client->SetSmallestWaitInterval(chrono::milliseconds(100));

	int user_num_callbacks = 0;

	http::ResponseHandler user_header_handler =
		[&user_num_callbacks](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();
			auto resp = exp_resp.value();

			user_num_callbacks++;

			ASSERT_EQ(resp->GetStatusCode(), http::StatusOK);
			auto content_length = resp->GetHeader("Content-Length");
			ASSERT_TRUE(content_length);
			ASSERT_EQ(content_length.value(), to_string(RangeBodyOfXes::TARGET_BODY_SIZE));

			auto body_writer = make_shared<io::Discard>();
			resp->SetBodyWriter(body_writer);
		};

	http::ResponseHandler user_body_handler = [&loop](http::ExpectedIncomingResponsePtr exp_resp) {
		EXPECT_TRUE(exp_resp);
		// There was resuming, the incoming response shall be 206 Partial Content

		EXPECT_EQ(exp_resp.value()->GetStatusCode(), http::StatusPartialContent);
		loop.Stop();
	};

	auto err = client->AsyncCall(req, user_header_handler, user_body_handler);
	EXPECT_EQ(err, error::NoError) << "Unexpected error: " << err.message;

	loop.Run();

	EXPECT_EQ(server_num_requests, 2);
	EXPECT_EQ(user_num_callbacks, 1);

	// Reuse the client to check state clean-up
	err = client->AsyncCall(req, user_header_handler, user_body_handler);
	EXPECT_EQ(err, error::NoError) << "Unexpected error: " << err.message;

	loop.Run();

	EXPECT_EQ(server_num_requests, 4);
	EXPECT_EQ(user_num_callbacks, 2);
}


TEST_F(DownloadResumerTest, SmallIntervalsErrorOnFirstRead) {
	TestEventLoop loop(chrono::seconds(10));

	// Server
	http::ServerConfig server_config;
	http::Server server(server_config, loop);

	int server_num_requests = 0;

	server.AsyncServeUrl(
		"http://127.0.0.1:" TEST_PORT,
		[](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
		},
		[&server_num_requests](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			auto req = exp_req.value();

			auto result = exp_req.value()->MakeResponse();
			ASSERT_TRUE(result);
			auto resp = result.value();

			server_num_requests++;
			if (server_num_requests == 1) {
				resp->SetHeader("Content-Length", "15");
				auto string_reader = make_shared<io::StringReader>("abcde");
				resp->SetBodyReader(string_reader);
				resp->SetStatusCodeAndMessage(200, "Success");
			} else if (server_num_requests == 2) {
				auto exp_header = req->GetHeader("Range");
				EXPECT_TRUE(exp_header);
				if (exp_header) {
					EXPECT_EQ(exp_header.value(), "bytes=5-14");
				}

				auto string_reader = make_shared<io::StringReader>("fghij");
				resp->SetBodyReader(string_reader);
				resp->SetHeader("Content-Range", "bytes 5-14/15");
				resp->SetHeader("Content-Length", "10");
				resp->SetStatusCodeAndMessage(206, "Partial Content");
			} else if (server_num_requests == 3) {
				auto exp_header = req->GetHeader("Range");
				EXPECT_TRUE(exp_header);
				if (exp_header) {
					EXPECT_EQ(exp_header.value(), "bytes=10-14");
				}

				auto string_reader = make_shared<io::StringReader>("");
				resp->SetBodyReader(string_reader);
				resp->SetHeader("Content-Range", "bytes 10-14/15");
				resp->SetHeader("Content-Length", "5");
				resp->SetStatusCodeAndMessage(206, "Partial Content");
			} else if (server_num_requests == 4) {
				auto exp_header = req->GetHeader("Range");
				EXPECT_TRUE(exp_header);
				if (exp_header) {
					EXPECT_EQ(exp_header.value(), "bytes=10-14");
				}

				auto string_reader = make_shared<io::StringReader>("12345");
				resp->SetBodyReader(string_reader);
				resp->SetHeader("Content-Range", "bytes 10-14/15");
				resp->SetHeader("Content-Length", "5");
				resp->SetStatusCodeAndMessage(206, "Partial Content");
			} else {
				FAIL() << "Should not get here";
			}
			resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
		});

	// Request
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("http://127.0.0.1:" TEST_PORT);

	// Client
	http::ClientConfig client_config;
	shared_ptr<http_resumer::DownloadResumerClient> client =
		make_shared<http_resumer::DownloadResumerClient>(client_config, loop);
	client->SetSmallestWaitInterval(chrono::milliseconds(100));

	int user_num_callbacks = 0;
	vector<uint8_t> received_body;

	http::ResponseHandler user_header_handler =
		[&received_body, &user_num_callbacks](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();
			auto resp = exp_resp.value();

			user_num_callbacks++;

			auto content_length = resp->GetHeader("Content-Length");
			ASSERT_TRUE(content_length);
			ASSERT_EQ(content_length.value(), "15");

			auto body_writer = make_shared<io::ByteWriter>(received_body);
			body_writer->SetUnlimited(true);
			resp->SetBodyWriter(body_writer);
		};

	http::ResponseHandler user_body_handler = [&loop](http::ExpectedIncomingResponsePtr exp_resp) {
		EXPECT_TRUE(exp_resp);
		loop.Stop();
	};

	auto err = client->AsyncCall(req, user_header_handler, user_body_handler);
	EXPECT_EQ(err, error::NoError) << "Unexpected error: " << err.message;

	loop.Run();

	EXPECT_EQ(server_num_requests, 4);
	EXPECT_EQ(user_num_callbacks, 1);
	EXPECT_EQ(
		received_body,
		(vector<uint8_t> {
			'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', '1', '2', '3', '4', '5'}));
}

TEST_F(DownloadResumerTest, OtherStatusThan200) {
	TestEventLoop loop(chrono::seconds(10));

	// Server
	http::ServerConfig server_config;
	http::Server server(server_config, loop);

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

			resp->SetStatusCodeAndMessage(204, "No artifact, no need to resume");
			resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
		});

	// Request
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("http://127.0.0.1:" TEST_PORT);

	// Client
	http::ClientConfig client_config;
	shared_ptr<http_resumer::DownloadResumerClient> client =
		make_shared<http_resumer::DownloadResumerClient>(client_config, loop);
	client->SetSmallestWaitInterval(chrono::milliseconds(100));

	client->AsyncCall(
		req,
		[](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();
			auto resp = exp_resp.value();

			EXPECT_EQ(resp->GetStatusCode(), 204);
			EXPECT_EQ(resp->GetStatusMessage(), "No artifact, no need to resume");
		},
		[&loop](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();
			auto resp = exp_resp.value();

			EXPECT_EQ(resp->GetStatusCode(), 204);
			EXPECT_EQ(resp->GetStatusMessage(), "No artifact, no need to resume");

			loop.Stop();
		});

	loop.Run();
}

TEST_F(DownloadResumerTest, ContentLengthZero) {
	TestEventLoop loop(chrono::seconds(10));

	// Server
	http::ServerConfig server_config;
	http::Server server(server_config, loop);

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

			resp->SetStatusCodeAndMessage(200, "OK, almost");
			resp->SetHeader("Content-Length", "0");
			resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
		});

	// Request
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("http://127.0.0.1:" TEST_PORT);

	// Client
	http::ClientConfig client_config;
	shared_ptr<http_resumer::DownloadResumerClient> client =
		make_shared<http_resumer::DownloadResumerClient>(client_config, loop);
	client->SetSmallestWaitInterval(chrono::milliseconds(100));

	bool body_handler_called = false;

	client->AsyncCall(
		req,
		[](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();
			auto resp = exp_resp.value();

			EXPECT_EQ(resp->GetStatusCode(), 200);
			EXPECT_EQ(resp->GetStatusMessage(), "OK, almost");
		},
		[&loop, &body_handler_called](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();
			auto resp = exp_resp.value();

			EXPECT_EQ(resp->GetStatusCode(), 200);
			EXPECT_EQ(resp->GetStatusMessage(), "OK, almost");

			body_handler_called = true;

			loop.Stop();
		});

	loop.Run();

	EXPECT_TRUE(body_handler_called);
}

TEST_F(DownloadResumerTest, UserCancelInHeaderHandler) {
	TestEventLoop loop(chrono::seconds(10));

	// Server
	http::ServerConfig server_config;
	http::Server server(server_config, loop);

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

			resp->SetHeader("Content-Length", to_string(RangeBodyOfXes::TARGET_BODY_SIZE));
			resp->SetBodyReader(make_shared<RangeBodyOfXes>());
			resp->SetStatusCodeAndMessage(200, "Success");
			resp->AsyncReply([](error::Error err) { ASSERT_NE(error::NoError, err); });
		});

	// Request
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("http://127.0.0.1:" TEST_PORT);

	// Client
	http::ClientConfig client_config;
	shared_ptr<http_resumer::DownloadResumerClient> client =
		make_shared<http_resumer::DownloadResumerClient>(client_config, loop);
	client->SetSmallestWaitInterval(chrono::milliseconds(100));

	bool body_handler_called = false;

	client->AsyncCall(
		req,
		[&client](http::ExpectedIncomingResponsePtr exp_resp) { client->Cancel(); },
		[&loop, &body_handler_called](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_FALSE(exp_resp);
			EXPECT_EQ(exp_resp.error().code, make_error_condition(errc::operation_canceled));
			body_handler_called = true;
			loop.Stop();
		});

	loop.Run();

	EXPECT_TRUE(body_handler_called);
}

TEST_F(DownloadResumerTest, UserCancelInBodyHandler) {
	TestEventLoop loop(chrono::seconds(10));

	// Server
	http::ServerConfig server_config;
	http::Server server(server_config, loop);

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

			resp->SetHeader("Content-Length", to_string(RangeBodyOfXes::TARGET_BODY_SIZE));
			resp->SetBodyReader(make_shared<RangeBodyOfXes>());
			resp->SetStatusCodeAndMessage(200, "Success");
			resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
		});

	// Request
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("http://127.0.0.1:" TEST_PORT);

	// Client
	http::ClientConfig client_config;
	shared_ptr<http_resumer::DownloadResumerClient> client =
		make_shared<http_resumer::DownloadResumerClient>(client_config, loop);
	client->SetSmallestWaitInterval(chrono::milliseconds(100));

	bool body_handler_called = false;

	client->AsyncCall(
		req,
		[](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();
			auto resp = exp_resp.value();
			auto body_writer = make_shared<io::Discard>();
			resp->SetBodyWriter(body_writer);
		},
		[&loop, &body_handler_called](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_FALSE(exp_resp);
			EXPECT_EQ(exp_resp.error().code, make_error_condition(errc::operation_canceled));
			body_handler_called = true;
			loop.Stop();
		});

	events::Timer timer(loop);
	timer.AsyncWait(chrono::milliseconds(10), [&client](error::Error err) { client->Cancel(); });

	loop.Run();

	EXPECT_TRUE(body_handler_called);
}

TEST_F(DownloadResumerTest, UserDestroysReader) {
	TestEventLoop loop(chrono::seconds(10));

	// Server
	http::ServerConfig server_config;
	http::Server server(server_config, loop);

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

			resp->SetHeader("Content-Length", to_string(RangeBodyOfXes::TARGET_BODY_SIZE));
			resp->SetBodyReader(make_shared<RangeBodyOfXes>());
			resp->SetStatusCodeAndMessage(200, "Success");
			resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
		});

	// Request
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("http://127.0.0.1:" TEST_PORT);

	// Client
	http::ClientConfig client_config;
	shared_ptr<http_resumer::DownloadResumerClient> client =
		make_shared<http_resumer::DownloadResumerClient>(client_config, loop);
	client->SetSmallestWaitInterval(chrono::milliseconds(100));

	bool body_handler_called = false;

	io::AsyncReaderPtr reader;
	auto err = client->AsyncCall(
		req,
		[&reader](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();
			auto resp = exp_resp.value();
			auto body_writer = make_shared<io::Discard>();
			auto exp_reader = resp->MakeBodyAsyncReader();
			ASSERT_TRUE(exp_reader);
			reader = exp_reader.value();
		},
		[&loop, &body_handler_called](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_FALSE(exp_resp);
			EXPECT_EQ(exp_resp.error().code, make_error_condition(errc::operation_canceled));
			body_handler_called = true;
			loop.Stop();
		});
	ASSERT_EQ(err, error::NoError);

	events::Timer timer(loop);
	timer.AsyncWait(chrono::milliseconds(10), [&reader](error::Error err) { reader.reset(); });

	loop.Run();

	// It should now be possibly to make a new request, but we won't complete it.
	err = client->AsyncCall(
		req, [](http::ExpectedIncomingResponsePtr) {}, [](http::ExpectedIncomingResponsePtr) {});
	ASSERT_EQ(err, error::NoError);

	EXPECT_TRUE(body_handler_called);
}
