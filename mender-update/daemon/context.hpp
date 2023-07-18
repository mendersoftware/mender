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

#ifndef MENDER_UPDATE_DAEMON_CONTEXT_HPP
#define MENDER_UPDATE_DAEMON_CONTEXT_HPP

#include <memory>

#include <common/error.hpp>
#include <common/events.hpp>
#include <common/expected.hpp>
#include <common/http.hpp>
#include <common/io.hpp>
#include <common/json.hpp>

#include <artifact/artifact.hpp>

#include <mender-update/context.hpp>
#include <mender-update/update_module/v3/update_module.hpp>

namespace mender {
namespace update {
namespace daemon {

using namespace std;

namespace error = mender::common::error;
namespace events = mender::common::events;
namespace expected = mender::common::expected;
namespace http = mender::http;
namespace io = mender::common::io;
namespace json = mender::common::json;

namespace artifact = mender::artifact;

namespace update_module = mender::update::update_module::v3;

// current version of the format of StateData;
// increase the version number once the format of StateData is changed
// StateDataVersion = 2 was introduced in Mender 2.0.0.
extern const int STATE_DATA_VERSION;

struct ArtifactSource {
	string uri;
	string expire;
};

struct ArtifactData {
	ArtifactSource source;
	// Compatible devices for dependency checking.
	vector<string> compatible_devices;
	// What kind of payloads are embedded in the artifact
	// (e.g. rootfs-image).
	vector<string> payload_types;
	// The following two properties implements ArtifactProvides header-info
	// field of artifact version >= 3. The Attributes are moved to the root
	// of the Artifact structure for backwards compatibility.
	string artifact_name;
	string artifact_group;
	// Holds optional provides fields in the type-info header
	unordered_map<string, string> type_info_provides;
	// Holds options clears_artifact_provides fields from the type-info header.
	// Added in Mender client 2.5.
	vector<string> clears_artifact_provides;
};

string SupportsRollbackToDbString(bool support);
expected::ExpectedBool DbStringToSupportsRollback(const string &str);

string NeedsRebootToDbString(update_module::RebootAction action);
update_module::ExpectedRebootAction DbStringToNeedsReboot(const string &str);

struct UpdateInfo {
	ArtifactData artifact;
	string id;
	// Whether the currently running payloads asked for reboots. It is
	// indexed the same as PayloadTypes above.
	vector<string> reboot_requested;
	// Whether the currently running update supports rollback. All payloads
	// must either support rollback or not, so this is one global flag for
	// all of them.
	string supports_rollback;
	// How many times this update's state has been stored. This is roughly,
	// but not exactly, equivalent to the number of state transitions, and
	// is used to break out of loops.
	int state_data_store_count {0};
	// Whether the current update includes a DB schema update (this
	// structure, and the StateData structure). This is set if we load state
	// data and discover that it is a different version. See also the
	// state_data_key_uncommitted key.
	bool has_db_schema_update {false};
};

struct StateData {
	// version is providing information about the format of the data
	int version {-1};
	// number representing the id of the last state to execute
	string state;
	// update info and response data for the update that was in progress
	UpdateInfo update_info;
};
using ExpectedStateData = expected::expected<StateData, error::Error>;

ExpectedStateData ApiResponseJsonToStateData(const json::Json &json);

class Context {
public:
	Context(mender::update::context::MenderContext &mender_context, events::EventLoop &event_loop);

	mender::update::context::MenderContext &mender_context;
	events::EventLoop &event_loop;

	// For polling, and for making status updates.
	http::Client deployment_client;
	// For the artifact download.
	http::Client download_client;

	struct {
		unique_ptr<StateData> state_data;
		io::ReaderPtr artifact_reader;
		unique_ptr<artifact::Artifact> artifact_parser;
		unique_ptr<artifact::Payload> artifact_payload;
		unique_ptr<update_module::UpdateModule> update_module;
	} deployment;
};

} // namespace daemon
} // namespace update
} // namespace mender

#endif // MENDER_UPDATE_DAEMON_CONTEXT_HPP
