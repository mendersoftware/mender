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

#include <common/platform/dbus.hpp>

#include <string>

namespace mender {
namespace common {
namespace dbus {

using namespace std;

const DBusErrorCategoryClass DBusErrorCategory;

const char *DBusErrorCategoryClass::name() const noexcept {
	return "DBusErrorCategory";
}

string DBusErrorCategoryClass::message(int code) const {
	switch (code) {
	case NoError:
		return "Success";
	case ConnectionError:
		return "DBus connection error";
	case MessageError:
		return "DBus message error";
	case ReplyError:
		return "DBus reply error";
	case ValueError:
		return "DBus value error";
	default:
		return "Unknown DBus error";
	}
}

error::Error MakeError(DBusErrorCode code, const string &msg) {
	return error::Error(error_condition(code, DBusErrorCategory), msg);
}

} // namespace dbus
} // namespace common
} // namespace mender
