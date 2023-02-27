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

namespace mender {
namespace common {
namespace conf {

using namespace std;
namespace error = mender::common::error;
namespace expected = mender::common::expected;

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

class CmdlineOptionsIterator {
public:
	CmdlineOptionsIterator(
		const vector<string> &args,
		const OptsSet &opts_with_value,
		const OptsSet &opts_without_value) :
		args_(args),
		opts_with_value_(opts_with_value),
		opts_wo_value_(opts_without_value) {};
	ExpectedOptionValue Next();

private:
	const vector<string> &args_;
	OptsSet opts_with_value_;
	OptsSet opts_wo_value_;
	size_t pos_ = 0;
	bool past_double_dash_ = false;
};

} // namespace conf
} // namespace common
} // namespace mender

#endif // MENDER_COMMON_CONF_HPP
