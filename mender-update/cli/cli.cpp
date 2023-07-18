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

namespace mender {
namespace update {
namespace cli {

namespace conf = mender::common::conf;

const int NoUpdateInProgressExitStatus = 2;
const int RebootExitStatus = 4;

ExpectedActionPtr ParseUpdateArguments(
	vector<string>::const_iterator start, vector<string>::const_iterator end) {
	if (start == end) {
		return expected::unexpected(conf::MakeError(conf::InvalidOptionsError, "Need an action"));
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
	} else {
		return expected::unexpected(
			conf::MakeError(conf::InvalidOptionsError, "No such action: " + start[0]));
	}
}

int Main(
	const vector<string> &args,
	function<void(mender::update::context::MenderContext &ctx)> test_hook) {
	mender::common::conf::MenderConfig config;

	auto args_pos = config.ProcessCmdlineArgs(args.begin(), args.end());
	if (!args_pos) {
		cerr << "Failed to process command line options: " + args_pos.error().String() << endl;
		return 1;
	}

	auto action = ParseUpdateArguments(args.begin() + args_pos.value(), args.end());
	if (!action) {
		cerr << "Failed to process command line options: " + action.error().String() << endl;
		return 1;
	}

	mender::update::context::MenderContext main_context(config);

	test_hook(main_context);

	auto err = main_context.Initialize();
	if (error::NoError != err) {
		cerr << "Failed to intialize main context: " + err.String() << endl;
		return 1;
	}

	err = action.value()->Execute(main_context);

	if (err.code == context::MakeError(context::NoUpdateInProgressError, "").code) {
		return NoUpdateInProgressExitStatus;
	} else if (err.code == context::MakeError(context::RebootRequiredError, "").code) {
		return RebootExitStatus;
	} else if (err != error::NoError) {
		if (err.code != context::MakeError(context::ExitStatusOnlyError, "").code) {
			cerr << "Could not fulfill request: " + err.String() << endl;
		}
		return 1;
	}

	return 0;
}

} // namespace cli
} // namespace update
} // namespace mender
