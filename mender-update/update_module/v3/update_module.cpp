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

#include <mender-update/update_module/v3/update_module.hpp>

#include <common/events.hpp>
#include <common/error.hpp>
#include <common/expected.hpp>
#include <common/path.hpp>

namespace mender {
namespace update {
namespace update_module {
namespace v3 {

namespace error = mender::common::error;
namespace expected = mender::common::expected;
namespace path = mender::common::path;

static std::string StateString[] = {
	"Download",
	"ArtifactInstall",
	"NeedsArtifactReboot",
	"ArtifactReboot",
	"ArtifactCommit",
	"SupportsRollback",
	"ArtifactRollback",
	"ArtifactVerifyReboot",
	"ArtifactRollbackReboot",
	"ArtifactVerifyRollbackReboot",
	"ArtifactFailure",
	"Cleanup"};

std::string StateToString(State state) {
	static_assert(
		sizeof(StateString) / sizeof(*StateString) == static_cast<int>(State::LastState),
		"Make sure to keep State and StateString in sync!");
	return StateString[static_cast<int>(state)];
}

UpdateModule::UpdateModule(MenderContext &ctx, const string &payload_type) :
	ctx_(ctx) {
	update_module_path_ = path::Join(ctx.modules_path, payload_type);
	update_module_workdir_ =
		path::Join(ctx.modules_work_path, "modules", "v3", "payloads", "0000", "tree");
}

UpdateModule::DownloadData::DownloadData(artifact::Payload &payload) :
	payload_(payload) {
	buffer_.resize(MENDER_BUFSIZE);
}

error::Error UpdateModule::CallStateNoCapture(State state) {
	return CallState(state, nullptr);
}

error::Error UpdateModule::Download(artifact::Payload &payload) {
	download_ = make_unique<DownloadData>(payload);

	download_->event_loop_.Post([this]() { StartDownloadProcess(); });

	download_->event_loop_.Run();

	auto result = move(download_->result_);
	download_.reset();
	return result;
}

error::Error UpdateModule::ArtifactInstall() {
	return CallStateNoCapture(State::ArtifactInstall);
}

ExpectedRebootAction UpdateModule::NeedsReboot() {
	std::string processStdOut;
	auto err = CallState(State::NeedsReboot, &processStdOut);
	if (err != error::NoError) {
		return expected::unexpected(err);
	}
	if (processStdOut == "Yes") {
		return RebootAction::Yes;
	} else if (processStdOut == "No" || processStdOut == "") {
		return RebootAction::No;
	} else if (processStdOut == "Automatic") {
		return RebootAction::Automatic;
	}
	return expected::unexpected(error::Error(
		make_error_condition(errc::protocol_error),
		"Unexpected output from the process for NeedsReboot state"));
}

error::Error UpdateModule::ArtifactReboot() {
	return CallStateNoCapture(State::ArtifactReboot);
}

error::Error UpdateModule::ArtifactCommit() {
	return CallStateNoCapture(State::ArtifactCommit);
}

expected::ExpectedBool UpdateModule::SupportsRollback() {
	std::string processStdOut;
	auto err = CallState(State::SupportsRollback, &processStdOut);
	if (err != error::NoError) {
		return expected::unexpected(err);
	}
	if (processStdOut == "Yes") {
		return true;
	} else if (processStdOut == "No" || processStdOut == "") {
		return false;
	}
	return expected::unexpected(error::Error(
		make_error_condition(errc::protocol_error),
		"Unexpected output from the process for SupportsRollback state"));
}

error::Error UpdateModule::ArtifactRollback() {
	return CallStateNoCapture(State::ArtifactRollback);
}

error::Error UpdateModule::ArtifactVerifyReboot() {
	return CallStateNoCapture(State::ArtifactVerifyReboot);
}

error::Error UpdateModule::ArtifactRollbackReboot() {
	return CallStateNoCapture(State::ArtifactRollbackReboot);
}

error::Error UpdateModule::ArtifactVerifyRollbackReboot() {
	return CallStateNoCapture(State::ArtifactVerifyRollbackReboot);
}

error::Error UpdateModule::ArtifactFailure() {
	return CallStateNoCapture(State::ArtifactFailure);
}

error::Error UpdateModule::Cleanup() {
	return CallStateNoCapture(State::Cleanup);
}

string UpdateModule::GetModulePath() const {
	return update_module_path_;
}

string UpdateModule::GetModulesWorkPath() const {
	return update_module_workdir_;
}

error::Error UpdateModule::GetProcessError(const error::Error &err) {
	if (err.code == make_error_condition(errc::no_such_file_or_directory)) {
		return context::MakeError(context::NoSuchUpdateModuleError, err.message);
	}
	return err;
}

} // namespace v3
} // namespace update_module
} // namespace update
} // namespace mender
