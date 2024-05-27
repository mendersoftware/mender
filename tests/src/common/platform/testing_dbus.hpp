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

#ifndef MENDER_COMMON_TESTING_DBUS
#define MENDER_COMMON_TESTING_DBUS

#include <gtest/gtest.h>

#include <common/platform/dbus.hpp>
#include <common/log.hpp>
#include <common/processes.hpp>
#include <common/testing.hpp>

namespace mender {
namespace common {
namespace testing {
namespace dbus {

using namespace std;

namespace dbus = mender::common::dbus;
namespace procs = mender::common::processes;
namespace mlog = mender::common::log;
namespace mtesting = mender::common::testing;

class DBusTests : public ::testing::Test {
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
	}
	static mtesting::TemporaryDirectory tmp_dir_;
	static unique_ptr<procs::Process> dbus_daemon_proc_;
	// static unique_ptr<procs::Process> dbus_monitor_proc_;
};
mtesting::TemporaryDirectory DBusTests::tmp_dir_;
unique_ptr<procs::Process> DBusTests::dbus_daemon_proc_;
// unique_ptr<procs::Process> DBusTests::dbus_monitor_proc_;

} // namespace dbus
} // namespace testing
} // namespace common
} // namespace mender

#endif // MENDER_COMMON_TESTING_DBUS
