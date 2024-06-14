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

#include <common/config.h>

#ifndef MENDER_USE_DBUS
#error Cannot include dbus.hpp when MENDER_USE_DBUS is disabled.
#endif

#include <functional>
#include <memory>
#include <string>
#include <unordered_map>
#include <utility>
#include <vector>

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
//   struct {string iface; string signal;}
// in the future.
using SignalSpec = string;

using StringPair = std::pair<string, string>;
using ExpectedStringPair = expected::expected<StringPair, error::Error>;

class DBusPeer : public events::EventLoopObject {
public:
	explicit DBusPeer(events::EventLoop &loop) :
		loop_ {loop} {};

	virtual ~DBusPeer() {};

#ifdef MENDER_USE_ASIO_LIBDBUS
	// These take an instance of this class as the *data argument and need to
	// access its private members. But they cannot be private member functions,
	// we need them to be plain C functions.
	friend void HandleDispatch(DBusConnection *conn, DBusDispatchStatus status, void *data);
	friend dbus_bool_t AddDBusWatch(DBusWatch *w, void *data);
	friend dbus_bool_t AddDBusTimeout(DBusTimeout *t, void *data);
#endif // MENDER_USE_ASIO_LIBDBUS

protected:
	events::EventLoop &loop_;

#ifdef MENDER_USE_ASIO_LIBDBUS
	// Cannot initialize this in the constructor to a real connection because
	// the connecting can fail.
	unique_ptr<DBusConnection, decltype(&dbus_connection_unref)> dbus_conn_ {
		nullptr, [](DBusConnection *conn) {
			if (dbus_connection_get_is_connected(conn)) {
				dbus_connection_close(conn);
			}
			dbus_connection_unref(conn);
		}};
#endif // MENDER_USE_ASIO_LIBDBUS

	virtual error::Error InitializeConnection();
};

// Note: Not a thread-safe class, create multiple instances if needed. However,
// the implementation based on libdbus is likely to suffer from potential race
// conditions in the library itself.
class DBusClient : public DBusPeer {
public:
	explicit DBusClient(events::EventLoop &loop) :
		DBusPeer(loop) {};

	template <typename ReplyType>
	error::Error CallMethod(
		const string &destination,
		const string &path,
		const string &iface,
		const string &method,
		DBusCallReplyHandler<ReplyType> handler);

	template <typename SignalValueType>
	error::Error RegisterSignalHandler(
		const string &iface, const string &signal, DBusSignalHandler<SignalValueType> handler);
	void UnregisterSignalHandler(const string &iface, const string &signal);

#ifdef MENDER_USE_ASIO_LIBDBUS
	// see DBusPeer's friends for some details
	friend DBusHandlerResult MsgFilter(
		DBusConnection *connection, DBusMessage *message, void *data);
#endif // MENDER_USE_ASIO_LIBDBUS

private:
	unordered_map<SignalSpec, DBusSignalHandler<expected::ExpectedString>> signal_handlers_string_;
	unordered_map<SignalSpec, DBusSignalHandler<ExpectedStringPair>> signal_handlers_string_pair_;

	error::Error InitializeConnection() override;

	template <typename SignalValueType>
	void AddSignalHandler(const string &spec, DBusSignalHandler<SignalValueType> handler);

	template <typename SignalValueType>
	optional<DBusSignalHandler<SignalValueType>> GetSignalHandler(const SignalSpec &spec);
};

// Might need something like
//   struct {string service; string iface; string method;}
// in the future.
using MethodSpec = string;

template <typename ReturnType>
using DBusMethodHandler = function<ReturnType(void)>;

class DBusObject {
public:
	explicit DBusObject(const string &path) :
		path_ {path} {};

	const string &GetPath() {
		return path_;
	}

	template <typename ReturnType>
	void AddMethodHandler(
		const string &interface, const string &method, DBusMethodHandler<ReturnType> handler);

	friend DBusHandlerResult HandleMethodCall(
		DBusConnection *connection, DBusMessage *message, void *data);

private:
	const string path_;

	unordered_map<MethodSpec, DBusMethodHandler<expected::ExpectedString>> method_handlers_string_;
	unordered_map<MethodSpec, DBusMethodHandler<ExpectedStringPair>> method_handlers_string_pair_;
	unordered_map<MethodSpec, DBusMethodHandler<expected::ExpectedBool>> method_handlers_bool_;

	template <typename ReturnType>
	optional<DBusMethodHandler<ReturnType>> GetMethodHandler(const MethodSpec &spec);
};

using DBusObjectPtr = shared_ptr<DBusObject>;

class DBusServer : public DBusPeer {
public:
	explicit DBusServer(events::EventLoop &loop, const string &service_name) :
		DBusPeer(loop),
		service_name_ {service_name} {};

	~DBusServer() override;

	error::Error AdvertiseObject(DBusObjectPtr obj);

	// Only a convenience version for tests. Double-check that obj outlives the
	// DBusServer!
	error::Error AdvertiseObject(DBusObject &obj) {
		return AdvertiseObject(shared_ptr<DBusObject> {&obj, [](DBusObject *obj) {}});
	}

	template <typename SignalValueType>
	error::Error EmitSignal(
		const string &path, const string &iface, const string &signal, SignalValueType value);

	friend DBusHandlerResult HandleMethodCall(
		DBusConnection *connection, DBusMessage *message, void *data);

private:
	string service_name_;
	vector<DBusObjectPtr> objects_;

	error::Error InitializeConnection() override;
};

} // namespace dbus
} // namespace common
} // namespace mender

#endif // MENDER_COMMON_DBUS_HPP
