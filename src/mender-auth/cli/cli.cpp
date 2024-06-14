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

#include <mender-auth/cli/cli.hpp>

#include <string>

#include <client_shared/conf.hpp>
#include <common/expected.hpp>
#include <common/io.hpp>

#include <mender-auth/cli/actions.hpp>
#include <mender-auth/context.hpp>

#ifdef MENDER_USE_DBUS
#include <mender-auth/ipc/server.hpp>
#endif

namespace mender {
namespace auth {
namespace cli {

using namespace std;

namespace conf = mender::client_shared::conf;
namespace expected = mender::common::expected;
namespace io = mender::common::io;

namespace context = mender::auth::context;

#ifdef MENDER_USE_DBUS
namespace ipc = mender::auth::ipc;
#endif

const vector<conf::CliOption> opts_bootstrap_daemon {
	conf::CliOption {
		.long_option = "forcebootstrap",
		.short_option = "F",
		.description = "Force bootstrap",
	},
	conf::CliOption {
		.long_option = "passphrase-file",
		.description =
			"Passphrase file for decrypting an encrypted private key. '-' loads passphrase from stdin",
		.default_value = "''",
	},
};

const conf::CliCommand cmd_bootstrap {
	.name = "bootstrap",
	.description = "Perform bootstrap and exit",
	.options = opts_bootstrap_daemon,
};

const conf::CliCommand cmd_daemon {
	.name = "daemon",
	.description = "Start the client as a background service",
	.options = opts_bootstrap_daemon,
};

const conf::CliApp cli_mender_auth = {
	.name = "mender-auth",
	.short_description = "manage and start Mender Auth",
	.long_description =
		R"(mender-auth integrates both the mender-auth daemon and commands for manually
   performing tasks performed by the daemon (see list of COMMANDS below).)",
	.commands =
		{
			cmd_bootstrap,
			cmd_daemon,
		},
};

static expected::ExpectedString GetPassphraseFromFile(const string &filepath) {
	string passphrase = "";
	if (filepath == "") {
		return passphrase;
	}

	auto ex_ifs = io::OpenIfstream(filepath == "-" ? io::paths::Stdin : filepath);
	if (!ex_ifs) {
		return expected::unexpected(ex_ifs.error());
	}
	auto &ifs = ex_ifs.value();

	errno = 0;
	getline(ifs, passphrase);
	if (ifs.bad()) {
		int io_errno = errno;
		error::Error err {
			generic_category().default_error_condition(io_errno),
			"Failed to read passphrase from '" + filepath + "'"};
		return expected::unexpected(err);
	}

	return passphrase;
}

static ExpectedActionPtr ParseAuthArguments(
	const conf::MenderConfig &config,
	vector<string>::const_iterator start,
	vector<string>::const_iterator end) {
	if (start == end) {
		return expected::unexpected(conf::MakeError(conf::InvalidOptionsError, "Need an action"));
	}

	bool help_arg = conf::FindCmdlineHelpArg(start + 1, end);
	if (help_arg) {
		conf::PrintCliCommandHelp(cli_mender_auth, start[0]);
		return expected::unexpected(error::MakeError(error::ExitWithSuccessError, ""));
	}

	string passphrase = "";
	bool forcebootstrap = false;
	if (start[0] == "bootstrap" || start[0] == "daemon") {
		conf::CmdlineOptionsIterator opts_iter(
			start + 1,
			end,
			conf::CommandOptsSetWithValue(opts_bootstrap_daemon),
			conf::CommandOptsSetWithoutValue(opts_bootstrap_daemon));
		auto ex_opt_val = opts_iter.Next();

		while (ex_opt_val
			   && ((ex_opt_val.value().option != "") || (ex_opt_val.value().value != ""))) {
			auto opt_val = ex_opt_val.value();
			if ((opt_val.option == "--passphrase-file")) {
				auto ex_passphrase = GetPassphraseFromFile(opt_val.value);
				if (!ex_passphrase) {
					return expected::unexpected(ex_passphrase.error());
				}
				passphrase = ex_passphrase.value();
			} else if ((opt_val.option == "--forcebootstrap" || opt_val.option == "-F")) {
				forcebootstrap = true;
			} else {
				assert(false);
			}
			ex_opt_val = opts_iter.Next();
		}
		if (!ex_opt_val) {
			return expected::unexpected(ex_opt_val.error());
		}
	}

	if (start[0] == "bootstrap") {
		return BootstrapAction::Create(config, passphrase, forcebootstrap);
	} else if (start[0] == "daemon") {
#ifdef MENDER_USE_DBUS
		return DaemonAction::Create(config, passphrase, forcebootstrap);
#else
		return expected::unexpected(error::Error(
			make_error_condition(errc::not_supported),
			"Daemon mode not support when DBus support is compiled out"));
#endif
	} else {
		return expected::unexpected(
			conf::MakeError(conf::InvalidOptionsError, "No such action: " + start[0]));
	}
}

error::Error DoMain(
	const vector<string> &args, function<void(context::MenderContext &ctx)> test_hook) {
	conf::MenderConfig config;
	auto arg_pos = config.ProcessCmdlineArgs(args.begin(), args.end(), cli_mender_auth);
	if (!arg_pos) {
		if (arg_pos.error().code != error::MakeError(error::ExitWithSuccessError, "").code) {
			conf::PrintCliHelp(cli_mender_auth);
		}
		return arg_pos.error();
	}

	auto action = ParseAuthArguments(config, args.begin() + arg_pos.value(), args.end());
	if (!action) {
		if (action.error().code != error::MakeError(error::ExitWithSuccessError, "").code) {
			if (args.size() > 0) {
				conf::PrintCliCommandHelp(cli_mender_auth, args[0]);
			} else {
				conf::PrintCliHelp(cli_mender_auth);
			}
		}
		return action.error();
	}

	context::MenderContext context(config);

	test_hook(context);

	return action.value()->Execute(context);
}

int Main(const vector<string> &args, function<void(context::MenderContext &ctx)> test_hook) {
	auto err = mender::auth::cli::DoMain(args, test_hook);

	if (err != error::NoError) {
		if (err.code == error::MakeError(error::ExitWithSuccessError, "").code) {
			return 0;
		} else if (err.code != error::MakeError(error::ExitWithFailureError, "").code) {
			cerr << "Failed to process command line options: " + err.String() << endl;
		}
		return 1;
	}

	return 0;
}

} // namespace cli
} // namespace auth
} // namespace mender
