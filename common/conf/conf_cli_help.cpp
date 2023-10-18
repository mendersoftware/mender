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

#include <common/conf.hpp>

#include <functional>
#include <iomanip>
#include <iostream>
#include <iterator>
#include <string>
#include <vector>

#include <common/common.hpp>

namespace mender {
namespace common {
namespace conf {

using namespace std;

namespace common = mender::common;

const size_t max_width = 78;
const string indent = "   ";   // 3 spaces
const string separator = "  "; // 2 spaces

const Paths default_paths = Paths {};

const CliOption help_option = {
	.long_option = "help",
	.short_option = "h",
	.description = "Show help and exit",
};

const vector<CliOption> common_global_options = {
	CliOption {
		.long_option = "config",
		.short_option = "c",
		.description = "Configuration FILE path",
		.default_value = default_paths.GetConfFile(),
		.parameter = "FILE",
	},
	CliOption {
		.long_option = "fallback-config",
		.short_option = "b",
		.description = "Fallback configuration FILE path",
		.default_value = default_paths.GetFallbackConfFile(),
		.parameter = "FILE",
	},
	CliOption {
		.long_option = "data",
		.short_option = "d",
		.description = "Mender state data DIRECTORY path",
		.default_value = default_paths.GetPathDataDir(),
		.parameter = "DIR",
	},
	CliOption {
		.long_option = "log-file",
		.short_option = "L",
		.description = "FILE to log to",
		.parameter = "FILE",
	},
	CliOption {
		.long_option = "log-level",
		.short_option = "l",
		.description = "Set logging level",
		.default_value = "info",
		.parameter = "LEVEL",
	},
	CliOption {
		.long_option = "trusted-certs",
		.short_option = "E",
		.description = "Trusted server certificates FILE path",
		.parameter = "FILE",
	},
	CliOption {
		.long_option = "skipverify",
		.description = "Skip certificate verification",
	},
	CliOption {
		.long_option = "version",
		.short_option = "v",
		.description = "Print version and exit",
	},
	help_option,
};

const string common_description_append = R"(Global flag remarks:
   - Supported log levels incudes: 'trace', 'debug', 'info', 'warning', 'error', and
     'fatal'.

Environment variables:
   - MENDER_CONF_DIR - configuration (default: )"
										 + default_paths.GetPathConfDir() + R"().
   - MENDER_DATA_DIR - identity, inventory and update modules (default: )"
										 + default_paths.GetPathDataDir() + R"().
   - MENDER_DATASTORE_DIR - runtime datastore (default: )"
										 + default_paths.GetDataStore() + R"().)";

template <typename InputIterator>
using ColumnFormatter = function<string(typename iterator_traits<InputIterator>::value_type)>;

template <typename InputIterator>
void PrintInTwoColumns(
	InputIterator start,
	InputIterator end,
	ColumnFormatter<InputIterator> column_one_fmt,
	ColumnFormatter<InputIterator> column_two_fmt,
	ostream &stream) {
	// First pass to calculate the max size for the elements in the first column
	size_t column_one_size = 0;
	for (auto it = start; it != end; ++it) {
		if (column_one_fmt(*it).size() > column_one_size) {
			column_one_size = column_one_fmt(*it).size();
		}
	}

	// The total with will be the size of the largest element + indent + separator
	const size_t column_one_width {column_one_size + indent.size() + separator.size()};
	// The second column takes the rest of the available width
	const size_t column_two_width {max_width - column_one_width};
	for (auto it = start; it != end; ++it) {
		stream << indent << setw(column_one_size) << left << column_one_fmt(*it) << separator;
		// Wrap around and align the text for the second column
		auto lines = common::JoinStringsMaxWidth(
			common::SplitString(column_two_fmt(*it), " "), " ", column_two_width);
		stream << lines.front() << endl;
		for_each(lines.begin() + 1, lines.end(), [&stream, column_one_width](const string &l) {
			stream << setw(column_one_width) << left << " " << l << endl;
		});
	}
}

void PrintOptions(const vector<CliOption> &options, ostream &stream) {
	PrintInTwoColumns(
		options.begin(),
		options.end(),
		[](const CliOption &option) {
			// Format: --long-option[ PARAM][, -l[ PARAM]]
			string str = "--" + option.long_option;
			if (!option.parameter.empty()) {
				str += " " + option.parameter;
			}
			if (!option.short_option.empty()) {
				str += ", -" + option.short_option;
				if (!option.parameter.empty()) {
					str += " " + option.parameter;
				}
			}
			return str;
		},
		[](const CliOption &option) {
			// Format: description[ (default: DEFAULT)]
			string str = option.description;
			if (!option.default_value.empty()) {
				str += " (default: " + option.default_value + ")";
			}
			return str;
		},
		stream);
}

void PrintCommandHelp(const string &cli_name, const CliCommand &command, ostream &stream) {
	stream << "NAME:" << endl;
	stream << indent << cli_name << " " << command.name;
	if (!command.description.empty()) {
		stream << " - " << command.description;
	}
	stream << endl << endl;

	// Append --help option at the command level
	vector<CliOption> options_with_help = command.options;
	options_with_help.push_back(help_option);
	stream << "OPTIONS:" << endl;
	PrintOptions(options_with_help, stream);
}

void PrintCliHelp(const CliApp &cli, ostream &stream) {
	stream << "NAME:" << endl;
	stream << indent << cli.name;
	if (!cli.short_description.empty()) {
		stream << " - " << cli.short_description;
	}
	stream << endl << endl;

	stream << "USAGE:" << endl
		   << indent << cli.name << " [global options] command [command options] [arguments...]";
	stream << endl << endl;

	stream << "VERSION:" << endl << indent << kMenderVersion;
	stream << endl << endl;

	if (!cli.long_description.empty()) {
		stream << "DESCRIPTION:" << endl << indent << cli.long_description << endl;
		;
		stream << common_description_append;
		stream << endl << endl;
	}

	stream << "COMMANDS:" << endl;
	PrintInTwoColumns(
		cli.commands.begin(),
		cli.commands.end(),
		[](const CliCommand &command) { return command.name; },
		[](const CliCommand &command) { return command.description; },
		stream);
	stream << endl;

	stream << "GLOBAL OPTIONS:" << endl;
	PrintOptions(common_global_options, stream);
}

bool FindCmdlineHelpArg(vector<string>::const_iterator start, vector<string>::const_iterator end) {
	bool found = false;

	conf::CmdlineOptionsIterator opts_iter(
		start, end, {}, CommandOptsSetWithoutValue(vector<CliOption> {help_option}));
	auto ex_opt_val = opts_iter.Next();
	while (ex_opt_val && ((ex_opt_val.value().option != "") || (ex_opt_val.value().value != ""))) {
		auto opt_val = ex_opt_val.value();
		if ((opt_val.option == "--help") || (opt_val.option == "-h")) {
			found = true;
			break;
		}
		ex_opt_val = opts_iter.Next();
	}

	return found;
}

void PrintCliCommandHelp(const CliApp &cli, const string &command_name, ostream &stream) {
	auto match_on_name = [command_name](const CliCommand &cmd) { return cmd.name == command_name; };

	auto cmd = std::find_if(cli.commands.begin(), cli.commands.end(), match_on_name);
	if (cmd != cli.commands.end()) {
		PrintCommandHelp(cli.name, *cmd, stream);
	} else {
		PrintCliHelp(cli, stream);
	}
}

const OptsSet OptsSetFromCliOpts(const vector<CliOption> &options, bool without_value) {
	OptsSet opts {};
	for (auto const &opt : options) {
		if ((without_value && opt.parameter.empty())
			|| (!without_value && !opt.parameter.empty())) {
			if (!opt.long_option.empty()) {
				opts.insert("--" + opt.long_option);
			}
			if (!opt.short_option.empty()) {
				opts.insert("-" + opt.short_option);
			}
		}
	}
	return opts;
}

const OptsSet GlobalOptsSetWithValue() {
	return OptsSetFromCliOpts(common_global_options, false);
}

const OptsSet GlobalOptsSetWithoutValue() {
	return OptsSetFromCliOpts(common_global_options, true);
}

const OptsSet CommandOptsSetWithValue(const vector<CliOption> &options) {
	return OptsSetFromCliOpts(options, false);
}

const OptsSet CommandOptsSetWithoutValue(const vector<CliOption> &options) {
	return OptsSetFromCliOpts(options, true);
}

} // namespace conf
} // namespace common
} // namespace mender
