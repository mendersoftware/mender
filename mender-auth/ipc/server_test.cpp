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

#include <mender-auth/ipc/server.hpp>

#include <iostream>
#include <string>
#include <thread>
#include <vector>

#include <gtest/gtest.h>

#include <common/conf.hpp>
#include <common/dbus.hpp>
#include <common/error.hpp>
#include <common/events.hpp>
#include <common/expected.hpp>
#include <common/http.hpp>
#include <common/io.hpp>
#include <common/log.hpp>
#include <common/path.hpp>
#include <common/processes.hpp>
#include <common/testing.hpp>

#define TEST_PORT "8001"
#define TEST_PORT_2 "8002"

using namespace std;

namespace conf = mender::common::conf;
namespace dbus = mender::common::dbus;
namespace error = mender::common::error;
namespace events = mender::common::events;
namespace expected = mender::common::expected;
namespace http = mender::http;
namespace io = mender::common::io;
namespace mlog = mender::common::log;
namespace path = mender::common::path;
namespace procs = mender::common::processes;
namespace mtesting = mender::common::testing;

namespace ipc = mender::auth::ipc;

using TestEventLoop = mender::common::testing::TestEventLoop;

class ListenClientTests : public testing::Test {
protected:
	// Have to use static setup/teardown/data because libdbus doesn't seem to
	// respect changing value of DBUS_SYSTEM_BUS_ADDRESS environment variable
	// and keeps connecting to the first address specified.
	static void SetUpTestSuite() {
		// avoid debug noise from process handling
		mlog::SetLevel(mlog::LogLevel::Warning);

		string dbus_sock_path = "unix:path=" + tmp_dir_.Path() + "/dbus.sock";
		dbus_daemon_proc_.reset(
			new procs::Process {{"dbus-daemon", "--session", "--address", dbus_sock_path}});
		dbus_daemon_proc_->Start();
		// give the DBus daemon time to start and initialize
		std::this_thread::sleep_for(chrono::seconds {1});

		// TIP: Uncomment the code below (and dbus_monitor_proc_
		//      declaration+definition and termination further below) to see
		//      what's going on in the DBus world.
		// dbus_monitor_proc_.reset(
		// 	new procs::Process {{"dbus-monitor", "--address", dbus_sock_path}});
		// dbus_monitor_proc_->Start();
		// // give the DBus monitor time to start and initialize
		// std::this_thread::sleep_for(chrono::seconds {1});

		setenv("DBUS_SYSTEM_BUS_ADDRESS", dbus_sock_path.c_str(), 1);
	};

	static void TearDownTestSuite() {
		dbus_daemon_proc_->EnsureTerminated();
		// dbus_monitor_proc_->EnsureTerminated();
		unsetenv("DBUS_SYSTEM_BUS_ADDRESS");
	};

	void SetUp() override {
#if defined(__has_feature)
#if __has_feature(thread_sanitizer)
		GTEST_SKIP() << "Thread sanitizer doesn't like what libdbus is doing with locks";
#endif
#endif
		// Create the device-identity script
		string script = R"(#!/bin/sh
echo "key1=value1"
echo "key2=value2"
echo "key3=value3"
echo "key1=value11"
exit 0
)";

		{
			ofstream os(test_device_identity_script);
			os << script;
		}

		int ret = chmod(test_device_identity_script.c_str(), S_IRUSR | S_IXUSR);
		ASSERT_EQ(ret, 0);
	}

	string test_device_identity_script {path::Join(tmp_dir_.Path(), "mender-device-identity")};

	static mtesting::TemporaryDirectory tmp_dir_;
	static unique_ptr<procs::Process> dbus_daemon_proc_;
	// static unique_ptr<procs::Process> dbus_monitor_proc_;
};
mtesting::TemporaryDirectory ListenClientTests::tmp_dir_;
unique_ptr<procs::Process> ListenClientTests::dbus_daemon_proc_;
// unique_ptr<procs::Process> ListenClientTests::dbus_monitor_proc_;

TEST_F(ListenClientTests, TestListenGetJWTToken) {
	TestEventLoop loop;

	conf::MenderConfig config {};
	config.server_url = "http://127.0.0.1:" TEST_PORT;
	ipc::Server server {loop, config};
	server.Cache("foobar", "http://127.0.0.1:" TEST_PORT);
	auto err = server.Listen("./private-key.rsa.pem", test_device_identity_script);
	ASSERT_EQ(err, error::NoError);

	// Set up the test client (Emulating mender-update)
	dbus::DBusClient client {loop};
	err = client.CallMethod<dbus::ExpectedStringPair>(
		"io.mender.AuthenticationManager",
		"/io/mender/AuthenticationManager",
		"io.mender.Authentication1",
		"GetJwtToken",
		[&loop](dbus::ExpectedStringPair ex_values) {
			ASSERT_TRUE(ex_values) << ex_values.error().message;
			EXPECT_EQ(ex_values.value().first, "foobar");
			EXPECT_EQ(ex_values.value().second, "http://127.0.0.1:" TEST_PORT);
			loop.Stop();
		});

	loop.Run();
}

TEST_F(ListenClientTests, TestListenFetchJWTToken) {
	TestEventLoop loop;

	string expected_jwt_token {"foobarbazbatz"};
	string expected_server_url {"http://127.0.0.1:" TEST_PORT_2};

	// Set up the test server (Emulating hosted mender)
	http::ServerConfig test_server_config {};
	http::Server http_server(test_server_config, loop);
	auto err = http_server.AsyncServeUrl(
		"http://127.0.0.1:" TEST_PORT_2,
		[](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();
			auto &req = exp_req.value();

			EXPECT_EQ(req->GetPath(), "/api/devices/v1/authentication/auth_requests");
			req->SetBodyWriter(make_shared<io::Discard>());
		},
		[&expected_jwt_token](http::ExpectedIncomingRequestPtr exp_req) {
			ASSERT_TRUE(exp_req) << exp_req.error().String();

			auto result = exp_req.value()->MakeResponse();
			ASSERT_TRUE(result);
			auto resp = result.value();

			resp->SetStatusCodeAndMessage(200, "Success");
			resp->SetBodyReader(make_shared<io::StringReader>(expected_jwt_token));
			resp->SetHeader("Content-Length", to_string(expected_jwt_token.size()));
			resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
		});
	ASSERT_EQ(error::NoError, err);

	conf::MenderConfig config {};
	config.server_url = "http://127.0.0.1:" TEST_PORT_2;
	config.tenant_token = "dummytenanttoken";

	ipc::Server server {loop, config};
	server.Cache("NotYourJWTTokenBitch", "http://127.1.1.1:" TEST_PORT_2);
	err = server.Listen("./private-key.rsa.pem", test_device_identity_script);
	ASSERT_EQ(err, error::NoError);

	dbus::DBusClient client {loop};
	err = client.RegisterSignalHandler<dbus::ExpectedStringPair>(
		"io.mender.Authentication1",
		"JwtTokenStateChange",
		[&loop, expected_jwt_token, expected_server_url](dbus::ExpectedStringPair ex_value) {
			ASSERT_TRUE(ex_value);
			EXPECT_EQ(ex_value.value().first, expected_jwt_token);
			EXPECT_EQ(ex_value.value().second, expected_server_url);
			loop.Stop();
		});
	ASSERT_EQ(err, error::NoError);

	err = client.CallMethod<expected::ExpectedBool>(
		"io.mender.AuthenticationManager",
		"/io/mender/AuthenticationManager",
		"io.mender.Authentication1",
		"FetchJwtToken",
		[](expected::ExpectedBool ex_value) {
			ASSERT_TRUE(ex_value) << ex_value.error().message;
			EXPECT_TRUE(ex_value.value());
		});
	ASSERT_EQ(err, error::NoError);

	loop.Run();

	EXPECT_EQ(expected_jwt_token, server.GetJWTToken());
	EXPECT_EQ(expected_server_url, server.GetServerURL());
}
