// Copyright 2026 Northern.tech AS
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

#include <common/platform/varlink.hpp>

#include <fcntl.h>
#include <sys/resource.h>
#include <sys/stat.h>
#include <unistd.h>

#include <memory>
#include <string>
#include <vector>

#include <boost/asio.hpp>
#include <gtest/gtest.h>

#include <common/events.hpp>
#include <common/json.hpp>
#include <common/path.hpp>
#include <common/testing.hpp>

using namespace std;

namespace asio = boost::asio;
namespace error = mender::common::error;
namespace events = mender::common::events;
namespace json = mender::common::json;
namespace mtesting = mender::common::testing;
namespace varlink = mender::common::varlink;

using TestEventLoop = mtesting::TestEventLoop;

const string kSep {varlink::kMessageSeparator};

// Raw client socket + read buffer on the same loop as the server.
class TestClient : public events::EventLoopObject, public std::enable_shared_from_this<TestClient> {
public:
	TestClient(events::EventLoop &loop, const string &path) :
		socket_ {GetAsioIoContext(loop)},
		path_ {path} {
	}

	asio::local::stream_protocol::socket &Socket() {
		return socket_;
	}

	void Connect(function<void()> on_connected) {
		auto self = shared_from_this();
		socket_.async_connect(
			asio::local::stream_protocol::endpoint {path_},
			[self, on_connected](const boost::system::error_code &ec) {
				ASSERT_FALSE(ec) << ec.message();
				on_connected();
			});
	}

	void Write(const string &frame, function<void()> on_written) {
		auto self = shared_from_this();
		auto data = make_shared<string>(frame);
		asio::async_write(
			socket_,
			asio::buffer(*data),
			[self, data, on_written](const boost::system::error_code &ec, size_t) {
				ASSERT_FALSE(ec) << ec.message();
				on_written();
			});
	}

	void ReadFrame(function<void(const string &)> on_frame) {
		auto self = shared_from_this();
		asio::async_read_until(
			socket_,
			buf_,
			varlink::kMessageSeparator,
			[self, on_frame](const boost::system::error_code &ec, size_t) {
				ASSERT_FALSE(ec) << ec.message();
				std::istream is(&self->buf_);
				string frame;
				std::getline(is, frame, varlink::kMessageSeparator);
				on_frame(frame);
			});
	}

	// Keeps reading frames until the loop stops.
	void ReadFrames(function<void(const string &)> on_frame) {
		auto self = shared_from_this();
		ReadFrame([self, on_frame](const string &frame) {
			on_frame(frame);
			self->ReadFrames(on_frame);
		});
	}

private:
	asio::local::stream_protocol::socket socket_;
	asio::streambuf buf_;
	string path_;
};

// Connects, writes one call frame and hands every reply frame to on_frame.
void Call(
	shared_ptr<TestClient> client, const string &call, function<void(const string &)> on_frame) {
	client->Connect([client, call, on_frame]() {
		client->Write(call + kSep, [client, on_frame]() { client->ReadFrames(on_frame); });
	});
}

string GetString(const json::Json &j, const string &key) {
	auto exp = j.Get(key).and_then([](const json::Json &v) { return v.GetString(); });
	return exp ? exp.value() : "";
}

// Asserts frame is an error reply of the given name carrying parameters[key] == value.
void ExpectErrorReply(
	const string &frame, const string &error_name, const string &key, const string &value) {
	auto j = json::Load(frame);
	ASSERT_TRUE(j) << frame;
	EXPECT_EQ(GetString(j.value(), "error"), error_name);
	EXPECT_EQ(GetString(j.value().Get("parameters").value(), key), value);
}

class VarlinkServerTest : public testing::Test {
protected:
	void SetUp() override {
		ASSERT_EQ(server.Listen(socket_path), error::NoError);
	}

	shared_ptr<TestClient> Client() {
		return make_shared<TestClient>(loop, socket_path);
	}

	// Connects a fresh client, sends calls and runs the loop until n frames came back.
	vector<string> Exchange(const string &calls, size_t n = 1) {
		auto frames = make_shared<vector<string>>();
		Call(Client(), calls, [this, frames, n](const string &frame) {
			frames->push_back(frame);
			if (frames->size() == n) {
				loop.Stop();
			}
		});
		loop.Run();
		return *frames;
	}

	// Echoes the call parameters back.
	void AddEcho() {
		server.AddMethodHandler(
			"io.test.Echo", [](const varlink::MethodCall &call, const varlink::Replier &) {
				varlink::Reply r;
				r.parameters = call.parameters;
				return r;
			});
	}

	// Replies at once; pipelined behind another call it shows whether ordering holds.
	void AddPing() {
		server.AddMethodHandler(
			"io.test.Ping", [](const varlink::MethodCall &, const varlink::Replier &) {
				varlink::Reply r;
				r.parameters.Set("pong", true);
				return r;
			});
	}

	// Defers the reply: hands the Replier to stored and returns nullopt.
	void AddDeferred(const string &method, varlink::Replier &stored) {
		server.AddMethodHandler(
			method, [&stored](const varlink::MethodCall &, const varlink::Replier &emit) {
				stored = emit;
				return mender::nullopt;
			});
	}

	// Sends frame and expects the server to hang up instead of replying.
	void ExpectConnectionDropped(const string &frame) {
		boost::system::error_code read_ec;
		char byte;
		auto client = Client();
		client->Connect([&]() {
			client->Write(frame + kSep, [&]() {
				asio::async_read(
					client->Socket(),
					asio::buffer(&byte, 1),
					[&](const boost::system::error_code &ec, size_t) {
						read_ec = ec;
						loop.Stop();
					});
			});
		});
		loop.Run();
		EXPECT_TRUE(read_ec) << "expected the server to close the connection";
	}

	TestEventLoop loop;
	mtesting::TemporaryDirectory tmpdir;
	string socket_path {tmpdir.Path() + "/varlink.sock"};
	varlink::Server server {loop};
};

TEST_F(VarlinkServerTest, DispatchesMethodAndReplies) {
	AddEcho();

	auto frames = Exchange(R"({"method":"io.test.Echo","parameters":{"hello":"world"}})");

	ASSERT_EQ(frames.size(), 1);
	EXPECT_EQ(frames[0], R"({"parameters":{"hello":"world"}})");
}

TEST_F(VarlinkServerTest, UnknownMethodReturnsError) {
	AddPing();

	auto frames = Exchange(R"({"method":"io.test.DoesNotExist","parameters":{}})");

	ASSERT_EQ(frames.size(), 1);
	ExpectErrorReply(
		frames[0], "org.varlink.service.MethodNotFound", "method", "io.test.DoesNotExist");
}

TEST_F(VarlinkServerTest, UnknownInterfaceReturnsInterfaceNotFound) {
	auto frames = Exchange(
		R"({"method":"io.nope.Ping"})" + kSep + R"({"method":"NoDot"})" + kSep
			+ R"({"method":"org.varlink.service.Nope"})",
		3);

	ASSERT_EQ(frames.size(), 3);
	ExpectErrorReply(frames[0], "org.varlink.service.InterfaceNotFound", "interface", "io.nope");
	ExpectErrorReply(frames[1], "org.varlink.service.InterfaceNotFound", "interface", "");
	ExpectErrorReply(
		frames[2], "org.varlink.service.MethodNotFound", "method", "org.varlink.service.Nope");
}

TEST_F(VarlinkServerTest, MalformedJsonDropsConnection) {
	ExpectConnectionDropped("not json");
}

TEST_F(VarlinkServerTest, MissingMethodReturnsError) {
	auto frames = Exchange(R"({"parameters":{}})");

	ASSERT_EQ(frames.size(), 1);
	ExpectErrorReply(frames[0], "org.varlink.service.InvalidParameter", "parameter", "method");
}

TEST_F(VarlinkServerTest, GetInterfaceDescriptionWithoutInterfaceReturnsError) {
	auto frames = Exchange(
		R"({"method":"org.varlink.service.GetInterfaceDescription","parameters":{"interface":1}})");

	ASSERT_EQ(frames.size(), 1);
	ExpectErrorReply(frames[0], "org.varlink.service.InvalidParameter", "parameter", "interface");
}

TEST_F(VarlinkServerTest, OnewayGetsNoReplyEvenOnError) {
	bool handler_called = false;

	server.AddMethodHandler(
		"io.test.Fire",
		[&handler_called](const varlink::MethodCall &call, const varlink::Replier &) {
			EXPECT_TRUE(call.oneway);
			handler_called = true;
			return varlink::Reply {};
		});
	AddEcho();

	// Two oneway calls (one to an unknown method), then a normal call. The first
	// frame we read must belong to the normal call.
	auto frames = Exchange(
		R"({"method":"io.test.Fire","oneway":true})" + kSep
		+ R"({"method":"io.test.DoesNotExist","oneway":true})" + kSep
		+ R"({"method":"io.test.Echo","parameters":{"n":1}})");

	EXPECT_TRUE(handler_called);
	ASSERT_EQ(frames.size(), 1);
	EXPECT_EQ(frames[0], R"({"parameters":{"n":1}})");
}

TEST_F(VarlinkServerTest, ErrorReplyEscapesControlCharacters) {
	AddPing();

	auto frames = Exchange(R"({"method":"io.test.\u0001\u001f\"x"})");

	ASSERT_EQ(frames.size(), 1);
	ExpectErrorReply(
		frames[0], "org.varlink.service.MethodNotFound", "method", string("io.test.\x01\x1f\"x"));
}

TEST_F(VarlinkServerTest, StreamingRepliesWithMore) {
	server.AddMethodHandler(
		"io.test.Count", [](const varlink::MethodCall &call, const varlink::Replier &emit) {
			EXPECT_TRUE(call.more);
			for (int i = 1; i <= 3; i++) {
				varlink::Reply r;
				r.parameters.Set("n", i);
				r.continues = true;
				EXPECT_TRUE(emit(r));
			}
			varlink::Reply done;
			done.parameters.Set("n", 4);
			return done;
		});

	auto frames = Exchange(R"({"method":"io.test.Count","more":true})", 4);

	ASSERT_EQ(frames.size(), 4);
	for (size_t i = 0; i < frames.size(); i++) {
		auto j = json::Load(frames[i]);
		ASSERT_TRUE(j) << j.error().String();
		auto n =
			j.value().Get("parameters").and_then([](const json::Json &p) { return p.Get("n"); });
		ASSERT_TRUE(n);
		EXPECT_EQ(n.value().Get<int64_t>().value(), static_cast<int64_t>(i + 1));
		auto continues = j.value().Get("continues");
		if (i < 3) {
			ASSERT_TRUE(continues) << frames[i];
			EXPECT_TRUE(continues.value().GetBool().value());
		} else {
			EXPECT_FALSE(continues) << frames[i];
		}
	}
}

TEST_F(VarlinkServerTest, IntermediateFrameRefusedWithoutMore) {
	server.AddMethodHandler(
		"io.test.Count", [](const varlink::MethodCall &call, const varlink::Replier &emit) {
			EXPECT_FALSE(call.more);
			varlink::Reply intermediate;
			intermediate.continues = true;
			EXPECT_FALSE(emit(intermediate));
			return varlink::Reply {};
		});

	auto frames = Exchange(R"({"method":"io.test.Count"})");

	// Only the terminal reply, with no "continues".
	ASSERT_EQ(frames.size(), 1);
	EXPECT_EQ(frames[0], R"({"parameters":{}})");
}

TEST_F(VarlinkServerTest, ServiceIntrospection) {
	const string idl = "interface io.test\n\nmethod Echo(hello: string) -> (hello: string)\n";

	server.SetServiceInfo("Vendor", "Product", "1.2.3", "https://example.com");
	server.AddInterface("io.test", idl);

	auto frames = Exchange(
		R"({"method":"org.varlink.service.GetInfo"})" + kSep
			+ R"({"method":"org.varlink.service.GetInterfaceDescription","parameters":{"interface":"io.test"}})"
			+ kSep
			+ R"({"method":"org.varlink.service.GetInterfaceDescription","parameters":{"interface":"io.nope"}})",
		3);

	ASSERT_EQ(frames.size(), 3);

	auto info = json::Load(frames[0]);
	ASSERT_TRUE(info) << info.error().String();
	auto params = info.value().Get("parameters").value();
	EXPECT_EQ(GetString(params, "vendor"), "Vendor");
	EXPECT_EQ(GetString(params, "product"), "Product");
	EXPECT_EQ(GetString(params, "version"), "1.2.3");
	EXPECT_EQ(GetString(params, "url"), "https://example.com");
	auto interfaces = json::ToStringVector(params.Get("interfaces").value());
	ASSERT_TRUE(interfaces);
	EXPECT_EQ(interfaces.value(), (vector<string> {"io.test", "org.varlink.service"}));

	auto desc = json::Load(frames[1]);
	ASSERT_TRUE(desc) << desc.error().String();
	EXPECT_FALSE(desc.value().Get("error"));
	EXPECT_EQ(GetString(desc.value().Get("parameters").value(), "description"), idl);

	ExpectErrorReply(frames[2], "org.varlink.service.InterfaceNotFound", "interface", "io.nope");
}

TEST(VarlinkServerListen, SocketPermissionsAndStaleSocketReplaced) {
	TestEventLoop loop;
	mtesting::TemporaryDirectory tmpdir;
	string socket_path = tmpdir.Path() + "/sub/dir/varlink.sock";

	{
		varlink::Server server {loop};
		ASSERT_EQ(server.Listen(socket_path), error::NoError);
		struct stat st;
		ASSERT_EQ(stat(socket_path.c_str(), &st), 0);
		EXPECT_TRUE(S_ISSOCK(st.st_mode));
		EXPECT_EQ(st.st_mode & 0777, 0660);
		// Parent dirs we created ourselves are owner+group only as well.
		ASSERT_EQ(stat((tmpdir.Path() + "/sub/dir").c_str(), &st), 0);
		EXPECT_EQ(st.st_mode & 0777, 0750);
		server.Cancel();
		EXPECT_FALSE(mender::common::path::FileExists(socket_path));
	}

	// Stale socket file from a crashed run must not block Listen.
	ASSERT_EQ(mknod(socket_path.c_str(), S_IFSOCK | 0600, 0), 0);
	varlink::Server server {loop};
	EXPECT_EQ(server.Listen(socket_path), error::NoError);
}

TEST(VarlinkServerListen, ListenRefusesSocketInUse) {
	TestEventLoop loop;
	mtesting::TemporaryDirectory tmpdir;
	string socket_path = tmpdir.Path() + "/varlink.sock";

	varlink::Server first {loop};
	ASSERT_EQ(first.Listen(socket_path), error::NoError);

	// A second instance must not steal the socket from the live one.
	varlink::Server second {loop};
	auto err = second.Listen(socket_path);
	EXPECT_NE(err, error::NoError);
	EXPECT_EQ(err.code, varlink::MakeError(varlink::TransportError, "").code);
	EXPECT_NE(err.String().find("Varlink transport error"), string::npos) << err.String();
	EXPECT_TRUE(mender::common::path::FileExists(socket_path));
}

TEST(VarlinkServerListen, ListenFailsWhenParentCannotBeCreated) {
	TestEventLoop loop;
	mtesting::TemporaryDirectory tmpdir;
	// A regular file where the socket directory tree should go.
	string blocker = tmpdir.Path() + "/file";
	ASSERT_EQ(mknod(blocker.c_str(), S_IFREG | 0600, 0), 0);

	varlink::Server server {loop};
	auto err = server.Listen(blocker + "/sub/varlink.sock");
	EXPECT_NE(err, error::NoError);
	EXPECT_NE(err.String().find("Could not create varlink socket directory"), string::npos)
		<< err.String();
}

TEST(VarlinkServerListen, ListenRejectsTooLongSocketPath) {
	TestEventLoop loop;
	mtesting::TemporaryDirectory tmpdir;
	string socket_path = tmpdir.Path() + "/" + string(200, 'x') + ".sock";

	varlink::Server server {loop};
	auto err = server.Listen(socket_path);
	EXPECT_NE(err, error::NoError);
	EXPECT_EQ(err.code, varlink::MakeError(varlink::TransportError, "").code);
}

TEST_F(VarlinkServerTest, StreamStaysOpenUntilTerminalEmit) {
	// Handler returns nullopt and finishes the call later, like a signal feed would.
	varlink::Replier stored;
	AddDeferred("io.test.Subscribe", stored);

	events::Timer timer {loop};
	timer.AsyncWait(chrono::milliseconds(100), [&](error::Error) {
		ASSERT_TRUE(stored);
		varlink::Reply r;
		r.parameters.Set("n", 1);
		r.continues = true;
		EXPECT_TRUE(stored(r));
		r.parameters.Set("n", 2);
		r.continues = false;
		EXPECT_TRUE(stored(r));
	});
	auto frames = Exchange(R"({"method":"io.test.Subscribe","more":true})", 2);

	ASSERT_EQ(frames.size(), 2);
	EXPECT_EQ(frames[0], R"({"continues":true,"parameters":{"n":1}})");
	EXPECT_EQ(frames[1], R"({"parameters":{"n":2}})");
}

TEST_F(VarlinkServerTest, DeferredSingleReplyWithoutMore) {
	// A plain call may also be answered later through the Replier; a call
	// pipelined behind it waits like it would behind a stream.
	varlink::Replier stored;
	AddDeferred("io.test.Slow", stored);
	AddPing();

	events::Timer timer {loop};
	timer.AsyncWait(chrono::milliseconds(100), [&](error::Error) {
		ASSERT_TRUE(stored);
		varlink::Reply done;
		done.parameters.Set("slow", true);
		EXPECT_TRUE(stored(done));
		EXPECT_FALSE(stored(done)) << "second terminal must be refused";
	});
	// Frame order proves Ping waited: its reply must come after the deferred one.
	auto frames =
		Exchange(R"({"method":"io.test.Slow"})" + kSep + R"({"method":"io.test.Ping"})", 2);

	ASSERT_EQ(frames.size(), 2);
	EXPECT_EQ(frames[0], R"({"parameters":{"slow":true}})");
	EXPECT_EQ(frames[1], R"({"parameters":{"pong":true}})");
}

TEST_F(VarlinkServerTest, EmitAfterTerminalIsRefused) {
	// Once a handler emits its terminal frame the call is done: a stale emit and a
	// returned continues=true stay off the wire, nullopt keeps nothing open, and
	// the pipelined Ping is answered either way.
	server.AddMethodHandler(
		"io.test.StreamReturn", [](const varlink::MethodCall &, const varlink::Replier &emit) {
			varlink::Reply r;
			r.parameters.Set("n", 1);
			EXPECT_TRUE(emit(r));
			r.parameters.Set("n", 2);
			EXPECT_FALSE(emit(r));
			r.continues = true;
			return mender::optional<varlink::Reply> {r};
		});
	server.AddMethodHandler(
		"io.test.StreamNullopt", [](const varlink::MethodCall &, const varlink::Replier &emit) {
			varlink::Reply r;
			r.parameters.Set("n", 1);
			EXPECT_TRUE(emit(r));
			return mender::nullopt;
		});
	server.AddMethodHandler(
		"io.test.Ping", [](const varlink::MethodCall &, const varlink::Replier &) {
			varlink::Reply r;
			r.continues = true;
			return r;
		});

	auto frames = Exchange(
		R"({"method":"io.test.StreamReturn","more":true})" + kSep
			+ R"({"method":"io.test.StreamNullopt","more":true})" + kSep
			+ R"({"method":"io.test.Ping"})",
		3);

	ASSERT_EQ(frames.size(), 3);
	EXPECT_EQ(frames[0], R"({"parameters":{"n":1}})");
	EXPECT_EQ(frames[1], R"({"parameters":{"n":1}})");
	EXPECT_EQ(frames[2], R"({"parameters":{}})");
}

TEST_F(VarlinkServerTest, FrameSplitAcrossWrites) {
	AddEcho();

	string call = R"({"method":"io.test.Echo","parameters":{"x":42}})";
	string first = call.substr(0, 20);
	string second = call.substr(20) + kSep;

	string reply;
	auto client = Client();
	client->Connect([&]() {
		client->Write(first, [&]() {
			client->Write(second, [&]() {
				client->ReadFrame([&](const string &frame) {
					reply = frame;
					loop.Stop();
				});
			});
		});
	});
	loop.Run();

	EXPECT_EQ(reply, R"({"parameters":{"x":42}})");
}

TEST_F(VarlinkServerTest, EmptyFrameDropsConnection) {
	ExpectConnectionDropped("");
}

TEST_F(VarlinkServerTest, OversizedFrameDropsConnection) {
	// More than kMaxFrameSize bytes and never a separator.
	auto data = make_shared<string>(varlink::kMaxFrameSize + 1, 'x');
	boost::system::error_code read_ec;
	char byte;
	auto client = Client();
	client->Connect([&]() {
		// The server may hang up before we finish writing, so ignore write errors.
		asio::async_write(
			client->Socket(), asio::buffer(*data), [&](const boost::system::error_code &, size_t) {
				asio::async_read(
					client->Socket(),
					asio::buffer(&byte, 1),
					[&](const boost::system::error_code &ec, size_t) {
						read_ec = ec;
						loop.Stop();
					});
			});
	});
	loop.Run();

	EXPECT_TRUE(read_ec) << "expected the server to close the connection";
}

TEST_F(VarlinkServerTest, WriteQueueOverflowDropsPeer) {
	// Emitting synchronously means nothing drains until the handler returns, so
	// the queue fills deterministically regardless of socket buffer sizes.
	size_t emitted = 0;
	server.AddMethodHandler(
		"io.test.Flood", [&emitted](const varlink::MethodCall &, const varlink::Replier &emit) {
			varlink::Reply r;
			r.continues = true;
			while (emit(r)) {
				emitted++;
			}
			return mender::nullopt;
		});

	boost::system::error_code read_ec;
	asio::streambuf received;
	auto client = Client();
	client->Connect([&]() {
		client->Write(R"({"method":"io.test.Flood","more":true})" + kSep, [&]() {
			// Read until the server hangs up on us.
			asio::async_read(
				client->Socket(), received, [&](const boost::system::error_code &ec, size_t) {
					read_ec = ec;
					loop.Stop();
				});
		});
	});
	loop.Run();

	EXPECT_EQ(emitted, varlink::kMaxQueuedFrames);
	EXPECT_EQ(read_ec, asio::error::eof);
}

TEST_F(VarlinkServerTest, CancelClosesLiveConnections) {
	boost::system::error_code read_ec;
	char byte;
	events::Timer timer {loop};
	auto client = Client();
	client->Connect([&]() {
		asio::async_read(
			client->Socket(),
			asio::buffer(&byte, 1),
			[&](const boost::system::error_code &ec, size_t) {
				read_ec = ec;
				loop.Stop();
			});
		// Let the server accept us before pulling the plug.
		timer.AsyncWait(chrono::milliseconds(100), [&](error::Error) { server.Cancel(); });
	});
	loop.Run();

	EXPECT_TRUE(read_ec) << "expected the server to close the connection";
	EXPECT_FALSE(mender::common::path::FileExists(socket_path));
}

TEST_F(VarlinkServerTest, AcceptErrorBacksOffAndRecovers) {
#ifdef MENDER_TEST_SKIP_NOFILE_BASED_TESTS
	GTEST_SKIP();
#endif
	AddPing();
	auto client = Client();
	// Open it now: once capped, only the server's accept() needs a new fd.
	client->Socket().open();

	// Cap the fd limit at the lowest free fd so accept() fails with EMFILE.
	int lowest_free = open("/dev/null", O_RDONLY);
	ASSERT_GE(lowest_free, 0);
	close(lowest_free);
	rlimit orig;
	ASSERT_EQ(getrlimit(RLIMIT_NOFILE, &orig), 0);
	rlimit capped = orig;
	capped.rlim_cur = lowest_free;

	string reply;
	events::Timer lift {loop};
	testing::internal::CaptureStderr();
	EXPECT_EQ(setrlimit(RLIMIT_NOFILE, &capped), 0);
	Call(client, R"({"method":"io.test.Ping"})", [&](const string &frame) {
		reply = frame;
		loop.Stop();
	});
	lift.AsyncWait(
		chrono::milliseconds(200), [&](error::Error) { setrlimit(RLIMIT_NOFILE, &orig); });
	loop.Run();
	setrlimit(RLIMIT_NOFILE, &orig);
	string captured = testing::internal::GetCapturedStderr();

	// One failure, then the delayed retry picks up the waiting client.
	size_t errors = 0;
	for (auto pos = captured.find("Could not accept"); pos != string::npos;
		 pos = captured.find("Could not accept", pos + 1)) {
		errors++;
	}
	EXPECT_EQ(errors, 1);
	EXPECT_NE(reply.find("pong"), string::npos) << reply;
}

TEST_F(VarlinkServerTest, ErrorReplyNeverCarriesContinues) {
	// error+continues is a protocol violation (sd-varlink drops the connection).
	server.AddMethodHandler(
		"io.test.Fail", [](const varlink::MethodCall &, const varlink::Replier &) {
			varlink::Reply r;
			r.error_name = "io.test.Broken";
			r.continues = true;
			return r;
		});

	auto frames = Exchange(R"({"method":"io.test.Fail"})");

	ASSERT_EQ(frames.size(), 1);
	EXPECT_EQ(frames[0], R"({"error":"io.test.Broken","parameters":{}})");
}

TEST_F(VarlinkServerTest, OnewayIsSuppressedEvenWhenStreaming) {
	// oneway suppresses streamed frames too, not only the terminal reply.
	bool first_emit_result = true;
	server.AddMethodHandler(
		"io.test.Count",
		[&first_emit_result](const varlink::MethodCall &call, const varlink::Replier &emit) {
			EXPECT_TRUE(call.oneway);
			EXPECT_TRUE(call.more);
			varlink::Reply r;
			r.parameters.Set("n", 1);
			r.continues = true;
			first_emit_result = emit(r);
			varlink::Reply done;
			done.parameters.Set("n", 2);
			return done;
		});
	AddPing();

	auto frames = Exchange(
		R"({"method":"io.test.Count","more":true,"oneway":true})" + kSep
		+ R"({"method":"io.test.Ping"})");

	EXPECT_FALSE(first_emit_result) << "emit() must refuse a oneway stream";
	ASSERT_EQ(frames.size(), 1) << "only the Ping reply, nothing from the oneway stream";
	EXPECT_EQ(frames[0], R"({"parameters":{"pong":true}})");
}

TEST_F(VarlinkServerTest, NonObjectParametersRejected) {
	bool handler_called = false;
	server.AddMethodHandler(
		"io.test.Echo", [&handler_called](const varlink::MethodCall &, const varlink::Replier &) {
			handler_called = true;
			return varlink::Reply {};
		});

	auto frames = Exchange(R"({"method":"io.test.Echo","parameters":42})");

	EXPECT_FALSE(handler_called);
	ASSERT_EQ(frames.size(), 1);
	ExpectErrorReply(frames[0], "org.varlink.service.InvalidParameter", "parameter", "parameters");
}

TEST_F(VarlinkServerTest, NullParametersTreatedAsAbsent) {
	// Method-Call declares parameters as `?object`; sd-varlink sends/accepts null.
	bool handler_called = false;
	server.AddMethodHandler(
		"io.test.Echo",
		[&handler_called](const varlink::MethodCall &call, const varlink::Replier &) {
			handler_called = true;
			EXPECT_FALSE(call.parameters.Get("x"));
			return varlink::Reply {};
		});

	auto frames = Exchange(R"({"method":"io.test.Echo","parameters":null})");

	EXPECT_TRUE(handler_called);
	ASSERT_EQ(frames.size(), 1);
	EXPECT_EQ(frames[0], R"({"parameters":{}})");
}

TEST_F(VarlinkServerTest, PipelinedCallReplyStaysInOrderBehindDeferredCall) {
	// A Ping pipelined behind a deferred call must wait for that call's reply
	// (varlink.org/FAQ: "all messages are strictly in order on the same connection").
	varlink::Replier stored;
	AddDeferred("io.test.Subscribe", stored);
	AddPing();

	events::Timer timer {loop};
	timer.AsyncWait(chrono::milliseconds(100), [&](error::Error) {
		ASSERT_TRUE(stored);
		varlink::Reply done;
		done.parameters.Set("n", 1);
		EXPECT_TRUE(stored(done));
	});
	auto frames = Exchange(
		R"({"method":"io.test.Subscribe","more":true})" + kSep + R"({"method":"io.test.Ping"})", 2);

	ASSERT_EQ(frames.size(), 2);
	EXPECT_EQ(frames[0], R"({"parameters":{"n":1}})");
	EXPECT_EQ(frames[1], R"({"parameters":{"pong":true}})");
}

TEST_F(VarlinkServerTest, EmitReturnsFalseAfterClientDisconnect) {
	// Handler keeps the Replier around like a subscription would. Reading must
	// continue while the call is outstanding, or EOF goes unnoticed; the queued
	// Ping is dropped with the connection, never dispatched.
	varlink::Replier stored;
	bool ping_called = false;
	server.AddMethodHandler(
		"io.test.Subscribe", [&stored](const varlink::MethodCall &, const varlink::Replier &emit) {
			stored = emit;
			varlink::Reply r;
			r.parameters.Set("n", 0);
			r.continues = true;
			EXPECT_TRUE(emit(r));
			return mender::nullopt;
		});
	server.AddMethodHandler(
		"io.test.Ping", [&ping_called](const varlink::MethodCall &, const varlink::Replier &) {
			ping_called = true;
			return varlink::Reply {};
		});

	auto client = Client();
	events::Timer timer {loop};
	client->Connect([&]() {
		string calls = R"({"method":"io.test.Subscribe","more":true})" + kSep
					   + R"({"method":"io.test.Ping"})" + kSep;
		client->Write(calls, [&]() {
			client->ReadFrame([&](const string &) {
				// Got the first streamed frame: hang up, then let the server notice EOF.
				client->Socket().close();
				timer.AsyncWait(chrono::milliseconds(100), [&](error::Error) { loop.Stop(); });
			});
		});
	});
	loop.Run();

	ASSERT_TRUE(stored);
	EXPECT_FALSE(stored(varlink::Reply {}));
	EXPECT_FALSE(ping_called);
}

TEST_F(VarlinkServerTest, SubscribersFanOutAndPruneDisconnected) {
	// Signal shape: Monitor with "more" subscribes, a plain Monitor call just gets
	// the current state. Emit reaches every live subscriber and drops the dead one.
	varlink::Subscribers subscribers;
	server.AddMethodHandler(
		"io.test.Monitor",
		[&subscribers](const varlink::MethodCall &call, const varlink::Replier &emit) {
			varlink::Reply state;
			state.parameters.Set("seq", 0);
			if (!call.more) {
				return mender::optional<varlink::Reply> {state};
			}
			subscribers.Add(emit);
			state.continues = true;
			EXPECT_TRUE(emit(state));
			return mender::optional<varlink::Reply> {};
		});

	vector<string> plain, a_frames, b_frames;
	auto a = Client();
	auto b = Client();
	Call(Client(), R"({"method":"io.test.Monitor"})", [&](const string &frame) {
		plain.push_back(frame);
	});
	Call(a, R"({"method":"io.test.Monitor","more":true})", [&](const string &frame) {
		a_frames.push_back(frame);
	});
	// b reads two frames and hangs up (Call() would keep reading a closed socket).
	b->Connect([&]() {
		b->Write(R"({"method":"io.test.Monitor","more":true})" + kSep, [&]() {
			b->ReadFrame([&](const string &frame) {
				b_frames.push_back(frame);
				b->ReadFrame([&](const string &frame) {
					b_frames.push_back(frame);
					b->Socket().close();
				});
			});
		});
	});

	varlink::Reply event;
	events::Timer first {loop}, second {loop}, stop {loop};
	first.AsyncWait(chrono::milliseconds(100), [&](error::Error) {
		EXPECT_EQ(subscribers.Size(), 2);
		event.parameters.Set("seq", 1);
		subscribers.Emit(event);
	});
	second.AsyncWait(chrono::milliseconds(200), [&](error::Error) {
		// b hung up after seq 1; the server has seen EOF by now.
		event.parameters.Set("seq", 2);
		subscribers.Emit(event);
		EXPECT_EQ(subscribers.Size(), 1);
	});
	stop.AsyncWait(chrono::milliseconds(300), [&](error::Error) { loop.Stop(); });
	loop.Run();

	ASSERT_EQ(plain.size(), 1);
	EXPECT_EQ(plain[0], R"({"parameters":{"seq":0}})");
	ASSERT_EQ(a_frames.size(), 3);
	EXPECT_EQ(a_frames[2], R"({"continues":true,"parameters":{"seq":2}})");
	ASSERT_EQ(b_frames.size(), 2);
	EXPECT_EQ(b_frames[1], R"({"continues":true,"parameters":{"seq":1}})");
}

TEST_F(VarlinkServerTest, PendingQueueOverflowDropsPeer) {
	varlink::Replier stored;
	AddDeferred("io.test.Subscribe", stored);

	// One call that never completes, then more pipelined behind it than we queue.
	string calls = R"({"method":"io.test.Subscribe","more":true})" + kSep;
	for (size_t i = 0; i <= varlink::kMaxQueuedFrames; i++) {
		calls += R"({"method":"io.test.Ping"})" + kSep;
	}

	boost::system::error_code read_ec;
	asio::streambuf received;
	auto client = Client();
	client->Connect([&]() {
		client->Write(calls, [&]() {
			// Read until the server hangs up on us.
			asio::async_read(
				client->Socket(), received, [&](const boost::system::error_code &ec, size_t) {
					read_ec = ec;
					loop.Stop();
				});
		});
	});
	loop.Run();

	EXPECT_EQ(read_ec, asio::error::eof);
}

TEST_F(VarlinkServerTest, PendingQueueByteBudgetDropsPeer) {
	varlink::Replier stored;
	AddDeferred("io.test.Subscribe", stored);

	// Two calls pipelined behind one that never completes: far fewer than
	// kMaxQueuedFrames, but together more than kMaxFrameSize bytes.
	const string pad(varlink::kMaxFrameSize / 2, 'x');
	string calls = R"({"method":"io.test.Subscribe","more":true})" + kSep;
	for (int i = 0; i < 2; i++) {
		calls += R"({"method":"io.test.Ping","parameters":{"pad":")" + pad + R"("}})" + kSep;
	}

	boost::system::error_code read_ec;
	asio::streambuf received;
	auto client = Client();
	client->Connect([&]() {
		client->Write(calls, [&]() {
			asio::async_read(
				client->Socket(), received, [&](const boost::system::error_code &ec, size_t) {
					read_ec = ec;
					loop.Stop();
				});
		});
	});
	loop.Run();

	EXPECT_EQ(read_ec, asio::error::eof);
}
