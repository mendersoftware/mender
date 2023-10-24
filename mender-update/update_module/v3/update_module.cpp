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
	"ProvidePayloadFileSizes",
	"Download",
	"DownloadWithFileSizes",
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
	ctx_ {ctx} {
	update_module_path_ = path::Join(ctx.GetConfig().paths.GetModulesPath(), payload_type);
	update_module_workdir_ =
		path::Join(ctx.GetConfig().paths.GetModulesWorkPath(), "payloads", "0000", "tree");
}

UpdateModule::DownloadData::DownloadData(
	events::EventLoop &event_loop, artifact::Payload &payload) :
	payload_ {payload},
	event_loop_ {event_loop} {
	buffer_.resize(MENDER_BUFSIZE);
}

static expected::ExpectedBool HandleProvidePayloadFileSizesOutput(
	const expected::ExpectedString &exp_output) {
	if (!exp_output) {
		return expected::unexpected(error::Error(exp_output.error()));
	}
	auto &processStdOut = exp_output.value();
	if (processStdOut == "Yes") {
		return true;
	} else if (processStdOut == "No" || processStdOut == "") {
		return false;
	}
	return expected::unexpected(error::Error(
		make_error_condition(errc::protocol_error),
		"Unexpected output from the process for ProvidePayloadFileSizes state: " + processStdOut));
}

expected::ExpectedBool UpdateModule::ProvidePayloadFileSizes() {
	return HandleProvidePayloadFileSizesOutput(CallStateCapture(State::ProvidePayloadFileSizes));
}

error::Error UpdateModule::AsyncProvidePayloadFileSizes(
	events::EventLoop &event_loop, ProvidePayloadFileSizesFinishedHandler handler) {
	return AsyncCallStateCapture(
		event_loop, State::ProvidePayloadFileSizes, [handler](expected::ExpectedString exp_output) {
			handler(HandleProvidePayloadFileSizesOutput(exp_output));
		});
}

error::Error UpdateModule::Download(artifact::Payload &payload) {
	events::EventLoop event_loop;
	error::Error err;
	AsyncDownload(event_loop, payload, [&event_loop, &err](error::Error inner_err) {
		err = inner_err;
		event_loop.Stop();
	});
	event_loop.Run();
	return err;
}

void UpdateModule::AsyncDownload(
	events::EventLoop &event_loop,
	artifact::Payload &payload,
	UpdateModule::StateFinishedHandler handler) {
	download_ = make_unique<DownloadData>(event_loop, payload);

	download_->download_finished_handler_ = [this, handler](error::Error err) {
		handler(err);
		download_.reset();
	};

	download_->event_loop_.Post([this]() { StartDownloadProcess(); });
}

error::Error UpdateModule::DownloadWithFileSizes(artifact::Payload &payload) {
	events::EventLoop event_loop;
	error::Error err;
	AsyncDownloadWithFileSizes(event_loop, payload, [&event_loop, &err](error::Error inner_err) {
		err = inner_err;
		event_loop.Stop();
	});
	event_loop.Run();
	return err;
}

void UpdateModule::AsyncDownloadWithFileSizes(
	events::EventLoop &event_loop,
	artifact::Payload &payload,
	UpdateModule::StateFinishedHandler handler) {
	download_ = make_unique<DownloadData>(event_loop, payload);
	download_->downloading_with_sizes_ = true;

	download_->download_finished_handler_ = [this, handler](error::Error err) {
		handler(err);
		download_.reset();
	};

	download_->event_loop_.Post([this]() { StartDownloadProcess(); });
}

error::Error UpdateModule::ArtifactInstall() {
	return CallStateNoCapture(State::ArtifactInstall);
}

error::Error UpdateModule::AsyncArtifactInstall(
	events::EventLoop &event_loop, StateFinishedHandler handler) {
	return AsyncCallStateNoCapture(event_loop, State::ArtifactInstall, handler);
}

static ExpectedRebootAction HandleNeedsRebootOutput(const expected::ExpectedString &exp_output) {
	if (!exp_output) {
		return expected::unexpected(error::Error(exp_output.error()));
	}
	auto &processStdOut = exp_output.value();
	if (processStdOut == "Yes") {
		return RebootAction::Yes;
	} else if (processStdOut == "No" || processStdOut == "") {
		return RebootAction::No;
	} else if (processStdOut == "Automatic") {
		return RebootAction::Automatic;
	}
	return expected::unexpected(error::Error(
		make_error_condition(errc::protocol_error),
		"Unexpected output from the process for NeedsReboot state: " + processStdOut));
}

ExpectedRebootAction UpdateModule::NeedsReboot() {
	return HandleNeedsRebootOutput(CallStateCapture(State::NeedsReboot));
}

error::Error UpdateModule::AsyncNeedsReboot(
	events::EventLoop &event_loop, NeedsRebootFinishedHandler handler) {
	return AsyncCallStateCapture(
		event_loop, State::NeedsReboot, [handler](expected::ExpectedString exp_output) {
			handler(HandleNeedsRebootOutput(exp_output));
		});
}

error::Error UpdateModule::ArtifactReboot() {
	return CallStateNoCapture(State::ArtifactReboot);
}

error::Error UpdateModule::AsyncArtifactReboot(
	events::EventLoop &event_loop, StateFinishedHandler handler) {
	return AsyncCallStateNoCapture(event_loop, State::ArtifactReboot, handler);
}

error::Error UpdateModule::ArtifactCommit() {
	return CallStateNoCapture(State::ArtifactCommit);
}

error::Error UpdateModule::AsyncArtifactCommit(
	events::EventLoop &event_loop, StateFinishedHandler handler) {
	return AsyncCallStateNoCapture(event_loop, State::ArtifactCommit, handler);
}

static expected::ExpectedBool HandleSupportsRollbackOutput(
	const expected::ExpectedString &exp_output) {
	if (!exp_output) {
		return expected::unexpected(error::Error(exp_output.error()));
	}
	auto &processStdOut = exp_output.value();
	if (processStdOut == "Yes") {
		return true;
	} else if (processStdOut == "No" || processStdOut == "") {
		return false;
	}
	return expected::unexpected(error::Error(
		make_error_condition(errc::protocol_error),
		"Unexpected output from the process for SupportsRollback state: " + processStdOut));
}

expected::ExpectedBool UpdateModule::SupportsRollback() {
	return HandleSupportsRollbackOutput(CallStateCapture(State::SupportsRollback));
}

error::Error UpdateModule::AsyncSupportsRollback(
	events::EventLoop &event_loop, SupportsRollbackFinishedHandler handler) {
	return AsyncCallStateCapture(
		event_loop, State::SupportsRollback, [handler](expected::ExpectedString exp_output) {
			handler(HandleSupportsRollbackOutput(exp_output));
		});
}

error::Error UpdateModule::ArtifactRollback() {
	return CallStateNoCapture(State::ArtifactRollback);
}

error::Error UpdateModule::AsyncArtifactRollback(
	events::EventLoop &event_loop, StateFinishedHandler handler) {
	return AsyncCallStateNoCapture(event_loop, State::ArtifactRollback, handler);
}

error::Error UpdateModule::ArtifactVerifyReboot() {
	return CallStateNoCapture(State::ArtifactVerifyReboot);
}

error::Error UpdateModule::AsyncArtifactVerifyReboot(
	events::EventLoop &event_loop, StateFinishedHandler handler) {
	return AsyncCallStateNoCapture(event_loop, State::ArtifactVerifyReboot, handler);
}

error::Error UpdateModule::ArtifactRollbackReboot() {
	return CallStateNoCapture(State::ArtifactRollbackReboot);
}

error::Error UpdateModule::AsyncArtifactRollbackReboot(
	events::EventLoop &event_loop, StateFinishedHandler handler) {
	return AsyncCallStateNoCapture(event_loop, State::ArtifactRollbackReboot, handler);
}

error::Error UpdateModule::ArtifactVerifyRollbackReboot() {
	return CallStateNoCapture(State::ArtifactVerifyRollbackReboot);
}

error::Error UpdateModule::AsyncArtifactVerifyRollbackReboot(
	events::EventLoop &event_loop, StateFinishedHandler handler) {
	return AsyncCallStateNoCapture(event_loop, State::ArtifactVerifyRollbackReboot, handler);
}

error::Error UpdateModule::ArtifactFailure() {
	return CallStateNoCapture(State::ArtifactFailure);
}

error::Error UpdateModule::AsyncArtifactFailure(
	events::EventLoop &event_loop, StateFinishedHandler handler) {
	return AsyncCallStateNoCapture(event_loop, State::ArtifactFailure, handler);
}

error::Error UpdateModule::Cleanup() {
	return CallStateNoCapture(State::Cleanup);
}

error::Error UpdateModule::AsyncCleanup(
	events::EventLoop &event_loop, StateFinishedHandler handler) {
	return AsyncCallStateNoCapture(event_loop, State::Cleanup, handler);
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

error::Error UpdateModule::AsyncCallStateCapture(
	events::EventLoop &loop, State state, function<void(expected::ExpectedString)> handler) {
	state_runner_.reset(new StateRunner(loop, state, GetModulePath(), GetModulesWorkPath()));

	return state_runner_->AsyncCallState(
		state,
		true,
		chrono::seconds(ctx_.GetConfig().module_timeout_seconds),
		[handler](expected::expected<optional<string>, error::Error> exp_output) {
			if (!exp_output) {
				handler(expected::unexpected(exp_output.error()));
			} else {
				assert(exp_output.value());
				handler(exp_output.value().value());
			}
		});
}

expected::ExpectedString UpdateModule::CallStateCapture(State state) {
	events::EventLoop loop;
	expected::ExpectedString ret;
	auto err = AsyncCallStateCapture(loop, state, [&ret, &loop](expected::ExpectedString str) {
		ret = str;
		loop.Stop();
	});

	if (err != error::NoError) {
		return expected::unexpected(err);
	}

	loop.Run();

	state_runner_.reset();

	return ret;
}

error::Error UpdateModule::AsyncCallStateNoCapture(
	events::EventLoop &loop, State state, function<void(error::Error)> handler) {
	state_runner_.reset(new StateRunner(loop, state, GetModulePath(), GetModulesWorkPath()));

	return state_runner_->AsyncCallState(
		state,
		false,
		chrono::seconds(ctx_.GetConfig().module_timeout_seconds),
		[handler](expected::expected<optional<string>, error::Error> exp_output) {
			if (!exp_output) {
				handler(exp_output.error());
			} else {
				assert(!exp_output.value());
				handler(error::NoError);
			}
		});
}

error::Error UpdateModule::CallStateNoCapture(State state) {
	events::EventLoop loop;
	error::Error err;
	err = AsyncCallStateNoCapture(loop, state, [&err, &loop](error::Error inner_err) {
		err = inner_err;
		loop.Stop();
	});

	if (err != error::NoError) {
		return err;
	}

	loop.Run();

	state_runner_.reset();

	return err;
}

} // namespace v3
} // namespace update_module
} // namespace update
} // namespace mender
