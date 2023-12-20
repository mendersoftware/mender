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

#include <string>
// Need POSIX header for setenv.
#include <stdlib.h>
#include <vector>

#include <gtest/gtest.h>
#include <gmock/gmock.h>

#include <common/error.hpp>
#include <common/log.hpp>
#include <common/path.hpp>
#include <common/testing.hpp>

namespace conf = mender::common::conf;
namespace error = mender::common::error;
namespace mlog = mender::common::log;
namespace path = mender::common::path;
namespace mtesting = mender::common::testing;

using namespace std;

TEST(ConfTests, GetEnvTest) {
	auto value = conf::GetEnv("MENDER_CONF_TEST_VAR", "default_value");
	EXPECT_EQ(value, "default_value");

	char var[] = "MENDER_CONF_TEST_VAR=mender_conf_test_value";
	int ret = putenv(var);
	ASSERT_EQ(ret, 0);

	value = conf::GetEnv("MENDER_CONF_TEST_VAR", "default_value");
	EXPECT_EQ(value, "mender_conf_test_value");
}

TEST(ConfTests, CmdlineOptionsIteratorGoodTest) {
	vector<string> args = {
		"--opt1",
		"val1",
		"-o2",
		"val2",
		"--opt3",
		"arg1",
		"--opt4=val4",
		"arg2",
		"--opt5",
		"-o6=val6",
		"arg3",
		"-o7",
	};

	conf::CmdlineOptionsIterator opts_iter(
		args.begin(), args.end(), {"--opt1", "-o2", "--opt4", "-o6"}, {"--opt3", "--opt5", "-o7"});
	opts_iter.SetArgumentsMode(conf::ArgumentsMode::AcceptBareArguments);
	auto ex_opt_val = opts_iter.Next();
	ASSERT_TRUE(ex_opt_val);
	auto opt_val = ex_opt_val.value();
	EXPECT_EQ(opt_val.option, "--opt1");
	EXPECT_EQ(opt_val.value, "val1");

	ex_opt_val = opts_iter.Next();
	ASSERT_TRUE(ex_opt_val);
	opt_val = ex_opt_val.value();
	EXPECT_EQ(opt_val.option, "-o2");
	EXPECT_EQ(opt_val.value, "val2");

	ex_opt_val = opts_iter.Next();
	ASSERT_TRUE(ex_opt_val);
	opt_val = ex_opt_val.value();
	EXPECT_EQ(opt_val.option, "--opt3");
	EXPECT_EQ(opt_val.value, "");

	ex_opt_val = opts_iter.Next();
	ASSERT_TRUE(ex_opt_val);
	opt_val = ex_opt_val.value();
	EXPECT_EQ(opt_val.option, "");
	EXPECT_EQ(opt_val.value, "arg1");

	ex_opt_val = opts_iter.Next();
	ASSERT_TRUE(ex_opt_val);
	opt_val = ex_opt_val.value();
	EXPECT_EQ(opt_val.option, "--opt4");
	EXPECT_EQ(opt_val.value, "val4");

	ex_opt_val = opts_iter.Next();
	ASSERT_TRUE(ex_opt_val);
	opt_val = ex_opt_val.value();
	EXPECT_EQ(opt_val.option, "");
	EXPECT_EQ(opt_val.value, "arg2");

	ex_opt_val = opts_iter.Next();
	ASSERT_TRUE(ex_opt_val);
	opt_val = ex_opt_val.value();
	EXPECT_EQ(opt_val.option, "--opt5");
	EXPECT_EQ(opt_val.value, "");

	ex_opt_val = opts_iter.Next();
	ASSERT_TRUE(ex_opt_val);
	opt_val = ex_opt_val.value();
	EXPECT_EQ(opt_val.option, "-o6");
	EXPECT_EQ(opt_val.value, "val6");

	ex_opt_val = opts_iter.Next();
	ASSERT_TRUE(ex_opt_val);
	opt_val = ex_opt_val.value();
	EXPECT_EQ(opt_val.option, "");
	EXPECT_EQ(opt_val.value, "arg3");

	ex_opt_val = opts_iter.Next();
	ASSERT_TRUE(ex_opt_val);
	opt_val = ex_opt_val.value();
	EXPECT_EQ(opt_val.option, "-o7");
	EXPECT_EQ(opt_val.value, "");

	// termination object
	ex_opt_val = opts_iter.Next();
	ASSERT_TRUE(ex_opt_val);
	opt_val = ex_opt_val.value();
	EXPECT_EQ(opt_val.option, "");
	EXPECT_EQ(opt_val.value, "");

	// should stay at the termination object and not fail
	ex_opt_val = opts_iter.Next();
	ASSERT_TRUE(ex_opt_val);
	opt_val = ex_opt_val.value();
	EXPECT_EQ(opt_val.option, "");
	EXPECT_EQ(opt_val.value, "");
}

TEST(ConfTests, CmdlineOptionsIteratorDoubleDashTest) {
	vector<string> args = {
		"--opt1",
		"val1",
		"-o2",
		"val2",
		"--",
		"--opt3",
		"arg1",
		"--opt4=val4",
	};

	conf::CmdlineOptionsIterator opts_iter(
		args.begin(), args.end(), {"--opt1", "-o2", "--opt4", "-o6"}, {"--opt3", "--opt5", "-o7"});
	auto ex_opt_val = opts_iter.Next();
	ASSERT_TRUE(ex_opt_val);
	auto opt_val = ex_opt_val.value();
	EXPECT_EQ(opt_val.option, "--opt1");
	EXPECT_EQ(opt_val.value, "val1");

	ex_opt_val = opts_iter.Next();
	ASSERT_TRUE(ex_opt_val);
	opt_val = ex_opt_val.value();
	EXPECT_EQ(opt_val.option, "-o2");
	EXPECT_EQ(opt_val.value, "val2");

	ex_opt_val = opts_iter.Next();
	ASSERT_TRUE(ex_opt_val);
	opt_val = ex_opt_val.value();
	EXPECT_EQ(opt_val.option, "--");
	EXPECT_EQ(opt_val.value, "");

	ex_opt_val = opts_iter.Next();
	ASSERT_TRUE(ex_opt_val);
	opt_val = ex_opt_val.value();
	EXPECT_EQ(opt_val.option, "");
	EXPECT_EQ(opt_val.value, "--opt3");

	ex_opt_val = opts_iter.Next();
	ASSERT_TRUE(ex_opt_val);
	opt_val = ex_opt_val.value();
	EXPECT_EQ(opt_val.option, "");
	EXPECT_EQ(opt_val.value, "arg1");

	ex_opt_val = opts_iter.Next();
	ASSERT_TRUE(ex_opt_val);
	opt_val = ex_opt_val.value();
	EXPECT_EQ(opt_val.option, "");
	EXPECT_EQ(opt_val.value, "--opt4=val4");

	// termination object
	ex_opt_val = opts_iter.Next();
	ASSERT_TRUE(ex_opt_val);
	opt_val = ex_opt_val.value();
	EXPECT_EQ(opt_val.option, "");
	EXPECT_EQ(opt_val.value, "");

	// should stay at the termination object and not fail
	ex_opt_val = opts_iter.Next();
	ASSERT_TRUE(ex_opt_val);
	opt_val = ex_opt_val.value();
	EXPECT_EQ(opt_val.option, "");
	EXPECT_EQ(opt_val.value, "");
}

TEST(ConfTests, CmdlineOptionsIteratorBadOptionTest) {
	vector<string> args = {
		"--opt1",
		"val1",
		"-o2",
	};

	conf::CmdlineOptionsIterator opts_iter(
		args.begin(), args.end(), {"--opt1", "--opt4", "-o6"}, {"--opt3", "--opt5", "-o7"});
	auto ex_opt_val = opts_iter.Next();
	ASSERT_TRUE(ex_opt_val);
	auto opt_val = ex_opt_val.value();
	EXPECT_EQ(opt_val.option, "--opt1");
	EXPECT_EQ(opt_val.value, "val1");

	ex_opt_val = opts_iter.Next();
	ASSERT_FALSE(ex_opt_val);
	EXPECT_EQ(ex_opt_val.error().message, "Unrecognized option '-o2'");
}

TEST(ConfTests, CmdlineOptionsIteratorOptionMissingValueTest) {
	vector<string> args = {
		"--opt1",
		"-o2",
		"val2",
	};

	conf::CmdlineOptionsIterator opts_iter(
		args.begin(), args.end(), {"--opt1", "-o2", "--opt4", "-o6"}, {"--opt3", "--opt5", "-o7"});
	auto ex_opt_val = opts_iter.Next();
	ASSERT_FALSE(ex_opt_val);
	EXPECT_EQ(ex_opt_val.error().message, "Option --opt1 missing value");
}

TEST(ConfTests, CmdlineOptionsIteratorOptionMissingValueTrailingTest) {
	vector<string> args = {
		"--opt1",
	};

	conf::CmdlineOptionsIterator opts_iter(
		args.begin(), args.end(), {"--opt1", "-o2", "--opt4", "-o6"}, {"--opt3", "--opt5", "-o7"});
	auto ex_opt_val = opts_iter.Next();
	ASSERT_FALSE(ex_opt_val);
	EXPECT_EQ(ex_opt_val.error().message, "Option --opt1 missing value");
}

TEST(ConfTests, CmdlineOptionsIteratorOptionExtraValueTest) {
	vector<string> args = {
		"--opt3=val3",
		"-o2",
		"val2",
	};

	conf::CmdlineOptionsIterator opts_iter(
		args.begin(), args.end(), {"--opt1", "-o2", "--opt4", "-o6"}, {"--opt3", "--opt5", "-o7"});
	auto ex_opt_val = opts_iter.Next();
	ASSERT_FALSE(ex_opt_val);
	EXPECT_EQ(ex_opt_val.error().message, "Option --opt3 doesn't expect a value");
}

TEST(ConfTests, CmdlineOptionsIteratorArgumentsModes) {
	vector<string> args = {
		"val2",
	};

	{
		conf::CmdlineOptionsIterator opts_iter(args.begin(), args.end(), {"--opt1"}, {"--o2"});
		opts_iter.SetArgumentsMode(conf::ArgumentsMode::AcceptBareArguments);
		auto ex_opt_val = opts_iter.Next();
		ASSERT_TRUE(ex_opt_val);
		EXPECT_EQ(ex_opt_val.value().option, "");
		EXPECT_EQ(ex_opt_val.value().value, "val2");

		ex_opt_val = opts_iter.Next();
		ASSERT_TRUE(ex_opt_val);
		EXPECT_EQ(ex_opt_val.value().option, "");
		EXPECT_EQ(ex_opt_val.value().value, "");

		EXPECT_EQ(opts_iter.GetPos(), 1);
	}

	{
		conf::CmdlineOptionsIterator opts_iter(args.begin(), args.end(), {"--opt1"}, {"--o2"});
		opts_iter.SetArgumentsMode(conf::ArgumentsMode::RejectBareArguments);
		auto ex_opt_val = opts_iter.Next();
		ASSERT_FALSE(ex_opt_val);

		EXPECT_EQ(opts_iter.GetPos(), 0);
	}

	{
		conf::CmdlineOptionsIterator opts_iter(args.begin(), args.end(), {"--opt1"}, {"--o2"});
		opts_iter.SetArgumentsMode(conf::ArgumentsMode::StopAtBareArguments);
		auto ex_opt_val = opts_iter.Next();
		ASSERT_TRUE(ex_opt_val);
		EXPECT_EQ(ex_opt_val.value().option, "");
		EXPECT_EQ(ex_opt_val.value().value, "");
		EXPECT_EQ(opts_iter.GetPos(), 0);

		// It should stay there.
		ex_opt_val = opts_iter.Next();
		ASSERT_TRUE(ex_opt_val);
		EXPECT_EQ(ex_opt_val.value().option, "");
		EXPECT_EQ(ex_opt_val.value().value, "");
		EXPECT_EQ(opts_iter.GetPos(), 0);
	}
}

TEST(ConfTests, LogLevel) {
	// Just a way to clean up the log level no matter where we exit the function.
	class LogReset {
	public:
		LogReset() {
			level = mlog::Level();
		}
		~LogReset() {
			mlog::SetLevel(level);
		}
		mlog::LogLevel level;
	} log_reset;

	mtesting::TemporaryDirectory tmpdir;

	string conf_file = path::Join(tmpdir.Path(), "mender.conf");
	{
		ofstream f(conf_file);
		f << R"({"DaemonLogLevel": "warning"})";
		ASSERT_TRUE(f.good());
	}

	{
		vector<string> args {"--log-level", "error"};
		conf::MenderConfig config;
		config.ProcessCmdlineArgs(args.begin(), args.end(), conf::CliApp {});
		EXPECT_EQ(mlog::Level(), mlog::LogLevel::Error);
	}

	{
		vector<string> args {"--log-level", "debug", "--config", conf_file};
		conf::MenderConfig config;
		config.ProcessCmdlineArgs(args.begin(), args.end(), conf::CliApp {});
		EXPECT_EQ(mlog::Level(), mlog::LogLevel::Debug);
	}

	{
		vector<string> args {"--config", conf_file};
		conf::MenderConfig config;
		config.ProcessCmdlineArgs(args.begin(), args.end(), conf::CliApp {});
		EXPECT_EQ(mlog::Level(), mlog::LogLevel::Warning);
	}
}

TEST(ConfTests, UpdateLogPath) {
	mtesting::TemporaryDirectory tmpdir;

	string update_log_path = path::Join(tmpdir.Path(), "mylog-folder");

	string conf_file = path::Join(tmpdir.Path(), "mender.conf");
	{
		ofstream f(conf_file);
		f << R"({"UpdateLogPath": ")" + update_log_path + R"("})";
		ASSERT_TRUE(f.good());
	}

	vector<string> args {"--config", conf_file};
	conf::MenderConfig config;
	config.ProcessCmdlineArgs(args.begin(), args.end(), conf::CliApp {});
	EXPECT_EQ(config.paths.GetUpdateLogPath(), update_log_path);
}

// Test helper to wrap a conf::CliCommand in a conf::CliApp and print the help for the command
void PrintCommandHelp(const conf::CliCommand &command, ostream &stream) {
	const conf::CliApp cli_wrapper {
		.name = "wrapper",
		.commands =
			{
				command,
			},
	};
	conf::PrintCliCommandHelp(cli_wrapper, command.name, stream);
}

TEST(ConfTests, CliCommandHelpBasicCases) {
	const conf::CliCommand cmd_minimal = {.name = "command", .description = "Minimal command"};
	std::ostringstream help_text_minimal;
	PrintCommandHelp(cmd_minimal, help_text_minimal);
	EXPECT_THAT(help_text_minimal.str(), testing::HasSubstr(R"(NAME:
   wrapper command - Minimal command)"))
		<< help_text_minimal.str();
	EXPECT_THAT(help_text_minimal.str(), testing::HasSubstr(R"(OPTIONS:
   --help, -h)"))
		<< help_text_minimal.str();

	const conf::CliCommand cmd_with_options = {
		.name = "command",
		.description = "Command with options",
		.options =
			{
				conf::CliOption {
					.long_option = "long-option",
					.short_option = "l",
					.description = "Do something",
					.default_value = "false",
				},
				conf::CliOption {
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

	const conf::CliCommand cmd_option_with_argument = {
		.name = "command",
		.description = "Option with argument",
		.options =
			{
				conf::CliOption {
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

TEST(ConfTests, CliCommandHelpWrappingText) {
	const conf::CliCommand cmd_wrapping_text = {
		.name = "command",
		.description = "Command with options",
		.options =
			{
				conf::CliOption {
					.long_option = "something",
					.short_option = "s",
					.description = "Do something",
					.default_value = "true",
				},
				conf::CliOption {
					.long_option = "very-important-first-column-wide",
					.short_option = "I",
					.description =
						"Do something very important with a very long description so that it wraps around in the terminal",
					.default_value = "false",
				},
				conf::CliOption {
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
   --help, -h                              Show help and exit)"))
		<< help_text_wrapping.str();

	const conf::CliCommand cmd_exact_width = {
		.name = "command",
		.description = "Command with options",
		.options =
			{
				conf::CliOption {
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
   --help, -h        Show help and exit)"))
		<< help_text_exact.str();
}

TEST(ConfTests, CliCliHelpWholeApplication) {
	const conf::CliApp cli_something = {
		.name = "mender-something",
		.short_description = "manage and start the Mender something",
		.long_description =
			R"(something long
that can cas multiple lines
and scaped chars	like tab
	more	tab
and even with very long lines it should not wrap and let the user have it his/her way)",
		.commands =
			{
				conf::CliCommand {
					.name = "do-something",
					.description = "Perform something",
					.options =
						{
							conf::CliOption {
								.long_option = "force",
								.short_option = "F",
								.description = "Force bootstrap",
								.default_value = "false",
							},
						},
				},
				conf::CliCommand {
					.name = "do-other-thing-long-command",
					.description =
						"Perform the other thing and exit. Just remember to have a long description to also verify the wrapping",
				},
			},
	};
	std::ostringstream help_text;
	conf::PrintCliHelp(cli_something, help_text);
	// We cannot match against the whole text because PrintCliHelp adds version, appends
	// to the description, and adds global options.
	EXPECT_THAT(help_text.str(), testing::StartsWith(R"(NAME:
   mender-something - manage and start the Mender something

USAGE:
   mender-something [global options] command [command options] [arguments...]

VERSION:)"))
		<< help_text.str();
	EXPECT_THAT(help_text.str(), testing::HasSubstr(R"(DESCRIPTION:
   something long
that can cas multiple lines
and scaped chars	like tab
	more	tab
and even with very long lines it should not wrap and let the user have it his/her way)"))
		<< help_text.str();
	EXPECT_THAT(help_text.str(), testing::HasSubstr(R"(COMMANDS:
   do-something                 Perform something
   do-other-thing-long-command  Perform the other thing and exit. Just
                                remember to have a long description to also
                                verify the wrapping

GLOBAL OPTIONS:
)")) << help_text.str();
}

TEST(ConfTests, CliCliHelpCommandLookup) {
	const conf::CliApp cli_lookup = {
		.name = "mender-something",
		.short_description = "manage and start the Mender something",
		.long_description = "description only visible on top level app help",
		.commands =
			{
				conf::CliCommand {
					.name = "command-one",
					.description = "command 1 description",
					.options =
						{
							conf::CliOption {
								.long_option = "option-one",
								.description = "description only visible on command 1 help",
							},
						},
				},
				conf::CliCommand {
					.name = "command-two",
					.description = "command 2 description",
					.options =
						{
							conf::CliOption {
								.long_option = "option-two",
								.description = "description only visible on command 2 help",
							},
						},
				},
				conf::CliCommand {
					.name = "command-one",
					.description = "masked command - it will never show",
					.options =
						{
							conf::CliOption {
								.long_option = "masked-command",
								.description = "description will never show",
							},
						},
				},
			},
	};
	std::ostringstream help_non_existing;
	conf::PrintCliCommandHelp(cli_lookup, "non-existing-command", help_non_existing);
	EXPECT_THAT(
		help_non_existing.str(),
		testing::HasSubstr(
			R"(DESCRIPTION:
   description only visible on top level app help)"))
		<< help_non_existing.str();
	EXPECT_THAT(
		help_non_existing.str(),
		testing::HasSubstr(
			R"(COMMANDS:
   command-one  command 1 description
   command-two  command 2 description
   command-one  masked command - it will never show)"))
		<< help_non_existing.str();

	std::ostringstream help_command_1;
	conf::PrintCliCommandHelp(cli_lookup, "command-one", help_command_1);
	EXPECT_EQ(
		R"(NAME:
   mender-something command-one - command 1 description

OPTIONS:
   --option-one  description only visible on command 1 help
   --help, -h    Show help and exit
)",
		help_command_1.str())
		<< help_command_1.str();


	std::ostringstream help_command_2;
	conf::PrintCliCommandHelp(cli_lookup, "command-two", help_command_2);
	EXPECT_EQ(
		R"(NAME:
   mender-something command-two - command 2 description

OPTIONS:
   --option-two  description only visible on command 2 help
   --help, -h    Show help and exit
)",
		help_command_2.str())
		<< help_command_2.str();
}

class TestEnvClearer {
public:
	~TestEnvClearer() {
		unsetenv("HTTP_PROXY");
		unsetenv("HTTPS_PROXY");
		unsetenv("NO_PROXY");
		unsetenv("http_proxy");
		unsetenv("https_proxy");
		unsetenv("no_proxy");
	}
};

TEST(ConfTests, ProxyEnvironmentVariables) {
	// These might interfere, and also won't be reset correctly afterwards.
	ASSERT_EQ(getenv("HTTP_PROXY"), nullptr);
	ASSERT_EQ(getenv("HTTPS_PROXY"), nullptr);
	ASSERT_EQ(getenv("NO_PROXY"), nullptr);
	ASSERT_EQ(getenv("http_proxy"), nullptr);
	ASSERT_EQ(getenv("https_proxy"), nullptr);
	ASSERT_EQ(getenv("no_proxy"), nullptr);

	{
		conf::MenderConfig config;
		vector<string> args;
		auto result = config.ProcessCmdlineArgs(args.begin(), args.end(), conf::CliApp {});
		EXPECT_TRUE(result);
		EXPECT_EQ(config.GetHttpClientConfig().http_proxy, "");
		EXPECT_EQ(config.GetHttpClientConfig().https_proxy, "");
		EXPECT_EQ(config.GetHttpClientConfig().no_proxy, "");
	}

	{
		TestEnvClearer clearer;

		setenv("http_proxy", "abc", 1);
		setenv("https_proxy", "def", 1);
		setenv("no_proxy", "xyz", 1);

		conf::MenderConfig config;
		vector<string> args;
		auto result = config.ProcessCmdlineArgs(args.begin(), args.end(), conf::CliApp {});
		EXPECT_TRUE(result);
		EXPECT_EQ(config.GetHttpClientConfig().http_proxy, "abc");
		EXPECT_EQ(config.GetHttpClientConfig().https_proxy, "def");
		EXPECT_EQ(config.GetHttpClientConfig().no_proxy, "xyz");
	}

	{
		TestEnvClearer clearer;

		setenv("http_proxy", "abc", 1);
		setenv("HTTP_PROXY", "def", 1);

		conf::MenderConfig config;
		vector<string> args;
		auto result = config.ProcessCmdlineArgs(args.begin(), args.end(), conf::CliApp {});
		EXPECT_FALSE(result);
	}

	{
		TestEnvClearer clearer;

		setenv("https_proxy", "abc", 1);
		setenv("HTTPS_PROXY", "def", 1);

		conf::MenderConfig config;
		vector<string> args;
		auto result = config.ProcessCmdlineArgs(args.begin(), args.end(), conf::CliApp {});
		EXPECT_FALSE(result);
	}

	{
		TestEnvClearer clearer;

		setenv("no_proxy", "abc", 1);
		setenv("NO_PROXY", "def", 1);

		conf::MenderConfig config;
		vector<string> args;
		auto result = config.ProcessCmdlineArgs(args.begin(), args.end(), conf::CliApp {});
		EXPECT_FALSE(result);
	}
}

TEST(ConfTests, FallbackConfig) {
	mtesting::TemporaryDirectory tmpdir;

	string conf_file = path::Join(tmpdir.Path(), "mender.conf");
	{
		ofstream f(conf_file);
		f << R"({"ServerURL": "https://right-server.com"})";
		ASSERT_TRUE(f.good());
	}

	string fallback_conf_file = path::Join(tmpdir.Path(), "fallback-mender.conf");
	{
		ofstream f(fallback_conf_file);
		f << R"({"ServerURL": "https://wrong-server.com"})";
		ASSERT_TRUE(f.good());
	}

	vector<string> args {"--config", conf_file, "--fallback-config", fallback_conf_file};
	conf::MenderConfig config;
	config.ProcessCmdlineArgs(args.begin(), args.end(), conf::CliApp {});
	ASSERT_EQ(config.servers.size(), 1);
	EXPECT_EQ(config.servers[0], "https://right-server.com");
}
