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

#ifndef MENDER_UPDATE_STANDALONE_HPP
#define MENDER_UPDATE_STANDALONE_HPP

#include <unordered_map>

#include <common/error.hpp>
#include <common/expected.hpp>
#include <common/key_value_database.hpp>
#include <common/json.hpp>
#include <common/optional.hpp>

#include <artifact/artifact.hpp>

#include <mender-update/context.hpp>

#include <mender-update/update_module/v3/update_module.hpp>

namespace mender {
namespace update {
namespace standalone {

using namespace std;

namespace database = mender::common::key_value_database;
namespace error = mender::common::error;
namespace expected = mender::common::expected;
namespace json = mender::common::json;
namespace optional = mender::common::optional;

namespace artifact = mender::artifact;

namespace context = mender::update::context;

namespace update_module = mender::update::update_module::v3;

// The keys and data, respectively, of the JSON object living under the `standalone_data_key` entry
// in the database. Be sure to take into account upgrades when changing this.
struct DataKeys {
	static const string version;
	static const string artifact_name;
	static const string artifact_group;
	static const string artifact_provides;
	static const string artifact_clears_provides;
	static const string payload_types;
};
struct Data {
	int version;
	string artifact_name;
	string artifact_group;
	optional::optional<unordered_map<string, string>> artifact_provides;
	optional::optional<vector<string>> artifact_clears_provides;
	vector<string> payload_types;
};
using ExpectedOptionalData = expected::expected<optional::optional<Data>, error::Error>;

enum class Result {
	InstalledAndCommitted,
	Installed,
	InstalledRebootRequired,
	InstalledAndCommittedRebootRequired,
	Committed,
	InstalledButFailedInPostCommit,
	NoUpdateInProgress,
	RolledBack,
	NoRollback,
	RollbackFailed,
	FailedNothingDone,
	FailedAndRolledBack,
	FailedAndNoRollback,
	FailedAndRollbackFailed,
};

struct ResultAndError {
	Result result;
	error::Error err;
};

// Return true if there is standalone data (indicating that an update is in progress), false if not.
// Note: Data is expected to be empty. IOW it will not clear fields that happen to be
// empty in the database.
ExpectedOptionalData LoadData(database::KeyValueDatabase &db);

void DataFromPayloadHeaderView(const artifact::PayloadHeaderView &header, Data &dst);
error::Error SaveData(database::KeyValueDatabase &db, const Data &data);

error::Error RemoveData(database::KeyValueDatabase &db);

ResultAndError Install(context::MenderContext &main_context, const string &src);
ResultAndError Commit(context::MenderContext &main_context);
ResultAndError Rollback(context::MenderContext &main_context);

ResultAndError DoInstallStates(
	context::MenderContext &main_context,
	Data &data,
	artifact::Artifact &artifact,
	update_module::UpdateModule &update_module);
ResultAndError DoCommit(
	context::MenderContext &main_context, Data &data, update_module::UpdateModule &update_module);
ResultAndError DoRollback(
	context::MenderContext &main_context, Data &data, update_module::UpdateModule &update_module);

ResultAndError DoEmptyPayloadArtifact(context::MenderContext &main_context, Data &data);

ResultAndError InstallationFailureHandler(
	context::MenderContext &main_context, Data &data, update_module::UpdateModule &update_module);

error::Error CommitBrokenArtifact(context::MenderContext &main_context, Data &data);

} // namespace standalone
} // namespace update
} // namespace mender

#endif // MENDER_UPDATE_STANDALONE_HPP
