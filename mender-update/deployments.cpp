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

#include <mender-update/deployments.hpp>

#include <sstream>
#include <string>

#include <common/common.hpp>
#include <common/error.hpp>
#include <common/events.hpp>
#include <common/expected.hpp>
#include <common/http.hpp>
#include <common/io.hpp>
#include <common/json.hpp>
#include <common/log.hpp>
#include <common/optional.hpp>
#include <common/path.hpp>
#include <mender-update/context.hpp>

namespace mender {
namespace update {
namespace deployments {

using namespace std;

namespace common = mender::common;
namespace context = mender::update::context;
namespace error = mender::common::error;
namespace events = mender::common::events;
namespace expected = mender::common::expected;
namespace io = mender::common::io;
namespace json = mender::common::json;
namespace log = mender::common::log;
namespace optional = mender::common::optional;
namespace path = mender::common::path;

const DeploymentsErrorCategoryClass DeploymentsErrorCategory;

const char *DeploymentsErrorCategoryClass::name() const noexcept {
	return "DeploymentsErrorCategory";
}

string DeploymentsErrorCategoryClass::message(int code) const {
	switch (code) {
	case NoError:
		return "Success";
	case InvalidDataError:
		return "Invalid data error";
	case BadResponseError:
		return "Bad response error";
	}
	assert(false);
	return "Unknown";
}

error::Error MakeError(DeploymentsErrorCode code, const string &msg) {
	return error::Error(error_condition(code, DeploymentsErrorCategory), msg);
}

const string v1_uri = "api/devices/v1/deployments/device/deployments/next";
const string v2_uri = "api/devices/v2/deployments/device/deployments/next";

error::Error CheckNewDeployments(
	context::MenderContext &ctx,
	const string &server_url,
	http::Client &client,
	events::EventLoop &loop,
	APIResponseHandler api_handler) {
	auto ex_dev_type = ctx.GetDeviceType();
	if (!ex_dev_type) {
		return ex_dev_type.error();
	}
	string device_type = ex_dev_type.value();

	auto ex_provides = ctx.LoadProvides();
	if (!ex_provides) {
		return ex_provides.error();
	}
	auto provides = ex_provides.value();
	if (provides.find("artifact_name") == provides.end()) {
		return MakeError(InvalidDataError, "Missing artifact name data");
	}

	stringstream ss;
	ss << R"({"update_control_map":false,"device_provides":{)";
	ss << R"("device_type":")";
	ss << json::EscapeString(device_type);

	for (const auto &kv : provides) {
		ss << "\",\"" + json::EscapeString(kv.first) + "\":\"";
		ss << json::EscapeString(kv.second);
	}

	ss << R"("}})";

	string v2_payload = ss.str();
	http::BodyGenerator payload_gen = [v2_payload]() {
		return make_shared<io::StringReader>(v2_payload);
	};

	// TODO: APIRequest
	auto v2_req = make_shared<http::OutgoingRequest>();
	v2_req->SetAddress(path::Join(server_url, v2_uri));
	v2_req->SetMethod(http::Method::POST);
	v2_req->SetHeader("Content-Type", "application/json");
	v2_req->SetHeader("Content-Length", to_string(v2_payload.size()));
	v2_req->SetHeader("Accept", "application/json");
	v2_req->SetBodyGenerator(payload_gen);

	string v1_args = "artifact_name=" + http::URLEncode(provides["artifact_name"])
					 + "&device_type=" + http::URLEncode(device_type);
	auto v1_req = make_shared<http::OutgoingRequest>();
	v1_req->SetAddress(path::Join(server_url, v1_uri) + "?" + v1_args);
	v1_req->SetMethod(http::Method::GET);
	v1_req->SetHeader("Accept", "application/json");

	auto received_body = make_shared<vector<uint8_t>>();
	auto handle_data = [received_body, api_handler](unsigned status) {
		if (status == http::StatusOK) {
			auto ex_j = json::Load(common::StringFromByteVector(*(received_body.get())));
			if (ex_j) {
				APIResponse response {optional::optional<json::Json> {ex_j.value()}};
				api_handler(response);
			} else {
				api_handler(expected::unexpected(ex_j.error()));
			}
		} else if (status == http::StatusNoContent) {
			api_handler(APIResponse {optional::nullopt});
		}
	};

	http::ResponseHandler header_handler =
		[received_body, api_handler](http::ExpectedIncomingResponsePtr exp_resp) {
			if (!exp_resp) {
				log::Error("Request to check new deployments failed: " + exp_resp.error().message);
				APIResponse response = expected::unexpected(exp_resp.error());
				api_handler(response);
			}

			auto resp = exp_resp.value();
			received_body->clear();
			auto body_writer = make_shared<io::ByteWriter>(received_body);
			body_writer->SetUnlimited(true);
			resp->SetBodyWriter(body_writer);
		};

	http::ResponseHandler v1_body_handler = [received_body, api_handler, handle_data](
												http::ExpectedIncomingResponsePtr exp_resp) {
		if (!exp_resp) {
			log::Error("Request to check new deployments failed: " + exp_resp.error().message);
			APIResponse response = expected::unexpected(exp_resp.error());
			api_handler(response);
		}
		auto resp = exp_resp.value();
		auto status = resp->GetStatusCode();
		if ((status == http::StatusOK) || (status == http::StatusNoContent)) {
			handle_data(status);
		} else {
			api_handler(expected::unexpected(MakeError(
				BadResponseError,
				"Got unexpected response [" + to_string(status) + "]: " + resp->GetStatusMessage()
					+ "\n" + common::StringFromByteVector(*(received_body.get())))));
		}
	};

	auto run_v1_fallback = [v1_req, header_handler, v1_body_handler, &client]() {
		client.AsyncCall(v1_req, header_handler, v1_body_handler);
	};

	http::ResponseHandler v2_body_handler = [received_body,
											 run_v1_fallback,
											 api_handler,
											 handle_data,
											 &loop](http::ExpectedIncomingResponsePtr exp_resp) {
		if (!exp_resp) {
			log::Error("Request to check new deployments failed: " + exp_resp.error().message);
			APIResponse response = expected::unexpected(exp_resp.error());
			api_handler(response);
		}
		auto resp = exp_resp.value();
		auto status = resp->GetStatusCode();
		if ((status == http::StatusOK) || (status == http::StatusNoContent)) {
			handle_data(status);
		} else if (status == http::StatusNotFound) {
			log::Info(
				"POST request to v2 version of the deployments API failed, falling back to v1 version and GET");
			loop.Post(run_v1_fallback);
		} else {
			api_handler(expected::unexpected(MakeError(
				BadResponseError,
				"Got unexpected response [" + to_string(status) + "]: " + resp->GetStatusMessage()
					+ "\n" + common::StringFromByteVector(*(received_body.get())))));
		}
	};

	return client.AsyncCall(v2_req, header_handler, v2_body_handler);
}

} // namespace deployments
} // namespace update
} // namespace mender
