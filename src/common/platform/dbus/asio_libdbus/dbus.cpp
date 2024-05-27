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

#include <cassert>
#include <functional>
#include <memory>
#include <string>
#include <utility>

#include <boost/asio.hpp>
#include <dbus/dbus.h>

#include <common/error.hpp>
#include <common/expected.hpp>
#include <common/events.hpp>
#include <common/log.hpp>
#include <common/optional.hpp>

namespace mender {
namespace common {
namespace dbus {

namespace asio = boost::asio;

namespace error = mender::common::error;
namespace expected = mender::common::expected;
namespace events = mender::common::events;
namespace log = mender::common::log;

using namespace std;

// The code below integrates ASIO and libdbus. Or, more precisely, it uses
// asio::io_context as the main/event loop for libdbus.
//
// HandleDispatch() makes sure message dispatch is done. The *Watch() functions
// allow libdbus to set up and cancel watching of its connection's file
// descriptor(s). The *Timeout() functions do the same just for
// timeouts. HandleReply() is a C function we use to extract the DBus reply and
// pass it to a handler given to DBusClient::CallMethod().
// (see the individual functions below for details)

// friends can't be static (see class DBusClient)
void HandleDispatch(DBusConnection *conn, DBusDispatchStatus status, void *data);
dbus_bool_t AddDBusWatch(DBusWatch *w, void *data);
static void RemoveDBusWatch(DBusWatch *w, void *data);
static void ToggleDBusWatch(DBusWatch *w, void *data);
dbus_bool_t AddDBusTimeout(DBusTimeout *t, void *data);
static void RemoveDBusTimeout(DBusTimeout *t, void *data);
static void ToggleDBusTimeout(DBusTimeout *t, void *data);

template <typename ReplyType>
void HandleReply(DBusPendingCall *pending, void *data);

DBusHandlerResult MsgFilter(DBusConnection *connection, DBusMessage *message, void *data);

error::Error DBusPeer::InitializeConnection() {
	DBusError dbus_error;
	dbus_error_init(&dbus_error);
	dbus_conn_.reset(dbus_bus_get_private(DBUS_BUS_SYSTEM, &dbus_error));
	if (!dbus_conn_) {
		auto err = MakeError(
			ConnectionError,
			string("Failed to get connection to system bus: ") + dbus_error.message + "["
				+ dbus_error.name + "]");
		dbus_error_free(&dbus_error);
		return err;
	}

	dbus_connection_set_exit_on_disconnect(dbus_conn_.get(), FALSE);
	if (!dbus_connection_set_watch_functions(
			dbus_conn_.get(), AddDBusWatch, RemoveDBusWatch, ToggleDBusWatch, this, NULL)) {
		dbus_conn_.reset();
		return MakeError(ConnectionError, "Failed to set watch functions");
	}
	if (!dbus_connection_set_timeout_functions(
			dbus_conn_.get(), AddDBusTimeout, RemoveDBusTimeout, ToggleDBusTimeout, this, NULL)) {
		dbus_conn_.reset();
		return MakeError(ConnectionError, "Failed to set timeout functions");
	}

	dbus_connection_set_dispatch_status_function(dbus_conn_.get(), HandleDispatch, this, NULL);

	return error::NoError;
}

error::Error DBusClient::InitializeConnection() {
	auto err = DBusPeer::InitializeConnection();
	if (err != error::NoError) {
		return err;
	}

	if (!dbus_connection_add_filter(dbus_conn_.get(), MsgFilter, this, NULL)) {
		dbus_conn_.reset();
		return MakeError(ConnectionError, "Failed to set message filter");
	}

	return error::NoError;
}

template <typename ReplyType>
void FreeHandlerCopy(void *data) {
	auto *handler = static_cast<DBusCallReplyHandler<ReplyType> *>(data);
	delete handler;
}

template <typename ReplyType>
error::Error DBusClient::CallMethod(
	const string &destination,
	const string &path,
	const string &iface,
	const string &method,
	DBusCallReplyHandler<ReplyType> handler) {
	if (!dbus_conn_) {
		auto err = InitializeConnection();
		if (err != error::NoError) {
			return err;
		}
	}

	unique_ptr<DBusMessage, decltype(&dbus_message_unref)> dbus_msg {
		dbus_message_new_method_call(
			destination.c_str(), path.c_str(), iface.c_str(), method.c_str()),
		dbus_message_unref};
	if (!dbus_msg) {
		return MakeError(MessageError, "Failed to create new message");
	}

	DBusPendingCall *pending;
	if (!dbus_connection_send_with_reply(
			dbus_conn_.get(), dbus_msg.get(), &pending, DBUS_TIMEOUT_USE_DEFAULT)) {
		return MakeError(MessageError, "Unable to add message to the queue");
	}

	// We need to create a copy here because we need to make sure the handler,
	// which might be a lambda, even with captures, will live long enough for
	// the finished pending call to use it.
	unique_ptr<DBusCallReplyHandler<ReplyType>> handler_copy {
		new DBusCallReplyHandler<ReplyType>(handler)};
	if (!dbus_pending_call_set_notify(
			pending, HandleReply<ReplyType>, handler_copy.get(), FreeHandlerCopy<ReplyType>)) {
		return MakeError(MessageError, "Failed to set reply handler");
	}

	// FreeHandlerCopy() takes care of the allocated handler copy
	handler_copy.release();

	return error::NoError;
}

template error::Error DBusClient::CallMethod(
	const string &destination,
	const string &path,
	const string &iface,
	const string &method,
	DBusCallReplyHandler<expected::ExpectedString> handler);

template error::Error DBusClient::CallMethod(
	const string &destination,
	const string &path,
	const string &iface,
	const string &method,
	DBusCallReplyHandler<dbus::ExpectedStringPair> handler);

template error::Error DBusClient::CallMethod(
	const string &destination,
	const string &path,
	const string &iface,
	const string &method,
	DBusCallReplyHandler<expected::ExpectedBool> handler);

template <>
void DBusClient::AddSignalHandler(
	const SignalSpec &spec, DBusSignalHandler<expected::ExpectedString> handler) {
	signal_handlers_string_[spec] = handler;
}

template <>
void DBusClient::AddSignalHandler(
	const SignalSpec &spec, DBusSignalHandler<ExpectedStringPair> handler) {
	signal_handlers_string_pair_[spec] = handler;
}

static inline string GetSignalMatchRule(const string &iface, const string &signal) {
	return string("type='signal'") + ",interface='" + iface + "',member='" + signal + "'";
}

template <typename SignalValueType>
error::Error DBusClient::RegisterSignalHandler(
	const string &iface, const string &signal, DBusSignalHandler<SignalValueType> handler) {
	if (!dbus_conn_) {
		auto err = InitializeConnection();
		if (err != error::NoError) {
			return err;
		}
	}

	// Registering a signal with the low-level DBus API means telling the DBus
	// daemon that we are interested in messages matching a rule. It could be
	// anything, but we are interested in (specific) signals. The MsgFilter()
	// function below takes care of actually invoking the right handler.
	const string match_rule = GetSignalMatchRule(iface, signal);

	DBusError dbus_error;
	dbus_error_init(&dbus_error);
	dbus_bus_add_match(dbus_conn_.get(), match_rule.c_str(), &dbus_error);
	if (dbus_error_is_set(&dbus_error)) {
		auto err = MakeError(
			ConnectionError, string("Failed to register signal reception: ") + dbus_error.message);
		dbus_error_free(&dbus_error);
		return err;
	}
	AddSignalHandler<SignalValueType>(match_rule, handler);
	return error::NoError;
}

template error::Error DBusClient::RegisterSignalHandler(
	const string &iface, const string &signal, DBusSignalHandler<expected::ExpectedString> handler);

template error::Error DBusClient::RegisterSignalHandler(
	const string &iface, const string &signal, DBusSignalHandler<ExpectedStringPair> handler);

void DBusClient::UnregisterSignalHandler(const string &iface, const string &signal) {
	// we use the match rule as a unique string for the given signal
	const string spec = GetSignalMatchRule(iface, signal);

	// should be in at most one set, but erase() is a noop if not found
	signal_handlers_string_.erase(spec);
	signal_handlers_string_pair_.erase(spec);
}

void HandleDispatch(DBusConnection *conn, DBusDispatchStatus status, void *data) {
	DBusClient *client = static_cast<DBusClient *>(data);
	if (status == DBUS_DISPATCH_DATA_REMAINS) {
		// This must give other things in the loop a chance to run because
		// dbus_connection_dispatch() below can cause this to be called again.
		client->loop_.Post([conn]() {
			while (dbus_connection_get_dispatch_status(conn) == DBUS_DISPATCH_DATA_REMAINS) {
				dbus_connection_dispatch(conn);
			}
		});
	}
}

dbus_bool_t AddDBusWatch(DBusWatch *w, void *data) {
	// libdbus adds watches in two steps -- using AddDBusWatch() with a disabled
	// watch which should allocate all the necessary data (and can fail)
	// followed by ToggleDBusWatch() to enable the watch (see below). We
	// simplify things for ourselves by ignoring disabled watches and only
	// actually adding them when ToggleDBusWatch() is called.
	if (!dbus_watch_get_enabled(w)) {
		return TRUE;
	}

	DBusClient *client = static_cast<DBusClient *>(data);
	unique_ptr<asio::posix::stream_descriptor> sd {
		new asio::posix::stream_descriptor(DBusClient::GetAsioIoContext(client->loop_))};
	boost::system::error_code ec;
	sd->assign(dbus_watch_get_unix_fd(w), ec);
	if (ec) {
		log::Error("Failed to assign DBus FD to ASIO stream descriptor");
		return FALSE;
	}

	class RepeatedWaitFunctor {
	public:
		RepeatedWaitFunctor(
			asio::posix::stream_descriptor *sd,
			asio::posix::stream_descriptor::wait_type type,
			DBusWatch *watch,
			DBusClient *client,
			unsigned int flags) :
			sd_ {sd},
			type_ {type},
			watch_ {watch},
			client_ {client},
			flags_ {flags} {
		}

		void operator()(boost::system::error_code ec) {
			if (ec == boost::asio::error::operation_aborted) {
				return;
			}
			if (!dbus_watch_handle(watch_, flags_)) {
				log::Error("Failed to handle watch");
			}
			HandleDispatch(client_->dbus_conn_.get(), DBUS_DISPATCH_DATA_REMAINS, client_);
			sd_->async_wait(type_, *this);
		}

	private:
		asio::posix::stream_descriptor *sd_;
		asio::posix::stream_descriptor::wait_type type_;
		DBusWatch *watch_;
		DBusClient *client_;
		unsigned int flags_;
	};

	unsigned int flags {dbus_watch_get_flags(w)};
	if (flags & DBUS_WATCH_READABLE) {
		RepeatedWaitFunctor read_ftor {
			sd.get(), asio::posix::stream_descriptor::wait_read, w, client, flags};
		sd->async_wait(asio::posix::stream_descriptor::wait_read, read_ftor);
	}
	if (flags & DBUS_WATCH_WRITABLE) {
		RepeatedWaitFunctor write_ftor {
			sd.get(), asio::posix::stream_descriptor::wait_write, w, client, flags};
		sd->async_wait(asio::posix::stream_descriptor::wait_write, write_ftor);
	}
	// Always watch for errors.
	RepeatedWaitFunctor error_ftor {
		sd.get(), asio::posix::stream_descriptor::wait_error, w, client, DBUS_WATCH_ERROR};
	sd->async_wait(asio::posix::stream_descriptor::wait_error, error_ftor);

	// Assign the stream_descriptor so that we have access to it in
	// RemoveDBusWatch() and we can delete it.
	dbus_watch_set_data(w, sd.release(), NULL);
	return TRUE;
}

static void RemoveDBusWatch(DBusWatch *w, void *data) {
	asio::posix::stream_descriptor *sd =
		static_cast<asio::posix::stream_descriptor *>(dbus_watch_get_data(w));
	dbus_watch_set_data(w, NULL, NULL);
	if (sd != nullptr) {
		sd->cancel();
		delete sd;
	}
}

static void ToggleDBusWatch(DBusWatch *w, void *data) {
	if (dbus_watch_get_enabled(w)) {
		AddDBusWatch(w, data);
	} else {
		RemoveDBusWatch(w, data);
	}
}

dbus_bool_t AddDBusTimeout(DBusTimeout *t, void *data) {
	// See AddDBusWatch() for the details about this trick.
	if (!dbus_timeout_get_enabled(t)) {
		return TRUE;
	}

	DBusClient *client = static_cast<DBusClient *>(data);
	asio::steady_timer *timer =
		new asio::steady_timer {DBusClient::GetAsioIoContext(client->loop_)};
	timer->expires_after(chrono::milliseconds {dbus_timeout_get_interval(t)});
	timer->async_wait([t](boost::system::error_code ec) {
		if (ec == boost::asio::error::operation_aborted) {
			return;
		}
		if (!dbus_timeout_handle(t)) {
			log::Error("Failed to handle timeout");
		}
	});

	dbus_timeout_set_data(t, timer, NULL);

	return TRUE;
}

static void RemoveDBusTimeout(DBusTimeout *t, void *data) {
	asio::steady_timer *timer = static_cast<asio::steady_timer *>(dbus_timeout_get_data(t));
	dbus_timeout_set_data(t, NULL, NULL);
	if (timer != nullptr) {
		timer->cancel();
		delete timer;
	}
}

static void ToggleDBusTimeout(DBusTimeout *t, void *data) {
	if (dbus_timeout_get_enabled(t)) {
		AddDBusTimeout(t, data);
	} else {
		RemoveDBusTimeout(t, data);
	}
}

template <typename ReplyType>
bool CheckDBusMessageSignature(const string &signature);

template <>
bool CheckDBusMessageSignature<expected::ExpectedString>(const string &signature) {
	return signature == DBUS_TYPE_STRING_AS_STRING;
}

template <>
bool CheckDBusMessageSignature<ExpectedStringPair>(const string &signature) {
	return signature == (string(DBUS_TYPE_STRING_AS_STRING) + DBUS_TYPE_STRING_AS_STRING);
}

template <>
bool CheckDBusMessageSignature<expected::ExpectedBool>(const string &signature) {
	return signature == DBUS_TYPE_BOOLEAN_AS_STRING;
}

template <typename ReplyType>
ReplyType ExtractValueFromDBusMessage(DBusMessage *message);

template <>
expected::ExpectedString ExtractValueFromDBusMessage(DBusMessage *message) {
	DBusError dbus_error;
	dbus_error_init(&dbus_error);
	const char *result;
	if (!dbus_message_get_args(
			message, &dbus_error, DBUS_TYPE_STRING, &result, DBUS_TYPE_INVALID)) {
		auto err = MakeError(
			ValueError,
			string("Failed to extract reply data from reply message: ") + dbus_error.message + " ["
				+ dbus_error.name + "]");
		dbus_error_free(&dbus_error);
		return expected::unexpected(err);
	}
	return string(result);
}

template <>
ExpectedStringPair ExtractValueFromDBusMessage(DBusMessage *message) {
	DBusError dbus_error;
	dbus_error_init(&dbus_error);
	const char *value1;
	const char *value2;
	if (!dbus_message_get_args(
			message,
			&dbus_error,
			DBUS_TYPE_STRING,
			&value1,
			DBUS_TYPE_STRING,
			&value2,
			DBUS_TYPE_INVALID)) {
		auto err = MakeError(
			ValueError,
			string("Failed to extract reply data from reply message: ") + dbus_error.message + " ["
				+ dbus_error.name + "]");
		dbus_error_free(&dbus_error);
		return expected::unexpected(err);
	}
	return StringPair {string(value1), string(value2)};
}

template <>
expected::ExpectedBool ExtractValueFromDBusMessage(DBusMessage *message) {
	DBusError dbus_error;
	dbus_error_init(&dbus_error);
	bool result;
	if (!dbus_message_get_args(
			message, &dbus_error, DBUS_TYPE_BOOLEAN, &result, DBUS_TYPE_INVALID)) {
		auto err = MakeError(
			ValueError,
			string("Failed to extract reply data from reply message: ") + dbus_error.message + " ["
				+ dbus_error.name + "]");
		dbus_error_free(&dbus_error);
		return expected::unexpected(err);
	}
	return result;
}

template <typename ReplyType>
void HandleReply(DBusPendingCall *pending, void *data) {
	auto *handler = static_cast<DBusCallReplyHandler<ReplyType> *>(data);

	// for easier resource control
	unique_ptr<DBusPendingCall, decltype(&dbus_pending_call_unref)> pending_ptr {
		pending, dbus_pending_call_unref};
	unique_ptr<DBusMessage, decltype(&dbus_message_unref)> reply_ptr {
		dbus_pending_call_steal_reply(pending), dbus_message_unref};

	if (dbus_message_get_type(reply_ptr.get()) == DBUS_MESSAGE_TYPE_ERROR) {
		DBusError dbus_error;
		dbus_error_init(&dbus_error);
		const char *error;
		if (!dbus_message_get_args(
				reply_ptr.get(), &dbus_error, DBUS_TYPE_STRING, &error, DBUS_TYPE_INVALID)) {
			auto err = MakeError(
				ValueError,
				string("Got error reply, but failed to extrac the error from it: ")
					+ dbus_error.message + "[" + dbus_error.name + "]");
			dbus_error_free(&dbus_error);
			(*handler)(expected::unexpected(err));
		} else {
			const string error_str {error};
			auto err = MakeError(ReplyError, "Got error reply: " + error_str);
			(*handler)(expected::unexpected(err));
		}
		return;
	}

	const string signature {dbus_message_get_signature(reply_ptr.get())};
	if (!CheckDBusMessageSignature<ReplyType>(signature)) {
		auto err = MakeError(ValueError, "Unexpected reply signature: " + signature);
		(*handler)(expected::unexpected(err));
		return;
	}

	auto ex_reply = ExtractValueFromDBusMessage<ReplyType>(reply_ptr.get());
	(*handler)(ex_reply);
}

template <>
optional<DBusSignalHandler<expected::ExpectedString>> DBusClient::GetSignalHandler(
	const SignalSpec &spec) {
	if (signal_handlers_string_.find(spec) != signal_handlers_string_.cend()) {
		return signal_handlers_string_[spec];
	} else {
		return nullopt;
	}
}

template <>
optional<DBusSignalHandler<ExpectedStringPair>> DBusClient::GetSignalHandler(
	const SignalSpec &spec) {
	if (signal_handlers_string_pair_.find(spec) != signal_handlers_string_pair_.cend()) {
		return signal_handlers_string_pair_[spec];
	} else {
		return nullopt;
	}
}

DBusHandlerResult MsgFilter(DBusConnection *connection, DBusMessage *message, void *data) {
	if (dbus_message_get_type(message) != DBUS_MESSAGE_TYPE_SIGNAL) {
		return DBUS_HANDLER_RESULT_NOT_YET_HANDLED;
	}

	DBusClient *client = static_cast<DBusClient *>(data);

	// we use the match rule as a unique string for the given signal
	const string spec =
		GetSignalMatchRule(dbus_message_get_interface(message), dbus_message_get_member(message));

	const string signature {dbus_message_get_signature(message)};

	auto opt_string_handler = client->GetSignalHandler<expected::ExpectedString>(spec);
	auto opt_string_pair_handler = client->GetSignalHandler<ExpectedStringPair>(spec);

	// either no match or only one match
	assert(
		!(static_cast<bool>(opt_string_handler) || static_cast<bool>(opt_string_pair_handler))
		|| (static_cast<bool>(opt_string_handler) ^ static_cast<bool>(opt_string_pair_handler)));

	if (opt_string_handler) {
		if (!CheckDBusMessageSignature<expected::ExpectedString>(signature)) {
			auto err = MakeError(ValueError, "Unexpected reply signature: " + signature);
			(*opt_string_handler)(expected::unexpected(err));
			return DBUS_HANDLER_RESULT_NOT_YET_HANDLED;
		}

		auto ex_value = ExtractValueFromDBusMessage<expected::ExpectedString>(message);
		(*opt_string_handler)(ex_value);
		return DBUS_HANDLER_RESULT_HANDLED;
	} else if (opt_string_pair_handler) {
		if (!CheckDBusMessageSignature<ExpectedStringPair>(signature)) {
			auto err = MakeError(ValueError, "Unexpected reply signature: " + signature);
			(*opt_string_pair_handler)(expected::unexpected(err));
			return DBUS_HANDLER_RESULT_NOT_YET_HANDLED;
		}

		auto ex_value = ExtractValueFromDBusMessage<ExpectedStringPair>(message);
		(*opt_string_pair_handler)(ex_value);
		return DBUS_HANDLER_RESULT_HANDLED;
	} else {
		return DBUS_HANDLER_RESULT_NOT_YET_HANDLED;
	}
}

static inline string GetMethodSpec(const string &interface, const string &method) {
	return interface + "." + method;
}

template <>
void DBusObject::AddMethodHandler(
	const string &interface,
	const string &method,
	DBusMethodHandler<expected::ExpectedString> handler) {
	string spec = GetMethodSpec(interface, method);
	method_handlers_string_[spec] = handler;
}

template <>
void DBusObject::AddMethodHandler(
	const string &interface, const string &method, DBusMethodHandler<ExpectedStringPair> handler) {
	string spec = GetMethodSpec(interface, method);
	method_handlers_string_pair_[spec] = handler;
}

template <>
void DBusObject::AddMethodHandler(
	const string &interface,
	const string &method,
	DBusMethodHandler<expected::ExpectedBool> handler) {
	string spec = GetMethodSpec(interface, method);
	method_handlers_bool_[spec] = handler;
}

template <>
optional<DBusMethodHandler<expected::ExpectedString>> DBusObject::GetMethodHandler(
	const MethodSpec &spec) {
	if (method_handlers_string_.find(spec) != method_handlers_string_.cend()) {
		return method_handlers_string_[spec];
	} else {
		return nullopt;
	}
}

template <>
optional<DBusMethodHandler<ExpectedStringPair>> DBusObject::GetMethodHandler(
	const MethodSpec &spec) {
	if (method_handlers_string_pair_.find(spec) != method_handlers_string_pair_.cend()) {
		return method_handlers_string_pair_[spec];
	} else {
		return nullopt;
	}
}

template <>
optional<DBusMethodHandler<expected::ExpectedBool>> DBusObject::GetMethodHandler(
	const MethodSpec &spec) {
	if (method_handlers_bool_.find(spec) != method_handlers_bool_.cend()) {
		return method_handlers_bool_[spec];
	} else {
		return nullopt;
	}
}

error::Error DBusServer::InitializeConnection() {
	auto err = DBusPeer::InitializeConnection();
	if (err != error::NoError) {
		return err;
	}

	DBusError dbus_error;
	dbus_error_init(&dbus_error);

	// We could also do DBUS_NAME_FLAG_ALLOW_REPLACEMENT for cases where two of
	// processes request the same name, but it would require handling of the
	// NameLost signal and triggering termination.
	if (dbus_bus_request_name(
			dbus_conn_.get(), service_name_.c_str(), DBUS_NAME_FLAG_DO_NOT_QUEUE, &dbus_error)
		== -1) {
		dbus_conn_.reset();
		auto err = MakeError(
			ConnectionError,
			(string("Failed to register DBus name: ") + dbus_error.message + " [" + dbus_error.name
			 + "]"));
		dbus_error_free(&dbus_error);
		return err;
	}

	return error::NoError;
}

DBusServer::~DBusServer() {
	if (!dbus_conn_) {
		// nothing to do without a DBus connection
		return;
	}

	for (auto obj : objects_) {
		if (!dbus_connection_unregister_object_path(dbus_conn_.get(), obj->GetPath().c_str())) {
			log::Warning("Failed to unregister DBus object " + obj->GetPath());
		}
	}

	DBusError dbus_error;
	dbus_error_init(&dbus_error);
	if (dbus_bus_release_name(dbus_conn_.get(), service_name_.c_str(), &dbus_error) == -1) {
		log::Warning(
			string("Failed to release DBus name: ") + dbus_error.message + " [" + dbus_error.name
			+ "]");
		dbus_error_free(&dbus_error);
	}
}

template <typename ReturnType>
bool AddReturnDataToDBusMessage(DBusMessage *message, ReturnType data);

template <>
bool AddReturnDataToDBusMessage(DBusMessage *message, string data) {
	const char *data_cstr = data.c_str();
	return static_cast<bool>(
		dbus_message_append_args(message, DBUS_TYPE_STRING, &data_cstr, DBUS_TYPE_INVALID));
}

template <>
bool AddReturnDataToDBusMessage(DBusMessage *message, StringPair data) {
	const char *data_cstr1 = data.first.c_str();
	const char *data_cstr2 = data.second.c_str();
	return static_cast<bool>(dbus_message_append_args(
		message, DBUS_TYPE_STRING, &data_cstr1, DBUS_TYPE_STRING, &data_cstr2, DBUS_TYPE_INVALID));
}

template <>
bool AddReturnDataToDBusMessage(DBusMessage *message, bool data) {
	// (with clang) bool may be neither 0 nor 1 and libdbus has an assertion
	// requiring one of these two integer values.
	dbus_bool_t value = static_cast<dbus_bool_t>(data);
	return static_cast<bool>(
		dbus_message_append_args(message, DBUS_TYPE_BOOLEAN, &value, DBUS_TYPE_INVALID));
}

DBusHandlerResult HandleMethodCall(DBusConnection *connection, DBusMessage *message, void *data) {
	DBusObject *obj = static_cast<DBusObject *>(data);

	string spec =
		GetMethodSpec(dbus_message_get_interface(message), dbus_message_get_member(message));

	auto opt_string_handler = obj->GetMethodHandler<expected::ExpectedString>(spec);
	auto opt_string_pair_handler = obj->GetMethodHandler<ExpectedStringPair>(spec);
	auto opt_bool_handler = obj->GetMethodHandler<expected::ExpectedBool>(spec);

	if (!opt_string_handler && !opt_string_pair_handler && !opt_bool_handler) {
		return DBUS_HANDLER_RESULT_NOT_YET_HANDLED;
	}

	unique_ptr<DBusMessage, decltype(&dbus_message_unref)> reply_msg {nullptr, dbus_message_unref};

	if (opt_string_handler) {
		expected::ExpectedString ex_return_data = (*opt_string_handler)();
		if (!ex_return_data) {
			auto &err = ex_return_data.error();
			reply_msg.reset(
				dbus_message_new_error(message, DBUS_ERROR_FAILED, err.String().c_str()));
			if (!reply_msg) {
				log::Error("Failed to create new DBus message when handling method " + spec);
				return DBUS_HANDLER_RESULT_NOT_YET_HANDLED;
			}
		} else {
			reply_msg.reset(dbus_message_new_method_return(message));
			if (!reply_msg) {
				log::Error("Failed to create new DBus message when handling method " + spec);
				return DBUS_HANDLER_RESULT_NOT_YET_HANDLED;
			}
			if (!AddReturnDataToDBusMessage<string>(reply_msg.get(), ex_return_data.value())) {
				log::Error(
					"Failed to add return value to reply DBus message when handling method "
					+ spec);
				return DBUS_HANDLER_RESULT_NOT_YET_HANDLED;
			}
		}
	} else if (opt_string_pair_handler) {
		ExpectedStringPair ex_return_data = (*opt_string_pair_handler)();
		if (!ex_return_data) {
			auto &err = ex_return_data.error();
			reply_msg.reset(
				dbus_message_new_error(message, DBUS_ERROR_FAILED, err.String().c_str()));
			if (!reply_msg) {
				log::Error("Failed to create new DBus message when handling method " + spec);
				return DBUS_HANDLER_RESULT_NOT_YET_HANDLED;
			}
		} else {
			reply_msg.reset(dbus_message_new_method_return(message));
			if (!reply_msg) {
				log::Error("Failed to create new DBus message when handling method " + spec);
				return DBUS_HANDLER_RESULT_NOT_YET_HANDLED;
			}
			if (!AddReturnDataToDBusMessage<StringPair>(reply_msg.get(), ex_return_data.value())) {
				log::Error(
					"Failed to add return value to reply DBus message when handling method "
					+ spec);
				return DBUS_HANDLER_RESULT_NOT_YET_HANDLED;
			}
		}
	} else if (opt_bool_handler) {
		expected::ExpectedBool ex_return_data = (*opt_bool_handler)();
		if (!ex_return_data) {
			auto &err = ex_return_data.error();
			reply_msg.reset(
				dbus_message_new_error(message, DBUS_ERROR_FAILED, err.String().c_str()));
			if (!reply_msg) {
				log::Error("Failed to create new DBus message when handling method " + spec);
				return DBUS_HANDLER_RESULT_NOT_YET_HANDLED;
			}
		} else {
			reply_msg.reset(dbus_message_new_method_return(message));
			if (!reply_msg) {
				log::Error("Failed to create new DBus message when handling method " + spec);
				return DBUS_HANDLER_RESULT_NOT_YET_HANDLED;
			}
			if (!AddReturnDataToDBusMessage<bool>(reply_msg.get(), ex_return_data.value())) {
				log::Error(
					"Failed to add return value to reply DBus message when handling method "
					+ spec);
				return DBUS_HANDLER_RESULT_NOT_YET_HANDLED;
			}
		}
	}

	if (!dbus_connection_send(connection, reply_msg.get(), NULL)) {
		// can only happen in case of no memory
		log::Error("Failed to send reply DBus message when handling method " + spec);
		return DBUS_HANDLER_RESULT_NOT_YET_HANDLED;
	}

	return DBUS_HANDLER_RESULT_HANDLED;
}

static DBusObjectPathVTable dbus_vtable = {.message_function = HandleMethodCall};

error::Error DBusServer::AdvertiseObject(DBusObjectPtr obj) {
	if (!dbus_conn_) {
		auto err = InitializeConnection();
		if (err != error::NoError) {
			return err;
		}
	}

	const string &obj_path {obj->GetPath()};
	DBusError dbus_error;
	dbus_error_init(&dbus_error);

	if (!dbus_connection_try_register_object_path(
			dbus_conn_.get(), obj_path.c_str(), &dbus_vtable, obj.get(), &dbus_error)) {
		auto err = MakeError(
			ConnectionError,
			(string("Failed to register object ") + obj_path + ": " + dbus_error.message + " ["
			 + dbus_error.name + "]"));
		dbus_error_free(&dbus_error);
		return err;
	}

	objects_.push_back(obj);
	return error::NoError;
}

template <typename SignalValueType>
error::Error DBusServer::EmitSignal(
	const string &path, const string &iface, const string &signal, SignalValueType value) {
	if (!dbus_conn_) {
		auto err = InitializeConnection();
		if (err != error::NoError) {
			return err;
		}
	}

	unique_ptr<DBusMessage, decltype(&dbus_message_unref)> signal_msg {
		dbus_message_new_signal(path.c_str(), iface.c_str(), signal.c_str()), dbus_message_unref};
	if (!signal_msg) {
		return MakeError(MessageError, "Failed to create signal message");
	}

	if (!AddReturnDataToDBusMessage<SignalValueType>(signal_msg.get(), value)) {
		return MakeError(MessageError, "Failed to add data to the signal message");
	}

	if (!dbus_connection_send(dbus_conn_.get(), signal_msg.get(), NULL)) {
		// can only happen in case of no memory
		return MakeError(ConnectionError, "Failed to send signal message");
	}

	return error::NoError;
}

template error::Error DBusServer::EmitSignal(
	const string &path, const string &iface, const string &signal, string value);

template error::Error DBusServer::EmitSignal(
	const string &path, const string &iface, const string &signal, StringPair value);

} // namespace dbus
} // namespace common
} // namespace mender
