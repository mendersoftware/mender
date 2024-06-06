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
#include <common/key_value_database.hpp>

#include <artifact/artifact.hpp>

#include <api/auth.hpp>
#include <api/client.hpp>

#include <mender-update/context.hpp>
#include <mender-update/deployments.hpp>
#include <mender-update/inventory.hpp>
#include <mender-update/update_module/v3/update_module.hpp>

#ifdef MENDER_EMBED_MENDER_AUTH
#include <mender-auth/api/auth.hpp>
#endif

namespace mender {
namespace update {
namespace daemon {

using namespace std;

namespace error = mender::common::error;
namespace events = mender::common::events;
namespace expected = mender::common::expected;
namespace http = mender::common::http;
namespace io = mender::common::io;
namespace json = mender::common::json;
namespace kv_db = mender::common::key_value_database;

namespace artifact = mender::artifact;

namespace api = mender::api;
namespace auth = mender::api::auth;

namespace deployments = mender::update::deployments;
namespace inventory = mender::update::inventory;

namespace update_module = mender::update::update_module::v3;

// current version of the format of StateData;
// increase the version number once the format of StateData is changed
// StateDataVersion = 2 was introduced in Mender 2.0.0.
extern const int kStateDataVersion;

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
	// indexed the same as `payload_types` above.
	vector<string> reboot_requested;
	// Whether the currently running update supports rollback. All payloads
	// must either support rollback or not, so this is one global flag for
	// all of them.
	string supports_rollback;
	// How many times this update's state has been stored. This is roughly,
	// but not exactly, equivalent to the number of state transitions, and
	// is used to break out of loops.
	int64_t state_data_store_count {0};
	// Whether the current update includes a DB schema update (this
	// structure, and the StateData structure). This is set if we load state
	// data and discover that it is a different version. See also the
	// state_data_key_uncommitted key.
	bool has_db_schema_update {false};

	// Added in Mender v4.0.0. Set to true when *all* payloads have successfully rolled
	// back. This is used to remember the rollback status if one of the states after the
	// rollback experiences a spontaneous reboot and must be restarted. Note that the schema was
	// not bumped when this was added, since it does not affect the actual update, it can only
	// affect which deployment status you get at the end of the update, as well as the
	// "INCONSISTENT" label on artifact_name.
	bool all_rollbacks_successful {false};
};

struct StateData {
	// version is providing information about the format of the data
	int version {kStateDataVersion};
	// number representing the id of the last state to execute
	string state;
	// update info and response data for the update that was in progress
	UpdateInfo update_info;

	void FillUpdateDataFromArtifact(artifact::PayloadHeaderView &view);
};
using ExpectedStateData = expected::expected<StateData, error::Error>;

ExpectedStateData ApiResponseJsonToStateData(const json::Json &json);

class Context {
public:
	Context(mender::update::context::MenderContext &mender_context, events::EventLoop &event_loop);

	// Note: Both storing and loading the state data updates the state_data_store_count,
	// which is the reason for the non-const argument.
	error::Error SaveDeploymentStateData(StateData &state_data);
	error::Error SaveDeploymentStateData(kv_db::Transaction &txn, StateData &state_data);
	// True if there is data, false if there is no data, and error if there was a problem
	// loading the data. Note that if the returned error is StateDataStoreCountExceededError,
	// then the state_data is still filled in and valid.
	expected::ExpectedBool LoadDeploymentStateData(StateData &state_data);

	void BeginDeploymentLogging();
	void FinishDeploymentLogging();

	mender::update::context::MenderContext &mender_context;
	events::EventLoop &event_loop;

#ifdef MENDER_USE_DBUS
	auth::AuthenticatorDBus authenticator;
#elif defined(MENDER_EMBED_MENDER_AUTH)
	mender::auth::api::auth::AuthenticatorHttp authenticator;
#endif

public:
	// For polling, and for making status updates.
	api::HTTPClient http_client;
	// For the artifact download.
	shared_ptr<http::ClientInterface> download_client;

	shared_ptr<deployments::DeploymentAPI> deployment_client;
	shared_ptr<inventory::InventoryAPI> inventory_client;

	bool has_submitted_inventory {false};

	struct {
		unique_ptr<StateData> state_data;
		io::ReaderPtr artifact_reader;
		unique_ptr<artifact::Artifact> artifact_parser;
		unique_ptr<artifact::Payload> artifact_payload;
		unique_ptr<update_module::UpdateModule> update_module;

		bool failed {false};
		// Also true if rollback is unsupported.
		bool rollback_failed {false};

		bool download_with_sizes {false};

		unique_ptr<deployments::DeploymentLog> logger;
	} deployment;

	// Database values for the `StateData::state` member above.
	static const string kUpdateStateDownload;
	static const string kUpdateStateArtifactInstall;
	static const string kUpdateStateArtifactReboot;
	static const string kUpdateStateArtifactVerifyReboot;
	static const string kUpdateStateArtifactCommit;
	static const string kUpdateStateAfterArtifactCommit;
	static const string kUpdateStateArtifactRollback;
	static const string kUpdateStateArtifactRollbackReboot;
	static const string kUpdateStateArtifactVerifyRollbackReboot;
	static const string kUpdateStateArtifactFailure;
	static const string kUpdateStateCleanup;
	static const string kUpdateStateStatusReportRetry;

	static const string kUpdateStateUpdateAfterFirstCommit;
	static const string kUpdateStateVerifyRollbackReboot;
	static const string UpdateStateReportStatusError;

	// Database values for the `supports_rollback` member above.
	static const string kRollbackNotSupported;
	static const string kRollbackSupported;

	// Database values for the `reboot_requested` member above.
	static const string kRebootTypeNone;
	static const string kRebootTypeCustom;
	static const string kRebootTypeAutomatic;
};

} // namespace daemon
} // namespace update
} // namespace mender

#endif // MENDER_UPDATE_DAEMON_CONTEXT_HPP
