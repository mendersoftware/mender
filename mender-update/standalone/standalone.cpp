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

#include <mender-update/standalone.hpp>

#include <common/common.hpp>
#include <common/events_io.hpp>
#include <common/http.hpp>
#include <common/log.hpp>
#include <common/conf/paths.hpp>

namespace mender {
namespace update {
namespace standalone {

namespace common = mender::common;
namespace events = mender::common::events;
namespace http = mender::http;
namespace io = mender::common::io;
namespace log = mender::common::log;
namespace paths = mender::common::conf::paths;

const string StateDataKeys::version {"Version"};
const string StateDataKeys::artifact_name {"ArtifactName"};
const string StateDataKeys::artifact_group {"ArtifactGroup"};
const string StateDataKeys::artifact_provides {"ArtifactTypeInfoProvides"};
const string StateDataKeys::artifact_clears_provides {"ArtifactClearsProvides"};
const string StateDataKeys::payload_types {"PayloadTypes"};

ExpectedOptionalStateData LoadStateData(database::KeyValueDatabase &db) {
	StateDataKeys keys;
	StateData dst;

	auto exp_bytes = db.Read(context::MenderContext::standalone_state_key);
	if (!exp_bytes) {
		auto &err = exp_bytes.error();
		if (err.code == database::MakeError(database::KeyError, "").code) {
			return optional::optional<StateData>();
		} else {
			return expected::unexpected(err);
		}
	}

	auto exp_json = json::Load(common::StringFromByteVector(exp_bytes.value()));
	if (!exp_json) {
		return expected::unexpected(exp_json.error());
	}
	auto &json = exp_json.value();

	auto exp_int = json::Get<int64_t>(json, keys.version, json::MissingOk::No);
	if (!exp_int) {
		return expected::unexpected(exp_int.error());
	}
	dst.version = exp_int.value();

	auto exp_string = json::Get<string>(json, keys.artifact_name, json::MissingOk::No);
	if (!exp_string) {
		return expected::unexpected(exp_string.error());
	}
	dst.artifact_name = exp_string.value();

	exp_string = json::Get<string>(json, keys.artifact_group, json::MissingOk::Yes);
	if (!exp_string) {
		return expected::unexpected(exp_string.error());
	}
	dst.artifact_group = exp_string.value();

	auto exp_map = json::Get<json::KeyValueMap>(json, keys.artifact_provides, json::MissingOk::No);
	if (exp_map) {
		dst.artifact_provides = exp_map.value();
	} else {
		dst.artifact_provides.reset();
	}

	auto exp_array =
		json::Get<vector<string>>(json, keys.artifact_clears_provides, json::MissingOk::No);
	if (exp_array) {
		dst.artifact_clears_provides = exp_array.value();
	} else {
		dst.artifact_clears_provides.reset();
	}

	exp_array = json::Get<vector<string>>(json, keys.payload_types, json::MissingOk::No);
	if (!exp_array) {
		return expected::unexpected(exp_array.error());
	}
	dst.payload_types = exp_array.value();

	if (dst.version != context::MenderContext::standalone_data_version) {
		return expected::unexpected(error::Error(
			make_error_condition(errc::not_supported),
			"State data has a version which is not supported by this client"));
	}

	if (dst.artifact_name == "") {
		return expected::unexpected(context::MakeError(
			context::DatabaseValueError, "`" + keys.artifact_name + "` is empty"));
	}

	if (dst.payload_types.size() == 0) {
		return expected::unexpected(context::MakeError(
			context::DatabaseValueError, "`" + keys.payload_types + "` is empty"));
	}
	if (dst.payload_types.size() >= 2) {
		return expected::unexpected(error::Error(
			make_error_condition(errc::not_supported),
			"`" + keys.payload_types + "` contains multiple payloads"));
	}

	return dst;
}

StateData StateDataFromPayloadHeaderView(const artifact::PayloadHeaderView &header) {
	StateData dst;
	dst.version = context::MenderContext::standalone_data_version;
	dst.artifact_name = header.header.artifact_name;
	dst.artifact_group = header.header.artifact_group;
	dst.artifact_provides = header.header.type_info.artifact_provides;
	dst.artifact_clears_provides = header.header.type_info.clears_artifact_provides;
	dst.payload_types.clear();
	dst.payload_types.push_back(header.header.payload_type);
	return dst;
}

error::Error SaveStateData(database::KeyValueDatabase &db, const StateData &data) {
	StateDataKeys keys;
	stringstream ss;
	ss << "{";
	ss << "\"" << keys.version << "\":" << data.version;

	ss << ",";
	ss << "\"" << keys.artifact_name << "\":\"" << data.artifact_name << "\"";

	ss << ",";
	ss << "\"" << keys.artifact_group << "\":\"" << data.artifact_group << "\"";

	ss << ",";
	ss << "\"" << keys.payload_types << "\": [";
	bool first = true;
	for (auto elem : data.payload_types) {
		if (!first) {
			ss << ",";
		}
		ss << "\"" << elem << "\"";
		first = false;
	}
	ss << "]";

	if (data.artifact_provides) {
		ss << ",";
		ss << "\"" << keys.artifact_provides << "\": {";
		bool first = true;
		for (auto elem : data.artifact_provides.value()) {
			if (!first) {
				ss << ",";
			}
			ss << "\"" << elem.first << "\":\"" << elem.second << "\"";
			first = false;
		}
		ss << "}";
	}

	if (data.artifact_clears_provides) {
		ss << ",";
		ss << "\"" << keys.artifact_clears_provides << "\": [";
		bool first = true;
		for (auto elem : data.artifact_clears_provides.value()) {
			if (!first) {
				ss << ",";
			}
			ss << "\"" << elem << "\"";
			first = false;
		}
		ss << "]";
	}

	ss << "}";

	string strdata = ss.str();
	vector<uint8_t> bytedata(common::ByteVectorFromString(strdata));

	return db.Write(context::MenderContext::standalone_state_key, bytedata);
}

error::Error RemoveStateData(database::KeyValueDatabase &db) {
	return db.Remove(context::MenderContext::standalone_state_key);
}

static expected::expected<pair<http::ClientPtr, io::ReaderPtr>, error::Error>
ClientAndReaderFromUrl(const string &src) {
	http::ClientPtr http_client;
	auto maybe_artifact_reader = events::io::ReaderFromAsyncReader::Construct(
		[&http_client, &src](events::EventLoop &loop) -> io::ExpectedAsyncReaderPtr {
			http::ClientConfig conf;
			http_client = make_shared<http::Client>(conf, loop);
			auto req = make_shared<http::OutgoingRequest>();
			req->SetMethod(http::Method::GET);
			auto err = req->SetAddress(src);
			if (err != error::NoError) {
				return expected::unexpected(err);
			}
			error::Error inner_err;
			io::AsyncReaderPtr reader;
			err = http_client->AsyncCall(
				req,
				[&loop, &inner_err, &reader](http::ExpectedIncomingResponsePtr exp_resp) {
					if (!exp_resp) {
						inner_err = exp_resp.error();
						return;
					}

					auto resp = exp_resp.value();

					if (resp->GetStatusCode() != http::StatusOK) {
						inner_err = context::MakeError(
							context::UnexpectedHttpResponse,
							to_string(resp->GetStatusCode()) + ": " + resp->GetStatusMessage());
						return;
					}

					reader = resp->MakeBodyAsyncReader();

					loop.Stop();
				},
				[](http::ExpectedIncomingResponsePtr exp_resp) {
					if (!exp_resp) {
						log::Warning("While reading HTTP body: " + exp_resp.error().String());
					}
				});

			// Loop until the headers are received. Then we return and let the reader drive the
			// rest of the download.
			loop.Run();

			if (inner_err != error::NoError) {
				return expected::unexpected(inner_err);
			}

			return reader;
		});

	if (!maybe_artifact_reader) {
		return expected::unexpected(maybe_artifact_reader.error());
	}

	return pair<http::ClientPtr, io::ReaderPtr> {http_client, maybe_artifact_reader.value()};
}

ResultAndError Install(context::MenderContext &main_context, const string &src) {
	auto exp_in_progress = LoadStateData(main_context.GetMenderStoreDB());
	if (!exp_in_progress) {
		return {Result::FailedNothingDone, exp_in_progress.error()};
	}
	auto &in_progress = exp_in_progress.value();

	if (in_progress) {
		return {
			Result::FailedNothingDone,
			error::Error(
				make_error_condition(errc::operation_in_progress),
				"Update already in progress. Please commit or roll back first")};
	}

	io::ReaderPtr artifact_reader;

	http::ClientPtr http_client;

	if (src.find("http://") == 0 || src.find("https://") == 0) {
		auto client_and_reader = ClientAndReaderFromUrl(src);
		if (!client_and_reader) {
			return {Result::FailedNothingDone, client_and_reader.error()};
		}
		http_client = client_and_reader.value().first;
		artifact_reader = client_and_reader.value().second;
	} else {
		auto stream = io::OpenIfstream(src);
		if (!stream) {
			return {Result::FailedNothingDone, stream.error()};
		}
		auto file_stream = make_shared<ifstream>(std::move(stream.value()));
		artifact_reader = make_shared<io::StreamReader>(file_stream);
	}

	artifact::config::ParserConfig config {
		paths::DefaultArtScriptsPath,
	};
	auto exp_parser = artifact::Parse(*artifact_reader, config);
	if (!exp_parser) {
		return {Result::FailedNothingDone, exp_parser.error()};
	}
	auto &parser = exp_parser.value();

	auto exp_header = artifact::View(parser, 0);
	if (!exp_header) {
		return {Result::FailedNothingDone, exp_header.error()};
	}
	auto &header = exp_header.value();

	cout << "Installing artifact..." << endl;

	if (header.header.payload_type == "") {
		auto data = StateDataFromPayloadHeaderView(header);
		return DoEmptyPayloadArtifact(main_context, data);
	}

	update_module::UpdateModule update_module(main_context, header.header.payload_type);

	auto err =
		update_module.CleanAndPrepareFileTree(update_module.GetUpdateModuleWorkDir(), header);
	if (err != error::NoError) {
		err = err.FollowedBy(update_module.Cleanup());
		return {Result::FailedNothingDone, err};
	}

	StateData data = StateDataFromPayloadHeaderView(header);
	err = SaveStateData(main_context.GetMenderStoreDB(), data);
	if (err != error::NoError) {
		err = err.FollowedBy(update_module.Cleanup());
		return {Result::FailedNothingDone, err};
	}

	return DoInstallStates(main_context, data, parser, update_module);
}

ResultAndError Commit(context::MenderContext &main_context) {
	auto exp_in_progress = LoadStateData(main_context.GetMenderStoreDB());
	if (!exp_in_progress) {
		return {Result::FailedNothingDone, exp_in_progress.error()};
	}
	auto &in_progress = exp_in_progress.value();

	if (!in_progress) {
		return {
			Result::NoUpdateInProgress,
			context::MakeError(context::NoUpdateInProgressError, "Cannot commit")};
	}
	auto &data = in_progress.value();

	update_module::UpdateModule update_module(main_context, data.payload_types[0]);

	if (data.payload_types[0] == "rootfs-image") {
		// Special case for rootfs-image upgrades. See comments inside the function.
		auto err = update_module.EnsureRootfsImageFileTree(update_module.GetUpdateModuleWorkDir());
		if (err != error::NoError) {
			return {Result::FailedNothingDone, err};
		}
	}

	return DoCommit(main_context, data, update_module);
}

ResultAndError Rollback(context::MenderContext &main_context) {
	auto exp_in_progress = LoadStateData(main_context.GetMenderStoreDB());
	if (!exp_in_progress) {
		return {Result::FailedNothingDone, exp_in_progress.error()};
	}
	auto &in_progress = exp_in_progress.value();

	if (!in_progress) {
		return {
			Result::NoUpdateInProgress,
			context::MakeError(context::NoUpdateInProgressError, "Cannot roll back")};
	}
	auto &data = in_progress.value();

	update_module::UpdateModule update_module(main_context, data.payload_types[0]);

	if (data.payload_types[0] == "rootfs-image") {
		// Special case for rootfs-image upgrades. See comments inside the function.
		auto err = update_module.EnsureRootfsImageFileTree(update_module.GetUpdateModuleWorkDir());
		if (err != error::NoError) {
			return {Result::FailedNothingDone, err};
		}
	}

	auto result = DoRollback(main_context, data, update_module);

	if (result.result == Result::NoRollback) {
		// No support for rollback. Return instead of clearing update data. It should be
		// cleared by calling commit or restoring the rollback capability.
		return result;
	}

	auto err = update_module.Cleanup();
	if (err != error::NoError) {
		result.result = Result::FailedAndRollbackFailed;
		result.err = result.err.FollowedBy(err);
	}

	if (result.result == Result::RolledBack) {
		err = RemoveStateData(main_context.GetMenderStoreDB());
	} else {
		err = CommitBrokenArtifact(main_context, data);
	}
	if (err != error::NoError) {
		result.result = Result::RollbackFailed;
		result.err = result.err.FollowedBy(err);
	}

	return result;
}

ResultAndError DoInstallStates(
	context::MenderContext &main_context,
	StateData &data,
	artifact::Artifact &artifact,
	update_module::UpdateModule &update_module) {
	auto payload = artifact.Next();
	if (!payload) {
		return {Result::FailedNothingDone, payload.error()};
	}

	auto err = update_module.Download(payload.value());
	if (err != error::NoError) {
		err = err.FollowedBy(update_module.Cleanup());
		err = err.FollowedBy(RemoveStateData(main_context.GetMenderStoreDB()));
		return {Result::FailedNothingDone, err};
	}

	err = update_module.ArtifactInstall();
	if (err != error::NoError) {
		log::Error("Installation failed: " + err.String());
		return InstallationFailureHandler(main_context, data, update_module);
	}

	auto reboot = update_module.NeedsReboot();
	if (!reboot) {
		log::Error("Could not query for reboot: " + reboot.error().String());
		return InstallationFailureHandler(main_context, data, update_module);
	}

	auto rollback_support = update_module.SupportsRollback();
	if (!rollback_support) {
		log::Error("Could not query for rollback support: " + rollback_support.error().String());
		return InstallationFailureHandler(main_context, data, update_module);
	}

	if (rollback_support.value()) {
		if (reboot.value() != update_module::RebootAction::No) {
			return {Result::InstalledRebootRequired, error::NoError};
		} else {
			return {Result::Installed, error::NoError};
		}
	}

	cout << "Update Module doesn't support rollback. Committing immediately." << endl;

	auto result = DoCommit(main_context, data, update_module);
	if (result.result == Result::Committed) {
		if (reboot.value() != update_module::RebootAction::No) {
			result.result = Result::InstalledAndCommittedRebootRequired;
		} else {
			result.result = Result::InstalledAndCommitted;
		}
	}
	return result;
}

ResultAndError DoCommit(
	context::MenderContext &main_context,
	StateData &data,
	update_module::UpdateModule &update_module) {
	auto err = update_module.ArtifactCommit();
	if (err != error::NoError) {
		log::Error("Commit failed: " + err.String());
		return InstallationFailureHandler(main_context, data, update_module);
	}

	auto result = Result::Committed;
	error::Error return_err;

	err = update_module.Cleanup();
	if (err != error::NoError) {
		result = Result::InstalledButFailedInPostCommit;
		return_err = return_err.FollowedBy(err);
	}

	err = main_context.CommitArtifactData(
		data.artifact_name,
		data.artifact_group,
		data.artifact_provides,
		data.artifact_clears_provides,
		[](database::Transaction &txn) {
			return txn.Remove(context::MenderContext::standalone_state_key);
		});
	if (err != error::NoError) {
		result = Result::InstalledButFailedInPostCommit;
		return_err = return_err.FollowedBy(err);
	}

	return {result, return_err};
}

ResultAndError DoRollback(
	context::MenderContext &main_context,
	StateData &data,
	update_module::UpdateModule &update_module) {
	auto exp_rollback_support = update_module.SupportsRollback();
	if (!exp_rollback_support) {
		return {Result::NoRollback, exp_rollback_support.error()};
	}

	if (exp_rollback_support.value()) {
		auto err = update_module.ArtifactRollback();
		if (err != error::NoError) {
			return {Result::RollbackFailed, err};
		}

		return {Result::RolledBack, error::NoError};
	} else {
		return {Result::NoRollback, error::NoError};
	}
}

ResultAndError DoEmptyPayloadArtifact(context::MenderContext &main_context, StateData &data) {
	cout << "Artifact with empty payload. Committing immediately." << endl;

	auto err = main_context.CommitArtifactData(
		data.artifact_name,
		data.artifact_group,
		data.artifact_provides,
		data.artifact_clears_provides,
		[](database::Transaction &txn) { return error::NoError; });
	if (err != error::NoError) {
		return {Result::InstalledButFailedInPostCommit, err};
	}
	return {Result::InstalledAndCommitted, err};
}

ResultAndError InstallationFailureHandler(
	context::MenderContext &main_context,
	StateData &data,
	update_module::UpdateModule &update_module) {
	error::Error err;

	auto result = DoRollback(main_context, data, update_module);
	switch (result.result) {
	case Result::RolledBack:
		result.result = Result::FailedAndRolledBack;
		break;
	case Result::NoRollback:
		result.result = Result::FailedAndNoRollback;
		break;
	case Result::RollbackFailed:
		result.result = Result::FailedAndRollbackFailed;
		break;
	default:
		// Should not happen.
		assert(false);
		return {
			Result::FailedAndRollbackFailed,
			error::MakeError(
				error::ProgrammingError,
				"Unexpected result in InstallationFailureHandler. This is a bug.")};
	}

	err = update_module.ArtifactFailure();
	if (err != error::NoError) {
		result.result = Result::FailedAndRollbackFailed;
		result.err = result.err.FollowedBy(err);
	}

	err = update_module.Cleanup();
	if (err != error::NoError) {
		result.result = Result::FailedAndRollbackFailed;
		result.err = result.err.FollowedBy(err);
	}

	if (result.result == Result::FailedAndRolledBack) {
		err = RemoveStateData(main_context.GetMenderStoreDB());
	} else {
		err = CommitBrokenArtifact(main_context, data);
	}
	if (err != error::NoError) {
		result.result = Result::FailedAndRollbackFailed;
		result.err = result.err.FollowedBy(err);
	}

	return result;
}

error::Error CommitBrokenArtifact(context::MenderContext &main_context, StateData &data) {
	data.artifact_name += main_context.broken_artifact_name_suffix;
	if (data.artifact_provides) {
		data.artifact_provides.value()["artifact_name"] = data.artifact_name;
	}
	return main_context.CommitArtifactData(
		data.artifact_name,
		data.artifact_group,
		data.artifact_provides,
		data.artifact_clears_provides,
		[](database::Transaction &txn) {
			return txn.Remove(context::MenderContext::standalone_state_key);
		});
}

} // namespace standalone
} // namespace update
} // namespace mender
