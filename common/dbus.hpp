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

#ifndef MENDER_COMMON_DBUS_HPP
#define MENDER_COMMON_DBUS_HPP

#include <config.h>

#include <functional>
#include <memory>
#include <string>
#include <unordered_map>
#include <utility>

#ifdef MENDER_USE_ASIO_LIBDBUS
#include <dbus/dbus.h>
#endif // MENDER_USE_ASIO_LIBDBUS

#include <common/error.hpp>
#include <common/expected.hpp>
#include <common/events.hpp>
#include <common/optional.hpp>

namespace mender {
namespace common {
namespace dbus {

namespace error = mender::common::error;
namespace expected = mender::common::expected;
namespace events = mender::common::events;
namespace optional = mender::common::optional;

using namespace std;

enum DBusErrorCode {
	NoError = 0,
	ConnectionError,
	MessageError,
	ReplyError,
	ValueError,
};

class DBusErrorCategoryClass : public std::error_category {
public:
	const char *name() const noexcept override;
	string message(int code) const override;
};
extern const DBusErrorCategoryClass DBusErrorCategory;

error::Error MakeError(DBusErrorCode code, const string &msg);

template <typename ReplyType>
using DBusCallReplyHandler = function<void(ReplyType)>;

template <typename SignalValueType>
using DBusSignalHandler = function<void(SignalValueType)>;

// Might need something like
//   struct {string sender; string iface; string signal;}
// in the future.
using SignalSpec = string;

using ExpectedStringPair = expected::expected<std::pair<string, string>, error::Error>;

// Note: Not a thread-safe class, create multiple instances if needed. However,
// the implementation based on libdbus is likely to suffer from potential race
// conditions in the library itself.
class DBusClient : public events::EventLoopObject {
public:
	explicit DBusClient(events::EventLoop &loop) :
		loop_ {loop} {};

	~DBusClient();

	template <typename ReplyType>
	error::Error CallMethod(
		const string &destination,
		const string &path,
		const string &iface,
		const string &method,
		DBusCallReplyHandler<ReplyType> handler);

	template <typename SignalValueType>
	error::Error RegisterSignalHandler(
		const string &sender,
		const string &iface,
		const string &signal,
		DBusSignalHandler<SignalValueType> handler);
	void UnregisterSignalHandler(const string &sender, const string &iface, const string &signal);

#ifdef MENDER_USE_ASIO_LIBDBUS
	// These take an instance of this class as the *data argument and need to
	// access its private members. But they cannot be private member functions,
	// we need them to be plain C functions.
	friend void HandleDispatch(DBusConnection *conn, DBusDispatchStatus status, void *data);
	friend dbus_bool_t AddDBusWatch(DBusWatch *w, void *data);
	friend dbus_bool_t AddDBusTimeout(DBusTimeout *t, void *data);
	friend DBusHandlerResult MsgFilter(
		DBusConnection *connection, DBusMessage *message, void *data);
#endif // MENDER_USE_ASIO_LIBDBUS

private:
	events::EventLoop &loop_;

#ifdef MENDER_USE_ASIO_LIBDBUS
	// Cannot initialize this in the constructor to a real connection because
	// the connecting can fail.
	unique_ptr<DBusConnection, decltype(&dbus_connection_unref)> dbus_conn_ {
		nullptr, dbus_connection_unref};
#endif // MENDER_USE_ASIO_LIBDBUS

	unordered_map<SignalSpec, DBusSignalHandler<expected::ExpectedString>> signal_handlers_string_;
	unordered_map<SignalSpec, DBusSignalHandler<ExpectedStringPair>> signal_handlers_string_pair_;

	error::Error InitializeConnection();

	template <typename SignalValueType>
	void AddSignalHandler(const string &spec, DBusSignalHandler<SignalValueType> handler);

	template <typename SignalValueType>
	optional::optional<DBusSignalHandler<SignalValueType>> GetSignalHandler(const SignalSpec &spec);
};

} // namespace dbus
} // namespace common
} // namespace mender

#endif // MENDER_COMMON_DBUS_HPP
