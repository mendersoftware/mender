// Copyright 2024 Northern.tech AS
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

#ifndef MENDER_UPDATE_STANDALONE_CONTEXT_HPP
#define MENDER_UPDATE_STANDALONE_CONTEXT_HPP

#include <unordered_map>

#include <common/error.hpp>
#include <common/events.hpp>
#include <common/expected.hpp>
#include <common/http.hpp>
#include <common/io.hpp>
#include <common/optional.hpp>

#include <artifact/v3/scripts/executor.hpp>

#include <mender-update/update_module/v3/update_module.hpp>

namespace mender {
namespace update {
namespace standalone {

using namespace std;

namespace error = mender::common::error;
namespace events = mender::common::events;
namespace expected = mender::common::expected;
namespace http = mender::common::http;
namespace io = mender::common::io;

namespace executor = mender::artifact::scripts::executor;

namespace context = mender::update::context;
namespace update_module = mender::update::update_module::v3;

// The keys and data, respectively, of the JSON object living under the `standalone_data_key` entry
// in the database. Be sure to take into account upgrades when changing this.
struct StateDataKeys {
	static const string version;
	static const string artifact_name;
	static const string artifact_group;
	static const string artifact_provides;
	static const string artifact_clears_provides;
	static const string payload_types;

	// Introduced in version 2, not valid in version 1.

	static const string in_state;

	static const string failed;
	static const string rolled_back;
};

struct StateData {
	int version;
	string artifact_name;
	string artifact_group;
	optional<unordered_map<string, string>> artifact_provides;
	optional<vector<string>> artifact_clears_provides;
	vector<string> payload_types;

	string in_state;

	bool failed {false};
	bool rolled_back {false};

	static const string kInStateArtifactInstall_Enter;
	static const string kInStateArtifactCommit_Enter;
	static const string kInStatePostArtifactCommit;
	static const string kInStateArtifactCommit_Leave;
	static const string kInStateArtifactRollback_Enter;
	static const string kInStateArtifactFailure_Enter;
	static const string kInStateCleanup;
};
using ExpectedOptionalStateData = expected::expected<optional<StateData>, error::Error>;

enum class Result {
	NoResult = 0x0,

	// Flags
	NothingDone = 0x0,
	NoUpdateInProgress = 0x1,
	Downloaded = 0x2,
	DownloadFailed = 0x4,
	Installed = 0x8,
	InstallFailed = 0x10,
	RebootRequired = 0x20,
	Committed = 0x40,
	CommitFailed = 0x80,
	Failed = 0x100,
	FailedInPostCommit = 0x200,
	NoRollback = 0x400,
	RolledBack = 0x800,
	NoRollbackNecessary = 0x1000,
	RollbackFailed = 0x2000,
	Cleaned = 0x4000,
	CleanupFailed = 0x8000,
};

// enum classes cannot ordinarily be used as bit flags, but let's provide some convenience functions
// so that we can use it as such while still getting some of the type safety. What we don't get,
// obviously, is that a variable is not guaranteed to be any of the above values.
inline Result operator|(Result a, Result b) {
	return static_cast<Result>(static_cast<int>(a) | static_cast<int>(b));
}
inline Result operator~(Result a) {
	return static_cast<Result>(~static_cast<int>(a));
}

inline bool ResultContains(Result result, Result flags) {
	return (static_cast<int>(result) & static_cast<int>(flags)) == static_cast<int>(flags);
}

inline bool ResultNoneOf(Result result, Result flags) {
	return (static_cast<int>(result) & static_cast<int>(flags)) == 0;
}

struct ResultAndError {
	Result result;
	error::Error err;
};

enum class InstallOptions {
	None,
	NoStdout,
};

struct Context {
	Context(context::MenderContext &main_context, events::EventLoop &loop) :
		main_context {main_context},
		loop {loop} {
		result_and_error.result = Result::NoResult;
	}

	context::MenderContext &main_context;
	events::EventLoop &loop;

	StateData state_data;

	vector<string> stop_before;

	string artifact_src;

	unique_ptr<update_module::UpdateModule> update_module;
	unique_ptr<executor::ScriptRunner> script_runner;

	http::ClientPtr http_client;
	io::ReaderPtr artifact_reader;
	unique_ptr<artifact::Artifact> parser;

	artifact::config::Signature verify_signature;
	InstallOptions options;

	ResultAndError result_and_error;
};

} // namespace standalone
} // namespace update
} // namespace mender

#endif // MENDER_UPDATE_STANDALONE_CONTEXT_HPP
