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

#include <api/client.hpp>

#include <common/error.hpp>
#include <common/expected.hpp>
#include <common/http.hpp>
#include <common/log.hpp>

namespace mender {
namespace api {

namespace error = mender::common::error;
namespace expected = mender::common::expected;
namespace http = mender::http;
namespace log = mender::common::log;

error::Error Client::AsyncCall(
	http::OutgoingRequestPtr req,
	http::ResponseHandler header_handler,
	http::ResponseHandler body_handler) {
	// If the first request fails with 401, we need to get a new token and then
	// try again with the new token. We should avoid using the same
	// OutgoingRequest object for the two different requests, hence a copy and a
	// different handler using the copy instead of the original OutgoingRequest
	// given.
	auto reauth_req = make_shared<http::OutgoingRequest>(*req);
	auto reauthenticated_handler =
		[this, reauth_req, header_handler, body_handler](auth::ExpectedToken ex_tok) {
			if (!ex_tok) {
				log::Error("Failed to obtain authentication credentials");
				event_loop_.Post([header_handler, ex_tok]() {
					error::Error err = ex_tok.error();
					header_handler(expected::unexpected(err));
				});
				return;
			}
			reauth_req->SetHeader("Authorization", "Bearer " + ex_tok.value());
			auto err = http::Client::AsyncCall(reauth_req, header_handler, body_handler);
			if (err != error::NoError) {
				log::Error("Failed to schedule an HTTP request with the new token");
				event_loop_.Post([header_handler, err]() {
					error::Error err_copy {err};
					header_handler(expected::unexpected(err_copy));
				});
				return;
			}
		};

	return authenticator_.WithToken(
		[this, req, header_handler, body_handler, reauthenticated_handler](
			auth::ExpectedToken ex_tok) {
			if (!ex_tok) {
				log::Error("Failed to obtain authentication credentials");
				event_loop_.Post([header_handler, ex_tok]() {
					error::Error err = ex_tok.error();
					header_handler(expected::unexpected(err));
				});
				return;
			}
			req->SetHeader("Authorization", "Bearer " + ex_tok.value());
			auto err = http::Client::AsyncCall(
				req,
				[this, header_handler, reauthenticated_handler](
					http::ExpectedIncomingResponsePtr ex_resp) {
					if (!ex_resp) {
						header_handler(ex_resp);
						return;
					}
					auto resp = ex_resp.value();
					auto status = resp->GetStatusCode();
					if (status != http::StatusUnauthorized) {
						header_handler(ex_resp);
						return;
					}
					logger_.Debug(
						"Got " + to_string(http::StatusUnauthorized)
						+ " from the server, expiring token");
					authenticator_.ExpireToken();
					authenticator_.WithToken(reauthenticated_handler);
				},
				[body_handler](http::ExpectedIncomingResponsePtr ex_resp) {
					if (!ex_resp) {
						body_handler(ex_resp);
						return;
					}
					auto resp = ex_resp.value();
					auto status = resp->GetStatusCode();
					if (status != http::StatusUnauthorized) {
						body_handler(ex_resp);
					}
					// 401 handled by the header handler
				});
			if (err != error::NoError) {
				log::Error("Failed to schedule an HTTP request with an existing new token");
				event_loop_.Post([header_handler, err]() {
					error::Error err_copy {err};
					header_handler(expected::unexpected(err_copy));
				});
				return;
			}
		});
}

} // namespace api
} // namespace mender
