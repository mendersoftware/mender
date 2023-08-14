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
namespace optional = mender::common::optional;


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

error::Error FetchJWTToken(
	mender::http::Client &client,
	const string &server_url,
	const string &private_key_path,
	const string &device_identity_script_path,
	APIResponseHandler api_handler,
	const string &tenant_token) {
	key_value_parser::ExpectedKeyValuesMap expected_identity_data =
		identity_parser::GetIdentityData(device_identity_script_path);
	if (!expected_identity_data) {
		return expected_identity_data.error();
	}
	expected::ExpectedString expected_identity_data_json =
		json::Dump(expected_identity_data.value());
	if (!expected_identity_data_json) {
		mlog::Error("Failed to dump the identity data to JSON");
		return expected_identity_data_json.error();
	}
	auto identity_data_json = expected_identity_data_json.value();
	mlog::Debug("Got identity data: " + identity_data_json);

	// Create the request body
	unordered_map<string, string> request_body_map {
		{"id_data", identity_data_json},
	};

	if (tenant_token.size() > 0) {
		request_body_map.insert({"tenant_token", tenant_token});
	}

	auto expected_public_key = crypto::ExtractPublicKey(private_key_path);
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
		crypto::SignRawData(private_key_path, common::ByteVectorFromString(request_body));
	if (!expected_signature) {
		return expected_signature.error();
	}
	auto signature = expected_signature.value();

	auto whole_url = mender::http::JoinUrl(server_url, request_uri);

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

	return client.AsyncCall(
		req,
		[received_body, api_handler](mender::http::ExpectedIncomingResponsePtr exp_resp) {
			if (!exp_resp) {
				mlog::Error("Request failed: " + exp_resp.error().message);
				api_handler(expected::unexpected(exp_resp.error()));
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
		[received_body, api_handler](mender::http::ExpectedIncomingResponsePtr exp_resp) {
			if (!exp_resp) {
				mlog::Error("Request failed: " + exp_resp.error().message);
				api_handler(expected::unexpected(exp_resp.error()));
				return;
			}
			auto resp = exp_resp.value();

			string response_body = common::StringFromByteVector(*received_body);

			switch (resp->GetStatusCode()) {
			case mender::http::StatusOK:
				api_handler(response_body);
				return;
			case mender::http::StatusUnauthorized:
				api_handler(expected::unexpected(MakeHTTPResponseError(
					UnauthorizedError,
					resp,
					response_body,
					"Failed to authorize with the server.")));
				return;
			case mender::http::StatusBadRequest:
			case mender::http::StatusInternalServerError:
				api_handler(expected::unexpected(MakeHTTPResponseError(
					APIError, resp, response_body, "Failed to authorize with the server.")));
				return;
			default:
				mlog::Error("Unexpected error code " + resp->GetStatusMessage());
				api_handler(expected::unexpected(MakeError(
					ResponseError, "Unexpected error code: " + resp->GetStatusMessage())));
				return;
			}
		});
}
} // namespace http

error::Error FetchJWTToken(
	mender::http::Client &client,
	const string &server_url,
	const string &private_key_path,
	const string &device_identity_script_path,
	APIResponseHandler api_handler,
	const string &tenant_token) {
	return http::FetchJWTToken(
		client,
		server_url,
		private_key_path,
		device_identity_script_path,
		api_handler,
		tenant_token);
}

void Authenticator::ExpireToken() {
	unique_lock<mutex> lock {auth_lock_};
	token_ = optional::nullopt;
}

void Authenticator::RunPendingActions(ExpectedToken ex_token) {
	unique_lock<mutex> lock {auth_lock_};
	for (auto action : pending_actions_) {
		loop_.Post([action, ex_token]() { action(ex_token); });
	}
	pending_actions_.clear();
}

error::Error Authenticator::WithToken(AuthenticatedAction action) {
	unique_lock<mutex> lock {auth_lock_};
	if (token_) {
		string token = *token_;
		lock.unlock();
		action(ExpectedToken(token));
		return error::NoError;
	}
	// else => no token
	if (auth_in_progress_) {
		pending_actions_.push_back(action);
		lock.unlock();
		return error::NoError;
	}
	// else => should fetch the token, cache it and call all pending actions
	auth_in_progress_ = true;
	lock.unlock();

	return FetchJWTToken(
		client_,
		server_url_,
		private_key_path_,
		device_identity_script_path_,
		[this, action](APIResponse resp) {
			if (resp) {
				unique_lock<mutex> lock {auth_lock_};
				token_ = resp.value();
				auth_in_progress_ = false;
			}
			action(resp);
			RunPendingActions(resp);
		},
		tenant_token_);
}

} // namespace auth
} // namespace api
} // namespace mender
