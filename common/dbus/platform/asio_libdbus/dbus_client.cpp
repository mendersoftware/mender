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

#include <common/dbus.hpp>

#include <functional>
#include <memory>
#include <string>

#include <boost/asio.hpp>
#include <dbus/dbus.h>

#include <common/error.hpp>
#include <common/expected.hpp>
#include <common/events.hpp>
#include <common/log.hpp>

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
static void HandleReply(DBusPendingCall *pending, void *data);

error::Error DBusClient::InitializeConnection() {
	DBusError dbus_error;
	dbus_error_init(&dbus_error);
	dbus_conn_.reset(dbus_bus_get(DBUS_BUS_SYSTEM, &dbus_error));
	if (!dbus_conn_) {
		return MakeError(
			ConnectionError,
			string("Failed to get connection to system bus: ") + dbus_error.message + "["
				+ dbus_error.name + "]");
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

void FreeHandlerCopy(void *data) {
	DBusCallReplyHandler *handler = static_cast<DBusCallReplyHandler *>(data);
	delete handler;
}

error::Error DBusClient::CallMethod(
	const string &destination,
	const string &path,
	const string &iface,
	const string &method,
	DBusCallReplyHandler handler) {
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
	DBusCallReplyHandler *handler_copy = new DBusCallReplyHandler(handler);
	if (!dbus_pending_call_set_notify(pending, HandleReply, handler_copy, FreeHandlerCopy)) {
		return MakeError(MessageError, "Failed to set reply handler");
	}

	return error::NoError;
}

void HandleDispatch(DBusConnection *conn, DBusDispatchStatus status, void *data) {
	DBusClient *client = static_cast<DBusClient *>(data);
	if (status == DBUS_DISPATCH_DATA_REMAINS) {
		// This must give other things in the loop a chance to run because
		// dbus_connection_dispatch() below can cause this to be called again.
		client->loop_.Post([conn, status]() {
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

	unsigned int flags {dbus_watch_get_flags(w)};
	if (flags & DBUS_WATCH_READABLE) {
		sd->async_wait(
			asio::posix::stream_descriptor::wait_read,
			[w, client, flags](boost::system::error_code ec) {
				if (ec == boost::asio::error::operation_aborted) {
					return;
				}
				if (!dbus_watch_handle(w, flags)) {
					log::Error("Failed to handle readable watch");
				}
				HandleDispatch(client->dbus_conn_.get(), DBUS_DISPATCH_DATA_REMAINS, client);
			});
	}
	if (flags & DBUS_WATCH_WRITABLE) {
		sd->async_wait(
			asio::posix::stream_descriptor::wait_write,
			[w, client, flags](boost::system::error_code ec) {
				if (ec == boost::asio::error::operation_aborted) {
					return;
				}
				if (!dbus_watch_handle(w, flags)) {
					log::Error("Failed to handle writable watch");
				}
				HandleDispatch(client->dbus_conn_.get(), DBUS_DISPATCH_DATA_REMAINS, client);
			});
	}
	// Always watch for errors.
	sd->async_wait(asio::posix::stream_descriptor::wait_error, [w](boost::system::error_code ec) {
		if (ec == boost::asio::error::operation_aborted) {
			return;
		}
		if (!dbus_watch_handle(w, DBUS_WATCH_ERROR)) {
			log::Error("Failed to handle error watch");
		}
	});

	// Assign the stream_descriptor so that we have access to it in
	// RemoveDBusWatch() and we can delete it.
	dbus_watch_set_data(w, sd.release(), NULL);
	return TRUE;
}

static void RemoveDBusWatch(DBusWatch *w, void *data) {
	asio::posix::stream_descriptor *sd =
		static_cast<asio::posix::stream_descriptor *>(dbus_watch_get_data(w));
	dbus_watch_set_data(w, NULL, NULL);
	sd->cancel();
	delete sd;
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

static void HandleReply(DBusPendingCall *pending, void *data) {
	DBusCallReplyHandler *handler = static_cast<DBusCallReplyHandler *>(data);

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
			(*handler)(expected::unexpected(err));
		} else {
			const string error_str {error};
			auto err = MakeError(ReplyError, "Got error reply: " + error_str);
			(*handler)(expected::unexpected(err));
		}
		return;
	}

	const string signature {dbus_message_get_signature(reply_ptr.get())};
	if (signature != DBUS_TYPE_STRING_AS_STRING) {
		auto err = MakeError(ValueError, "Unexpected reply signature: " + signature);
		(*handler)(expected::unexpected(err));
		return;
	}

	DBusError dbus_error;
	dbus_error_init(&dbus_error);
	const char *result;
	if (!dbus_message_get_args(
			reply_ptr.get(), &dbus_error, DBUS_TYPE_STRING, &result, DBUS_TYPE_INVALID)) {
		auto err = MakeError(
			ValueError,
			string("Failed to extract reply data from reply message: ") + dbus_error.message + "["
				+ dbus_error.name + "]");
		(*handler)(expected::unexpected(err));
	} else {
		string result_str {result};
		(*handler)(result_str);
	}
}

} // namespace dbus
} // namespace common
} // namespace mender
