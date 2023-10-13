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

#include <mender-update/cli/cli.hpp>

#include <iostream>

#include <common/conf.hpp>
#include <common/error.hpp>
#include <common/expected.hpp>
#include <common/cli.hpp>
#include <mender-version.h>

namespace mender {
namespace update {
namespace cli {

namespace conf = mender::common::conf;
namespace error = mender::common::error;
namespace expected = mender::common::expected;
namespace cli = mender::common::cli;

const int NoUpdateInProgressExitStatus = 2;
const int RebootExitStatus = 4;

const cli::Command cmd_check_update {
	.name = "check-update",
	.description = "Force update check",
};

const cli::Command cmd_commit {
	.name = "commit",
	.description = "Commit current Artifact. Returns (2) if no update in progress",
};

const cli::Command cmd_daemon {
	.name = "daemon",
	.description = "Start the client as a background service",
};

const cli::Command cmd_install {
	.name = "install",
	.description = "Mender Artifact to install - local file or a URL",
	.options =
		{
			cli::Option {
				.long_option = "reboot-exit-code",
				.description =
					"Return exit code 4 if a manual reboot is required after the Artifact installation",
			},
		},
};

const cli::Command cmd_rollback {
	.name = "rollback",
	.description = "Rollback current Artifact. Returns (2) if no update in progress",
};

const cli::Command cmd_send_inventory {
	.name = "send-inventory",
	.description = "Force inventory update",
};

const cli::Command cmd_show_artifact {
	.name = "show-artifact",
	.description = "Print the current artifact name to the command line and exit",
};

const cli::Command cmd_show_provides {
	.name = "show-provides",
	.description = "Print the current provides to the command line and exit",
};

const conf::Paths default_paths {};

const cli::App cli_mender_update = {
	.name = "mender-update",
	.short_description = "manage and start Mender Update",
	.long_description =
		R"(mender-update integrates both the mender-auth daemon and commands for manually
   performing tasks performed by the daemon (see list of COMMANDS below).

Global flag remarks:
   - Supported log levels incudes: 'trace', 'debug', 'info', 'warning', 'error', and
     'fatal'.

Environment variables:
   - MENDER_CONF_DIR - configuration (default: )"
		+ default_paths.GetPathConfDir() + R"().
   - MENDER_DATA_DIR - identity, inventory and update modules (default: )"
		+ default_paths.GetPathDataDir() + R"().
   - MENDER_DATASTORE_DIR - runtime datastore (default: )"
		+ default_paths.GetDataStore() + R"().)",
	.version = string {MENDER_VERSION},
	.commands =
		{
			cmd_check_update,
			cmd_commit,
			cmd_daemon,
			cmd_install,
			cmd_rollback,
			cmd_send_inventory,
			cmd_show_artifact,
			cmd_show_provides,
		},
	.global_options =
		{
			cli::Option {
				.long_option = "config",
				.short_option = "c",
				.description = "Configuration FILE path",
				.default_value = default_paths.GetConfFile(),
				.parameter = "FILE"},
			cli::Option {
				.long_option = "fallback-config",
				.short_option = "b",
				.description = "Fallback configuration FILE path",
				.default_value = default_paths.GetFallbackConfFile(),
				.parameter = "FILE"},
			cli::Option {
				.long_option = "data",
				.short_option = "d",
				.description = "Mender state data DIRECTORY path",
				.default_value = default_paths.GetPathDataDir(),
				.parameter = "DIR"},
			cli::Option {
				.long_option = "log-file",
				.short_option = "L",
				.description = "FILE to log to",
				.parameter = "FILE"},
			cli::Option {
				.long_option = "log-level",
				.short_option = "l",
				.description = "Set logging level",
				.default_value = "info",
			},
			cli::Option {
				.long_option = "trusted-certs",
				.short_option = "E",
				.description = "Trusted server certificates FILE path",
				.parameter = "FILE"},
			cli::Option {
				.long_option = "skipverify",
				.description = "Skip certificate verification",
			},
		},
};

ExpectedActionPtr ParseUpdateArguments(
	vector<string>::const_iterator start, vector<string>::const_iterator end) {
	if (start == end) {
		return expected::unexpected(conf::MakeError(conf::InvalidOptionsError, "Need an action"));
	}

	conf::CmdlineOptionsIterator opts_iter(
		start + 1,
		end,
		{},
		{
			"--help",
			"-h",
		});
	auto ex_opt_val = opts_iter.Next();

	bool help_arg = false;
	while (ex_opt_val && ((ex_opt_val.value().option != "") || (ex_opt_val.value().value != ""))) {
		auto opt_val = ex_opt_val.value();
		if ((opt_val.option == "--help") || (opt_val.option == "-h")) {
			help_arg = true;
			break;
		}
		ex_opt_val = opts_iter.Next();
	}

	if (help_arg) {
		cli::PrintCliCommandHelp(cli_mender_update, start[0]);
		return expected::unexpected(error::MakeError(error::ExitWithSuccessError, ""));
	}

	if (start[0] == "show-artifact") {
		unordered_set<string> options {};
		conf::CmdlineOptionsIterator iter(start + 1, end, options, options);
		auto arg = iter.Next();
		if (!arg) {
			return expected::unexpected(arg.error());
		}

		return make_shared<ShowArtifactAction>();
	} else if (start[0] == "show-provides") {
		unordered_set<string> options {};
		conf::CmdlineOptionsIterator iter(start + 1, end, options, options);
		auto arg = iter.Next();
		if (!arg) {
			return expected::unexpected(arg.error());
		}

		return make_shared<ShowProvidesAction>();
	} else if (start[0] == "install") {
		unordered_set<string> options {};
		conf::CmdlineOptionsIterator iter(start + 1, end, options, {"--reboot-exit-code"});
		iter.SetArgumentsMode(conf::ArgumentsMode::AcceptBareArguments);

		string filename;
		bool reboot_exit_code = false;
		while (true) {
			auto arg = iter.Next();
			if (!arg) {
				return expected::unexpected(arg.error());
			}

			auto value = arg.value();
			if (value.option == "--reboot-exit-code") {
				reboot_exit_code = true;
				continue;
			} else if (value.option != "") {
				return expected::unexpected(
					conf::MakeError(conf::InvalidOptionsError, "No such option: " + value.option));
			}

			if (value.value != "") {
				if (filename != "") {
					return expected::unexpected(conf::MakeError(
						conf::InvalidOptionsError, "Too many arguments: " + value.value));
				} else {
					filename = value.value;
				}
			} else {
				if (filename == "") {
					return expected::unexpected(
						conf::MakeError(conf::InvalidOptionsError, "Need a path to an artifact"));
				} else {
					break;
				}
			}
		}

		return make_shared<InstallAction>(filename, reboot_exit_code);
	} else if (start[0] == "commit") {
		unordered_set<string> options {};
		conf::CmdlineOptionsIterator iter(start + 1, end, options, options);
		auto arg = iter.Next();
		if (!arg) {
			return expected::unexpected(arg.error());
		}

		return make_shared<CommitAction>();
	} else if (start[0] == "rollback") {
		unordered_set<string> options {};
		conf::CmdlineOptionsIterator iter(start + 1, end, options, options);
		auto arg = iter.Next();
		if (!arg) {
			return expected::unexpected(arg.error());
		}

		return make_shared<RollbackAction>();
	} else if (start[0] == "daemon") {
		unordered_set<string> options {};
		conf::CmdlineOptionsIterator iter(start + 1, end, options, options);
		auto arg = iter.Next();
		if (!arg) {
			return expected::unexpected(arg.error());
		}

		return make_shared<DaemonAction>();
	} else if (start[0] == "send-inventory") {
		unordered_set<string> options {};
		conf::CmdlineOptionsIterator iter(start + 1, end, options, options);
		auto arg = iter.Next();
		if (!arg) {
			return expected::unexpected(arg.error());
		}

		return make_shared<SendInventoryAction>();
	} else if (start[0] == "check-update") {
		unordered_set<string> options {};
		conf::CmdlineOptionsIterator iter(start + 1, end, options, options);
		auto arg = iter.Next();
		if (!arg) {
			return expected::unexpected(arg.error());
		}

		return make_shared<CheckUpdateAction>();
	} else {
		return expected::unexpected(
			conf::MakeError(conf::InvalidOptionsError, "No such action: " + start[0]));
	}
}

static error::Error DoMain(
	const vector<string> &args,
	function<void(mender::update::context::MenderContext &ctx)> test_hook) {
	mender::common::conf::MenderConfig config;

	auto args_pos = config.ProcessCmdlineArgs(args.begin(), args.end(), cli_mender_update);
	if (!args_pos) {
		if (args_pos.error().code != error::MakeError(error::ExitWithSuccessError, "").code) {
			cli::PrintCliHelp(cli_mender_update);
		}
		return args_pos.error();
	}

	auto action = ParseUpdateArguments(args.begin() + args_pos.value(), args.end());
	if (!action) {
		if (action.error().code != error::MakeError(error::ExitWithSuccessError, "").code) {
			if (args.size() > 0) {
				cli::PrintCliCommandHelp(cli_mender_update, args[0]);
			} else {
				cli::PrintCliHelp(cli_mender_update);
			}
		}
		return action.error();
	}

	mender::update::context::MenderContext main_context(config);

	test_hook(main_context);

	auto err = main_context.Initialize();
	if (error::NoError != err) {
		return err;
	}

	return action.value()->Execute(main_context);
}

int Main(
	const vector<string> &args,
	function<void(mender::update::context::MenderContext &ctx)> test_hook) {
	auto err = DoMain(args, test_hook);

	if (err.code == context::MakeError(context::NoUpdateInProgressError, "").code) {
		return NoUpdateInProgressExitStatus;
	} else if (err.code == context::MakeError(context::RebootRequiredError, "").code) {
		return RebootExitStatus;
	} else if (err != error::NoError) {
		if (err.code == error::MakeError(error::ExitWithSuccessError, "").code) {
			return 0;
		} else if (err.code != error::MakeError(error::ExitWithFailureError, "").code) {
			cerr << "Could not fulfill request: " + err.String() << endl;
		}
		return 1;
	}

	return 0;
}

} // namespace cli
} // namespace update
} // namespace mender
