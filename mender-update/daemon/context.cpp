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

#include <mender-update/daemon/context.hpp>

#include <common/log.hpp>

namespace mender {
namespace update {
namespace daemon {

namespace log = mender::common::log;
namespace main_context = mender::update::context;

const int STATE_DATA_VERSION = 2;

ExpectedStateData ApiResponseJsonToStateData(const json::Json &json) {
	StateData data;

	expected::ExpectedString str = json.Get("id").and_then(json::ToString);
	if (!str) {
		return expected::unexpected(str.error().WithContext("Could not get deployment ID"));
	}
	data.update_info.id = str.value();

	str = json.Get("artifact")
			  .and_then([](const json::Json &json) { return json.Get("source"); })
			  .and_then([](const json::Json &json) { return json.Get("uri"); })
			  .and_then(json::ToString);
	if (!str) {
		return expected::unexpected(
			str.error().WithContext("Could not get artifact URI for deployment"));
	}
	data.update_info.artifact.source.uri = str.value();
	log::Debug("Artifact Download URL: " + data.update_info.artifact.source.uri);

	str = json.Get("artifact")
			  .and_then([](const json::Json &json) { return json.Get("source"); })
			  .and_then([](const json::Json &json) { return json.Get("expire"); })
			  .and_then(json::ToString);
	if (str) {
		data.update_info.artifact.source.expire = str.value();
		// If it's not available, we don't care.
	}

	// For later: Update Control Maps should be handled here.

	// Note: There is more information available in the response than we collect here, but we
	// prefer to get the information from the artifact instead, since it is the authoritative
	// source. And it's also signed, unlike the response.

	return data;
}

const string kRollbackNotSupported = "rollback-not-supported";
const string kRollbackSupported = "rollback-supported";

string SupportsRollbackToDbString(bool support) {
	return support ? kRollbackSupported : kRollbackNotSupported;
}

expected::ExpectedBool DbStringToSupportsRollback(const string &str) {
	if (str == kRollbackSupported) {
		return true;
	} else if (str == kRollbackNotSupported) {
		return false;
	} else {
		return expected::unexpected(main_context::MakeError(
			main_context::DatabaseValueError,
			"\"" + str + "\" is not a valid value for SupportsRollback"));
	}
}

const string kRebootTypeNone = "";
const string kRebootTypeCustom = "reboot-type-custom";
const string kRebootTypeAutomatic = "reboot-type-automatic";

string NeedsRebootToDbString(update_module::RebootAction action) {
	switch (action) {
	case update_module::RebootAction::No:
		return kRebootTypeNone;
	case update_module::RebootAction::Automatic:
		return kRebootTypeAutomatic;
	case update_module::RebootAction::Yes:
		return kRebootTypeCustom;
	default:
		// Should not happen.
		assert(false);
		return kRebootTypeNone;
	}
}

update_module::ExpectedRebootAction DbStringToNeedsReboot(const string &str) {
	if (str == kRebootTypeNone) {
		return update_module::RebootAction::No;
	} else if (str == kRebootTypeAutomatic) {
		return update_module::RebootAction::Automatic;
	} else if (str == kRebootTypeCustom) {
		return update_module::RebootAction::Yes;
	} else {
		return expected::unexpected(main_context::MakeError(
			main_context::DatabaseValueError,
			"\"" + str + "\" is not a valid value for RebootRequested"));
	}
}

void StateData::FillUpdateDataFromArtifact(artifact::PayloadHeaderView &view) {
	version = view.version;
	auto &artifact = update_info.artifact;
	auto &header = view.header;
	artifact.compatible_devices = header.header_info.depends.device_type;
	artifact.payload_types = {header.payload_type};
	artifact.artifact_name = header.artifact_name;
	artifact.artifact_group = header.artifact_group;
	if (header.type_info.artifact_provides) {
		artifact.type_info_provides = header.type_info.artifact_provides.value();
	} else {
		artifact.type_info_provides.clear();
	}
	if (header.type_info.clears_artifact_provides) {
		artifact.clears_artifact_provides = header.type_info.clears_artifact_provides.value();
	} else {
		artifact.clears_artifact_provides.clear();
	}
}

Context::Context(main_context::MenderContext &mender_context, events::EventLoop &event_loop) :
	mender_context(mender_context),
	event_loop(event_loop),
	deployment_client(http::ClientConfig(mender_context.GetConfig().server_url), event_loop),
	download_client(http::ClientConfig(mender_context.GetConfig().server_url), event_loop) {
}

} // namespace daemon
} // namespace update
} // namespace mender
