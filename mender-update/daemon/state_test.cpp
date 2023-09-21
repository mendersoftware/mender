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

#include <csignal>
#include <cstdlib>
#include <filesystem>
#include <fstream>
#include <string>
#include <vector>

#include <gtest/gtest.h>

#include <common/common.hpp>
#include <common/conf.hpp>
#include <common/error.hpp>
#include <common/events.hpp>
#include <common/path.hpp>
#include <common/processes.hpp>
#include <common/testing.hpp>

#include <mender-update/context.hpp>
#include <mender-update/inventory.hpp>
#include <mender-update/daemon/context.hpp>
#include <mender-update/daemon/state_machine.hpp>

#define DEPLOYMENT_ID "w81s4fae-7dec-11d0-a765-00a0c91e6bf6"

namespace mender {
namespace update {
namespace daemon {

namespace fs = std::filesystem;

namespace common = mender::common;
namespace conf = mender::common::conf;
namespace error = mender::common::error;
namespace events = mender::common::events;
namespace path = mender::common::path;
namespace processes = mender::common::processes;

namespace mtesting = mender::common::testing;

namespace context = mender::update::context;
namespace inventory = mender::update::inventory;

using namespace std;

enum class InstallOutcome {
	SuccessfulInstall,
	SuccessfulRollback,
	UnsuccessfulInstall,
};

enum class ProvideFileSizes {
	Empty,
	Yes,
	No,
};

static std::string ProvideFileSizesString[] = {"", "Yes", "No"};

std::string ProvideFileSizesToString(ProvideFileSizes sizes) {
	return ProvideFileSizesString[static_cast<int>(sizes)];
}

struct StateTransitionsTestCase {
	string case_name;
	vector<string> state_chain;
	vector<string> status_log;
	InstallOutcome install_outcome;
	int fail_status_report_count;
	deployments::DeploymentStatus fail_status_report_status;
	bool fail_status_aborted;
	bool long_retry_times;

	vector<string> error_states;
	bool error_forever;
	vector<string> spont_reboot_states;
	bool spont_reboot_forever;
	vector<string> hang_states;
	bool rollback_disabled;
	bool reboot_disabled;
	ProvideFileSizes provide_file_sizes;
	bool broken_download;
	int do_schema_update_at_invocation {-1};
	int use_non_writable_db_after_n_writes {-1};
	bool empty_payload_artifact;
	bool device_type_mismatch {false};
	bool generate_idle_sync_scripts {false};
};


vector<StateTransitionsTestCase> idle_and_sync_test_cases {
	StateTransitionsTestCase {
		.case_name = "Normal_flow_Idle_Sync_Idle",
		.state_chain =
			{
				"Idle_Enter_00",
				"Idle_Leave_00",
				"Sync_Enter_00",
				"Sync_Leave_00",
				"Idle_Enter_00",
				"Idle_Leave_00",
				"Sync_Enter_00",
				"Sync_Leave_00",
				"Download_Enter_00",
				"ProvidePayloadFileSizes",
				"Download",
				"Download_Leave_00",
				"ArtifactInstall_Enter_00",
				"ArtifactInstall",
				"ArtifactInstall_Leave_00",
				"ArtifactReboot_Enter_00",
				"ArtifactReboot",
				"ArtifactVerifyReboot",
				"ArtifactReboot_Leave_00",
				"ArtifactCommit_Enter_00",
				"ArtifactCommit",
				"ArtifactCommit_Leave_00",
				"Cleanup", // Leave me, or clang-format is not nice
			},
		.status_log =
			{
				"downloading",
				"installing",
				"rebooting",
				"installing",
				"success",
			},
		.install_outcome = InstallOutcome::SuccessfulInstall,
		.generate_idle_sync_scripts = true,
	},

	StateTransitionsTestCase {
		.case_name = "Normal_flow_DownloadWithFileSizesYes",
		.state_chain =
			{
				"Download_Enter_00",
				"ProvidePayloadFileSizes",
				"DownloadWithFileSizes",
				"Download_Leave_00",
				"ArtifactInstall_Enter_00",
				"ArtifactInstall",
				"ArtifactInstall_Leave_00",
				"ArtifactReboot_Enter_00",
				"ArtifactReboot",
				"ArtifactVerifyReboot",
				"ArtifactReboot_Leave_00",
				"ArtifactCommit_Enter_00",
				"ArtifactCommit",
				"ArtifactCommit_Leave_00",
				"Cleanup", // Leave me, or clang-format is not nice
			},
		.status_log =
			{
				"downloading",
				"installing",
				"rebooting",
				"installing",
				"success",
			},
		.install_outcome = InstallOutcome::SuccessfulInstall,
		.provide_file_sizes = ProvideFileSizes::Yes,
	},

	StateTransitionsTestCase {
		.case_name = "Normal_flow_DownloadWithFileSizesNo",
		.state_chain =
			{
				"Download_Enter_00",
				"ProvidePayloadFileSizes",
				"Download",
				"Download_Leave_00",
				"ArtifactInstall_Enter_00",
				"ArtifactInstall",
				"ArtifactInstall_Leave_00",
				"ArtifactReboot_Enter_00",
				"ArtifactReboot",
				"ArtifactVerifyReboot",
				"ArtifactReboot_Leave_00",
				"ArtifactCommit_Enter_00",
				"ArtifactCommit",
				"ArtifactCommit_Leave_00",
				"Cleanup", // Leave me, or clang-format is not nice
			},
		.status_log =
			{
				"downloading",
				"installing",
				"rebooting",
				"installing",
				"success",
			},
		.install_outcome = InstallOutcome::SuccessfulInstall,
		.provide_file_sizes = ProvideFileSizes::No,
	},

	StateTransitionsTestCase {
		.case_name = "Normal_flow_DownloadWithFileSizesEmpty",
		.state_chain =
			{
				"Download_Enter_00",
				"ProvidePayloadFileSizes",
				"Download",
				"Download_Leave_00",
				"ArtifactInstall_Enter_00",
				"ArtifactInstall",
				"ArtifactInstall_Leave_00",
				"ArtifactReboot_Enter_00",
				"ArtifactReboot",
				"ArtifactVerifyReboot",
				"ArtifactReboot_Leave_00",
				"ArtifactCommit_Enter_00",
				"ArtifactCommit",
				"ArtifactCommit_Leave_00",
				"Cleanup", // Leave me, or clang-format is not nice
			},
		.status_log =
			{
				"downloading",
				"installing",
				"rebooting",
				"installing",
				"success",
			},
		.install_outcome = InstallOutcome::SuccessfulInstall,
		.provide_file_sizes = ProvideFileSizes::Empty,
	},

	StateTransitionsTestCase {
		.case_name = "Idle_Leave_Error",
		.state_chain =
			{
				"Idle_Enter_00",
				"Idle_Leave_00",
				"Sync_Enter_00",
				"Sync_Leave_00",
				"Idle_Enter_00",
				"Idle_Leave_00",
				"Sync_Enter_00",
				"Sync_Leave_00",
				"Download_Enter_00",
				"ProvidePayloadFileSizes",
				"Download",
				"Download_Leave_00",
				"ArtifactInstall_Enter_00",
				"ArtifactInstall",
				"ArtifactInstall_Leave_00",
				"ArtifactReboot_Enter_00",
				"ArtifactReboot",
				"ArtifactVerifyReboot",
				"ArtifactReboot_Leave_00",
				"ArtifactCommit_Enter_00",
				"ArtifactCommit",
				"ArtifactCommit_Leave_00",
				"Cleanup", // Leave me, or clang-format is not nice
			},
		.status_log =
			{
				"downloading",
				"installing",
				"rebooting",
				"installing",
				"success",
			},
		.install_outcome = InstallOutcome::SuccessfulInstall,
		.error_states = {"Idle_Leave_00"},
		.generate_idle_sync_scripts = true,
	},

	StateTransitionsTestCase {
		.case_name = "Sync_Enter_Error",
		.state_chain =
			{
				"Idle_Enter_00",
				"Idle_Leave_00",
				"Sync_Enter_00",
				"Sync_Error_00",
				"Idle_Enter_00",
				"Idle_Leave_00",
				"Sync_Enter_00",
				"Sync_Leave_00",
				"Download_Enter_00",
				"ProvidePayloadFileSizes",
				"Download",
				"Download_Leave_00",
				"ArtifactInstall_Enter_00",
				"ArtifactInstall",
				"ArtifactInstall_Leave_00",
				"ArtifactReboot_Enter_00",
				"ArtifactReboot",
				"ArtifactVerifyReboot",
				"ArtifactReboot_Leave_00",
				"ArtifactCommit_Enter_00",
				"ArtifactCommit",
				"ArtifactCommit_Leave_00",
				"Cleanup", // Leave me, or clang-format is not nice
			},
		.status_log =
			{
				"downloading",
				"installing",
				"rebooting",
				"installing",
				"success",
			},
		.install_outcome = InstallOutcome::SuccessfulInstall,
		.error_states = {"Sync_Enter_00"},
		.generate_idle_sync_scripts = true,
	},

	StateTransitionsTestCase {
		.case_name = "Sync_Leave_Error",
		.state_chain =
			{
				"Idle_Enter_00",
				"Idle_Leave_00",
				"Sync_Enter_00",
				"Sync_Leave_00", // <- Only fails the first time, here
				"Sync_Error_00",
				"Idle_Enter_00",
				"Idle_Leave_00",
				"Sync_Enter_00",
				"Sync_Leave_00",
				"Download_Enter_00",
				"ProvidePayloadFileSizes",
				"Download",
				"Download_Leave_00",
				"ArtifactInstall_Enter_00",
				"ArtifactInstall",
				"ArtifactInstall_Leave_00",
				"ArtifactReboot_Enter_00",
				"ArtifactReboot",
				"ArtifactVerifyReboot",
				"ArtifactReboot_Leave_00",
				"ArtifactCommit_Enter_00",
				"ArtifactCommit",
				"ArtifactCommit_Leave_00",
				"Cleanup", // Leave me, or clang-format is not nice
			},
		.status_log =
			{
				"downloading",
				"installing",
				"rebooting",
				"installing",
				"success",
			},
		.install_outcome = InstallOutcome::SuccessfulInstall,
		.error_states = {"Sync_Leave_00"},
		.generate_idle_sync_scripts = true,
	},
};

vector<StateTransitionsTestCase> GenerateStateTransitionsTestCases() {
	return {
		StateTransitionsTestCase {
			.case_name = "Normal_install__no_reboot__no_rollback",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Leave_00",
					"ArtifactCommit_Enter_00",
					"ArtifactCommit",
					"ArtifactCommit_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					// Twice, due to the pre-commit status update.
					"installing",
					"success",
				},
			.install_outcome = InstallOutcome::SuccessfulInstall,
			.rollback_disabled = true,
			.reboot_disabled = true,
		},

		StateTransitionsTestCase {
			.case_name = "Normal_install__no_rollback",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Leave_00",
					"ArtifactReboot_Enter_00",
					"ArtifactReboot",
					"ArtifactVerifyReboot",
					"ArtifactReboot_Leave_00",
					"ArtifactCommit_Enter_00",
					"ArtifactCommit",
					"ArtifactCommit_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"rebooting",
					"installing",
					"success",
				},
			.install_outcome = InstallOutcome::SuccessfulInstall,
			.rollback_disabled = true,

		},

		StateTransitionsTestCase {
			.case_name = "Normal_install",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Leave_00",
					"ArtifactReboot_Enter_00",
					"ArtifactReboot",
					"ArtifactVerifyReboot",
					"ArtifactReboot_Leave_00",
					"ArtifactCommit_Enter_00",
					"ArtifactCommit",
					"ArtifactCommit_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"rebooting",
					"installing",
					"success",
				},
			.install_outcome = InstallOutcome::SuccessfulInstall,
		},

		StateTransitionsTestCase {
			.case_name = "Error_in_Download_state__no_rollback",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Error_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"failure",
				},
			.install_outcome = InstallOutcome::SuccessfulRollback,
			.error_states = {"Download"},
			.rollback_disabled = true,
		},

		StateTransitionsTestCase {
			.case_name = "Killed_in_Download_state__no_rollback",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"failure",
				},
			.install_outcome = InstallOutcome::SuccessfulRollback,
			.spont_reboot_states = {"Download"},
			.rollback_disabled = true,
		},

		StateTransitionsTestCase {
			.case_name = "Error_in_ArtifactInstall_state__no_rollback",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Error_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"failure",
				},
			.install_outcome = InstallOutcome::UnsuccessfulInstall,
			.error_states = {"ArtifactInstall"},
			.rollback_disabled = true,
		},

		StateTransitionsTestCase {
			.case_name = "Killed_in_ArtifactInstall_state__no_rollback",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"failure",
				},
			.install_outcome = InstallOutcome::UnsuccessfulInstall,
			.spont_reboot_states = {"ArtifactInstall"},
			.rollback_disabled = true,
		},

		StateTransitionsTestCase {
			.case_name = "Error_in_ArtifactInstall",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Error_00",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback",
					"ArtifactRollback_Leave_00",
					"ArtifactRollbackReboot_Enter_00",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot_Leave_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"failure",
				},
			.install_outcome = InstallOutcome::SuccessfulRollback,
			.error_states = {"ArtifactInstall"},
		},

		StateTransitionsTestCase {
			.case_name = "Killed_in_ArtifactInstall",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback",
					"ArtifactRollback_Leave_00",
					"ArtifactRollbackReboot_Enter_00",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot_Leave_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"failure",
				},
			.install_outcome = InstallOutcome::SuccessfulRollback,
			.spont_reboot_states = {"ArtifactInstall"},
		},

		StateTransitionsTestCase {
			.case_name = "Error_in_ArtifactReboot",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Leave_00",
					"ArtifactReboot_Enter_00",
					"ArtifactReboot",
					"ArtifactReboot_Error_00",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback",
					"ArtifactRollback_Leave_00",
					"ArtifactRollbackReboot_Enter_00",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot_Leave_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"rebooting",
					"failure",
				},
			.install_outcome = InstallOutcome::SuccessfulRollback,
			.error_states = {"ArtifactReboot"},
		},

		StateTransitionsTestCase {
			.case_name = "Killed_in_ArtifactReboot",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Leave_00",
					"ArtifactReboot_Enter_00",
					"ArtifactReboot",
					"ArtifactVerifyReboot",
					"ArtifactReboot_Leave_00",
					"ArtifactCommit_Enter_00",
					"ArtifactCommit",
					"ArtifactCommit_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"rebooting",
					"installing",
					"success",
				},
			.install_outcome = InstallOutcome::SuccessfulInstall,
			.spont_reboot_states = {"ArtifactReboot"},
		},

		StateTransitionsTestCase {
			.case_name = "Error_in_ArtifactVerifyReboot",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Leave_00",
					"ArtifactReboot_Enter_00",
					"ArtifactReboot",
					"ArtifactVerifyReboot",
					"ArtifactReboot_Error_00",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback",
					"ArtifactRollback_Leave_00",
					"ArtifactRollbackReboot_Enter_00",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot_Leave_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"rebooting",
					"failure",
				},
			.install_outcome = InstallOutcome::SuccessfulRollback,
			.error_states = {"ArtifactVerifyReboot"},
		},

		StateTransitionsTestCase {
			.case_name = "Killed_in_ArtifactVerifyReboot",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Leave_00",
					"ArtifactReboot_Enter_00",
					"ArtifactReboot",
					"ArtifactVerifyReboot",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback",
					"ArtifactRollback_Leave_00",
					"ArtifactRollbackReboot_Enter_00",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot_Leave_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"rebooting",
					"failure",
				},
			.install_outcome = InstallOutcome::SuccessfulRollback,
			.spont_reboot_states = {"ArtifactVerifyReboot"},
		},

		StateTransitionsTestCase {
			.case_name = "Error_in_ArtifactRollback",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Leave_00",
					"ArtifactReboot_Enter_00",
					"ArtifactReboot",
					"ArtifactVerifyReboot",
					"ArtifactReboot_Error_00",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback",          // <- Fails here
					"ArtifactRollback_Leave_00", // <- No error scripts called here
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"rebooting",
					"failure",
				},
			.install_outcome = InstallOutcome::UnsuccessfulInstall,
			.error_states = {"ArtifactVerifyReboot", "ArtifactRollback"},
		},

		StateTransitionsTestCase {
			.case_name = "Killed_in_ArtifactRollback",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Leave_00",
					"ArtifactReboot_Enter_00",
					"ArtifactReboot",
					"ArtifactVerifyReboot",
					"ArtifactReboot_Error_00",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback",
					"ArtifactRollback_Leave_00",
					"ArtifactRollbackReboot_Enter_00",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot_Leave_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"rebooting",
					"failure",
				},
			.install_outcome = InstallOutcome::SuccessfulRollback,
			.error_states = {"ArtifactVerifyReboot"},
			.spont_reboot_states = {"ArtifactRollback"},
		},

		StateTransitionsTestCase {
			.case_name = "Error_in_ArtifactRollbackReboot",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Leave_00",
					"ArtifactReboot_Enter_00",
					"ArtifactReboot",
					"ArtifactVerifyReboot",
					"ArtifactReboot_Error_00",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback",
					"ArtifactRollback_Leave_00",
					"ArtifactRollbackReboot_Enter_00",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot_Leave_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"rebooting",
					"failure",
				},
			.install_outcome = InstallOutcome::SuccessfulRollback,
			.error_states = {"ArtifactVerifyReboot", "ArtifactRollbackReboot"},
		},

		StateTransitionsTestCase {
			.case_name = "Killed_in_ArtifactRollbackReboot",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Leave_00",
					"ArtifactReboot_Enter_00",
					"ArtifactReboot",
					"ArtifactVerifyReboot",
					"ArtifactReboot_Error_00",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback",
					"ArtifactRollback_Leave_00",
					"ArtifactRollbackReboot_Enter_00",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot_Leave_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"rebooting",
					"failure",
				},
			.install_outcome = InstallOutcome::SuccessfulRollback,
			.error_states = {"ArtifactVerifyReboot"},
			.spont_reboot_states = {"ArtifactRollbackReboot"},
		},

		StateTransitionsTestCase {
			.case_name = "Error_in_ArtifactVerifyRollbackReboot",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Leave_00",
					"ArtifactReboot_Enter_00",
					"ArtifactReboot",
					"ArtifactVerifyReboot",
					"ArtifactReboot_Error_00",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback",
					"ArtifactRollback_Leave_00",
					"ArtifactRollbackReboot_Enter_00",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot_Leave_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"rebooting",
					"failure",
				},
			.install_outcome = InstallOutcome::SuccessfulRollback,
			.error_states = {"ArtifactVerifyReboot", "ArtifactVerifyRollbackReboot"},
		},

		StateTransitionsTestCase {
			.case_name = "Killed_in_ArtifactVerifyRollbackReboot",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Leave_00",
					"ArtifactReboot_Enter_00",
					"ArtifactReboot",
					"ArtifactVerifyReboot",
					"ArtifactReboot_Error_00",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback",
					"ArtifactRollback_Leave_00",
					"ArtifactRollbackReboot_Enter_00",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot_Leave_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"rebooting",
					"failure",
				},
			.install_outcome = InstallOutcome::SuccessfulRollback,
			.error_states = {"ArtifactVerifyReboot"},
			.spont_reboot_states = {"ArtifactVerifyRollbackReboot"},
		},

		StateTransitionsTestCase {
			.case_name = "Error_in_ArtifactFailure",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Error_00",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback",
					"ArtifactRollback_Leave_00",
					"ArtifactRollbackReboot_Enter_00",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot_Leave_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"failure",
				},
			.install_outcome = InstallOutcome::UnsuccessfulInstall,
			.error_states = {"ArtifactInstall", "ArtifactFailure"},
		},

		StateTransitionsTestCase {
			.case_name = "Killed_in_ArtifactFailure",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Error_00",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback",
					"ArtifactRollback_Leave_00",
					"ArtifactRollbackReboot_Enter_00",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot_Leave_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"failure",
				},
			.install_outcome = InstallOutcome::SuccessfulRollback,
			.error_states = {"ArtifactInstall"},
			.spont_reboot_states = {"ArtifactFailure"},
		},

		StateTransitionsTestCase {
			.case_name = "Killed_in_ArtifactFailure__no_rollback",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Error_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"failure",
				},
			.install_outcome = InstallOutcome::UnsuccessfulInstall,
			.error_states = {"ArtifactInstall"},
			.spont_reboot_states = {"ArtifactFailure"},
			.rollback_disabled = true,
		},

		StateTransitionsTestCase {
			.case_name = "Error_in_Cleanup",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Leave_00",
					"ArtifactReboot_Enter_00",
					"ArtifactReboot",
					"ArtifactVerifyReboot",
					"ArtifactReboot_Error_00",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback",
					"ArtifactRollback_Leave_00",
					"ArtifactRollbackReboot_Enter_00",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot_Leave_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"rebooting",
					"failure",
				},
			.install_outcome = InstallOutcome::SuccessfulRollback,
			.error_states = {"ArtifactVerifyReboot", "Cleanup"},
		},

		StateTransitionsTestCase {
			.case_name = "Killed_in_Cleanup",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Leave_00",
					"ArtifactReboot_Enter_00",
					"ArtifactReboot",
					"ArtifactVerifyReboot",
					"ArtifactReboot_Error_00",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback",
					"ArtifactRollback_Leave_00",
					"ArtifactRollbackReboot_Enter_00",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot_Leave_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"rebooting",
					"failure",
				},
			.install_outcome = InstallOutcome::SuccessfulRollback,
			.error_states = {"ArtifactVerifyReboot"},
			.spont_reboot_states = {"Cleanup"},
		},

		StateTransitionsTestCase {
			.case_name = "Killed_in_Cleanup__no_rollback",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Leave_00",
					"ArtifactReboot_Enter_00",
					"ArtifactReboot",
					"ArtifactVerifyReboot",
					"ArtifactReboot_Error_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"rebooting",
					"failure",
				},
			.install_outcome = InstallOutcome::UnsuccessfulInstall,
			.error_states = {"ArtifactVerifyReboot"},
			.spont_reboot_states = {"Cleanup"},
			.rollback_disabled = true,
		},

		StateTransitionsTestCase {
			.case_name = "Error_in_ArtifactCommit",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Leave_00",
					"ArtifactReboot_Enter_00",
					"ArtifactReboot",
					"ArtifactVerifyReboot",
					"ArtifactReboot_Leave_00",
					"ArtifactCommit_Enter_00",
					"ArtifactCommit",
					"ArtifactCommit_Error_00",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback",
					"ArtifactRollback_Leave_00",
					"ArtifactRollbackReboot_Enter_00",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot_Leave_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"rebooting",
					"installing",
					"failure",
				},
			.install_outcome = InstallOutcome::SuccessfulRollback,
			.error_states = {"ArtifactCommit"},
		},

		StateTransitionsTestCase {
			.case_name = "Killed_in_ArtifactCommit",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Leave_00",
					"ArtifactReboot_Enter_00",
					"ArtifactReboot",
					"ArtifactVerifyReboot",
					"ArtifactReboot_Leave_00",
					"ArtifactCommit_Enter_00",
					"ArtifactCommit",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback",
					"ArtifactRollback_Leave_00",
					"ArtifactRollbackReboot_Enter_00",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot_Leave_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"rebooting",
					"installing",
					"failure",
				},
			.install_outcome = InstallOutcome::SuccessfulRollback,
			.spont_reboot_states = {"ArtifactCommit"},
		},

		StateTransitionsTestCase {
			.case_name = "Error_in_ArtifactCommit__no_reboot",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Leave_00",
					"ArtifactCommit_Enter_00",
					"ArtifactCommit",
					"ArtifactCommit_Error_00",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback",
					"ArtifactRollback_Leave_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"installing",
					"failure",
				},
			.install_outcome = InstallOutcome::SuccessfulRollback,
			.error_states = {"ArtifactCommit"},
			.reboot_disabled = true,
		},

		StateTransitionsTestCase {
			.case_name = "Killed_in_ArtifactCommit__no_reboot",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Leave_00",
					"ArtifactCommit_Enter_00",
					"ArtifactCommit",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback",
					"ArtifactRollback_Leave_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"installing",
					"failure",
				},
			.install_outcome = InstallOutcome::SuccessfulRollback,
			.spont_reboot_states = {"ArtifactCommit"},
			.reboot_disabled = true,
		},

		StateTransitionsTestCase {
			.case_name = "Error_in_Download_Enter_00_state__no_rollback",
			.state_chain =
				{
					"Download_Enter_00",
					"Download_Error_00",
				},
			.status_log =
				{
					"downloading",
					"failure",
				},
			.install_outcome = InstallOutcome::SuccessfulRollback,
			.error_states = {"Download_Enter_00"},
			.rollback_disabled = true,
		},

		StateTransitionsTestCase {
			.case_name = "Killed_in_Download_Enter_00_state__no_rollback",
			.state_chain =
				{
					"Download_Enter_00",
					// "Cleanup", <- No download, no update module to execute
				},
			.status_log =
				{
					"downloading",
					"failure",
				},
			.install_outcome = InstallOutcome::SuccessfulRollback,
			.spont_reboot_states = {"Download_Enter_00"},
			.rollback_disabled = true,
		},

		StateTransitionsTestCase {
			.case_name = "Error_in_ArtifactInstall_Enter_00_state__no_rollback",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall_Error_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"failure",
				},
			.install_outcome = InstallOutcome::UnsuccessfulInstall,
			.error_states = {"ArtifactInstall_Enter_00"},
			.rollback_disabled = true,
		},

		StateTransitionsTestCase {
			.case_name = "Error_in_ArtifactInstall_depends_check",
			// This test never reaches the update module so there's nothing to
			// record the state chain.
			.state_chain =
				{
					"Download_Enter_00",
					"Download_Error_00",
				},
			.status_log =
				{
					"downloading",
					"failure",
				},
			.install_outcome = InstallOutcome::SuccessfulRollback,
			.device_type_mismatch = true,
		},

		StateTransitionsTestCase {
			.case_name = "Killed_in_ArtifactInstall_Enter_00_state__no_rollback",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"failure",
				},
			.install_outcome = InstallOutcome::UnsuccessfulInstall,
			.spont_reboot_states = {"ArtifactInstall_Enter_00"},
			.rollback_disabled = true,
		},

		StateTransitionsTestCase {
			.case_name = "Error_in_ArtifactInstall_Enter_00",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall_Error_00",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback",
					"ArtifactRollback_Leave_00",
					"ArtifactRollbackReboot_Enter_00",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot_Leave_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"failure",
				},
			.install_outcome = InstallOutcome::SuccessfulRollback,
			.error_states = {"ArtifactInstall_Enter_00"},
		},

		StateTransitionsTestCase {
			.case_name = "Killed_in_ArtifactInstall_Enter_00",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback",
					"ArtifactRollback_Leave_00",
					"ArtifactRollbackReboot_Enter_00",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot_Leave_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"failure",
				},
			.install_outcome = InstallOutcome::SuccessfulRollback,
			.spont_reboot_states = {"ArtifactInstall_Enter_00"},
		},

		StateTransitionsTestCase {
			.case_name = "Error_in_ArtifactReboot_Enter_00",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Leave_00",
					"ArtifactReboot_Enter_00",
					"ArtifactReboot_Error_00",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback",
					"ArtifactRollback_Leave_00",
					"ArtifactRollbackReboot_Enter_00",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot_Leave_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"rebooting",
					"failure",
				},
			.install_outcome = InstallOutcome::SuccessfulRollback,
			.error_states = {"ArtifactReboot_Enter_00"},
		},

		StateTransitionsTestCase {
			.case_name = "Killed_in_ArtifactReboot_Enter_00",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Leave_00",
					"ArtifactReboot_Enter_00",
					"ArtifactVerifyReboot",
					"ArtifactReboot_Leave_00",
					"ArtifactCommit_Enter_00",
					"ArtifactCommit",
					"ArtifactCommit_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"rebooting",
					"installing",
					"success",
				},
			.install_outcome = InstallOutcome::SuccessfulInstall,
			.spont_reboot_states = {"ArtifactReboot_Enter_00"},
		},

		StateTransitionsTestCase {
			.case_name = "Error_in_ArtifactRollback_Enter_00",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Leave_00",
					"ArtifactReboot_Enter_00",
					"ArtifactReboot",
					"ArtifactVerifyReboot",
					"ArtifactReboot_Error_00",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback",
					"ArtifactRollback_Leave_00",
					"ArtifactRollbackReboot_Enter_00",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot_Leave_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"rebooting",
					"failure",
				},
			.install_outcome = InstallOutcome::UnsuccessfulInstall,
			.error_states = {"ArtifactVerifyReboot", "ArtifactRollback_Enter_00"},
		},

		StateTransitionsTestCase {
			.case_name = "Killed_in_ArtifactRollback_Enter_00",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Leave_00",
					"ArtifactReboot_Enter_00",
					"ArtifactReboot",
					"ArtifactVerifyReboot",
					"ArtifactReboot_Error_00",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback",
					"ArtifactRollback_Leave_00",
					"ArtifactRollbackReboot_Enter_00",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot_Leave_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"rebooting",
					"failure",
				},
			.install_outcome = InstallOutcome::SuccessfulRollback,
			.error_states = {"ArtifactVerifyReboot"},
			.spont_reboot_states = {"ArtifactRollback_Enter_00"},
		},

		StateTransitionsTestCase {
			.case_name = "Error_in_ArtifactRollbackReboot_Enter_00",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Leave_00",
					"ArtifactReboot_Enter_00",
					"ArtifactReboot",
					"ArtifactVerifyReboot",
					"ArtifactReboot_Error_00",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback",
					"ArtifactRollback_Leave_00",
					"ArtifactRollbackReboot_Enter_00",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot_Leave_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"rebooting",
					"failure",
				},
			.install_outcome = InstallOutcome::UnsuccessfulInstall,
			.error_states = {"ArtifactVerifyReboot", "ArtifactRollbackReboot_Enter_00"},
		},

		StateTransitionsTestCase {
			.case_name = "Killed_in_ArtifactRollbackReboot_Enter_00",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Leave_00",
					"ArtifactReboot_Enter_00",
					"ArtifactReboot",
					"ArtifactVerifyReboot",
					"ArtifactReboot_Error_00",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback",
					"ArtifactRollback_Leave_00",
					"ArtifactRollbackReboot_Enter_00",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot_Leave_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"rebooting",
					"failure",
				},
			.install_outcome = InstallOutcome::SuccessfulRollback,
			.error_states = {"ArtifactVerifyReboot"},
			.spont_reboot_states = {"ArtifactRollbackReboot_Enter_00"},
		},

		StateTransitionsTestCase {
			.case_name = "Error_in_ArtifactFailure_Enter_00",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Error_00",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback",
					"ArtifactRollback_Leave_00",
					"ArtifactRollbackReboot_Enter_00",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot_Leave_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"failure",
				},
			.install_outcome = InstallOutcome::UnsuccessfulInstall,
			.error_states = {"ArtifactInstall", "ArtifactFailure_Enter_00"},
		},

		StateTransitionsTestCase {
			.case_name = "Killed_in_ArtifactFailure_Enter_00",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Error_00",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback",
					"ArtifactRollback_Leave_00",
					"ArtifactRollbackReboot_Enter_00",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot_Leave_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"failure",
				},
			.install_outcome = InstallOutcome::SuccessfulRollback,
			.error_states = {"ArtifactInstall"},
			.spont_reboot_states = {"ArtifactFailure_Enter_00"},
		},

		StateTransitionsTestCase {
			.case_name = "Error_in_ArtifactCommit_Enter_00",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Leave_00",
					"ArtifactReboot_Enter_00",
					"ArtifactReboot",
					"ArtifactVerifyReboot",
					"ArtifactReboot_Leave_00",
					"ArtifactCommit_Enter_00",
					"ArtifactCommit_Error_00",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback",
					"ArtifactRollback_Leave_00",
					"ArtifactRollbackReboot_Enter_00",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot_Leave_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"rebooting",
					"installing",
					"failure",
				},
			.install_outcome = InstallOutcome::SuccessfulRollback,
			.error_states = {"ArtifactCommit_Enter_00"},
		},

		StateTransitionsTestCase {
			.case_name = "Killed_in_ArtifactCommit_Enter_00",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Leave_00",
					"ArtifactReboot_Enter_00",
					"ArtifactReboot",
					"ArtifactVerifyReboot",
					"ArtifactReboot_Leave_00",
					"ArtifactCommit_Enter_00",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback",
					"ArtifactRollback_Leave_00",
					"ArtifactRollbackReboot_Enter_00",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot_Leave_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"rebooting",
					"installing",
					"failure",
				},
			.install_outcome = InstallOutcome::SuccessfulRollback,
			.spont_reboot_states = {"ArtifactCommit_Enter_00"},
		},

		StateTransitionsTestCase {
			.case_name = "Error_in_ArtifactCommit_Enter_00__no_reboot",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Leave_00",
					"ArtifactCommit_Enter_00",
					"ArtifactCommit_Error_00",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback",
					"ArtifactRollback_Leave_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"installing",
					"failure",
				},
			.install_outcome = InstallOutcome::SuccessfulRollback,
			.error_states = {"ArtifactCommit_Enter_00"},
			.reboot_disabled = true,
		},

		StateTransitionsTestCase {
			.case_name = "Killed_in_ArtifactCommit_Enter_00__no_reboot",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Leave_00",
					"ArtifactCommit_Enter_00",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback",
					"ArtifactRollback_Leave_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"installing",
					"failure",
				},
			.install_outcome = InstallOutcome::SuccessfulRollback,
			.spont_reboot_states = {"ArtifactCommit_Enter_00"},
			.reboot_disabled = true,
		},

		StateTransitionsTestCase {
			.case_name = "Error_in_Download_Leave_00_state__no_rollback",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"Download_Error_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"failure",
				},
			.install_outcome = InstallOutcome::SuccessfulRollback,
			.error_states = {"Download_Leave_00"},
			.rollback_disabled = true,
		},

		StateTransitionsTestCase {
			.case_name = "Killed_in_Download_Leave_00_state__no_rollback",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"failure",
				},
			.install_outcome = InstallOutcome::SuccessfulRollback,
			.spont_reboot_states = {"Download_Leave_00"},
			.rollback_disabled = true,
		},

		StateTransitionsTestCase {
			.case_name = "Error_in_ArtifactInstall_Leave_00_state__no_rollback",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Leave_00",
					"ArtifactInstall_Error_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"failure",
				},
			.install_outcome = InstallOutcome::UnsuccessfulInstall,
			.error_states = {"ArtifactInstall_Leave_00"},
			.rollback_disabled = true,
		},

		StateTransitionsTestCase {
			.case_name = "Killed_in_ArtifactInstall_Leave_00_state__no_rollback",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Leave_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"failure",
				},
			.install_outcome = InstallOutcome::UnsuccessfulInstall,
			.spont_reboot_states = {"ArtifactInstall_Leave_00"},
			.rollback_disabled = true,
		},

		StateTransitionsTestCase {
			.case_name = "Error_in_ArtifactInstall_Leave_00",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Leave_00",
					"ArtifactInstall_Error_00",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback",
					"ArtifactRollback_Leave_00",
					"ArtifactRollbackReboot_Enter_00",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot_Leave_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"failure",
				},
			.install_outcome = InstallOutcome::SuccessfulRollback,
			.error_states = {"ArtifactInstall_Leave_00"},
		},

		StateTransitionsTestCase {
			.case_name = "Killed_in_ArtifactInstall_Leave_00",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Leave_00",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback",
					"ArtifactRollback_Leave_00",
					"ArtifactRollbackReboot_Enter_00",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot_Leave_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"failure",
				},
			.install_outcome = InstallOutcome::SuccessfulRollback,
			.spont_reboot_states = {"ArtifactInstall_Leave_00"},
		},

		StateTransitionsTestCase {
			.case_name = "Error_in_ArtifactReboot_Leave_00",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Leave_00",
					"ArtifactReboot_Enter_00",
					"ArtifactReboot",
					"ArtifactVerifyReboot",
					"ArtifactReboot_Leave_00",
					"ArtifactReboot_Error_00",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback",
					"ArtifactRollback_Leave_00",
					"ArtifactRollbackReboot_Enter_00",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot_Leave_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"rebooting",
					"failure",
				},
			.install_outcome = InstallOutcome::SuccessfulRollback,
			.error_states = {"ArtifactReboot_Leave_00"},
		},

		StateTransitionsTestCase {
			.case_name = "Killed_in_ArtifactReboot_Leave_00",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Leave_00",
					"ArtifactReboot_Enter_00",
					"ArtifactReboot",
					"ArtifactVerifyReboot",
					"ArtifactReboot_Leave_00",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback",
					"ArtifactRollback_Leave_00",
					"ArtifactRollbackReboot_Enter_00",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot_Leave_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"rebooting",
					"failure",
				},
			.install_outcome = InstallOutcome::SuccessfulRollback,
			.spont_reboot_states = {"ArtifactReboot_Leave_00"},
		},

		StateTransitionsTestCase {
			.case_name = "Error_in_ArtifactRollback_Leave_00",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Leave_00",
					"ArtifactReboot_Enter_00",
					"ArtifactReboot",
					"ArtifactVerifyReboot",
					"ArtifactReboot_Error_00",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback",
					"ArtifactRollback_Leave_00",
					"ArtifactRollbackReboot_Enter_00",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot_Leave_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"rebooting",
					"failure",
				},
			.install_outcome = InstallOutcome::UnsuccessfulInstall,
			.error_states = {"ArtifactVerifyReboot", "ArtifactRollback_Leave_00"},
		},

		StateTransitionsTestCase {
			.case_name = "Killed_in_ArtifactRollback_Leave_00",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Leave_00",
					"ArtifactReboot_Enter_00",
					"ArtifactReboot",
					"ArtifactVerifyReboot",
					"ArtifactReboot_Error_00",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback",
					"ArtifactRollback_Leave_00",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback",
					"ArtifactRollback_Leave_00",
					"ArtifactRollbackReboot_Enter_00",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot_Leave_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"rebooting",
					"failure",
				},
			.install_outcome = InstallOutcome::SuccessfulRollback,
			.error_states = {"ArtifactVerifyReboot"},
			.spont_reboot_states = {"ArtifactRollback_Leave_00"},
		},

		StateTransitionsTestCase {
			.case_name = "Error_in_ArtifactRollbackReboot_Leave_00",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Leave_00",
					"ArtifactReboot_Enter_00",
					"ArtifactReboot",
					"ArtifactVerifyReboot",
					"ArtifactReboot_Error_00",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback",
					"ArtifactRollback_Leave_00",
					"ArtifactRollbackReboot_Enter_00",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot_Leave_00",
					"ArtifactRollbackReboot_Error_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"rebooting",
					"failure",
				},
			.install_outcome = InstallOutcome::UnsuccessfulInstall,
			.error_states = {"ArtifactVerifyReboot", "ArtifactRollbackReboot_Leave_00"},
		},

		StateTransitionsTestCase {
			.case_name = "Killed_in_ArtifactRollbackReboot_Leave_00",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Leave_00",
					"ArtifactReboot_Enter_00",
					"ArtifactReboot",
					"ArtifactVerifyReboot",
					"ArtifactReboot_Error_00",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback",
					"ArtifactRollback_Leave_00",
					"ArtifactRollbackReboot_Enter_00",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot_Leave_00",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot_Leave_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"rebooting",
					"failure",
				},
			.install_outcome = InstallOutcome::SuccessfulRollback,
			.error_states = {"ArtifactVerifyReboot"},
			.spont_reboot_states = {"ArtifactRollbackReboot_Leave_00"},
		},

		StateTransitionsTestCase {
			.case_name = "Error_in_ArtifactFailure_Leave_00",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Error_00",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback",
					"ArtifactRollback_Leave_00",
					"ArtifactRollbackReboot_Enter_00",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot_Leave_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"failure",
				},
			.install_outcome = InstallOutcome::SuccessfulRollback,
			.error_states = {"ArtifactInstall", "ArtifactFailure_Leave_00"},
		},

		StateTransitionsTestCase {
			.case_name = "Killed_in_ArtifactFailure_Leave_00",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Error_00",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback",
					"ArtifactRollback_Leave_00",
					"ArtifactRollbackReboot_Enter_00",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot_Leave_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"failure",
				},
			.install_outcome = InstallOutcome::SuccessfulRollback,
			.error_states = {"ArtifactInstall"},
			.spont_reboot_states = {"ArtifactFailure_Leave_00"},
		},

		StateTransitionsTestCase {
			.case_name = "Error_in_ArtifactCommit_Leave_00",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Leave_00",
					"ArtifactReboot_Enter_00",
					"ArtifactReboot",
					"ArtifactVerifyReboot",
					"ArtifactReboot_Leave_00",
					"ArtifactCommit_Enter_00",
					"ArtifactCommit",
					"ArtifactCommit_Leave_00",
					"ArtifactCommit_Error_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"rebooting",
					"installing",
					"failure",
				},
			.install_outcome = InstallOutcome::UnsuccessfulInstall,
			.error_states = {"ArtifactCommit_Leave_00"},
		},

		StateTransitionsTestCase {
			.case_name = "Killed_in_ArtifactCommit_Leave_00",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Leave_00",
					"ArtifactReboot_Enter_00",
					"ArtifactReboot",
					"ArtifactVerifyReboot",
					"ArtifactReboot_Leave_00",
					"ArtifactCommit_Enter_00",
					"ArtifactCommit",
					"ArtifactCommit_Leave_00",
					"ArtifactCommit_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"rebooting",
					"installing",
					"success",
				},
			.install_outcome = InstallOutcome::SuccessfulInstall,
			.spont_reboot_states = {"ArtifactCommit_Leave_00"},
		},

		StateTransitionsTestCase {
			.case_name = "Error_in_ArtifactCommit_Leave_00__no_reboot",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Leave_00",
					"ArtifactCommit_Enter_00",
					"ArtifactCommit",
					"ArtifactCommit_Leave_00",
					"ArtifactCommit_Error_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"installing", // Really commit
					"failure",
				},
			.install_outcome = InstallOutcome::UnsuccessfulInstall,
			.error_states = {"ArtifactCommit_Leave_00"},
			.reboot_disabled = true,
		},

		StateTransitionsTestCase {
			.case_name = "Killed_in_ArtifactCommit_Leave_00__no_reboot",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Leave_00",
					"ArtifactCommit_Enter_00",
					"ArtifactCommit",
					"ArtifactCommit_Leave_00",
					"ArtifactCommit_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"installing",
					"success",
				},
			.install_outcome = InstallOutcome::SuccessfulInstall,
			.spont_reboot_states = {"ArtifactCommit_Leave_00"},
			.reboot_disabled = true,
		},

		StateTransitionsTestCase {
			.case_name = "Break_out_of_error_loop",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Leave_00",
					"ArtifactReboot_Enter_00",
					"ArtifactReboot",
					"ArtifactVerifyReboot",
					"ArtifactReboot_Error_00",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback",
					"ArtifactRollback_Leave_00",
					"ArtifactRollbackReboot_Enter_00",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot",

					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot",
					// Truncated after maximum number of state transitions.
				},
			.status_log =
				{
					"downloading",
					"installing",
					"rebooting",
					"failure",
				},
			.install_outcome = InstallOutcome::UnsuccessfulInstall,
			.error_states = {"ArtifactVerifyReboot", "ArtifactVerifyRollbackReboot"},
			.error_forever = true,
		},

		StateTransitionsTestCase {
			.case_name = "Break_out_of_spontaneous_reboot_loop",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Leave_00",
					"ArtifactReboot_Enter_00",
					"ArtifactReboot",
					"ArtifactVerifyReboot",
					"ArtifactReboot_Error_00",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback",
					"ArtifactRollback_Leave_00",
					"ArtifactRollbackReboot_Enter_00",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot_Leave_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					// Truncated after maximum number of state transitions.
				},
			.status_log =
				{
					"downloading",
					"installing",
					"rebooting",
					"failure",
				},
			.install_outcome = InstallOutcome::UnsuccessfulInstall,
			.error_states = {"ArtifactVerifyReboot"},
			.spont_reboot_states = {"ArtifactFailure"},
			.spont_reboot_forever = true,
		},

		StateTransitionsTestCase {
			.case_name = "Hang_in_Download_state",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Error_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"failure",
				},
			.install_outcome = InstallOutcome::SuccessfulRollback,
			.hang_states = {"Download"},
		},

		StateTransitionsTestCase {
			.case_name = "Hang_in_ArtifactInstall",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Error_00",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback",
					"ArtifactRollback_Leave_00",
					"ArtifactRollbackReboot_Enter_00",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot_Leave_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"failure",
				},
			.install_outcome = InstallOutcome::SuccessfulRollback,
			.hang_states = {"ArtifactInstall"},
		},

		StateTransitionsTestCase {
			.case_name = "Temporary_failure_in_report_sending_after_reboot",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Leave_00",
					"ArtifactReboot_Enter_00",
					"ArtifactReboot",
					"ArtifactVerifyReboot",
					"ArtifactReboot_Leave_00",
					"ArtifactCommit_Enter_00",
					"ArtifactCommit",
					"ArtifactCommit_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					// "installing", // Missing because of fail_status_report_status below
					"rebooting",
					"installing",
					"success",
				},
			.install_outcome = InstallOutcome::SuccessfulInstall,
			.fail_status_report_count = 10,
			.fail_status_report_status = deployments::DeploymentStatus::Installing,
		},

		StateTransitionsTestCase {
			.case_name = "Permanent_failure_in_report_sending_after_reboot",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Leave_00",
					"ArtifactReboot_Enter_00",
					"ArtifactReboot",
					"ArtifactVerifyReboot",
					"ArtifactReboot_Leave_00",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback",
					"ArtifactRollback_Leave_00",
					"ArtifactRollbackReboot_Enter_00",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot_Leave_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					// "installing", // Missing because of fail_status_report_status below
					"rebooting",
					"failure",
				},
			.install_outcome = InstallOutcome::SuccessfulRollback,
			.fail_status_report_count = 100,
			.fail_status_report_status = deployments::DeploymentStatus::Installing,
		},

		StateTransitionsTestCase {
			.case_name = "Aborted_update",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Leave_00",
					"ArtifactReboot_Enter_00",
					"ArtifactReboot",
					"ArtifactVerifyReboot",
					"ArtifactReboot_Leave_00",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback",
					"ArtifactRollback_Leave_00",
					"ArtifactRollbackReboot_Enter_00",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot_Leave_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					// "installing", // Missing because of fail_status_report_status below
					"rebooting",
					"failure",
				},
			.install_outcome = InstallOutcome::SuccessfulRollback,
			.fail_status_report_count = 100,
			.fail_status_report_status = deployments::DeploymentStatus::Installing,
			.fail_status_aborted = true,
			// When aborting an update, it should react immediately.
			.long_retry_times = true,
		},

		StateTransitionsTestCase {
			.case_name = "Killed_in_ArtifactReboot_with_schema_update",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Leave_00",
					"ArtifactReboot_Enter_00",
					"ArtifactReboot",
					"ArtifactVerifyReboot",
					"ArtifactReboot_Leave_00",
					"ArtifactCommit_Enter_00",
					"ArtifactCommit",
					"ArtifactCommit_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"rebooting",
					"installing",
					"success",
				},
			.install_outcome = InstallOutcome::SuccessfulInstall,
			.spont_reboot_states = {"ArtifactReboot"},
			.do_schema_update_at_invocation = 1,
		},

		StateTransitionsTestCase {
			.case_name = "Completely_non_writable_database",
			.state_chain =
				{
					// No states at all, because we don't even get to the point
					// of calling update modules.
					"Download_Error_00",
				},
			.status_log =
				{
					"downloading",
					"failure",
				},
			.install_outcome = InstallOutcome::SuccessfulRollback,
			.use_non_writable_db_after_n_writes = 0,
		},

		StateTransitionsTestCase {
			.case_name = "Non_writable_database_in_ArtifactInstall_Enter",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Error_00",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback",
					"ArtifactRollback_Leave_00",
					"ArtifactRollbackReboot_Enter_00",
					"ArtifactRollbackReboot",
					"ArtifactVerifyRollbackReboot",
					"ArtifactRollbackReboot_Leave_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"failure",
				},
			.install_outcome = InstallOutcome::SuccessfulRollback,
			.use_non_writable_db_after_n_writes = 3,
		},

		StateTransitionsTestCase {
			.case_name = "Broken_Download",
			.state_chain =
				{
					// No states at all, because we don't even get to the point
					// of calling update modules.
					"Download_Enter_00",
					"Download_Error_00",
				},
			.status_log =
				{
					"downloading",
					"failure",
				},
			.install_outcome = InstallOutcome::SuccessfulRollback,
			.broken_download = true,
		},

		StateTransitionsTestCase {
			.case_name = "Error_in_NeedsArtifactReboot",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Leave_00",
					"ArtifactRollback_Enter_00",
					"ArtifactRollback",
					"ArtifactRollback_Leave_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"failure",
				},
			.install_outcome = InstallOutcome::UnsuccessfulInstall,
			.error_states = {"NeedsArtifactReboot"},
			.error_forever = true,
		},

		StateTransitionsTestCase {
			.case_name = "Error_in_SupportsRollback",
			.state_chain =
				{
					"Download_Enter_00",
					"ProvidePayloadFileSizes",
					"Download",
					"Download_Leave_00",
					"ArtifactInstall_Enter_00",
					"ArtifactInstall",
					"ArtifactInstall_Error_00",
					"ArtifactFailure_Enter_00",
					"ArtifactFailure",
					"ArtifactFailure_Leave_00",
					"Cleanup",
				},
			.status_log =
				{
					"downloading",
					"installing",
					"failure",
				},
			.install_outcome = InstallOutcome::UnsuccessfulInstall,
			.error_states = {"ArtifactInstall", "SupportsRollback"},
			.error_forever = true,
		},

		StateTransitionsTestCase {
			.case_name = "Empty_payload_artifact",
			.state_chain =
				{
					"Download_Enter_00", "Download_Leave_00",
					// No visible Cleanup, because there is no Update Module to
					// run. We do enter the state internally though.
				},
			.status_log =
				{
					"downloading",
					"success",
				},
			.install_outcome = InstallOutcome::SuccessfulInstall,
			.empty_payload_artifact = true,
		},
	};
}

class StateDeathTest : public testing::TestWithParam<StateTransitionsTestCase> {
public:
	void SetUp() override {
		{
			processes::Process proc({
				"mender-artifact",
				"write",
				"module-image",
				"--type",
				"test-module",
				"--device-type",
				"test-type",
				"--artifact-name",
				"artifact-name",
				"--output-path",
				ArtifactPath(),
			});
			auto err = proc.Run();
			ASSERT_EQ(err, error::NoError) << err.String();
		}
		{
			processes::Process proc({
				"mender-artifact",
				"write",
				"bootstrap-artifact",
				"--device-type",
				"test-type",
				"--artifact-name",
				"artifact-name",
				"--output-path",
				EmptyPayloadArtifactPath(),
			});
			auto err = proc.Run();
			ASSERT_EQ(err, error::NoError) << err.String();
		}
	}

	string ArtifactPath() const {
		return path::Join(tmpdir_.Path(), "artifact.mender");
	}

	string EmptyPayloadArtifactPath() const {
		return path::Join(tmpdir_.Path(), "bootstrap.mender");
	}

private:
	mtesting::TemporaryDirectory tmpdir_;
};

INSTANTIATE_TEST_SUITE_P(
	Regular_Non_Update_State_Tests,
	StateDeathTest,
	::testing::ValuesIn(idle_and_sync_test_cases),
	[](const testing::TestParamInfo<StateTransitionsTestCase> &test_case) {
		return test_case.param.case_name;
	});


INSTANTIATE_TEST_SUITE_P(
	,
	StateDeathTest,
	::testing::ValuesIn(GenerateStateTransitionsTestCases()),
	[](const testing::TestParamInfo<StateTransitionsTestCase> &test_case) {
		return test_case.param.case_name;
	});

void MakeTestUpdateModule(
	const StateTransitionsTestCase &test_case, const string &path, const string &log_path) {
	ofstream f(path);

	f << R"(#!/bin/bash
case "$1" in
    SupportsRollback|NeedsArtifactReboot)
        # Ignore these two, they are not important for the flow.
        ;;
    *)
        echo "$1" >> )"
	  << log_path << R"(
        ;;
esac

if [ "$1" = "SupportsRollback" ]; then
    echo )"
	  << (test_case.rollback_disabled ? "No" : "Yes") << R"(
fi

if [ "$1" = "NeedsArtifactReboot" ]; then
    echo )"
	  << (test_case.reboot_disabled ? "No" : "Yes") << R"(
fi

if [ "$1" = "ProvidePayloadFileSizes" ]; then
    echo )"
	  << ProvideFileSizesToString(test_case.provide_file_sizes) << R"(
fi
)";

	// Kill parent (mender) in specified state
	for (auto &state : test_case.spont_reboot_states) {
		f << R"(
if [ "$1" = ")"
		  << state << R"(" ]; then
)";

		// Prevent spontaneous rebooting forever.
		if (!test_case.spont_reboot_forever) {
			f << R"(
    if [ ! -e "$2/tmp/$1.already-killed" ]; then
        touch "$2/tmp/$1.already-killed"
        kill -9 $PPID
    fi
)";
		} else {
			f << R"(
    kill -9 $PPID
)";
		}
		f << R"(
fi
)";
	}

	// Produce error in specified state.
	for (auto &state : test_case.error_states) {
		f << R"(
if [ "$1" = ")"
		  << state << R"(" ]; then
)";
		// Prevent returning same error forever.
		if (!test_case.error_forever) {
			f << R"(
    if [ ! -e "$2/tmp/$1.already-errored" ]; then
        touch "$2/tmp/$1.already-errored"
        exit 1
    fi
)";
		} else {
			f << R"(
    exit 1
)";
		}
		f << R"(
fi
)";
	}

	// Hang in specified state
	for (auto &state : test_case.hang_states) {
		f << R"(
if [ "$1" = ")"
		  << state << R"(" ]; then
    sleep 120
fi
)";
	}

	ASSERT_TRUE(f.good());
	error_code ec;
	fs::permissions(path, fs::perms::owner_all, ec);
	ASSERT_FALSE(ec);
}

vector<string> MakeTestArtifactScripts(
	const StateTransitionsTestCase &test_case, const string &tmpdir, const string &log_path) {
	const auto rootfs_scripts_list = vector<string> {
		"Idle",
		"Sync",
	};
	auto state_script_list = vector<string> {
		"ProvidePayloadFileSizes",
		"Download",
		"ArtifactInstall",
		"ArtifactReboot",
		"ArtifactCommit",
		"ArtifactRollback",
		"ArtifactRollbackReboot",
		"ArtifactFailure",
	};

	if (test_case.generate_idle_sync_scripts) {
		state_script_list.insert(
			state_script_list.end(), rootfs_scripts_list.cbegin(), rootfs_scripts_list.cend());
	}

	const auto scripts_dir = path::Join(tmpdir, "scripts");
	error_code ec;
	EXPECT_TRUE(fs::create_directories(scripts_dir, ec)) << ec.message();

	{
		ofstream version_file(path::Join(scripts_dir, "version"));
		version_file << "3";
		EXPECT_TRUE(version_file.good());
	}

	vector<string> artifact_scripts;

	for (const auto &state : state_script_list) {
		for (const auto &enter_leave : vector<string> {"Enter", "Leave", "Error"}) {
			const auto script_file = state + "_" + enter_leave + "_00";
			const auto script_path = path::Join(scripts_dir, script_file);
			if (state != "Download") {
				artifact_scripts.push_back(script_path);
			}

			ofstream f(script_path);

			f << R"(#!/bin/bash
echo )" << script_file
			  << " >> " << log_path << R"(
)";

			f << R"(
echo )" << script_file;

			auto &error_states = test_case.error_states;
			if (find(error_states.begin(), error_states.end(), script_file) != error_states.end()) {
				if (!test_case.error_forever) {
					f << R"(
if [ ! -e ")" << tmpdir
					  << "/" << script_file << R"(.already-errored" ]; then
    touch ")" << tmpdir
					  << "/" << script_file << R"(.already-errored"
    exit 1
fi
)";
				} else {
					f << R"(
exit 1
)";
				}
			}

			auto &spont_reboot_states = test_case.spont_reboot_states;
			if (find(spont_reboot_states.begin(), spont_reboot_states.end(), script_file)
				!= spont_reboot_states.end()) {
				if (!test_case.spont_reboot_forever) {
					f << R"(
if [ ! -e ")" << tmpdir
					  << "/" << script_file << R"(.already-killed" ]; then
    touch ")" << tmpdir
					  << "/" << script_file << R"(.already-killed"
    kill -9 $PPID
fi
)";
				} else {
					f << R"(
kill -9 $PPID
)";
				}
			}

			f << R"(
exit 0
)";

			EXPECT_TRUE(f.good());

			// Make the script executable
			int ret {chmod(script_path.c_str(), S_IRUSR | S_IWUSR | S_IXUSR)};
			EXPECT_EQ(ret, 0);
		}
	}

	return artifact_scripts;
}

class NoopInventoryClient : virtual public inventory::InventoryAPI {
	error::Error PushData(
		const string &inventory_generators_dir,
		const string &server_url,
		events::EventLoop &loop,
		http::Client &client,
		inventory::APIResponseHandler api_handler) override {
		api_handler(error::NoError);
		return error::NoError;
	}
};

class TestDeploymentClient : virtual public deployments::DeploymentAPI {
public:
	TestDeploymentClient(
		events::EventLoop &event_loop,
		const string &artifact_url,
		string status_log_path = "",
		int fail_status_report_count = 0,
		deployments::DeploymentStatus fail_status_report_status =
			deployments::DeploymentStatus::Success,
		bool fail_status_aborted = false) :
		event_loop_(event_loop),
		artifact_url_(artifact_url),
		status_log_path_(status_log_path),
		fail_status_report_count_(fail_status_report_count),
		fail_status_report_status_(fail_status_report_status),
		fail_status_aborted_(fail_status_aborted) {
	}

	error::Error CheckNewDeployments(
		context::MenderContext &ctx,
		const string &server_url,
		http::Client &client,
		deployments::CheckUpdatesAPIResponseHandler api_handler) override {
		event_loop_.Post([this, api_handler]() {
			auto exp = json::Load(R"({
  "id": ")" + deployment_id_ + R"(",
  "artifact": {
    "artifact_name": "artifact-name",
    "source": {
      "uri": ")" + artifact_url_ + R"(",
      "expire": "2016-03-11T13:03:17.063493443Z"
    },
    "device_types_compatible": [
      "test-type"
    ],
    "update_control_map": {}
  }
})");
			api_handler(exp.value());
		});
		return error::NoError;
	}
	error::Error PushStatus(
		const string &deployment_id,
		deployments::DeploymentStatus status,
		const string &substate,
		const string &server_url,
		http::Client &client,
		deployments::StatusAPIResponseHandler api_handler) override {
		event_loop_.Post([this, status, api_handler]() {
			if (fail_status_report_status_ == status && fail_status_report_count_ > 0) {
				fail_status_report_count_--;
				if (fail_status_aborted_) {
					api_handler(deployments::MakeError(
						deployments::DeploymentAbortedError, "Cannot send status"));
				} else {
					api_handler(error::Error(
						make_error_condition(errc::host_unreachable), "Cannot send status"));
				}
				return;
			}

			if (status_log_path_ != "") {
				ofstream f(status_log_path_, ios::out | ios::app);
				f << deployments::DeploymentStatusString(status) << endl;
				if (!f) {
					api_handler(error::Error(
						generic_category().default_error_condition(errno),
						"Could not do PushStatus"));
				}
			}

			api_handler(error::NoError);
		});
		return error::NoError;
	}

	error::Error PushLogs(
		const string &deployment_id,
		const string &log_file_path,
		const string &server_url,
		http::Client &client,
		deployments::LogsAPIResponseHandler api_handler) override {
		// Just save the log file name so they can be checked later.
		log_files.push_back(log_file_path);
		event_loop_.Post([api_handler]() { api_handler(error::NoError); });
		return error::NoError;
	}

	void SetDeploymentId(const string &id) {
		deployment_id_ = id;
	}

	vector<string> log_files;

private:
	events::EventLoop &event_loop_;
	string artifact_url_;
	string status_log_path_;

	int fail_status_report_count_;
	deployments::DeploymentStatus fail_status_report_status_;
	bool fail_status_aborted_;

	string deployment_id_ {DEPLOYMENT_ID};
};

// Normal DB, but writes can fail.
class NonWritableDb : virtual public kv_db::KeyValueDatabase {
public:
	NonWritableDb(kv_db::KeyValueDatabase &db, int max_write_count) :
		db_(db),
		write_count_(0),
		max_write_count_(max_write_count) {
	}

	expected::ExpectedBytes Read(const string &key) override {
		return db_.Read(key);
	}

	error::Error Write(const string &key, const vector<uint8_t> &value) override {
		if (write_count_++ >= max_write_count_) {
			return error::Error(make_error_condition(errc::io_error), "Test error");
		}
		return db_.Write(key, value);
	}

	error::Error Remove(const string &key) override {
		if (write_count_++ >= max_write_count_) {
			return error::Error(make_error_condition(errc::io_error), "Test error");
		}
		return db_.Remove(key);
	}

	error::Error WriteTransaction(function<error::Error(Transaction &)> txnFunc) override {
		if (write_count_++ >= max_write_count_) {
			return error::Error(make_error_condition(errc::io_error), "Test error");
		}
		return db_.WriteTransaction(txnFunc);
	}

	error::Error ReadTransaction(function<error::Error(Transaction &)> txnFunc) override {
		return db_.ReadTransaction(txnFunc);
	}

private:
	kv_db::KeyValueDatabase &db_;
	int write_count_;
	int max_write_count_;
};

class NonWritableDbContext : public context::MenderContext {
public:
	NonWritableDbContext(conf::MenderConfig &config, int max_write_count) :
		MenderContext(config),
		test_db_(MenderContext::GetMenderStoreDB(), max_write_count) {
	}

	kv_db::KeyValueDatabase &GetMenderStoreDB() override {
		return test_db_;
	}

private:
	NonWritableDb test_db_;
};

void StateTransitionsTestSubProcess(
	const string &tmpdir, const StateDeathTest &test, const string &status_log_path) {
	// Make sure everything is destroyed before calling exit() below. This is important due to
	// exit handlers which should not be invoked while these objects are still alive.
	{
		conf::MenderConfig config {};
		config.module_timeout_seconds = 2;
		config.paths.SetDataStore(tmpdir);
		config.paths.SetArtScriptsPath(path::Join(tmpdir, "scripts"));
		config.paths.SetRootfsScriptsPath(path::Join(tmpdir, "scripts"));

		string artifact_path;
		if (test.GetParam().empty_payload_artifact) {
			artifact_path = test.EmptyPayloadArtifactPath();
		} else {
			artifact_path = test.ArtifactPath();
		}

		mtesting::HttpFileServer server(path::DirName(artifact_path));
		string artifact_url;
		if (test.GetParam().broken_download) {
			artifact_url = http::JoinUrl(server.GetBaseUrl(), "nonexisting.mender");
		} else {
			artifact_url = http::JoinUrl(server.GetBaseUrl(), path::BaseName(artifact_path));
		}

		if (test.GetParam().device_type_mismatch) {
			ofstream f(path::Join(tmpdir, "device_type"));
			f << "device_type=mismatch-type\n";
			ASSERT_IN_DEATH_TEST(f.good()) << "Failed to write mismatching device type";
		}

		unique_ptr<context::MenderContext> main_context;
		if (test.GetParam().use_non_writable_db_after_n_writes >= 0) {
			main_context.reset(new NonWritableDbContext(
				config, test.GetParam().use_non_writable_db_after_n_writes));
		} else {
			main_context.reset(new context::MenderContext(config));
		}
		auto err = main_context->Initialize();
		ASSERT_IN_DEATH_TEST(err == error::NoError) << err.String();
		main_context->GetConfig().paths.SetModulesPath(tmpdir);
		main_context->GetConfig().paths.SetModulesWorkPath(tmpdir);

		mtesting::TestEventLoop event_loop;

		Context ctx(*main_context, event_loop);

		// Avoid waiting by setting a short retry time.
		chrono::milliseconds retry_time = chrono::milliseconds(1);
		if (test.GetParam().long_retry_times) {
			retry_time = chrono::minutes(1);
		}
		StateMachine state_machine(ctx, event_loop, retry_time);
		state_machine.LoadStateFromDb();

		ctx.inventory_client = make_shared<NoopInventoryClient>();
		ctx.deployment_client = make_shared<TestDeploymentClient>(
			event_loop,
			artifact_url,
			status_log_path,
			test.GetParam().fail_status_report_count,
			test.GetParam().fail_status_report_status,
			test.GetParam().fail_status_aborted);

		state_machine.StopAfterDeployment();
		err = state_machine.Run();
		ASSERT_IN_DEATH_TEST(err == error::NoError) << err.String();
	}

	std::exit(0);
}

void DoSchemaUpdate(kv_db::KeyValueDatabase &db) {
	auto exp_bytes = db.Read(context::MenderContext::state_data_key);
	ASSERT_TRUE(exp_bytes) << exp_bytes.error();
	string state_data = common::StringFromByteVector(exp_bytes.value());

	// Store the original under the uncommitted key.
	auto err = db.Write(
		context::MenderContext::state_data_key_uncommitted,
		common::ByteVectorFromString(state_data));
	ASSERT_EQ(err, error::NoError);

	regex version_matcher {R"("Version": *[0-9]+)"};
	state_data = regex_replace(state_data, version_matcher, R"("Version":9876)");

	// Store the incompatible version under the original key, pretending that this is an upgrade
	// from a version we don't support.
	err =
		db.Write(context::MenderContext::state_data_key, common::ByteVectorFromString(state_data));
	ASSERT_EQ(err, error::NoError);
}


TEST_P(StateDeathTest, StateTransitionsTest) {
	// This test requires "fast" mode. The reason is that since we need to run a sub process
	// multiple times, we have to use "fork", we cannot use the start-from-scratch approach that
	// the "threadsafe" mode uses. Also, our temporary directory would not be the same across
	// multiple runs. See "Death Test Styles" in the Googletest documentation for more
	// information.
	GTEST_FLAG_SET(death_test_style, "fast");

	mtesting::TemporaryDirectory tmpdir;

	{
		ofstream f(path::Join(tmpdir.Path(), "device_type"));
		f << "device_type=test-type\n";
		ASSERT_TRUE(f.good());
	}

	const string state_log_path = path::Join(tmpdir.Path(), "state.log");
	const string update_module_name = "test-module";
	const string update_module_path = path::Join(tmpdir.Path(), update_module_name);
	const string status_log_path = path::Join(tmpdir.Path(), "status.log");

	{
		// We don't care about existence, only content, so pre-create these files in order
		// to make FileContains* functions happy.
		ofstream state_log(state_log_path);
		ofstream status_log(status_log_path);
	}

	const auto artifact_scripts =
		MakeTestArtifactScripts(GetParam(), tmpdir.Path(), state_log_path);
	ASSERT_FALSE(::testing::Test::HasFailure());

	MakeTestUpdateModule(GetParam(), update_module_path, state_log_path);
	ASSERT_FALSE(::testing::Test::HasFailure());

	conf::MenderConfig config {};
	config.paths.SetDataStore(tmpdir.Path());

	context::MenderContext main_context(config);
	auto err = main_context.Initialize();
	ASSERT_EQ(err, error::NoError) << err.String();

	// Initialize initial database content.
	err = main_context.GetMenderStoreDB().Write(
		main_context.artifact_name_key, common::ByteVectorFromString("old_name"));
	ASSERT_EQ(err, error::NoError) << err.String();

	int count = 0;
	for (bool finished = false; !finished; count++) {
		if (GetParam().do_schema_update_at_invocation == count) {
			DoSchemaUpdate(main_context.GetMenderStoreDB());
			ASSERT_FALSE(::testing::Test::HasFailure());
		}

		// Annoyingly, this doesn't produce any output when a later assert fails. To enable
		// output, change the debug variable below. Be aware that this in itself causes the
		// test to fail, but you can still see the results of later asserts.
		EXPECT_EXIT(
			StateTransitionsTestSubProcess(tmpdir.Path(), *this, status_log_path),
			[&finished](int arg) {
				bool debug = false;
				bool clean_exit = testing::ExitedWithCode(0)(arg);
				bool sig_kill = testing::KilledBySignal(9)(arg);
				finished = clean_exit || !sig_kill;
				bool success = clean_exit || sig_kill;
				return !debug && success;
			},
			"");
		ASSERT_LT(count, 100) << "Looped too many times";
	}

	auto exp_provides = main_context.LoadProvides();
	ASSERT_TRUE(exp_provides) << exp_provides.error().String();
	auto &provides = exp_provides.value();

	switch (GetParam().install_outcome) {
	case InstallOutcome::SuccessfulInstall:
		EXPECT_EQ(provides["artifact_name"], "artifact-name");
		break;
	case InstallOutcome::SuccessfulRollback:
		EXPECT_EQ(provides["artifact_name"], "old_name");
		break;
	case InstallOutcome::UnsuccessfulInstall:
		EXPECT_EQ(
			provides["artifact_name"], "artifact-name" + main_context.broken_artifact_name_suffix);
		break;
	}

	auto content = common::JoinStrings(GetParam().state_chain, "\n") + "\n";
	if (content == "\n") {
		content = "";
	}
	EXPECT_TRUE(mtesting::FileContainsExactly(state_log_path, content));

	content = common::JoinStrings(GetParam().status_log, "\n") + "\n";
	if (content == "\n") {
		content = "";
	}
	EXPECT_TRUE(mtesting::FileContainsExactly(status_log_path, content));
}

class StateTest : public testing::Test {
public:
	void SetUp() override {
		processes::Process proc({
			"mender-artifact",
			"write",
			"module-image",
			"--type",
			"test-module",
			"--device-type",
			"test-type",
			"--artifact-name",
			"artifact-name",
			"--output-path",
			ArtifactPath(),
		});
		auto err = proc.Run();
		ASSERT_EQ(err, error::NoError) << err.String();
	}

	string ArtifactPath() const {
		return path::Join(tmpdir_.Path(), "artifact.mender");
	}

private:
	mtesting::TemporaryDirectory tmpdir_;
};

TEST_F(StateTest, DeploymentLogging) {
	mtesting::TemporaryDirectory tmpdir;
	conf::MenderConfig config;
	config.paths.SetDataStore(tmpdir.Path());
	context::MenderContext main_context(config);
	auto err = main_context.Initialize();
	mtesting::TestEventLoop event_loop;
	Context ctx(main_context, event_loop);

	mtesting::HttpFileServer server(path::DirName(ArtifactPath()));

	auto artifact_url = http::JoinUrl(server.GetBaseUrl(), path::BaseName(ArtifactPath()));
	auto deployment_client = make_shared<TestDeploymentClient>(event_loop, artifact_url);
	ctx.deployment_client = deployment_client;
	ctx.inventory_client = make_shared<NoopInventoryClient>();

	{
		StateMachine state_machine(ctx, event_loop);
		state_machine.StopAfterDeployment();
		err = state_machine.Run();
		EXPECT_EQ(err, error::NoError);
	}

	auto deployment_log = path::Join(tmpdir.Path(), "deployments.0000." DEPLOYMENT_ID ".log");
	EXPECT_TRUE(mtesting::FileContains(deployment_log, "Running Mender client"));
	EXPECT_TRUE(
		mtesting::FileContains(deployment_log, "Deployment with ID " DEPLOYMENT_ID " started"));
	ASSERT_EQ(deployment_client->log_files.size(), 1);
	EXPECT_EQ(deployment_client->log_files[0], deployment_log);

	deployment_client->log_files.clear();

	string new_id = "01234567-89ab-cdef-0123-456789abcdef";
	deployment_client->SetDeploymentId(new_id);

	{
		StateMachine state_machine(ctx, event_loop);
		state_machine.StopAfterDeployment();
		err = state_machine.Run();
		EXPECT_EQ(err, error::NoError);
	}

	deployment_log = path::Join(tmpdir.Path(), "deployments.0000." + new_id + ".log");
	EXPECT_TRUE(mtesting::FileContains(deployment_log, "Running Mender client"));
	EXPECT_TRUE(
		mtesting::FileContains(deployment_log, "Deployment with ID " + new_id + " started"));
	ASSERT_EQ(deployment_client->log_files.size(), 1);
	EXPECT_EQ(deployment_client->log_files[0], deployment_log);

	auto moved_deployment_log = path::Join(tmpdir.Path(), "deployments.0001." DEPLOYMENT_ID ".log");
	EXPECT_TRUE(mtesting::FileContains(moved_deployment_log, "Running Mender client"));
	EXPECT_TRUE(mtesting::FileContains(
		moved_deployment_log, "Deployment with ID " DEPLOYMENT_ID " started"));

	auto no_such_deployment_log =
		path::Join(tmpdir.Path(), "deployments.0002." DEPLOYMENT_ID ".log");
	EXPECT_FALSE(mtesting::FileContains(no_such_deployment_log, "Running Mender client"));
}

TEST(SignalHandlingTests, SigquitHandlingTest) {
	mtesting::TemporaryDirectory tmpdir;
	conf::MenderConfig config {};
	config.paths.SetDataStore(tmpdir.Path());

	context::MenderContext main_context {config};
	auto err = main_context.Initialize();
	mtesting::TestEventLoop event_loop {chrono::seconds {3}};
	Context ctx {main_context, event_loop};

	events::Timer signal_timer {event_loop};
	signal_timer.AsyncWait(chrono::seconds {1}, [](error::Error err) { raise(SIGQUIT); });

	StateMachine state_machine {ctx, event_loop};
	err = state_machine.Run();
	ASSERT_EQ(err, error::NoError);

	// Nothing more to check here, either SIGQUIT is handled properly and
	// terminates the loop or the TestEventLoop's timer kicks in and marks this
	// test as timing out and thus failing.
}

TEST(SubmitInventoryTests, SubmitInventoryStateTest) {
	mtesting::TestEventLoop loop;

	http::ClientConfig client_config;
	http::Client client {client_config, loop};

	conf::MenderConfig config;
	config.inventory_poll_interval_seconds = 1;
	context::MenderContext main_context {config};
	auto err = main_context.Initialize();

	Context ctx {main_context, loop};

	int n_submissions = 0;
	class MockInventoryClient : public inventory::InventoryAPI {
	public:
		MockInventoryClient(int &recorder, conf::MenderConfig &config) :
			recorder_ {recorder},
			config_ {config} {};

		error::Error PushData(
			const string &inventory_generators_dir,
			const string &server_url,
			events::EventLoop &loop,
			http::Client &client,
			inventory::APIResponseHandler api_handler) override {
			recorder_++;
			api_handler(error::NoError);
			EXPECT_EQ(inventory_generators_dir, config_.paths.GetInventoryScriptsDir());
			if (recorder_ == 2) {
				loop.Stop();
			}
			return error::NoError;
		};

	private:
		int &recorder_;
		conf::MenderConfig &config_;
	} mock_inventory_client {n_submissions, config};

	ctx.inventory_client = shared_ptr<inventory::InventoryAPI>(
		&mock_inventory_client, [](inventory::InventoryAPI *cl) {});

	StateMachine state_machine {ctx, loop};
	err = state_machine.Run();
	ASSERT_EQ(err, error::NoError);

	EXPECT_EQ(n_submissions, 2);
}

} // namespace daemon
} // namespace update
} // namespace mender
