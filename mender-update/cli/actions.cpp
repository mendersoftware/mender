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

#include <common/events.hpp>
#include <common/log.hpp>

#include <mender-update/daemon.hpp>
#include <mender-update/standalone.hpp>

namespace mender {
namespace update {
namespace cli {

namespace conf = mender::common::conf;
namespace database = mender::common::key_value_database;
namespace events = mender::common::events;
namespace log = mender::common::log;

namespace daemon = mender::update::daemon;
namespace standalone = mender::update::standalone;

error::Error ShowArtifactAction::Execute(context::MenderContext &main_context) {
	auto exp_provides = main_context.LoadProvides();
	if (!exp_provides) {
		return exp_provides.error();
	}

	auto &provides = exp_provides.value();
	if (provides.count("artifact_name") == 0 || provides["artifact_name"] == "") {
		cout << "Unknown" << endl;
	} else {
		cout << provides["artifact_name"] << endl;
	}
	return error::NoError;
}

error::Error ShowProvidesAction::Execute(context::MenderContext &main_context) {
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
	switch (result.result) {
	case standalone::Result::InstalledAndCommitted:
	case standalone::Result::Committed:
	case standalone::Result::Installed:
	case standalone::Result::RolledBack:
		// There should not be any error for these.
		assert(result.err == error::NoError);
		break;
	case standalone::Result::InstalledAndCommittedRebootRequired:
	case standalone::Result::InstalledRebootRequired:
		if (result.err == error::NoError) {
			result.err = context::MakeError(context::RebootRequiredError, "Reboot required");
		}
		break;
	default:
		// All other states, make sure they have an error.
		if (result.err != error::NoError) {
			log::Error(result.err.String());
		} else {
			result.err = context::MakeError(context::ExitStatusOnlyError, "");
		}
		break;
	}

	switch (result.result) {
	case standalone::Result::InstalledAndCommitted:
	case standalone::Result::InstalledAndCommittedRebootRequired:
		cout << "Installed and committed." << endl;
		break;
	case standalone::Result::Committed:
		cout << "Committed." << endl;
		break;
	case standalone::Result::Installed:
	case standalone::Result::InstalledRebootRequired:
		cout << "Installed, but not committed." << endl;
		cout << "Use 'commit' to update, or 'rollback' to roll back the update." << endl;
		break;
	case standalone::Result::InstalledButFailedInPostCommit:
		cout << "Installed, but one or more post-commit steps failed." << endl;
		break;
	case standalone::Result::NoUpdateInProgress:
		cout << "No update in progress." << endl;
		break;
	case standalone::Result::FailedNothingDone:
		cout << "Installation failed. System not modified." << endl;
		break;
	case standalone::Result::RolledBack:
		cout << "Rolled back." << endl;
		break;
	case standalone::Result::NoRollback:
		cout << "Update Module does not support rollback." << endl;
		break;
	case standalone::Result::RollbackFailed:
		cout << "Rollback failed. System may be in an inconsistent state." << endl;
		break;
	case standalone::Result::FailedAndRolledBack:
		cout << "Installation failed. Rolled back modifications." << endl;
		break;
	case standalone::Result::FailedAndNoRollback:
		cout
			<< "Installation failed, and Update Module does not support rollback. System may be in an inconsistent state."
			<< endl;
		break;
	case standalone::Result::FailedAndRollbackFailed:
		cout
			<< "Installation failed, and rollback also failed. System may be in an inconsistent state."
			<< endl;
		break;
	}

	switch (result.result) {
	case standalone::Result::InstalledRebootRequired:
	case standalone::Result::InstalledAndCommittedRebootRequired:
		cout << "At least one payload requested a reboot of the device it updated." << endl;
		break;
	default:
		break;
	}

	return result.err;
}

error::Error InstallAction::Execute(context::MenderContext &main_context) {
	auto result = standalone::Install(main_context, src_);
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
	auto result = standalone::Commit(main_context);
	return ResultHandler(result);
}

error::Error RollbackAction::Execute(context::MenderContext &main_context) {
	auto result = standalone::Rollback(main_context);
	return ResultHandler(result);
}

error::Error DaemonAction::Execute(context::MenderContext &main_context) {
	events::EventLoop event_loop;
	daemon::Context ctx(main_context, event_loop);
	daemon::StateMachine state_machine(ctx, event_loop);
	return state_machine.Run();
}

} // namespace cli
} // namespace update
} // namespace mender
