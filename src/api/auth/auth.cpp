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

#include <api/auth.hpp>

#include <mutex>
#include <string>
#include <vector>

#include <common/common.hpp>
#include <common/crypto.hpp>
#include <common/json.hpp>
#include <common/error.hpp>
#include <common/log.hpp>
#include <common/path.hpp>
#include <common/expected.hpp>
#include <common/identity_parser.hpp>
#include <common/optional.hpp>

namespace mender {
namespace api {
namespace auth {


using namespace std;
namespace error = mender::common::error;
namespace common = mender::common;
namespace conf = mender::common::conf;


namespace identity_parser = mender::common::identity_parser;
namespace key_value_parser = mender::common::key_value_parser;
namespace path = mender::common::path;
namespace mlog = mender::common::log;
namespace expected = mender::common::expected;
namespace io = mender::common::io;
namespace json = mender::common::json;
namespace crypto = mender::common::crypto;


const string request_uri = "/api/devices/v1/authentication/auth_requests";

const AuthClientErrorCategoryClass AuthClientErrorCategory;

const char *AuthClientErrorCategoryClass::name() const noexcept {
	return "AuthClientErrorCategory";
}

string AuthClientErrorCategoryClass::message(int code) const {
	switch (code) {
	case NoError:
		return "Success";
	case SetupError:
		return "Error during setup";
	case RequestError:
		return "HTTP client request error";
	case ResponseError:
		return "HTTP client response error";
	case APIError:
		return "API error";
	case UnauthorizedError:
		return "Unauthorized error";
	case AuthenticationError:
		return "Authentication error";
	default:
		return "Unknown";
	}
}

error::Error MakeError(AuthClientErrorCode code, const string &msg) {
	return error::Error(error_condition(code, AuthClientErrorCategory), msg);
}

namespace http {

error::Error MakeHTTPResponseError(
	const AuthClientErrorCode code,
	const mender::http::ResponsePtr resp,
	const string &response_body,
	const string &msg) {
	return error::Error(
		error_condition(code, AuthClientErrorCategory),
		"Authentication error(" + resp->GetStatusMessage() + "): " + msg + "(" + response_body
			+ ")");
}

static void TryAuthenticate(
	vector<string>::const_iterator server_it,
	vector<string>::const_iterator end,
	mender::http::Client &client,
	const string request_body,
	const string signature,
	APIResponseHandler api_handler);

error::Error FetchJWTToken(
	mender::http::Client &client,
	const vector<string> &servers,
	const crypto::Args &crypto_args,
	const string &device_identity_script_path,
	APIResponseHandler api_handler,
	const string &tenant_token) {
	key_value_parser::ExpectedKeyValuesMap expected_identity_data =
		identity_parser::GetIdentityData(device_identity_script_path);
	if (!expected_identity_data) {
		return expected_identity_data.error();
	}

	auto identity_data_json = identity_parser::DumpIdentityData(expected_identity_data.value());
	mlog::Debug("Got identity data: " + identity_data_json);

	// Create the request body
	unordered_map<string, string> request_body_map {
		{"id_data", identity_data_json},
	};

	if (tenant_token.size() > 0) {
		request_body_map.insert({"tenant_token", tenant_token});
	}

	auto expected_public_key = crypto::ExtractPublicKey(crypto_args);
	if (!expected_public_key) {
		return expected_public_key.error();
	}
	request_body_map.insert({"pubkey", expected_public_key.value()});

	auto expected_request_body = json::Dump(request_body_map);
	if (!expected_request_body) {
		return expected_request_body.error();
	}
	auto request_body = expected_request_body.value();

	// Sign the body
	auto expected_signature =
		crypto::SignRawData(crypto_args, common::ByteVectorFromString(request_body));
	if (!expected_signature) {
		return expected_signature.error();
	}
	auto signature = expected_signature.value();

	// TryAuthenticate() calls the handler on any potential further errors, we
	// are done here with no errors.
	TryAuthenticate(servers.cbegin(), servers.cend(), client, request_body, signature, api_handler);
	return error::NoError;
}

static void TryAuthenticate(
	vector<string>::const_iterator server_it,
	vector<string>::const_iterator end,
	mender::http::Client &client,
	const string request_body,
	const string signature,
	APIResponseHandler api_handler) {
	if (server_it == end) {
		auto err = MakeError(AuthenticationError, "No more servers to try for authentication");
		api_handler(expected::unexpected(err));
		return;
	}

	auto whole_url = mender::http::JoinUrl(*server_it, request_uri);
	auto req = make_shared<mender::http::OutgoingRequest>();
	req->SetMethod(mender::http::Method::POST);
	req->SetAddress(whole_url);
	req->SetHeader("Content-Type", "application/json");
	req->SetHeader("Content-Length", to_string(request_body.size()));
	req->SetHeader("Accept", "application/json");
	req->SetHeader("X-MEN-Signature", signature);
	req->SetHeader("Authorization", "API_KEY");

	req->SetBodyGenerator([request_body]() -> io::ExpectedReaderPtr {
		return make_shared<io::StringReader>(request_body);
	});

	auto received_body = make_shared<vector<uint8_t>>();

	auto err = client.AsyncCall(
		req,
		[received_body, server_it, end, &client, request_body, signature, api_handler](
			mender::http::ExpectedIncomingResponsePtr exp_resp) {
			if (!exp_resp) {
				mlog::Info(
					"Authentication error trying server '" + *server_it
					+ "': " + exp_resp.error().String());
				TryAuthenticate(
					std::next(server_it), end, client, request_body, signature, api_handler);
				return;
			}
			auto resp = exp_resp.value();

			auto body_writer = make_shared<io::ByteWriter>(received_body);
			body_writer->SetUnlimited(true);
			resp->SetBodyWriter(body_writer);

			mlog::Debug("Received response header value:");
			mlog::Debug("Status code:" + to_string(resp->GetStatusCode()));
			mlog::Debug("Status message: " + resp->GetStatusMessage());
		},
		[received_body, server_it, end, &client, request_body, signature, api_handler](
			mender::http::ExpectedIncomingResponsePtr exp_resp) {
			if (!exp_resp) {
				mlog::Info(
					"Authentication error trying server '" + *server_it
					+ "': " + exp_resp.error().String());
				TryAuthenticate(
					std::next(server_it), end, client, request_body, signature, api_handler);
				return;
			}
			auto resp = exp_resp.value();

			string response_body = common::StringFromByteVector(*received_body);

			error::Error err;
			switch (resp->GetStatusCode()) {
			case mender::http::StatusOK:
				api_handler(AuthData {*server_it, response_body});
				return;
			case mender::http::StatusUnauthorized:
				err = MakeHTTPResponseError(
					UnauthorizedError, resp, response_body, "Failed to authorize with the server.");
				mlog::Info(
					"Authentication error trying server '" + *server_it + "': " + err.String());
				TryAuthenticate(
					std::next(server_it), end, client, request_body, signature, api_handler);
				return;
			case mender::http::StatusBadRequest:
			case mender::http::StatusInternalServerError:
				err = MakeHTTPResponseError(
					APIError, resp, response_body, "Failed to authorize with the server.");
				mlog::Info(
					"Authentication error trying server '" + *server_it + "': " + err.String());
				TryAuthenticate(
					std::next(server_it), end, client, request_body, signature, api_handler);
				return;
			default:
				err =
					MakeError(ResponseError, "Unexpected error code: " + resp->GetStatusMessage());
				mlog::Info(
					"Authentication error trying server '" + *server_it + "': " + err.String());
				TryAuthenticate(
					std::next(server_it), end, client, request_body, signature, api_handler);
				return;
			}
		});
	if (err != error::NoError) {
		api_handler(expected::unexpected(err));
	}
}
} // namespace http

error::Error FetchJWTToken(
	mender::http::Client &client,
	const vector<string> &servers,
	const crypto::Args &crypto_args,
	const string &device_identity_script_path,
	APIResponseHandler api_handler,
	const string &tenant_token) {
	return http::FetchJWTToken(
		client, servers, crypto_args, device_identity_script_path, api_handler, tenant_token);
}

void Authenticator::ExpireToken() {
	if (!token_fetch_in_progress_) {
		RequestNewToken(nullopt);
	}
}

error::Error Authenticator::StartWatchingTokenSignal() {
	auto err = dbus_client_.RegisterSignalHandler<dbus::ExpectedStringPair>(
		"io.mender.Authentication1",
		"JwtTokenStateChange",
		[this](dbus::ExpectedStringPair ex_auth_dbus_data) {
			auth_timeout_timer_.Cancel();
			token_fetch_in_progress_ = false;
			ExpectedAuthData ex_auth_data;
			if (!ex_auth_dbus_data) {
				mlog::Error(
					"Error from the JwtTokenStateChange DBus signal: "
					+ ex_auth_dbus_data.error().String());
				ex_auth_data = ExpectedAuthData(expected::unexpected(ex_auth_dbus_data.error()));
			} else {
				auto &token = ex_auth_dbus_data.value().first;
				auto &server_url = ex_auth_dbus_data.value().second;
				AuthData auth_data {server_url, token};
				ex_auth_data = ExpectedAuthData(std::move(auth_data));
			}
			PostPendingActions(ex_auth_data);
		});

	watching_token_signal_ = (err == error::NoError);
	return err;
}

void Authenticator::PostPendingActions(ExpectedAuthData &ex_auth_data) {
	for (auto action : pending_actions_) {
		loop_.Post([action, ex_auth_data]() { action(ex_auth_data); });
	}
	pending_actions_.clear();
}

error::Error Authenticator::RequestNewToken(optional<AuthenticatedAction> opt_action) {
	if (token_fetch_in_progress_) {
		// Just make sure the action (if any) is called once the token is
		// obtained.
		if (opt_action) {
			pending_actions_.push_back(*opt_action);
		}
		return error::NoError;
	}

	auto err = dbus_client_.CallMethod<expected::ExpectedBool>(
		"io.mender.AuthenticationManager",
		"/io/mender/AuthenticationManager",
		"io.mender.Authentication1",
		"FetchJwtToken",
		[this](expected::ExpectedBool ex_value) {
			if (!ex_value) {
				token_fetch_in_progress_ = false;
				mlog::Error("Failed to request new token fetching: " + ex_value.error().String());
				ExpectedAuthData ex_auth_data = expected::unexpected(ex_value.error());
				PostPendingActions(ex_auth_data);
			} else if (!ex_value.value()) {
				// mender-auth encountered an error not sent over DBus (should never happen)
				token_fetch_in_progress_ = false;
				mlog::Error(
					"Failed to request new token fetching (see mender-auth logs for details)");
				ExpectedAuthData ex_auth_data = expected::unexpected(MakeError(
					AuthenticationError, "Failed to request new token fetching from mender-auth"));
				PostPendingActions(ex_auth_data);
			}
		});
	if (err != error::NoError) {
		// A sync DBus error.
		mlog::Error("Failed to request new token fetching: " + err.String());
		token_fetch_in_progress_ = false;
		ExpectedAuthData ex_auth_data = expected::unexpected(err);
		PostPendingActions(ex_auth_data);
		if (opt_action) {
			(*opt_action)(ex_auth_data);
		}
		return err;
	}
	// else everything went OK

	token_fetch_in_progress_ = true;

	// Make sure the action (if any) is called once the token is
	// obtained.
	if (opt_action) {
		pending_actions_.push_back(*opt_action);
	}

	// Make sure we don't wait for the token forever.
	auth_timeout_timer_.AsyncWait(auth_timeout_, [this](error::Error err) {
		if (err.code == make_error_condition(errc::operation_canceled)) {
			return;
		} else if (err == error::NoError) {
			mlog::Warning("Timed-out waiting for a new token");
			token_fetch_in_progress_ = false;
			ExpectedAuthData ex_auth_data = expected::unexpected(
				MakeError(AuthenticationError, "Timed-out waiting for a new token"));
			PostPendingActions(ex_auth_data);
		} else {
			// should never happen
			assert(false);
			mlog::Error("Authentication timer error: " + err.String());

			// In case it did happen, run the stacked up actions and unset the
			// in_progress_ flag or things may got stuck.
			token_fetch_in_progress_ = false;
			ExpectedAuthData ex_auth_data = expected::unexpected(err);
			PostPendingActions(ex_auth_data);
		}
	});
	return error::NoError;
}

error::Error Authenticator::WithToken(AuthenticatedAction action) {
	if (!watching_token_signal_) {
		auto err = StartWatchingTokenSignal();
		if (err != error::NoError) {
			// Should never fail. We rely on the signal heavily so let's fail
			// hard if it does.
			action(expected::unexpected(err));
			return err;
		}
	}

	if (token_fetch_in_progress_) {
		// Already waiting for a new token, just make sure the action is called
		// once it arrives (or once the wait times out).
		pending_actions_.push_back(action);
		return error::NoError;
	}
	// else => should fetch the token, cache it and call all pending actions
	// Try to get token from mender-auth
	auto err = dbus_client_.CallMethod<dbus::ExpectedStringPair>(
		"io.mender.AuthenticationManager",
		"/io/mender/AuthenticationManager",
		"io.mender.Authentication1",
		"GetJwtToken",
		[this, action](dbus::ExpectedStringPair ex_auth_dbus_data) {
			token_fetch_in_progress_ = false;
			auth_timeout_timer_.Cancel();
			if (ex_auth_dbus_data && (ex_auth_dbus_data.value().first != "")
				&& (ex_auth_dbus_data.value().second != "")) {
				// Got a valid token, let's save it and then call action and any
				// previously-pending actions (if any) with it.
				auto &token = ex_auth_dbus_data.value().first;
				auto &server_url = ex_auth_dbus_data.value().second;
				AuthData auth_data {server_url, token};

				// Post/schedule pending actions before running the given action
				// because the action can actually add more actions or even
				// expire the token, etc. So post actions stacked up before we
				// got here with the current token and only then give action a
				// chance to mess with things.
				ExpectedAuthData ex_auth_data {std::move(auth_data)};
				PostPendingActions(ex_auth_data);
				action(ex_auth_data);
			} else {
				// No valid token, let's request fetching of a new one
				RequestNewToken(action);
			}
		});
	if (err != error::NoError) {
		// No token and failed to try to get one (should never happen).
		ExpectedAuthData ex_auth_data = expected::unexpected(err);
		PostPendingActions(ex_auth_data);
		action(ex_auth_data);
		return err;
	}
	// else record that token is already being fetched (by GetJwtToken()).
	token_fetch_in_progress_ = true;

	return error::NoError;
}

} // namespace auth
} // namespace api
} // namespace mender
