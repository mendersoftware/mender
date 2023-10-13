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

#include <iostream>
#include <string>
#include <unordered_set>
#include <vector>

#include <common/config_parser.hpp>
#include <common/path.hpp>

namespace mender {
namespace common {
namespace conf {

using namespace std;
namespace error = mender::common::error;
namespace expected = mender::common::expected;
namespace cfg_parser = mender::common::config_parser;

extern const string kMenderVersion;

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

// NOTE - When updating this class - either adding or removing variables. Be
// sure to update the transitive dependencies also.
class Paths {
private:
	string path_conf_dir = conf::GetEnv("MENDER_CONF_DIR", path::Join("/etc", "mender"));
	string rootfs_scripts_path = path::Join(path_conf_dir, "scripts");
	string conf_file = path::Join(path_conf_dir, "mender.conf");

	string path_data_dir = conf::GetEnv("MENDER_DATA_DIR", path::Join("/usr/share", "mender"));
	string modules_path = path::Join(path_data_dir, "modules/v3");
	string identity_script = path::Join(path_data_dir, "identity", "mender-device-identity");
	string inventory_scripts_dir = path::Join(path_data_dir, "inventory");

	string data_store = conf::GetEnv("MENDER_DATASTORE_DIR", path::Join("/var/lib", "mender"));
	string update_log_path = data_store;
	string artifact_script_path = path::Join(data_store, "scripts");
	string modules_work_path = path::Join(data_store, "modules/v3");
	string bootstrap_artifact_file = path::Join(data_store, "bootstrap.mender");
	string fallback_conf_file = path::Join(data_store, "mender.conf");

	string key_file = path::Join(data_store, "mender-agent.pem");

public:
	string GetPathConfDir() const {
		return path_conf_dir;
	}
	void SetPathConfDir(const string &conf_dir) {
		this->path_conf_dir = conf_dir;
		this->conf_file = path::Join(path_conf_dir, "mender.conf");
		this->rootfs_scripts_path = path::Join(path_conf_dir, "scripts");
	}

	string GetPathDataDir() const {
		return path_data_dir;
	}
	void SetPathDataDir(const string &path_data_dir) {
		this->path_data_dir = path_data_dir;
		this->identity_script = path::Join(path_data_dir, "identity", "mender-device-identity");
		this->inventory_scripts_dir = path::Join(path_data_dir, "inventory");
		this->modules_path = path::Join(path_data_dir, "modules/v3");
	}

	string GetDataStore() const {
		return data_store;
	}
	void SetDataStore(const string &data_store) {
		this->data_store = data_store;
		this->update_log_path = data_store;
		this->fallback_conf_file = path::Join(data_store, "mender.conf");
		this->artifact_script_path = path::Join(data_store, "scripts");
		this->modules_work_path = path::Join(data_store, "modules/v3");
		this->bootstrap_artifact_file = path::Join(data_store, "bootstrap.mender");
		this->key_file = path::Join(data_store, "mender-agent.pem");
		return;
	}

	string GetUpdateLogPath() const {
		return update_log_path;
	}
	void SetUpdateLogPath(const string &update_log_path) {
		this->update_log_path = update_log_path;
	}

	string GetKeyFile() const {
		return key_file;
	}
	void SetKeyFile(const string &key_file) {
		this->key_file = key_file;
	}


	string GetConfFile() const {
		return conf_file;
	}
	void SetConfFile(const string &conf_file) {
		this->conf_file = conf_file;
	}

	string GetFallbackConfFile() const {
		return fallback_conf_file;
	}
	void SetFallbackConfFile(const string &fallback_conf_file) {
		this->fallback_conf_file = fallback_conf_file;
	}

	string GetIdentityScript() const {
		return identity_script;
	}
	void SetIdentityScript(const string &identity_script) {
		this->identity_script = identity_script;
	}

	string GetInventoryScriptsDir() const {
		return inventory_scripts_dir;
	}
	void SetInventoryScriptsDir(const string &inventory_scripts_dir) {
		this->inventory_scripts_dir = inventory_scripts_dir;
	}

	string GetArtScriptsPath() const {
		return artifact_script_path;
	}
	void SetArtScriptsPath(const string &artifact_script_path) {
		this->artifact_script_path = artifact_script_path;
	}

	string GetRootfsScriptsPath() const {
		return rootfs_scripts_path;
	}
	void SetRootfsScriptsPath(const string &rootfs_scripts_path) {
		this->rootfs_scripts_path = rootfs_scripts_path;
	}

	string GetModulesPath() const {
		return modules_path;
	}
	void SetModulesPath(const string &modules_path) {
		this->modules_path = modules_path;
	}

	string GetModulesWorkPath() const {
		return modules_work_path;
	}
	void SetModulesWorkPath(const string &modules_work_path) {
		this->modules_work_path = modules_work_path;
	}

	string GetBootstrapArtifactFile() const {
		return bootstrap_artifact_file;
	}
	void SetBootstrapArtifactFile(const string &bootstrap_artifact_file) {
		this->bootstrap_artifact_file = bootstrap_artifact_file;
	}
};

struct CliOption {
	string long_option;
	string short_option;
	string description;
	string default_value;
	string parameter;
};

struct CliCommand {
	string name;
	string description;
	vector<CliOption> options;
};

struct CliApp {
	string name;
	string short_description;
	string long_description;
	vector<CliCommand> commands;
};

void PrintCliHelp(const CliApp &cli, ostream &stream = std::cout);
void PrintCliCommandHelp(
	const CliApp &cli, const string &command_name, ostream &stream = std::cout);

class MenderConfig : public cfg_parser::MenderConfigFromFile {
public:
	Paths paths {};

	// On success, returns the first non-flag index in `args`.
	expected::ExpectedSize ProcessCmdlineArgs(
		vector<string>::const_iterator start,
		vector<string>::const_iterator end,
		const CliApp &app);
	error::Error LoadDefaults();

private:
	error::Error LoadConfigFile_(const string &path, bool required);
};

} // namespace conf
} // namespace common
} // namespace mender

#endif // MENDER_COMMON_CONF_HPP
