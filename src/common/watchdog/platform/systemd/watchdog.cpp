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

#include <common/watchdog.hpp>

#include <systemd/sd-daemon.h>

#include <common/log.hpp>

namespace mender {
namespace common {
namespace watchdog {

using namespace std;

void Kick() {
	log::Trace("Kicking the application watchdog");
	int ret_code {sd_watchdog_enabled(
		0 /* Do not unset NOTIFY_SOCKET env var */,
		nullptr /* We don't use the 'usec' interval in our code */)};
	if (ret_code > 0) {
		int ret {sd_notify(0 /* Do not unset NOTIFY_SOCKET env var */, "WATCHDOG=1")};
		if (ret < 0) {
			log::Error(
				"Failed to kick the systemd service watchdog, received error code: "
				+ to_string(ret) + " " + strerror(-ret));
		}
		// ret == 0 is already handled in the `sd_watchdog_enabled` call
	} else if (ret_code == 0) {
		log::Error(
			"The service manager does not expect watchdog keep-alive messages. Unable to kick the watchdog");
	} else {
		log::Error(
			"The watchdog is not enabled. Not possible to kick: " + string(strerror(-ret_code)));
	}
}

} // namespace watchdog
} // namespace common
} // namespace mender
