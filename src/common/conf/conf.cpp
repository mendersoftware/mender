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
#include <cstdlib>
#include <cerrno>

#include <mender-version.h>

#include <common/error.hpp>
#include <common/expected.hpp>
#include <common/log.hpp>
#include <common/json.hpp>

namespace mender {
namespace common {
namespace conf {

using namespace std;
namespace error = mender::common::error;
namespace expected = mender::common::expected;
namespace log = mender::common::log;
namespace json = mender::common::json;

const string kMenderVersion = MENDER_VERSION;

const ConfigErrorCategoryClass ConfigErrorCategory;

const char *ConfigErrorCategoryClass::name() const noexcept {
	return "ConfigErrorCategory";
}

string ConfigErrorCategoryClass::message(int code) const {
	switch (code) {
	case NoError:
		return "Success";
	case InvalidOptionsError:
		return "Invalid options given";
	}
	assert(false);
	return "Unknown";
}

error::Error MakeError(ConfigErrorCode code, const string &msg) {
	return error::Error(error_condition(code, ConfigErrorCategory), msg);
}


string GetEnv(const string &var_name, const string &default_value) {
	const char *value = getenv(var_name.c_str());
	if (value == nullptr) {
		return string(default_value);
	} else {
		return string(value);
	}
}

ExpectedOptionValue CmdlineOptionsIterator::Next() {
	string option = "";
	string value = "";

	if (start_ + pos_ >= end_) {
		return ExpectedOptionValue({"", ""});
	}

	if (past_double_dash_) {
		OptionValue opt_val {"", start_[pos_]};
		pos_++;
		return ExpectedOptionValue(opt_val);
	}

	if (start_[pos_] == "--") {
		past_double_dash_ = true;
		pos_++;
		return ExpectedOptionValue({"--", ""});
	}

	if (start_[pos_][0] == '-') {
		auto eq_idx = start_[pos_].find('=');
		if (eq_idx != string::npos) {
			option = start_[pos_].substr(0, eq_idx);
			value = start_[pos_].substr(eq_idx + 1, start_[pos_].size() - eq_idx - 1);
			pos_++;
		} else {
			option = start_[pos_];
			pos_++;
		}

		if (opts_with_value_.count(option) != 0) {
			// option with value
			if ((value == "") && ((start_ + pos_ >= end_) || (start_[pos_][0] == '-'))) {
				// the next item is not a value
				error::Error err = MakeError(
					ConfigErrorCode::InvalidOptionsError, "Option " + option + " missing value");
				return ExpectedOptionValue(expected::unexpected(err));
			} else if (value == "") {
				// only assign the next item as value if there was no value
				// specified as '--opt=value' (parsed above)
				value = start_[pos_];
				pos_++;
			}
		} else if (opts_wo_value_.count(option) == 0) {
			// unknown option
			error::Error err = MakeError(
				ConfigErrorCode::InvalidOptionsError, "Unrecognized option '" + option + "'");
			return ExpectedOptionValue(expected::unexpected(err));
		} else if (value != "") {
			// option without a value, yet, there was a value specified as '--opt=value' (parsed
			// above)
			error::Error err = MakeError(
				ConfigErrorCode::InvalidOptionsError,
				"Option " + option + " doesn't expect a value");
			return ExpectedOptionValue(expected::unexpected(err));
		}
	} else {
		switch (mode_) {
		case ArgumentsMode::AcceptBareArguments:
			value = start_[pos_];
			pos_++;
			break;
		case ArgumentsMode::RejectBareArguments:
			return expected::unexpected(MakeError(
				ConfigErrorCode::InvalidOptionsError,
				"Unexpected argument '" + start_[pos_] + "'"));
		case ArgumentsMode::StopAtBareArguments:
			return ExpectedOptionValue({"", ""});
		}
	}

	return ExpectedOptionValue({std::move(option), std::move(value)});
}

expected::ExpectedSize MenderConfig::ProcessCmdlineArgs(
	vector<string>::const_iterator start, vector<string>::const_iterator end, const CliApp &app) {
	bool explicit_config_path = false;
	bool explicit_fallback_config_path = false;
	string log_file = "";
	string log_level;
	string trusted_cert;
	bool skip_verify_arg = false;
	bool version_arg = false;
	bool help_arg = false;

	CmdlineOptionsIterator opts_iter(
		start, end, GlobalOptsSetWithValue(), GlobalOptsSetWithoutValue());
	opts_iter.SetArgumentsMode(ArgumentsMode::StopAtBareArguments);
	auto ex_opt_val = opts_iter.Next();
	int arg_count = 0;
	while (ex_opt_val && ((ex_opt_val.value().option != "") || (ex_opt_val.value().value != ""))) {
		arg_count++;
		auto opt_val = ex_opt_val.value();
		if ((opt_val.option == "--config") || (opt_val.option == "-c")) {
			paths.SetConfFile(opt_val.value);
			explicit_config_path = true;
		} else if ((opt_val.option == "--fallback-config") || (opt_val.option == "-b")) {
			paths.SetFallbackConfFile(opt_val.value);
			explicit_fallback_config_path = true;
		} else if ((opt_val.option == "--data") || (opt_val.option == "-d")) {
			paths.SetDataStore(opt_val.value);
		} else if ((opt_val.option == "--log-file") || (opt_val.option == "-L")) {
			log_file = opt_val.value;
		} else if ((opt_val.option == "--log-level") || (opt_val.option == "-l")) {
			log_level = opt_val.value;
		} else if ((opt_val.option == "--trusted-certs") || (opt_val.option == "-E")) {
			trusted_cert = opt_val.value;
		} else if (opt_val.option == "--skipverify") {
			skip_verify_arg = true;
		} else if ((opt_val.option == "--version") || (opt_val.option == "-v")) {
			version_arg = true;
		} else if ((opt_val.option == "--help") || (opt_val.option == "-h")) {
			help_arg = true;
			break;
		} else {
			assert(false);
		}
		ex_opt_val = opts_iter.Next();
	}
	if (!ex_opt_val) {
		return expected::unexpected(ex_opt_val.error());
	}

	if (version_arg) {
		if (arg_count > 1 || opts_iter.GetPos() < static_cast<size_t>(end - start)) {
			return expected::unexpected(error::Error(
				make_error_condition(errc::invalid_argument),
				"--version can not be combined with other commands and arguments"));
		} else {
			cout << kMenderVersion << endl;
			return expected::unexpected(error::MakeError(error::ExitWithSuccessError, ""));
		}
	}

	if (help_arg) {
		PrintCliHelp(app);
		return expected::unexpected(error::MakeError(error::ExitWithSuccessError, ""));
	}

	if (log_file != "") {
		auto err = log::SetupFileLogging(log_file, true);
		if (error::NoError != err) {
			return expected::unexpected(err);
		}
	}

	SetLevel(log::kDefaultLogLevel);

	if (log_level != "") {
		auto ex_log_level = log::StringToLogLevel(log_level);
		if (!ex_log_level) {
			return expected::unexpected(ex_log_level.error());
		}
		SetLevel(ex_log_level.value());
	}

	auto err = LoadConfigFile_(paths.GetConfFile(), explicit_config_path);
	if (error::NoError != err) {
		this->Reset();
		return expected::unexpected(err);
	}

	err = LoadConfigFile_(paths.GetFallbackConfFile(), explicit_fallback_config_path);
	if (error::NoError != err) {
		this->Reset();
		return expected::unexpected(err);
	}

	if (this->update_log_path != "") {
		paths.SetUpdateLogPath(this->update_log_path);
	}

	if (log_level == "" && this->daemon_log_level != "") {
		auto ex_log_level = log::StringToLogLevel(this->daemon_log_level);
		if (!ex_log_level) {
			return expected::unexpected(ex_log_level.error());
		}
		SetLevel(ex_log_level.value());
	}

	if (trusted_cert != "") {
		this->server_certificate = trusted_cert;
	}

	if (skip_verify_arg) {
		this->skip_verify = true;
	}

	http_client_config_.server_cert_path = server_certificate;
	http_client_config_.client_cert_path = https_client.certificate;
	http_client_config_.client_cert_key_path = https_client.key;
	http_client_config_.skip_verify = skip_verify;

	auto proxy = http::GetHttpProxyStringFromEnvironment();
	if (proxy) {
		http_client_config_.http_proxy = proxy.value();
	} else {
		return expected::unexpected(proxy.error());
	}

	proxy = http::GetHttpsProxyStringFromEnvironment();
	if (proxy) {
		http_client_config_.https_proxy = proxy.value();
	} else {
		return expected::unexpected(proxy.error());
	}

	proxy = http::GetNoProxyStringFromEnvironment();
	if (proxy) {
		http_client_config_.no_proxy = proxy.value();
	} else {
		return expected::unexpected(proxy.error());
	}

	return opts_iter.GetPos();
}

error::Error MenderConfig::LoadConfigFile_(const string &path, bool required) {
	auto ret = this->LoadFile(path);
	if (!ret) {
		if (required) {
			// any failure when a file is required (e.g. path was given explicitly) means an error
			log::Error("Failed to load config from '" + path + "': " + ret.error().message);
			return ret.error();
		} else if (ret.error().IsErrno(ENOENT)) {
			// File doesn't exist, OK for non-required
			log::Debug("Failed to load config from '" + path + "': " + ret.error().message);
			return error::NoError;
		} else {
			// other errors (parsing errors,...) for default paths should produce warnings
			log::Warning("Failed to load config from '" + path + "': " + ret.error().message);
			return error::NoError;
		}
	}
	// else

	return error::NoError;
}

} // namespace conf
} // namespace common
} // namespace mender
