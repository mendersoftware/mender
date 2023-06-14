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

#include <mender-update/inventory.hpp>

#include <sstream>
#include <string>

#include <api/api.hpp>
#include <common/common.hpp>
#include <common/error.hpp>
#include <common/events.hpp>
#include <common/http.hpp>
#include <common/inventory_parser.hpp>
#include <common/io.hpp>
#include <common/json.hpp>
#include <common/log.hpp>
#include <common/path.hpp>

namespace mender {
namespace update {
namespace inventory {

using namespace std;

namespace api = mender::api;
namespace common = mender::common;
namespace error = mender::common::error;
namespace events = mender::common::events;
namespace expected = mender::common::expected;
namespace http = mender::http;
namespace inv_parser = mender::common::inventory_parser;
namespace io = mender::common::io;
namespace json = mender::common::json;
namespace log = mender::common::log;
namespace path = mender::common::path;

const InventoryErrorCategoryClass InventoryErrorCategory;

const char *InventoryErrorCategoryClass::name() const noexcept {
	return "InventoryErrorCategory";
}

string InventoryErrorCategoryClass::message(int code) const {
	switch (code) {
	case NoError:
		return "Success";
	case BadResponseError:
		return "Bad response error";
	}
	assert(false);
	return "Unknown";
}

error::Error MakeError(InventoryErrorCode code, const string &msg) {
	return error::Error(error_condition(code, InventoryErrorCategory), msg);
}

const string uri = "api/devices/v1/inventory/device/attributes";

error::Error PushInventoryData(
	const string &inventory_generators_dir,
	const string &server_url,
	http::Client &client,
	events::EventLoop &loop,
	APIResponseHandler api_handler) {
	auto ex_inv_data = inv_parser::GetInventoryData(inventory_generators_dir);
	if (!ex_inv_data) {
		return ex_inv_data.error();
	}

	stringstream top_ss;
	top_ss << "[";
	auto inv_data = ex_inv_data.value();
	for (const auto &kv : inv_data) {
		top_ss << R"({"name":")";
		top_ss << json::EscapeString(kv.first);
		top_ss << R"(","value":)";
		if (kv.second.size() == 1) {
			top_ss << "\"" + json::EscapeString(kv.second[0]) + "\"";
		} else {
			stringstream items_ss;
			items_ss << "[";
			for (const auto &str : kv.second) {
				items_ss << "\"" + json::EscapeString(str) + "\",";
			}
			auto items_str = items_ss.str();
			// replace the trailing comma with the closing square bracket
			items_str[items_str.size() - 1] = ']';
			top_ss << items_str;
		}
		top_ss << R"(},)";
	}
	auto payload = top_ss.str();
	// replace the trailing comma with the closing square bracket
	payload[payload.size() - 1] = ']';

	http::BodyGenerator payload_gen = [payload]() {
		return make_shared<io::StringReader>(payload);
	};

	// TODO: APIRequest
	auto req = make_shared<http::OutgoingRequest>();
	req->SetAddress(path::Join(server_url, uri));
	req->SetMethod(http::Method::PUT);
	req->SetHeader("Content-Type", "application/json");
	req->SetHeader("Content-Length", to_string(payload.size()));
	req->SetHeader("Accept", "application/json");
	req->SetBodyGenerator(payload_gen);

	auto received_body = make_shared<vector<uint8_t>>();
	return client.AsyncCall(
		req,
		[received_body, api_handler](http::ExpectedIncomingResponsePtr exp_resp) {
			if (!exp_resp) {
				log::Error("Request to push inventory data failed: " + exp_resp.error().message);
				api_handler(exp_resp.error());
			}

			auto body_writer = make_shared<io::ByteWriter>(received_body);
			auto resp = exp_resp.value();
			auto content_length = resp->GetHeader("Content-Length");
			auto ex_len = common::StringToLongLong(content_length.value());
			if (!ex_len) {
				log::Error("Failed to get content length from the inventory API response headers");
				body_writer->SetUnlimited(true);
			} else {
				received_body->resize(ex_len.value());
			}
			resp->SetBodyWriter(body_writer);
		},
		[received_body, api_handler](http::ExpectedIncomingResponsePtr exp_resp) {
			if (!exp_resp) {
				log::Error("Request to push inventory data failed: " + exp_resp.error().message);
				api_handler(exp_resp.error());
			}

			auto resp = exp_resp.value();
			auto status = resp->GetStatusCode();
			if (status == http::StatusOK) {
				api_handler(error::NoError);
			} else {
				auto ex_err_msg = api::ErrorMsgFromErrorResponse(*(received_body.get()));
				string err_str;
				if (ex_err_msg) {
					err_str = ex_err_msg.value();
				} else {
					err_str = resp->GetStatusMessage();
				}
				api_handler(MakeError(
					BadResponseError,
					"Got unexpected response " + to_string(status)
						+ " from inventory API: " + err_str));
			}
		});
}

} // namespace inventory
} // namespace update
} // namespace mender
