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

#ifndef MENDER_COMMON_CLI_HELP_HPP
#define MENDER_COMMON_CLI_HELP_HPP

#include <iostream>
#include <string>
#include <vector>

namespace mender {
namespace common {
namespace cli {

using namespace std;

struct Option {
	string long_option;
	string short_option;
	string description;
	string default_value;
	string parameter;
};

struct Command {
	string name;
	string description;
	vector<Option> options;
};

struct App {
	string name;
	string short_description;
	string long_description;
	string version;
	vector<Command> commands;
	vector<Option> global_options;
};

void PrintCliHelp(const App &cli, ostream &stream = std::cout);
void PrintCliCommandHelp(const App &cli, const string &command_name, ostream &stream = std::cout);

} // namespace cli
} // namespace common
} // namespace mender

#endif // MENDER_COMMON_CLI_HELP_HPP
