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

#ifndef MENDER_COMMON_CONTEXT_HPP
#define MENDER_COMMON_CONTEXT_HPP

#include <string>
#include <unordered_map>

#include <artifact/artifact.hpp>
#include <client_shared/conf.hpp>
#include <common/error.hpp>
#include <common/expected.hpp>
#include <common/key_value_database.hpp>
#include <common/optional.hpp>

#ifdef MENDER_USE_LMDB
#include <common/key_value_database_lmdb.hpp>
#else
#error MenderContext requires LMDB
#endif // MENDER_USE_LMDB

namespace mender {
namespace update {
namespace context {

namespace artifact = mender::artifact;
namespace conf = mender::client_shared::conf;
namespace error = mender::common::error;
namespace expected = mender::common::expected;
namespace kv_db = mender::common::key_value_database;

using namespace std;

enum MenderContextErrorCode {
	NoError = 0,
	ParseError,
	ValueError,
	NoSuchUpdateModuleError,
	DatabaseValueError,
	RebootRequiredError,
	NoUpdateInProgressError,
	UnexpectedHttpResponse,
	StateDataStoreCountExceededError,
};

class MenderContextErrorCategoryClass : public std::error_category {
public:
	const char *name() const noexcept override;
	string message(int code) const override;
};
extern const MenderContextErrorCategoryClass MenderContextErrorCategory;

error::Error MakeError(MenderContextErrorCode code, const string &msg);

using ProvidesData = unordered_map<string, string>;
using ClearsProvidesData = vector<string>;
using ExpectedProvidesData = expected::expected<ProvidesData, error::Error>;

class MenderContext {
public:
	MenderContext(conf::MenderConfig &config) :
		config_ {config} {};
	virtual ~MenderContext() {
	}

	error::Error Initialize();
	virtual kv_db::KeyValueDatabase &GetMenderStoreDB();
	ExpectedProvidesData LoadProvides();
	ExpectedProvidesData LoadProvides(kv_db::Transaction &txn);
	expected::ExpectedString GetDeviceType();
	// Stores new artifact data, taking existing provides, and clears_provides, into account.
	error::Error CommitArtifactData(
		string artifact_name,
		string artifact_group,
		const optional<ProvidesData> &new_provides,
		const optional<ClearsProvidesData> &clears_provides,
		function<error::Error(kv_db::Transaction &)> txn_func);
	conf::MenderConfig &GetConfig() {
		return config_;
	}

	expected::ExpectedBool MatchesArtifactDepends(const artifact::HeaderView &hdr_view);

	// Suffix used for updates that either can't roll back or fail their rollback.
	static const string broken_artifact_name_suffix;

	// DATABASE KEYS ------------------------------------------------------

	// Name of artifact currently installed. Introduced in Mender 2.0.0.
	static const string artifact_name_key;

	// Name of the group the currently installed artifact belongs to. For
	// artifact version >= 3, this is held in the header-info artifact-
	// provides field
	static const string artifact_group_key;

	// Holds the current artifact provides from the type-info header of
	// artifact version >= 3.
	// NOTE: These provides are held in a separate key due to the header-
	// info provides overlap with previous versions of mender artifact.
	static const string artifact_provides_key;

	// The key used by the standalone installer to track artifacts that have
	// been started, but not committed. We don't want to use the
	// StateDataKey for this, because it contains a lot less information.
	static const string standalone_state_key;

	// Name of key that state data is stored under across reboots. Uses the
	// StateData structure, marshalled to JSON.
	static const string state_data_key;

	// Added together with update modules in v2.0.0. This key is invoked if,
	// and only if, a client loads data using the StateDataKey, and
	// discovers that it is a different version than what it currently
	// supports. In that case it switches to using the
	// StateDataKeyUncommitted until the commit stage, where it switches
	// back to StateDataKey. This is intended to ensure that upgrading the
	// client to a new database schema doesn't overwrite the existing
	// schema, in case it is rolled back and the old client needs the
	// original schema again.
	static const string state_data_key_uncommitted;

	// ---------------------- NOT IN USE ANYMORE --------------------------
	// Key used to store the auth token.
	static const string auth_token_name;
	static const string auth_token_cache_invalidator_name;

	// Added in Mender v2.7.0. Updated every time a control map is updated
	// in memory. Removed in v4.0.0.
	static const string update_control_maps;

	// END OF DATABASE KEYS -----------------------------------------------

	static const int standalone_data_version;

private:
#ifdef MENDER_USE_LMDB
	kv_db::KeyValueDatabaseLmdb mender_store_;
#endif // MENDER_USE_LMDB
	conf::MenderConfig &config_;
};

// Only here to make testing easier, use MenderContext::MatchesArtifactDepends().
expected::ExpectedBool ArtifactMatchesContext(
	const ProvidesData &provides, const string &device_type, const artifact::HeaderView &hdr_view);

error::Error FilterProvides(
	const ProvidesData &new_provides,
	const ClearsProvidesData &clears_provides,
	ProvidesData &to_modify);


bool CheckClearsMatch(const string &to_match, const string &clears_string);

} // namespace context
} // namespace update
} // namespace mender

#endif // MENDER_COMMON_CONTEXT_HPP
