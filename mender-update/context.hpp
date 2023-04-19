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

#include <common/conf.hpp>
#include <common/error.hpp>
#include <common/expected.hpp>
#include <common/key_value_database.hpp>

#if MENDER_USE_LMDB
#include <common/key_value_database_lmdb.hpp>
#else
#error MenderContext requires LMDB
#endif // MENDER_USE_LMDB

namespace mender {
namespace update {
namespace context {

namespace conf = mender::common::conf;
namespace error = mender::common::error;
namespace expected = mender::common::expected;
namespace kv_db = mender::common::key_value_database;

using namespace std;

enum MenderContextErrorCode {
	NoError = 0,
	ParseError,
	ValueError,
};

class MenderContextErrorCategoryClass : public std::error_category {
public:
	const char *name() const noexcept override;
	string message(int code) const override;
};
extern const MenderContextErrorCategoryClass MenderContextErrorCategory;

error::Error MakeError(MenderContextErrorCode code, const string &msg);

using ProvidesData = unordered_map<string, string>;
using ExpectedProvidesData = expected::expected<ProvidesData, error::Error>;

class MenderContext {
public:
	MenderContext(conf::MenderConfig &config) :
		config_(config) {};

	error::Error Initialize();
	kv_db::KeyValueDatabase &GetMenderStoreDB();
	ExpectedProvidesData LoadProvides();
	expected::ExpectedString GetDeviceType();
	error::Error CommitArtifactData(const ProvidesData &data);

	// Name of artifact currently installed. Introduced in Mender 2.0.0.
	const string artifact_name_key {"artifact-name"};

	// Name of the group the currently installed artifact belongs to. For
	// artifact version >= 3, this is held in the header-info artifact-
	// provides field
	const string artifact_group_key {"artifact-group"};

	// Holds the current artifact provides from the type-info header of
	// artifact version >= 3.
	// NOTE: These provides are held in a separate key due to the header-
	// info provides overlap with previous versions of mender artifact.
	const string artifact_provides_key {"artifact-provides"};

	// The key used by the standalone installer to track artifacts that have
	// been started, but not committed. We don't want to use the
	// StateDataKey for this, because it contains a lot less information.
	const string standalone_state_key {"standalone-state"};

	// Name of key that state data is stored under across reboots. Uses the
	// StateData structure, marshalled to JSON.
	const string state_data_key {"state"};

	// Added together with update modules in v2.0.0. This key is invoked if,
	// and only if, a client loads data using the StateDataKey, and
	// discovers that it is a different version than what it currently
	// supports. In that case it switches to using the
	// StateDataKeyUncommitted until the commit stage, where it switches
	// back to StateDataKey. This is intended to ensure that upgrading the
	// client to a new database schema doesn't overwrite the existing
	// schema, in case it is rolled back and the old client needs the
	// original schema again.
	const string state_data_key_uncommitted {"state-uncommitted"};

	// Added in Mender v2.7.0. Updated every time a control map is updated
	// in memory.
	const string update_control_maps {"update-control-maps"};

	// ---------------------- NOT IN USE ANYMORE --------------------------
	// Key used to store the auth token.
	const string auth_token_name {"authtoken"};
	const string auth_token_cache_invalidator_name {"auth-token-cache-invalidator"};

private:
#if MENDER_USE_LMDB
	kv_db::KeyValueDatabaseLmdb mender_store_;
#endif // MENDER_USE_LMDB
	conf::MenderConfig &config_;
};

} // namespace context
} // namespace update
} // namespace mender

#endif // MENDER_COMMON_CONTEXT_HPP
