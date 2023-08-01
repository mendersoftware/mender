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

#include <boost/date_time/posix_time/posix_time.hpp>
#include <boost/log/attributes.hpp>
#include <boost/log/common.hpp>
#include <boost/log/sinks.hpp>
#include <boost/smart_ptr/shared_ptr.hpp>

#include <algorithm>
#include <cctype>
#include <filesystem>
#include <string>

#include <common/error.hpp>
#include <common/io.hpp>
#include <common/json.hpp>
#include <common/log.hpp>
#include <common/path.hpp>

namespace mender {
namespace update {
namespace deployments {

using namespace std;

namespace logging = boost::log;
namespace expr = boost::log::expressions;
namespace sinks = boost::log::sinks;

namespace fs = std::filesystem;

namespace error = mender::common::error;
namespace io = mender::common::io;
namespace json = mender::common::json;
namespace mlog = mender::common::log;
namespace path = mender::common::path;

static void JsonLogFormatter(logging::record_view const &rec, logging::formatting_ostream &strm) {
	strm << "{";

	auto val = logging::extract<boost::posix_time::ptime>("TimeStamp", rec);
	if (val) {
		strm << R"("timestamp":")"
			 << json::EscapeString(boost::posix_time::to_iso_extended_string(val.get())) << "\",";
	}

	auto level = logging::extract<mlog::LogLevel>("Severity", rec);
	if (level) {
		string lvl = mlog::ToStringLogLevel(level.get());
		strm << R"("level":")" << json::EscapeString(lvl) << "\",";
	}

	strm << R"("message":")" << json::EscapeString(*rec[expr::smessage]) << "\"}";
}

static const size_t kMaxExistingLogs = 5;
static const uintmax_t kLogsFreeSpaceRequired = 100 * 1024; // 100 KiB

error::Error DeploymentLog::PrepareLogDirectory() {
	fs::path dir_path(data_store_dir_);
	bool created = fs::create_directories(dir_path);
	if (created) {
		// should basically never happen, but if we happened to create the
		// directory, it's empty and thus well-prepared
		return error::NoError;
	}

	vector<string> old_logs;
	for (auto const &entry : fs::directory_iterator {dir_path}) {
		fs::path file_path = entry.path();
		if (!fs::is_regular_file(file_path)) {
			continue;
		}

		string file_name = file_path.filename().string();

		if (file_name == "deployments.0000." + id_ + ".log") {
			// this log file will be (re)used, leave it alone
			continue;
		}

		if ((file_name.find("deployments.") != 0)
			|| (file_name.substr(file_name.size() - 4) != ".log")) {
			continue;
		}

		// expected file name: deployments.NNNN.ID.log
		// "deployments.".size() == 12
		auto second_dot_pos = file_name.find('.', 12);
		auto last_dot_pos = file_name.find_last_of('.');
		if ((second_dot_pos == string::npos) || (last_dot_pos == string::npos)
			|| (second_dot_pos == last_dot_pos) || (second_dot_pos != 16)
			|| any_of(file_name.cbegin() + 12, file_name.cbegin() + second_dot_pos, [](char c) {
				   return !isdigit(c);
			   })) {
			mlog::Warning("Old deployment log with a malformed file name found: " + file_name);
			continue;
		}

		old_logs.push_back(file_name);
	}
	std::sort(old_logs.begin(), old_logs.end());

	error_code ec;
	fs::space_info space_info = fs::space(dir_path, ec);
	if (ec) {
		return error::Error(
			ec.default_error_condition(), "Failed to check free space for log files");
	}

	while ((old_logs.size() > 0)
		   && ((space_info.available < kLogsFreeSpaceRequired)
			   || (old_logs.size() > (kMaxExistingLogs - 1)))) {
		auto last_log_file = old_logs[old_logs.size() - 1];
		old_logs.pop_back();
		if (!fs::remove(dir_path / last_log_file, ec) && ec) {
			return error::Error(
				ec.default_error_condition(),
				"Failed to remove old log file '" + last_log_file + "'");
		}
		if (space_info.available < kLogsFreeSpaceRequired) {
			space_info = fs::space(dir_path, ec);
			if (ec) {
				return error::Error(
					ec.default_error_condition(), "Failed to check free space for log files");
			}
		}
	}

	// now let's make sure old logs have an increasing index starting with 0001
	for (ssize_t i = old_logs.size() - 1; i >= 0; i--) {
		// "deployments.".size() == 12
		auto second_dot_pos = old_logs[i].find('.', 12);
		auto last_dot_pos = old_logs[i].find_last_of('.');

		// should never happen due the filter above when populating old_logs and
		// due to how these files are named
		assert(second_dot_pos != string::npos);
		assert(last_dot_pos != string::npos);
		assert(second_dot_pos != last_dot_pos);

		string deployment_id;
		if ((second_dot_pos == string::npos) || (last_dot_pos == string::npos)) {
			deployment_id = "unknown_deployment";
		} else {
			deployment_id =
				old_logs[i].substr(second_dot_pos + 1, (last_dot_pos - second_dot_pos - 1));
		}
		stringstream ss;
		ss << "deployments.";
		ss << setfill('0') << setw(4) << to_string(i + 1);
		ss << "." + deployment_id;
		ss << ".log";

		string new_name = ss.str();
		fs::rename(dir_path / old_logs[i], dir_path / new_name, ec);
		if (ec) {
			return error::Error(
				ec.default_error_condition(),
				"Failed to rename old log file '" + old_logs[i] + "'");
		}
	}

	return error::NoError;
}

error::Error DeploymentLog::BeginLogging() {
	auto err = PrepareLogDirectory();
	if (err != error::NoError) {
		return err;
	}

	auto log_file_path = path::Join(data_store_dir_, "deployments.0000." + id_ + ".log");
	auto ex_ofstr = io::OpenOfstream(log_file_path, true);
	if (!ex_ofstr) {
		return ex_ofstr.error();
	}

	auto log_stream = boost::make_shared<std::ofstream>(std::move(ex_ofstr.value()));
	sink_ = boost::make_shared<text_sink>();
	sink_->set_formatter(&JsonLogFormatter);
	sink_->locked_backend()->add_stream(log_stream);
	sink_->locked_backend()->auto_flush(true);

	logging::core::get()->add_sink(sink_);

	return error::NoError;
}

error::Error DeploymentLog::FinishLogging() {
	logging::core::get()->remove_sink(sink_);
	sink_.reset();
	return error::NoError;
}

} // namespace deployments
} // namespace update
} // namespace mender
