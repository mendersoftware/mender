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

#include <mender-auth/ipc/server.hpp>

#include <functional>
#include <iostream>
#include <string>

#include <common/common.hpp>
#include <common/error.hpp>
#include <common/events_io.hpp>
#include <common/http.hpp>
#include <common/log.hpp>

namespace mender {
namespace auth {
namespace ipc {

using namespace std;

namespace type {
namespace webserver {

auto NoOpHeaderHandler = [](http::ExpectedIncomingRequestPtr exp_req) {};

// Run a webserver and handle the events as requests
error::Error Caching::Listen(
	const string &server_url, const string &private_key_path, const string &identity_script_path) {
	return this->server_.AsyncServeUrl(
		server_url,
		NoOpHeaderHandler,
		[this, private_key_path, identity_script_path](http::ExpectedIncomingRequestPtr exp_req) {
			if (!exp_req) {
				log::Error("Expected request failure: " + exp_req.error().message);
				return;
			}
			auto req = exp_req.value();

			auto exp_resp = req->MakeResponse();
			if (!exp_resp) {
				log::Error("Failed to create the response: " + exp_resp.error().message);
				return;
			}
			auto resp = exp_resp.value();

			log::Debug("Got path: " + req->GetPath());

			if (req->GetPath() == "/getjwttoken") {
				resp->SetHeader("X-MEN-JWTTOKEN", this->GetJWTToken());
				resp->SetHeader("X-MEN-SERVERURL", this->GetServerURL());
				resp->SetStatusCodeAndMessage(200, "Success");
				resp->AsyncReply([](error::Error err) {
					if (err != error::NoError) {
						log::Error("Failed to reply: " + err.message);
					};
				});
			} else if (req->GetPath() == "/fetchjwttoken") {
				auto err = auth_client::FetchJWTToken(
					this->client_,
					this->server_url_,
					private_key_path,
					identity_script_path == "" ? default_identity_script_path_
											   : identity_script_path,
					[this](auth_client::APIResponse resp) {
						this->CacheAPIResponse(this->server_url_, resp);
					},
					this->tenant_token_);
				if (err != error::NoError) {
					log::Error("Failed to set up the fetch request: " + err.message);
					resp->SetStatusCodeAndMessage(500, "Internal Server Error");
					resp->AsyncReply([](error::Error err) {
						if (err != error::NoError) {
							log::Error("Failed to reply: " + err.message);
						};
					});
					return;
				}
				resp->SetStatusCodeAndMessage(200, "Success");
				resp->AsyncReply([](error::Error err) {
					if (err != error::NoError) {
						log::Error("Failed to reply: " + err.message);
					};
				});
			} else {
				resp->SetStatusCodeAndMessage(404, "Not Found");
				resp->AsyncReply([](error::Error err) {
					if (err != error::NoError) {
						log::Error("Failed to reply: " + err.message);
					};
				});
			}
		});
}
} // namespace webserver
} // namespace type

} // namespace ipc
} // namespace auth
} // namespace mender
