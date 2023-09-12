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

#include <common/conf.hpp>
#include <common/expected.hpp>
#include <common/io.hpp>
#include <common/setup.hpp>

#include <mender-auth/cli/actions.hpp>
#include <mender-auth/context.hpp>
#include <mender-auth/ipc/server.hpp>

namespace mender {
namespace auth {
namespace cli {

using namespace std;

namespace conf = mender::common::conf;
namespace expected = mender::common::expected;
namespace io = mender::common::io;
namespace setup = mender::common::setup;

namespace context = mender::auth::context;
namespace ipc = mender::auth::ipc;

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

static ExpectedActionPtr ParseUpdateArguments(
	const conf::MenderConfig &config,
	vector<string>::const_iterator start,
	vector<string>::const_iterator end) {
	if (start == end) {
		return expected::unexpected(conf::MakeError(conf::InvalidOptionsError, "Need an action"));
	}

	string passphrase = "";
	bool forcebootstrap = false;
	if (start[0] == "bootstrap" || start[0] == "daemon") {
		conf::CmdlineOptionsIterator opts_iter(
			start + 1, end, {"--passphrase-file"}, {"--forcebootstrap", "-F"});
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
		return DaemonAction::Create(config, passphrase, forcebootstrap);
	} else {
		return expected::unexpected(
			conf::MakeError(conf::InvalidOptionsError, "No such action: " + start[0]));
	}
}

error::Error DoMain(const vector<string> &args) {
	setup::GlobalSetup();

	conf::MenderConfig config;
	auto arg_pos = config.ProcessCmdlineArgs(args.begin(), args.end());
	if (!arg_pos) {
		return arg_pos.error();
	}

	auto action = ParseUpdateArguments(config, args.begin() + arg_pos.value(), args.end());
	if (!action) {
		return action.error();
	}

	context::MenderContext context(config);

	return action.value()->Execute(context);
}

} // namespace cli
} // namespace auth
} // namespace mender
