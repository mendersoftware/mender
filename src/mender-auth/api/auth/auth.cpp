// Copyright 2024 Northern.tech AS
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

#include <mender-auth/api/auth.hpp>

#include <common/expected.hpp>
#include <common/io.hpp>
#include <common/json.hpp>
#include <common/key_value_parser.hpp>
#include <common/log.hpp>

#include <client_shared/identity_parser.hpp>

namespace mender {
namespace auth {
namespace api {
namespace auth {

namespace expected = mender::common::expected;
namespace io = mender::common::io;
namespace json = mender::common::json;
namespace key_value_parser = mender::common::key_value_parser;
namespace mlog = mender::common::log;

namespace identity_parser = mender::client_shared::identity_parser;

using AuthData = mender::api::auth::AuthData;

const string request_uri = "/api/devices/v1/authentication/auth_requests";

const AuthClientErrorCategoryClass AuthClientErrorCategory;

const char *AuthClientErrorCategoryClass::name() const noexcept {
	return "AuthClientErrorCategory";
}

string AuthClientErrorCategoryClass::message(int code) const {
	switch (code) {
	case NoError:
		return "Success";
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

error::Error MakeHTTPResponseError(
	const AuthClientErrorCode code,
	const mender::common::http::ResponsePtr resp,
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
	mender::common::http::Client &client,
	const string request_body,
	const string signature,
	APIResponseHandler api_handler);

error::Error FetchJWTToken(
	mender::common::http::Client &client,
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
	auto expected_signature = crypto::Sign(crypto_args, common::ByteVectorFromString(request_body));
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
	mender::common::http::Client &client,
	const string request_body,
	const string signature,
	APIResponseHandler api_handler) {
	if (server_it == end) {
		auto err = MakeError(AuthenticationError, "No more servers to try for authentication");
		api_handler(expected::unexpected(err));
		return;
	}

	auto whole_url = mender::common::http::JoinUrl(*server_it, request_uri);
	auto req = make_shared<mender::common::http::OutgoingRequest>();
	req->SetMethod(mender::common::http::Method::POST);
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
			mender::common::http::ExpectedIncomingResponsePtr exp_resp) {
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
			mender::common::http::ExpectedIncomingResponsePtr exp_resp) {
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
			case mender::common::http::StatusOK:
				api_handler(AuthData {*server_it, response_body});
				return;
			case mender::common::http::StatusUnauthorized:
				err = MakeHTTPResponseError(
					UnauthorizedError, resp, response_body, "Failed to authorize with the server.");
				mlog::Info(
					"Authentication error trying server '" + *server_it + "': " + err.String());
				TryAuthenticate(
					std::next(server_it), end, client, request_body, signature, api_handler);
				return;
			case mender::common::http::StatusBadRequest:
			case mender::common::http::StatusInternalServerError:
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

} // namespace auth
} // namespace api
} // namespace auth
} // namespace mender
