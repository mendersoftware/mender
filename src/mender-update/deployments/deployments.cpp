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

#include <algorithm>
#include <sstream>
#include <string>

#include <api/api.hpp>
#include <api/client.hpp>
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

namespace api = mender::api;
namespace common = mender::common;
namespace context = mender::update::context;
namespace error = mender::common::error;
namespace events = mender::common::events;
namespace expected = mender::common::expected;
namespace http = mender::common::http;
namespace io = mender::common::io;
namespace json = mender::common::json;
namespace log = mender::common::log;
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
	case DeploymentAbortedError:
		return "Deployment was aborted on the server";
	}
	assert(false);
	return "Unknown";
}

error::Error MakeError(DeploymentsErrorCode code, const string &msg) {
	return error::Error(error_condition(code, DeploymentsErrorCategory), msg);
}

static const string check_updates_v1_uri = "/api/devices/v1/deployments/device/deployments/next";
static const string check_updates_v2_uri = "/api/devices/v2/deployments/device/deployments/next";

error::Error DeploymentClient::CheckNewDeployments(
	context::MenderContext &ctx, api::Client &client, CheckUpdatesAPIResponseHandler api_handler) {
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
	ss << R"({"device_provides":{)";
	ss << R"("device_type":")";
	ss << json::EscapeString(device_type);

	for (const auto &kv : provides) {
		ss << "\",\"" + json::EscapeString(kv.first) + "\":\"";
		ss << json::EscapeString(kv.second);
	}

	ss << R"("}})";

	string v2_payload = ss.str();
	log::Debug("deployments/next v2 payload " + v2_payload);
	http::BodyGenerator payload_gen = [v2_payload]() {
		return make_shared<io::StringReader>(v2_payload);
	};

	auto v2_req = make_shared<api::APIRequest>();
	v2_req->SetPath(check_updates_v2_uri);
	v2_req->SetMethod(http::Method::POST);
	v2_req->SetHeader("Content-Type", "application/json");
	v2_req->SetHeader("Content-Length", to_string(v2_payload.size()));
	v2_req->SetHeader("Accept", "application/json");
	v2_req->SetBodyGenerator(payload_gen);

	string v1_args = "artifact_name=" + http::URLEncode(provides["artifact_name"])
					 + "&device_type=" + http::URLEncode(device_type);
	auto v1_req = make_shared<api::APIRequest>();
	v1_req->SetPath(check_updates_v1_uri + "?" + v1_args);
	v1_req->SetMethod(http::Method::GET);
	v1_req->SetHeader("Accept", "application/json");

	auto received_body = make_shared<vector<uint8_t>>();
	auto handle_data = [received_body, api_handler](unsigned status) {
		if (status == http::StatusOK) {
			auto ex_j = json::Load(common::StringFromByteVector(*received_body));
			if (ex_j) {
				CheckUpdatesAPIResponse response {optional<json::Json> {ex_j.value()}};
				api_handler(response);
			} else {
				api_handler(expected::unexpected(ex_j.error()));
			}
		} else if (status == http::StatusNoContent) {
			api_handler(CheckUpdatesAPIResponse {nullopt});
		} else {
			log::Warning(
				"DeploymentClient::CheckNewDeployments - received unhandled http response: "
				+ to_string(status));
			api_handler(expected::unexpected(MakeError(
				DeploymentAbortedError, "received unhandled HTTP response: " + to_string(status))));
		}
	};

	http::ResponseHandler header_handler =
		[received_body, api_handler](http::ExpectedIncomingResponsePtr exp_resp) {
			if (!exp_resp) {
				log::Error("Request to check new deployments failed: " + exp_resp.error().message);
				CheckUpdatesAPIResponse response = expected::unexpected(exp_resp.error());
				api_handler(response);
				return;
			}

			auto resp = exp_resp.value();
			received_body->clear();
			auto body_writer = make_shared<io::ByteWriter>(received_body);
			body_writer->SetUnlimited(true);
			resp->SetBodyWriter(body_writer);
		};

	http::ResponseHandler v1_body_handler =
		[received_body, api_handler, handle_data](http::ExpectedIncomingResponsePtr exp_resp) {
			if (!exp_resp) {
				log::Error("Request to check new deployments failed: " + exp_resp.error().message);
				CheckUpdatesAPIResponse response = expected::unexpected(exp_resp.error());
				api_handler(response);
				return;
			}
			auto resp = exp_resp.value();
			auto status = resp->GetStatusCode();
			if ((status == http::StatusOK) || (status == http::StatusNoContent)) {
				handle_data(status);
			} else {
				auto ex_err_msg = api::ErrorMsgFromErrorResponse(*received_body);
				string err_str;
				if (ex_err_msg) {
					err_str = ex_err_msg.value();
				} else {
					err_str = resp->GetStatusMessage();
				}
				api_handler(expected::unexpected(MakeError(
					BadResponseError,
					"Got unexpected response " + to_string(status) + ": " + err_str)));
			}
		};

	http::ResponseHandler v2_body_handler = [received_body,
											 v1_req,
											 header_handler,
											 v1_body_handler,
											 api_handler,
											 handle_data,
											 &client](http::ExpectedIncomingResponsePtr exp_resp) {
		if (!exp_resp) {
			log::Error("Request to check new deployments failed: " + exp_resp.error().message);
			CheckUpdatesAPIResponse response = expected::unexpected(exp_resp.error());
			api_handler(response);
			return;
		}
		auto resp = exp_resp.value();
		auto status = resp->GetStatusCode();
		if ((status == http::StatusOK) || (status == http::StatusNoContent)) {
			handle_data(status);
		} else if (status == http::StatusNotFound) {
			log::Debug(
				"POST request to v2 version of the deployments API failed, falling back to v1 version and GET");
			auto err = client.AsyncCall(v1_req, header_handler, v1_body_handler);
			if (err != error::NoError) {
				api_handler(expected::unexpected(err.WithContext("While calling v1 endpoint")));
			}
		} else {
			auto ex_err_msg = api::ErrorMsgFromErrorResponse(*received_body);
			string err_str;
			if (ex_err_msg) {
				err_str = ex_err_msg.value();
			} else {
				err_str = resp->GetStatusMessage();
			}
			api_handler(expected::unexpected(MakeError(
				BadResponseError,
				"Got unexpected response " + to_string(status) + ": " + err_str)));
		}
	};

	return client.AsyncCall(v2_req, header_handler, v2_body_handler);
}

static const string deployment_status_strings[static_cast<int>(DeploymentStatus::End_) + 1] = {
	"installing",
	"pause_before_installing",
	"downloading",
	"pause_before_rebooting",
	"rebooting",
	"pause_before_committing",
	"success",
	"failure",
	"already-installed"};

static const string deployments_uri_prefix = "/api/devices/v1/deployments/device/deployments";
static const string status_uri_suffix = "/status";

string DeploymentStatusString(DeploymentStatus status) {
	return deployment_status_strings[static_cast<int>(status)];
}

error::Error DeploymentClient::PushStatus(
	const string &deployment_id,
	DeploymentStatus status,
	const string &substate,
	api::Client &client,
	StatusAPIResponseHandler api_handler) {
	// Cannot push a status update without a deployment ID
	AssertOrReturnError(deployment_id != "");
	string payload = R"({"status":")" + DeploymentStatusString(status) + "\"";
	if (substate != "") {
		payload += R"(,"substate":")" + json::EscapeString(substate) + "\"}";
	} else {
		payload += "}";
	}
	http::BodyGenerator payload_gen = [payload]() {
		return make_shared<io::StringReader>(payload);
	};

	auto req = make_shared<api::APIRequest>();
	req->SetPath(http::JoinUrl(deployments_uri_prefix, deployment_id, status_uri_suffix));
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
				log::Error("Request to push status data failed: " + exp_resp.error().message);
				api_handler(exp_resp.error());
				return;
			}

			auto body_writer = make_shared<io::ByteWriter>(received_body);
			auto resp = exp_resp.value();
			auto content_length = resp->GetHeader("Content-Length");
			if (!content_length) {
				log::Debug(
					"Failed to get content length from the deployment status API response headers: "
					+ content_length.error().String());
				body_writer->SetUnlimited(true);
			} else {
				auto ex_len = common::StringTo<size_t>(content_length.value());
				if (!ex_len) {
					log::Error(
						"Failed to convert the content length from the deployment status API response headers to an integer: "
						+ ex_len.error().String());
					body_writer->SetUnlimited(true);
				} else {
					received_body->resize(ex_len.value());
				}
			}
			resp->SetBodyWriter(body_writer);
		},
		[received_body, api_handler](http::ExpectedIncomingResponsePtr exp_resp) {
			if (!exp_resp) {
				log::Error("Request to push status data failed: " + exp_resp.error().message);
				api_handler(exp_resp.error());
				return;
			}

			auto resp = exp_resp.value();
			auto status = resp->GetStatusCode();
			if (status == http::StatusNoContent) {
				api_handler(error::NoError);
			} else if (status == http::StatusConflict) {
				api_handler(
					MakeError(DeploymentAbortedError, "Could not send status update to server"));
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
						+ " from status API: " + err_str));
			}
		});
}

using mender::common::expected::ExpectedSize;

static ExpectedSize GetLogFileDataSize(const string &path) {
	auto ex_istr = io::OpenIfstream(path);
	if (!ex_istr) {
		return expected::unexpected(ex_istr.error());
	}
	auto istr = std::move(ex_istr.value());

	// We want the size of the actual data without a potential trailing
	// comma. So let's seek one byte before the end of file, check if the last
	// byte is a comma and return the appropriate number.
	istr.seekg(-1, ios_base::end);
	int c = istr.get();
	if (c == ',') {
		return istr.tellg() - static_cast<ifstream::off_type>(1);
	} else {
		return istr.tellg();
	}
}

const vector<uint8_t> JsonLogMessagesReader::header_ = {
	'{', '"', 'm', 'e', 's', 's', 'a', 'g', 'e', 's', '"', ':', '['};
const vector<uint8_t> JsonLogMessagesReader::closing_ = {']', '}'};
const string JsonLogMessagesReader::default_tstamp_ = "1970-01-01T00:00:00.000000000Z";
const string JsonLogMessagesReader::bad_data_msg_tmpl_ =
	R"d({"timestamp": "1970-01-01T00:00:00.000000000Z", "level": "ERROR", "message": "(THE ORIGINAL LOGS CONTAINED INVALID ENTRIES)"},)d";

JsonLogMessagesReader::~JsonLogMessagesReader() {
	reader_.reset();
	if (!sanitized_fpath_.empty() && path::FileExists(sanitized_fpath_)) {
		auto del_err = path::FileDelete(sanitized_fpath_);
		if (del_err != error::NoError) {
			log::Error("Failed to delete auxiliary logs file: " + del_err.String());
		}
	}
	sanitized_fpath_.erase();
}

static error::Error DoSanitizeLogs(
	const string &orig_path, const string &new_path, bool &all_valid, string &first_tstamp) {
	auto ex_ifs = io::OpenIfstream(orig_path);
	if (!ex_ifs) {
		return ex_ifs.error();
	}
	auto ex_ofs = io::OpenOfstream(new_path);
	if (!ex_ofs) {
		return ex_ofs.error();
	}
	auto &ifs = ex_ifs.value();
	auto &ofs = ex_ofs.value();

	string last_known_tstamp = first_tstamp;
	const string tstamp_prefix_data = R"d({"timestamp": ")d";
	const string corrupt_msg_suffix_data =
		R"d(", "level": "ERROR", "message": "(CORRUPTED LOG DATA)"},)d";

	string line;
	first_tstamp.erase();
	all_valid = true;
	error::Error err;
	while (!ifs.eof()) {
		getline(ifs, line);
		if (!ifs.eof() && !ifs) {
			int io_errno = errno;
			return error::Error(
				generic_category().default_error_condition(io_errno),
				"Failed to get line from deployment logs file '" + orig_path
					+ "': " + strerror(io_errno));
		}
		if (line.empty()) {
			// skip empty lines
			continue;
		}
		auto ex_json = json::Load(line);
		if (ex_json) {
			// valid JSON log line, just replace the newline after it with a comma and save the
			// timestamp for later
			auto ex_tstamp = ex_json.value().Get("timestamp").and_then(json::ToString);
			if (ex_tstamp) {
				if (first_tstamp.empty()) {
					first_tstamp = ex_tstamp.value();
				}
				last_known_tstamp = std::move(ex_tstamp.value());
			}
			line.append(1, ',');
			err = io::WriteStringIntoOfstream(ofs, line);
			if (err != error::NoError) {
				return err.WithContext("Failed to write pre-processed deployment logs data");
			}
		} else {
			all_valid = false;
			if (first_tstamp.empty()) {
				// If we still don't have the first valid tstamp, we need to
				// save the last known one (potentially pre-set) as the first
				// one.
				first_tstamp = last_known_tstamp;
			}
			err = io::WriteStringIntoOfstream(
				ofs, tstamp_prefix_data + last_known_tstamp + corrupt_msg_suffix_data);
			if (err != error::NoError) {
				return err.WithContext("Failed to write pre-processed deployment logs data");
			}
		}
	}
	return error::NoError;
}

error::Error JsonLogMessagesReader::SanitizeLogs() {
	if (!sanitized_fpath_.empty()) {
		return error::NoError;
	}

	string prep_fpath = log_fpath_ + ".sanitized";
	string first_tstamp = default_tstamp_;
	auto err = DoSanitizeLogs(log_fpath_, prep_fpath, clean_logs_, first_tstamp);
	if (err != error::NoError) {
		if (path::FileExists(prep_fpath)) {
			auto del_err = path::FileDelete(prep_fpath);
			if (del_err != error::NoError) {
				log::Error("Failed to delete auxiliary logs file: " + del_err.String());
			}
		}
	} else {
		sanitized_fpath_ = std::move(prep_fpath);
		reader_ = make_unique<io::FileReader>(sanitized_fpath_);
		auto ex_sz = GetLogFileDataSize(sanitized_fpath_);
		if (!ex_sz) {
			return ex_sz.error().WithContext("Failed to determine deployment logs size");
		}
		raw_data_size_ = ex_sz.value();
		rem_raw_data_size_ = raw_data_size_;
		if (!clean_logs_) {
			auto bad_data_msg_tstamp_start =
				bad_data_msg_.begin() + 15; // len(R"({"timestamp": ")")
			copy_n(first_tstamp.cbegin(), first_tstamp.size(), bad_data_msg_tstamp_start);
		}
	}
	return err;
}

error::Error JsonLogMessagesReader::Rewind() {
	AssertOrReturnError(!sanitized_fpath_.empty());
	header_rem_ = header_.size();
	closing_rem_ = closing_.size();
	bad_data_msg_rem_ = bad_data_msg_.size();

	// release/close the file first so that the FileDelete() below can actually
	// delete it and free space up
	reader_.reset();
	auto del_err = path::FileDelete(sanitized_fpath_);
	if (del_err != error::NoError) {
		log::Error("Failed to delete auxiliary logs file: " + del_err.String());
	}
	sanitized_fpath_.erase();
	return SanitizeLogs();
}

int64_t JsonLogMessagesReader::TotalDataSize() {
	assert(!sanitized_fpath_.empty());

	auto ret = raw_data_size_ + header_.size() + closing_.size();
	if (!clean_logs_) {
		ret += bad_data_msg_.size();
	}
	return ret;
}

ExpectedSize JsonLogMessagesReader::Read(
	vector<uint8_t>::iterator start, vector<uint8_t>::iterator end) {
	AssertOrReturnUnexpected(!sanitized_fpath_.empty());

	if (header_rem_ > 0) {
		io::Vsize target_size = end - start;
		auto copy_end = copy_n(
			header_.begin() + (header_.size() - header_rem_), min(header_rem_, target_size), start);
		auto n_copied = copy_end - start;
		header_rem_ -= n_copied;
		return static_cast<size_t>(n_copied);
	} else if (!clean_logs_ && (bad_data_msg_rem_ > 0)) {
		io::Vsize target_size = end - start;
		auto copy_end = copy_n(
			bad_data_msg_.begin() + (bad_data_msg_.size() - bad_data_msg_rem_),
			min(bad_data_msg_rem_, target_size),
			start);
		auto n_copied = copy_end - start;
		bad_data_msg_rem_ -= n_copied;
		return static_cast<size_t>(n_copied);
	} else if (rem_raw_data_size_ > 0) {
		if (end - start > rem_raw_data_size_) {
			end = start + static_cast<size_t>(rem_raw_data_size_);
		}
		auto ex_sz = reader_->Read(start, end);
		if (!ex_sz) {
			return ex_sz;
		}
		auto n_read = ex_sz.value();
		rem_raw_data_size_ -= n_read;

		// We control how much we read from the file so we should never read
		// 0 bytes (meaning EOF reached). If we do, it means the file is
		// smaller than what we were told.
		assert(n_read > 0);
		if (n_read == 0) {
			return expected::unexpected(
				MakeError(InvalidDataError, "Unexpected EOF when reading logs file"));
		}
		return n_read;
	} else if (closing_rem_ > 0) {
		io::Vsize target_size = end - start;
		auto copy_end = copy_n(
			closing_.begin() + (closing_.size() - closing_rem_),
			min(closing_rem_, target_size),
			start);
		auto n_copied = copy_end - start;
		closing_rem_ -= n_copied;
		return static_cast<size_t>(copy_end - start);
	} else {
		return 0;
	}
};

static const string logs_uri_suffix = "/log";

error::Error DeploymentClient::PushLogs(
	const string &deployment_id,
	const string &log_file_path,
	api::Client &client,
	LogsAPIResponseHandler api_handler) {
	auto logs_reader = make_shared<JsonLogMessagesReader>(log_file_path);
	auto err = logs_reader->SanitizeLogs();
	if (err != error::NoError) {
		return err;
	}

	auto req = make_shared<api::APIRequest>();
	req->SetPath(http::JoinUrl(deployments_uri_prefix, deployment_id, logs_uri_suffix));
	req->SetMethod(http::Method::PUT);
	req->SetHeader("Content-Type", "application/json");
	req->SetHeader("Content-Length", to_string(logs_reader->TotalDataSize()));
	req->SetHeader("Accept", "application/json");
	req->SetBodyGenerator([logs_reader]() {
		logs_reader->Rewind();
		return logs_reader;
	});

	auto received_body = make_shared<vector<uint8_t>>();
	return client.AsyncCall(
		req,
		[received_body, api_handler](http::ExpectedIncomingResponsePtr exp_resp) {
			if (!exp_resp) {
				log::Error("Request to push logs data failed: " + exp_resp.error().message);
				api_handler(exp_resp.error());
				return;
			}

			auto body_writer = make_shared<io::ByteWriter>(received_body);
			auto resp = exp_resp.value();
			auto content_length = resp->GetHeader("Content-Length");
			if (!content_length) {
				log::Debug(
					"Failed to get content length from the deployment log API response headers: "
					+ content_length.error().String());
				body_writer->SetUnlimited(true);
			} else {
				auto ex_len = common::StringTo<size_t>(content_length.value());
				if (!ex_len) {
					log::Error(
						"Failed to convert the content length from the deployment log API response headers to an integer: "
						+ ex_len.error().String());
					body_writer->SetUnlimited(true);
				} else {
					received_body->resize(ex_len.value());
				}
			}
			resp->SetBodyWriter(body_writer);
		},
		[received_body, api_handler](http::ExpectedIncomingResponsePtr exp_resp) {
			if (!exp_resp) {
				log::Error("Request to push logs data failed: " + exp_resp.error().message);
				api_handler(exp_resp.error());
				return;
			}

			auto resp = exp_resp.value();
			auto status = resp->GetStatusCode();
			if (status == http::StatusNoContent) {
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
					"Got unexpected response " + to_string(status) + " from logs API: " + err_str));
			}
		});
}

} // namespace deployments
} // namespace update
} // namespace mender
