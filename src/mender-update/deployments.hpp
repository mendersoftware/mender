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

#ifndef MENDER_UPDATE_DEPLOYMENTS_HPP
#define MENDER_UPDATE_DEPLOYMENTS_HPP

#include <config.h>

#ifdef MENDER_LOG_BOOST
#include <boost/log/sinks/sync_frontend.hpp>
#include <boost/log/sinks/text_ostream_backend.hpp>
#include <boost/smart_ptr/shared_ptr.hpp>
#endif // MENDER_LOG_BOOST

#include <string>
#include <vector>

#include <api/client.hpp>
#include <common/error.hpp>
#include <common/events.hpp>
#include <common/expected.hpp>
#include <common/http.hpp>
#include <common/io.hpp>
#include <common/json.hpp>
#include <common/optional.hpp>
#include <mender-update/context.hpp>

namespace mender {
namespace update {
namespace deployments {

using namespace std;

#ifdef MENDER_LOG_BOOST
namespace sinks = boost::log::sinks;
#endif // MENDER_LOG_BOOST

namespace api = mender::api;
namespace context = mender::update::context;
namespace error = mender::common::error;
namespace events = mender::common::events;
namespace expected = mender::common::expected;
namespace io = mender::common::io;
namespace json = mender::common::json;

enum DeploymentsErrorCode {
	NoError = 0,
	InvalidDataError,
	BadResponseError,
	DeploymentAbortedError,
};

class DeploymentsErrorCategoryClass : public std::error_category {
public:
	const char *name() const noexcept override;
	string message(int code) const override;
};
extern const DeploymentsErrorCategoryClass DeploymentsErrorCategory;

error::Error MakeError(DeploymentsErrorCode code, const string &msg);

using CheckUpdatesAPIResponse = expected::expected<optional<json::Json>, error::Error>;
using CheckUpdatesAPIResponseHandler = function<void(CheckUpdatesAPIResponse)>;

enum class DeploymentStatus {
	Installing = 0,
	PauseBeforeInstalling,
	Downloading,
	PauseBeforeRebooting,
	Rebooting,
	PauseBeforeCommitting,
	Success,
	Failure,
	AlreadyInstalled,

	// Not a valid status, just used as an int representing the number of values
	// above
	End_
};

string DeploymentStatusString(DeploymentStatus status);

using StatusAPIResponse = error::Error;
using StatusAPIResponseHandler = function<void(StatusAPIResponse)>;

using LogsAPIResponse = error::Error;
using LogsAPIResponseHandler = function<void(LogsAPIResponse)>;

class DeploymentAPI {
public:
	virtual ~DeploymentAPI() {
	}

	virtual error::Error CheckNewDeployments(
		context::MenderContext &ctx,
		api::Client &client,
		CheckUpdatesAPIResponseHandler api_handler) = 0;
	virtual error::Error PushStatus(
		const string &deployment_id,
		DeploymentStatus status,
		const string &substate,
		api::Client &client,
		StatusAPIResponseHandler api_handler) = 0;
	virtual error::Error PushLogs(
		const string &deployment_id,
		const string &log_file_path,
		api::Client &client,
		LogsAPIResponseHandler api_handler) = 0;
};

class DeploymentClient : virtual public DeploymentAPI {
public:
	error::Error CheckNewDeployments(
		context::MenderContext &ctx,
		api::Client &client,
		CheckUpdatesAPIResponseHandler api_handler) override;
	error::Error PushStatus(
		const string &deployment_id,
		DeploymentStatus status,
		const string &substate,
		api::Client &client,
		StatusAPIResponseHandler api_handler) override;
	error::Error PushLogs(
		const string &deployment_id,
		const string &log_file_path,
		api::Client &client,
		LogsAPIResponseHandler api_handler) override;
};

/**
 * A helper class only declared here because of testing. Not to be used
 * separately outside of PushLogs().
 */
class JsonLogMessagesReader : virtual public io::Reader {
public:
	/**
	 * @see GetLogFileDataSize() for details about #data_size
	 */
	JsonLogMessagesReader(shared_ptr<io::FileReader> raw_data_reader, size_t data_size) :
		reader_ {raw_data_reader},
		raw_data_size_ {data_size},
		rem_raw_data_size_ {data_size} {};

	expected::ExpectedSize Read(
		vector<uint8_t>::iterator start, vector<uint8_t>::iterator end) override;

	error::Error Rewind() {
		header_rem_ = header_.size();
		closing_rem_ = closing_.size();
		rem_raw_data_size_ = raw_data_size_;
		return reader_->Rewind();
	}

	static size_t TotalDataSize(size_t raw_data_size) {
		return raw_data_size + header_.size() + closing_.size();
	}

private:
	shared_ptr<io::FileReader> reader_;
	size_t raw_data_size_;
	size_t rem_raw_data_size_;
	static const vector<uint8_t> header_;
	static const vector<uint8_t> closing_;
	io::Vsize header_rem_ = header_.size();
	io::Vsize closing_rem_ = closing_.size();
};

class DeploymentLog {
public:
	DeploymentLog(const string &data_store_dir, const string &deployment_id) :
		data_store_dir_ {data_store_dir},
		id_ {deployment_id} {};
	error::Error BeginLogging();
	error::Error FinishLogging();
	~DeploymentLog() {
		if (sink_) {
			FinishLogging();
		}
	};

	string LogFileName();
	string LogFilePath();

private:
	const string data_store_dir_;
	const string id_;
#ifdef MENDER_LOG_BOOST
	typedef sinks::synchronous_sink<sinks::text_ostream_backend> text_sink;
	boost::shared_ptr<text_sink> sink_;
#endif // MENDER_LOG_BOOST
	error::Error PrepareLogDirectory();
};

} // namespace deployments
} // namespace update
} // namespace mender

#endif // MENDER_UPDATE_DEPLOYMENTS_HPP
