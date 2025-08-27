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

#include <cerrno>
#include <cmath>
#include <cstdlib>
#include <functional>
#include <sstream>
#include <string>

#include <api/api.hpp>
#include <api/client.hpp>
#include <common/common.hpp>
#include <client_shared/conf.hpp>
#include <common/error.hpp>
#include <common/events.hpp>
#include <common/http.hpp>
#include <client_shared/inventory_parser.hpp>
#include <common/io.hpp>
#include <common/json.hpp>
#include <common/log.hpp>

namespace mender {
namespace update {
namespace inventory {

using namespace std;

namespace api = mender::api;
namespace common = mender::common;
namespace conf = mender::client_shared::conf;
namespace error = mender::common::error;
namespace events = mender::common::events;
namespace expected = mender::common::expected;
namespace http = mender::common::http;
namespace inv_parser = mender::client_shared::inventory_parser;
namespace io = mender::common::io;
namespace json = mender::common::json;
namespace log = mender::common::log;

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

const string uri = "/api/devices/v1/inventory/device/attributes";

enum class ValueType {
	Integer,
	Float,
	String
};

ValueType GetValueType(const string& value) {
	if (value.empty()) {
		return ValueType::String;
	}

	// First check if it's an integer
	auto int_result = common::StringToLongLong(value);
	if (int_result) {
		return ValueType::Integer;
	}

	// Then check if it's a floating point number
	char* endptr;
	errno = 0;
	double parsed_value = strtod(value.c_str(), &endptr);
	
	// Validate that the entire string was consumed and no conversion errors occurred
	if (errno == 0 && endptr == value.c_str() + value.length() && endptr != value.c_str()) {
		// Additional validation: reject NaN and infinite values as they don't serialize well in JSON
		if (std::isfinite(parsed_value)) {
			return ValueType::Float;
		}
	}

	return ValueType::String;
}

error::Error PushInventoryData(
	const string &inventory_generators_dir,
	events::EventLoop &loop,
	api::Client &client,
	size_t &last_data_hash,
	APIResponseHandler api_handler) {
	auto ex_inv_data = inv_parser::GetInventoryData(inventory_generators_dir);
	if (!ex_inv_data) {
		return ex_inv_data.error();
	}
	auto &inv_data = ex_inv_data.value();

	if (inv_data.count("mender_client_version") != 0) {
		inv_data["mender_client_version"].push_back(conf::kMenderVersion);
	} else {
		inv_data["mender_client_version"] = {conf::kMenderVersion};
	}

	stringstream top_ss;
	top_ss << "[";
	auto key_vector = common::GetMapKeyVector(inv_data);
	std::sort(key_vector.begin(), key_vector.end());
	for (const auto &key : key_vector) {
		top_ss << R"({"name":")";
		top_ss << json::EscapeString(key);
		top_ss << R"(","value":)";
		if (inv_data[key].size() == 1) {
			const string& value = inv_data[key][0];
			ValueType value_type = GetValueType(value);
			
			if (value_type == ValueType::Integer || value_type == ValueType::Float) {
				// Serialize numeric values without quotes
				top_ss << value;
			} else {
				// Serialize string values with quotes and escaping
				top_ss << "\"" << json::EscapeString(value) << "\"";
			}
		} else {
			stringstream items_ss;
			items_ss << "[";
			for (const auto &str : inv_data[key]) {
				ValueType value_type = GetValueType(str);
				
				if (value_type == ValueType::Integer || value_type == ValueType::Float) {
					// Serialize numeric values without quotes
					items_ss << str << ",";
				} else {
					// Serialize string values with quotes and escaping
					items_ss << "\"" << json::EscapeString(str) << "\",";
				}
			}
			auto items_str = items_ss.str();
			// replace the trailing comma with the closing square bracket
			items_str[items_str.size() - 1] = ']';
			top_ss << items_str;
		}
		top_ss << R"(},)";
	}
	auto payload = top_ss.str();
	if (payload[payload.size() - 1] == ',') {
		// replace the trailing comma with the closing square bracket
		payload.pop_back();
	}
	payload.push_back(']');

	size_t payload_hash = std::hash<string> {}(payload);
	if (payload_hash == last_data_hash) {
		log::Info("Inventory data unchanged, not submitting");
		loop.Post([api_handler]() { api_handler(error::NoError); });
		return error::NoError;
	}

	http::BodyGenerator payload_gen = [payload]() {
		return make_shared<io::StringReader>(payload);
	};

	auto req = make_shared<api::APIRequest>();
	req->SetPath(uri);
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
				return;
			}

			auto body_writer = make_shared<io::ByteWriter>(received_body);
			auto resp = exp_resp.value();
			auto content_length = resp->GetHeader("Content-Length");
			auto ex_len = common::StringTo<size_t>(content_length.value());
			if (!ex_len) {
				log::Error("Failed to get content length from the inventory API response headers");
				body_writer->SetUnlimited(true);
			} else {
				received_body->resize(ex_len.value());
			}
			resp->SetBodyWriter(body_writer);
		},
		[received_body, api_handler, payload_hash, &last_data_hash](
			http::ExpectedIncomingResponsePtr exp_resp) {
			if (!exp_resp) {
				log::Error("Request to push inventory data failed: " + exp_resp.error().message);
				api_handler(exp_resp.error());
				return;
			}

			auto resp = exp_resp.value();
			auto status = resp->GetStatusCode();
			if (status == http::StatusOK) {
				log::Info("Inventory data submitted successfully");
				last_data_hash = payload_hash;
				api_handler(error::NoError);
			} else {
				auto ex_err_msg = api::ErrorMsgFromErrorResponse(*received_body);
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
