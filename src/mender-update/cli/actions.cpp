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

#include <mender-update/cli/actions.hpp>

#include <algorithm>
#include <iostream>
#include <string>

#include <artifact/config.hpp>

#include <common/common.hpp>
#include <common/error.hpp>
#include <common/events.hpp>
#include <common/expected.hpp>
#include <common/key_value_database.hpp>
#include <common/log.hpp>
#include <common/path.hpp>
#include <common/processes.hpp>

#include <mender-update/cli/cli.hpp>
#include <mender-update/daemon.hpp>
#include <mender-update/standalone.hpp>

#ifdef MENDER_EMBED_MENDER_AUTH
#include <mender-auth/cli/actions.hpp>
#endif

namespace mender {
namespace update {
namespace cli {

namespace processes = mender::common::processes;
namespace conf = mender::client_shared::conf;
namespace daemon = mender::update::daemon;
namespace database = mender::common::key_value_database;
namespace error = mender::common::error;
namespace events = mender::common::events;
namespace expected = mender::common::expected;
namespace http = mender::common::http;
namespace kv_db = mender::common::key_value_database;
namespace log = mender::common::log;
namespace path = mender::common::path;
namespace standalone = mender::update::standalone;

static error::Error DoMaybeInstallBootstrapArtifact(context::MenderContext &main_context) {
	const string bootstrap_artifact_path {
		main_context.GetConfig().paths.GetBootstrapArtifactFile()};
	// Check if the DB is populated - then install conditionally
	auto &db = main_context.GetMenderStoreDB();
	auto exp_key = db.Read(main_context.artifact_name_key);
	if (exp_key) {
		// Key exists. Do nothing
		return error::NoError;
	}
	error::Error err = exp_key.error();
	if (err.code != kv_db::MakeError(kv_db::KeyError, "Key Not found").code) {
		return err;
	}

	// Key does not exist, install the bootstrap artifact if it exists
	if (!path::FileExists(bootstrap_artifact_path)) {
		log::Debug("No Bootstrap Artifact found at: " + bootstrap_artifact_path);
		error::Error err =
			db.Write(main_context.artifact_name_key, common::ByteVectorFromString("unknown"));
		if (err != error::NoError) {
			return err;
		}
		return error::NoError;
	}
	log::Info("Installing the bootstrap Artifact");
	events::EventLoop loop;
	standalone::Context ctx {main_context, loop};
	auto result = standalone::Install(
		ctx,
		bootstrap_artifact_path,
		artifact::config::Signature::Skip,
		standalone::InstallOptions::NoStdout);

	if (result.err != error::NoError) {
		error::Error err =
			db.Write(main_context.artifact_name_key, common::ByteVectorFromString("unknown"));
		return result.err.FollowedBy(err).WithContext("Failed to install the bootstrap Artifact");
	}
	return error::NoError;
}

error::Error MaybeInstallBootstrapArtifact(context::MenderContext &main_context) {
	const string bootstrap_artifact_path {
		main_context.GetConfig().paths.GetBootstrapArtifactFile()};
	error::Error err = DoMaybeInstallBootstrapArtifact(main_context);

	// Unconditionally delete the bootstrap Artifact
	if (path::FileExists(bootstrap_artifact_path)) {
		error::Error delete_err = path::FileDelete(bootstrap_artifact_path);
		if (delete_err != error::NoError) {
			return err.FollowedBy(
				delete_err.WithContext("Failed to delete the bootstrap Artifact"));
		}
	}
	return err;
}

error::Error ShowArtifactAction::Execute(context::MenderContext &main_context) {
	error::Error err = MaybeInstallBootstrapArtifact(main_context);
	if (err != error::NoError) {
		return err;
	}

	auto exp_provides = main_context.LoadProvides();
	if (!exp_provides) {
		return exp_provides.error();
	}

	auto &provides = exp_provides.value();
	if (provides.count("artifact_name") == 0 || provides["artifact_name"] == "") {
		cout << "unknown" << endl;
	} else {
		cout << provides["artifact_name"] << endl;
	}
	return error::NoError;
}

error::Error ShowProvidesAction::Execute(context::MenderContext &main_context) {
	error::Error err = MaybeInstallBootstrapArtifact(main_context);
	if (err != error::NoError) {
		return err;
	}

	auto exp_provides = main_context.LoadProvides();
	if (!exp_provides) {
		return exp_provides.error();
	}

	auto &provides = exp_provides.value();
	for (const auto &elem : provides) {
		cout << elem.first << "=" << elem.second << endl;
	}

	return error::NoError;
}

static error::Error ResultHandler(standalone::ResultAndError result) {
	using Result = standalone::Result;

	if (result.err != error::NoError) {
		log::Error(result.err.String());
	} else if (ResultContains(result.result, Result::Failed)) {
		// All error states, make sure they have an error.
		result.err = error::MakeError(error::ExitWithFailureError, "");
	}

	// This tries to capture all things that can happen in a run, but there is a catchall in
	// case we don't.
	if (ResultIs(result.result, Result::Cleaned)) {
		cout << "Cleaned up." << endl;
	} else if (ResultContains(result.result, Result::Failed | Result::NoRollbackNecessary)) {
		cout << "Installation failed. System not modified." << endl;
	} else if (ResultContains(result.result, Result::Failed)) {
		if (ResultContains(result.result, Result::FailedInPostCommit)) {
			cout << "Installed, but one or more post-commit steps failed." << endl;
		} else if (
			ResultContains(result.result, Result::Downloaded | Result::NoRollback)
			or ResultContains(result.result, Result::Installed | Result::NoRollback)) {
			cout
				<< "Installation failed, and Update Module does not support rollback. System may be in an inconsistent state."
				<< endl;
		} else if (ResultContains(result.result, Result::NoRollback)) {
			cout << "Update Module does not support rollback." << endl;
		} else if (
			ResultContains(result.result, Result::Downloaded | Result::RollbackFailed)
			or ResultContains(result.result, Result::Installed | Result::RollbackFailed)) {
			cout
				<< "Installation failed, and rollback also failed. System may be in an inconsistent state."
				<< endl;
		} else if (
			ResultContains(result.result, Result::Downloaded | Result::RolledBack)
			or ResultContains(result.result, Result::Installed | Result::RolledBack)) {
			cout << "Installation failed. Rolled back modifications." << endl;
		} else if (ResultContains(result.result, Result::RollbackFailed)) {
			cout << "Rollback failed. System may be in an inconsistent state." << endl;
		} else if (ResultContains(result.result, Result::CleanupFailed)) {
			cout << "Installed, but one or more post-commit steps failed." << endl;
		} else {
			stringstream ss;
			ss << std::hex << static_cast<int>(result.result);
			log::Error("Unexpected result value: 0x" + ss.str());
		}
	} else if (ResultIs(result.result, Result::RolledBack)) {
		cout << "Rolled back." << endl;
	} else if (ResultIs(result.result, Result::NoUpdateInProgress)) {
		cout << "No update in progress." << endl;
	} else if (ResultIs(result.result, Result::Downloaded)) {
		cout << "Streamed to storage, but not installed/enabled." << endl;
	} else if (ResultContains(result.result, Result::Installed | Result::Committed)) {
		cout << "Installed and committed." << endl;
	} else if (ResultContains(result.result, Result::Installed)) {
		cout << "Installed, but not committed." << endl;
		cout << "Use 'commit' to update, or 'rollback' to roll back the update." << endl;
	} else if (ResultContains(result.result, Result::Committed)) {
		cout << "Committed." << endl;
	} else {
		stringstream ss;
		ss << std::hex << static_cast<int>(result.result);
		log::Error("Unexpected result value: 0x" + ss.str());
	}

	if (ResultContains(result.result, Result::RebootRequired)) {
		cout << "At least one payload requested a reboot of the device it updated." << endl;
		if (result.err == error::NoError) {
			result.err = context::MakeError(context::RebootRequiredError, "Reboot required");
		}
	}

	return result.err;
}

error::Error InstallAction::Execute(context::MenderContext &main_context) {
	error::Error err = MaybeInstallBootstrapArtifact(main_context);
	if (err != error::NoError) {
		return err;
	}
	events::EventLoop loop;
	standalone::Context ctx {main_context, loop};
	ctx.stop_after = std::move(stop_after_);
	auto result = standalone::Install(ctx, src_);
	err = ResultHandler(result);
	if (!reboot_exit_code_
		&& err.code == context::MakeError(context::RebootRequiredError, "").code) {
		// If reboot exit code isn't requested, then this type of error should be treated as
		// plain success.
		err = error::NoError;
	}
	return err;
}

error::Error ResumeAction::Execute(context::MenderContext &main_context) {
	events::EventLoop loop;
	standalone::Context ctx {main_context, loop};
	ctx.stop_after = std::move(stop_after_);

	auto result = standalone::Resume(ctx);
	auto err = ResultHandler(result);

	if (!reboot_exit_code_
		&& err.code == context::MakeError(context::RebootRequiredError, "").code) {
		// If reboot exit code isn't requested, then this type of error should be treated as
		// plain success.
		err = error::NoError;
	}
	return err;
}

error::Error CommitAction::Execute(context::MenderContext &main_context) {
	events::EventLoop loop;
	standalone::Context ctx {main_context, loop};
	auto result = standalone::Commit(ctx);
	return ResultHandler(result);
}

error::Error RollbackAction::Execute(context::MenderContext &main_context) {
	events::EventLoop loop;
	standalone::Context ctx {main_context, loop};
	auto result = standalone::Rollback(ctx);
	return ResultHandler(result);
}

error::Error DaemonAction::Execute(context::MenderContext &main_context) {
	events::EventLoop event_loop;
	daemon::Context ctx(main_context, event_loop);

#if not defined(MENDER_USE_DBUS) and defined(MENDER_EMBED_MENDER_AUTH)
	// Passphrase is not currently supported when launching from mender-update cli.
	auto key_store = mender::auth::cli::KeystoreFromConfig(ctx.mender_context.GetConfig(), "");
	ctx.authenticator.SetCryptoArgs(
		{key_store->KeyName(), key_store->PassPhrase(), key_store->SSLEngine()});
#endif

	daemon::StateMachine state_machine(ctx, event_loop);
	state_machine.LoadStateFromDb();
	error::Error err = MaybeInstallBootstrapArtifact(main_context);
	if (err != error::NoError) {
		return err;
	}

	event_loop.Post([]() {
		log::Info("The update client daemon is now ready to handle incoming deployments");
	});

	return state_machine.Run();
}

static expected::ExpectedString GetPID() {
	processes::Process proc({"systemctl", "show", "--property=MainPID", "mender-updated"});
	auto exp_line_data = proc.GenerateLineData();
	if (!exp_line_data) {
		return expected::unexpected(
			exp_line_data.error().WithContext("Failed to get the MainPID from systemctl"));
	}
	if (exp_line_data.value().size() < 1) {
		return expected::unexpected(error::Error(
			make_error_condition(errc::message_size), "No output received from systemctl"));
	}
	const string prefix {"MainPID="};
	const string line = exp_line_data.value().at(0);
	auto split_index = line.find(prefix);
	if (split_index == string::npos) {
		return expected::unexpected(error::Error(
			make_error_condition(errc::no_message), "No output received from systemctl"));
	}
	if (split_index != 0) {
		return expected::unexpected(error::Error(
			make_error_condition(errc::bad_message), "Unexpected output from systemctl"));
	}
	const string PID = line.substr(split_index + prefix.size(), line.size());
	if (PID == "" or PID == "0") {
		return expected::unexpected(error::Error(
			make_error_condition(errc::no_message),
			"No PID found for mender-updated. The service is not running"));
	}
	return PID;
}

static error::Error SendSignal(const string &signal, const string &pid) {
	const vector<string> command {"kill", "-" + signal, pid};
	const string command_string = common::JoinStrings(command, " ");
	processes::Process proc(command);
	auto err = proc.Start();
	if (err != error::NoError) {
		return err.WithContext("Command '" + command_string + "'");
	}
	return proc.Wait().WithContext("Command '" + command_string + "'");
}

error::Error SendInventoryAction::Execute(context::MenderContext &main_context) {
	auto pid = GetPID();
	if (!pid) {
		return pid.error().WithContext("Failed to force an inventory update");
	}

	return SendSignal("SIGUSR2", pid.value()).WithContext("Failed to force an inventory update");
}

error::Error CheckUpdateAction::Execute(context::MenderContext &main_context) {
	auto pid = GetPID();
	if (!pid) {
		return pid.error().WithContext("Failed to force an update check");
	}
	return SendSignal("SIGUSR1", pid.value()).WithContext("Failed to force an update check");
}

} // namespace cli
} // namespace update
} // namespace mender
