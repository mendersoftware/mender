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

#ifndef MENDER_COMMON_CONF_HPP
#define MENDER_COMMON_CONF_HPP

#include <string>
#include <vector>
#include <unordered_set>
#include <common/config_parser.hpp>
#include <common/conf/paths.hpp>

namespace mender {
namespace common {
namespace conf {

using namespace std;
namespace error = mender::common::error;
namespace expected = mender::common::expected;
namespace cfg_parser = mender::common::config_parser;

enum ConfigErrorCode {
	NoError = 0,
	InvalidOptionsError,
};

class ConfigErrorCategoryClass : public std::error_category {
public:
	const char *name() const noexcept override;
	string message(int code) const override;
};
extern const ConfigErrorCategoryClass ConfigErrorCategory;

error::Error MakeError(ConfigErrorCode code, const string &msg);


string GetEnv(const string &var_name, const string &default_value);

struct OptionValue {
	string option;
	string value;
};

using OptsSet = unordered_set<string>;
using ExpectedOptionValue = expected::expected<OptionValue, error::Error>;

enum class ArgumentsMode {
	AcceptBareArguments,
	RejectBareArguments,
	StopAtBareArguments,
};

class CmdlineOptionsIterator {
public:
	CmdlineOptionsIterator(
		vector<string>::const_iterator start,
		vector<string>::const_iterator end,
		const OptsSet &opts_with_value,
		const OptsSet &opts_without_value) :
		start_ {start},
		end_ {end},
		opts_with_value_ {opts_with_value},
		opts_wo_value_ {opts_without_value} {};
	ExpectedOptionValue Next();

	size_t GetPos() const {
		return pos_;
	}

	void SetArgumentsMode(ArgumentsMode mode) {
		mode_ = mode;
	}

private:
	vector<string>::const_iterator start_;
	vector<string>::const_iterator end_;
	OptsSet opts_with_value_;
	OptsSet opts_wo_value_;
	size_t pos_ = 0;
	bool past_double_dash_ = false;
	ArgumentsMode mode_ {ArgumentsMode::RejectBareArguments};
};

class MenderConfig : public cfg_parser::MenderConfigFromFile {
public:
	string data_store_dir = paths::DefaultDataStore;

	// On success, returns the first non-flag index in `args`.
	expected::ExpectedSize ProcessCmdlineArgs(
		vector<string>::const_iterator start, vector<string>::const_iterator end);
	error::Error LoadDefaults();

private:
	error::Error LoadConfigFile_(const string &path, bool required);
};

} // namespace conf
} // namespace common
} // namespace mender

#endif // MENDER_COMMON_CONF_HPP
