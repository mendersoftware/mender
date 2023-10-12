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

#include <vector>
#include <string>
#include <sstream>

#include <gtest/gtest.h>
#include <gmock/gmock.h>

#include <common/testing.hpp>
#include <common/common.hpp>
#include <common/cli.hpp>

namespace cli = mender::common::cli;

using namespace std;

// Test helper to wrap a cli::Command in a cli::App and print the help for the command
void PrintCommandHelp(const cli::Command &command, ostream &stream) {
	const cli::App cli_wrapper {
		.name = "wrapper",
		.commands =
			{
				command,
			},
	};
	cli::PrintCliCommandHelp(cli_wrapper, command.name, stream);
}

TEST(CliTests, CommandHelpBasicCases) {
	const cli::Command cmd_minimal = {.name = "command", .description = "Minimal command"};
	std::ostringstream help_text_minimal;
	PrintCommandHelp(cmd_minimal, help_text_minimal);
	EXPECT_THAT(help_text_minimal.str(), testing::HasSubstr(R"(NAME:
   wrapper command - Minimal command)"))
		<< help_text_minimal.str();
	EXPECT_THAT(help_text_minimal.str(), testing::HasSubstr(R"(OPTIONS:
   --help, -h)"))
		<< help_text_minimal.str();

	const cli::Command cmd_with_options = {
		.name = "command",
		.description = "Command with options",
		.options =
			{
				cli::Option {
					.long_option = "long-option",
					.short_option = "l",
					.description = "Do something",
					.default_value = "false",
				},
				cli::Option {
					.long_option = "other-option",
					.short_option = "o",
					.description = "Do something else",
					.default_value = "true",
				},
			},
	};
	std::ostringstream help_text_options;
	PrintCommandHelp(cmd_with_options, help_text_options);
	EXPECT_THAT(
		help_text_options.str(), testing::HasSubstr("wrapper command - Command with options"))
		<< help_text_options.str();
	EXPECT_THAT(help_text_options.str(), testing::HasSubstr("--long-option, -l"))
		<< help_text_options.str();
	EXPECT_THAT(help_text_options.str(), testing::HasSubstr("Do something (default: false)"))
		<< help_text_options.str();
	EXPECT_THAT(help_text_options.str(), testing::HasSubstr("--other-option, -o"))
		<< help_text_options.str();
	EXPECT_THAT(help_text_options.str(), testing::HasSubstr("Do something else (default: true)"))
		<< help_text_options.str();
	EXPECT_THAT(help_text_options.str(), testing::HasSubstr("--help, -h"))
		<< help_text_options.str();

	const cli::Command cmd_option_with_argument = {
		.name = "command",
		.description = "Option with argument",
		.options =
			{
				cli::Option {
					.long_option = "file-option",
					.short_option = "f",
					.description = "Path",
					.default_value = "/etc/here/or/there",
					.parameter = "FILE",
				},
			},
	};
	std::ostringstream help_text_argument;
	PrintCommandHelp(cmd_option_with_argument, help_text_argument);
	EXPECT_THAT(
		help_text_argument.str(), testing::HasSubstr("wrapper command - Option with argument"))
		<< help_text_argument.str();
	EXPECT_THAT(help_text_argument.str(), testing::HasSubstr("--file-option FILE, -f FILE"))
		<< help_text_argument.str();
	EXPECT_THAT(help_text_argument.str(), testing::HasSubstr("Path (default: /etc/here/or/there)"))
		<< help_text_argument.str();
}

TEST(CliTests, CommandHelpWrappingText) {
	const cli::Command cmd_wrapping_text = {
		.name = "command",
		.description = "Command with options",
		.options =
			{
				cli::Option {
					.long_option = "something",
					.short_option = "s",
					.description = "Do something",
					.default_value = "true",
				},
				cli::Option {
					.long_option = "very-important-first-column-wide",
					.short_option = "I",
					.description =
						"Do something very important with a very long description so that it wraps around in the terminal",
					.default_value = "false",
				},
				cli::Option {
					.long_option = "no-wrap",
					.short_option = "w",
					.description =
						"One-word-description-that-cannot-be-wrapped-out-so-it-will-just-flood",
					.default_value = "true",
				},
			},
	};
	std::ostringstream help_text_wrapping;
	PrintCommandHelp(cmd_wrapping_text, help_text_wrapping);
	EXPECT_THAT(help_text_wrapping.str(), testing::HasSubstr(R"(OPTIONS:
   --something, -s                         Do something (default: true)
   --very-important-first-column-wide, -I  Do something very important with a
                                           very long description so that it
                                           wraps around in the terminal
                                           (default: false)
   --no-wrap, -w                           One-word-description-that-cannot-be-wrapped-out-so-it-will-just-flood
                                           (default: true)
   --help, -h                              show help (default: false)"))
		<< help_text_wrapping.str();

	const cli::Command cmd_exact_width = {
		.name = "command",
		.description = "Command with options",
		.options =
			{
				cli::Option {
					.long_option = "exactly-10",
					.short_option = "e",
					.description = "Description of exactly 78-16-10-6-3-2=41!",
					.default_value = "true",
				},
			},
	};
	std::ostringstream help_text_exact;
	PrintCommandHelp(cmd_exact_width, help_text_exact);
	EXPECT_THAT(help_text_exact.str(), testing::HasSubstr(R"(OPTIONS:
   --exactly-10, -e  Description of exactly 78-16-10-6-3-2=41! (default: true)
   --help, -h        show help (default: false)"))
		<< help_text_exact.str();
}

TEST(CliTests, CliHelpWholeApplication) {
	const cli::App cli_something = {
		.name = "mender-something",
		.short_description = "manage and start the Mender something",
		.long_description =
			R"(something long
that can cas multiple lines
and scaped chars	like tab
	more	tab
and even with very long lines it should not wrap and let the user have it his/her way)",
		.version = "a.b.c",
		.commands =
			{
				cli::Command {
					.name = "do-something",
					.description = "Perform something",
					.options =
						{
							cli::Option {
								.long_option = "force",
								.short_option = "F",
								.description = "Force bootstrap",
								.default_value = "false",
							},
						},
				},
				cli::Command {
					.name = "do-other-thing-long-command",
					.description =
						"Perform the other thing and exit. Just remember to have a long description to also verify the wrapping",
				},
			},
		.global_options =
			{
				cli::Option {
					.long_option = "config",
					.short_option = "c",
					.description = "Configuration FILE path",
					.default_value = "/etc/some/thing.conf",
					.parameter = "FILE",
				},
			},
	};
	std::ostringstream help_text;
	cli::PrintCliHelp(cli_something, help_text);
	EXPECT_EQ(
		R"(NAME:
   mender-something - manage and start the Mender something

USAGE:
   mender-something [global options] command [command options] [arguments...]

VERSION:
   a.b.c

DESCRIPTION:
   something long
that can cas multiple lines
and scaped chars	like tab
	more	tab
and even with very long lines it should not wrap and let the user have it his/her way

COMMANDS:
   do-something                 Perform something
   do-other-thing-long-command  Perform the other thing and exit. Just
                                remember to have a long description to also
                                verify the wrapping

GLOBAL OPTIONS:
   --config FILE, -c FILE  Configuration FILE path (default:
                           /etc/some/thing.conf)
   --help, -h              show help (default: false)
)",
		help_text.str())
		<< help_text.str();
}

TEST(CliTests, CliHelpCommandLookup) {
	const cli::App cli_lookup = {
		.name = "mender-something",
		.short_description = "manage and start the Mender something",
		.long_description = "description only visible on top level app help",
		.commands =
			{
				cli::Command {
					.name = "command-one",
					.description = "command 1 description",
					.options =
						{
							cli::Option {
								.long_option = "option-one",
								.description = "description only visible on command 1 help",
							},
						},
				},
				cli::Command {
					.name = "command-two",
					.description = "command 2 description",
					.options =
						{
							cli::Option {
								.long_option = "option-two",
								.description = "description only visible on command 2 help",
							},
						},
				},
				cli::Command {
					.name = "command-one",
					.description = "masked command - it will never show",
					.options =
						{
							cli::Option {
								.long_option = "masked-command",
								.description = "description will never show",
							},
						},
				},
			},
	};
	std::ostringstream help_non_existing;
	cli::PrintCliCommandHelp(cli_lookup, "non-existing-command", help_non_existing);
	EXPECT_EQ(
		R"(NAME:
   mender-something - manage and start the Mender something

USAGE:
   mender-something [global options] command [command options] [arguments...]

DESCRIPTION:
   description only visible on top level app help

COMMANDS:
   command-one  command 1 description
   command-two  command 2 description
   command-one  masked command - it will never show

GLOBAL OPTIONS:
   --help, -h  show help (default: false)
)",
		help_non_existing.str())
		<< help_non_existing.str();

	std::ostringstream help_command_1;
	cli::PrintCliCommandHelp(cli_lookup, "command-one", help_command_1);
	EXPECT_EQ(
		R"(NAME:
   mender-something command-one - command 1 description

OPTIONS:
   --option-one  description only visible on command 1 help
   --help, -h    show help (default: false)
)",
		help_command_1.str())
		<< help_command_1.str();


	std::ostringstream help_command_2;
	cli::PrintCliCommandHelp(cli_lookup, "command-two", help_command_2);
	EXPECT_EQ(
		R"(NAME:
   mender-something command-two - command 2 description

OPTIONS:
   --option-two  description only visible on command 2 help
   --help, -h    show help (default: false)
)",
		help_command_2.str())
		<< help_command_2.str();
}
