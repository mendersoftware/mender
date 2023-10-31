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

#include <common/config_parser.hpp>

#include <string>
#include <vector>
#include <algorithm>

#include <common/json.hpp>
#include <common/log.hpp>

namespace mender {
namespace common {
namespace config_parser {

using namespace std;

namespace json = mender::common::json;
namespace log = mender::common::log;

const ConfigParserErrorCategoryClass ConfigParserErrorCategory;

const char *ConfigParserErrorCategoryClass::name() const noexcept {
	return "ConfigParserErrorCategory";
}

string ConfigParserErrorCategoryClass::message(int code) const {
	switch (code) {
	case NoError:
		return "Success";
	case ValidationError:
		return "Validation error";
	default:
		return "Unknown";
	}
}

error::Error MakeError(ConfigParserErrorCode code, const string &msg) {
	return error::Error(error_condition(code, ConfigParserErrorCategory), msg);
}

ExpectedBool MenderConfigFromFile::LoadFile(const string &path) {
	const json::ExpectedJson e_cfg_json = json::LoadFromFile(path);
	if (!e_cfg_json) {
		auto err = e_cfg_json.error();
		return expected::unexpected(err);
	}

	bool applied = false;

	const json::Json cfg_json = e_cfg_json.value();

	json::ExpectedJson e_cfg_value = cfg_json.Get("DeviceTypeFile");
	if (e_cfg_value) {
		const json::Json value_json = e_cfg_value.value();
		const json::ExpectedString e_cfg_string = value_json.GetString();
		if (e_cfg_string) {
			this->device_type_file = e_cfg_string.value();
			applied = true;
		}
	}

	e_cfg_value = cfg_json.Get("ServerCertificate");
	if (e_cfg_value) {
		const json::Json value_json = e_cfg_value.value();
		const json::ExpectedString e_cfg_string = value_json.GetString();
		if (e_cfg_string) {
			this->server_certificate = e_cfg_string.value();
			applied = true;
		}
	}

	e_cfg_value = cfg_json.Get("UpdateLogPath");
	if (e_cfg_value) {
		const json::Json value_json = e_cfg_value.value();
		const json::ExpectedString e_cfg_string = value_json.GetString();
		if (e_cfg_string) {
			this->update_log_path = e_cfg_string.value();
			applied = true;
		}
	}

	e_cfg_value = cfg_json.Get("TenantToken");
	if (e_cfg_value) {
		const json::Json value_json = e_cfg_value.value();
		const json::ExpectedString e_cfg_string = value_json.GetString();
		if (e_cfg_string) {
			this->tenant_token = e_cfg_string.value();
			applied = true;
		}
	}

	e_cfg_value = cfg_json.Get("DaemonLogLevel");
	if (e_cfg_value) {
		const json::Json value_json = e_cfg_value.value();
		const json::ExpectedString e_cfg_string = value_json.GetString();
		if (e_cfg_string) {
			this->daemon_log_level = e_cfg_string.value();
			applied = true;
		}
	}

	/* Boolean values now */
	e_cfg_value = cfg_json.Get("SkipVerify");
	if (e_cfg_value) {
		const json::Json value_json = e_cfg_value.value();
		const json::ExpectedBool e_cfg_bool = value_json.GetBool();
		if (e_cfg_bool) {
			this->skip_verify = e_cfg_bool.value();
			applied = true;
		}
	}

	e_cfg_value = cfg_json.Get("UpdatePollIntervalSeconds");
	if (e_cfg_value) {
		const json::Json value_json = e_cfg_value.value();
		const auto e_cfg_int = value_json.GetInt();
		if (e_cfg_int) {
			this->update_poll_interval_seconds = e_cfg_int.value();
			applied = true;
		}
	}

	e_cfg_value = cfg_json.Get("InventoryPollIntervalSeconds");
	if (e_cfg_value) {
		const json::Json value_json = e_cfg_value.value();
		const auto e_cfg_int = value_json.GetInt();
		if (e_cfg_int) {
			this->inventory_poll_interval_seconds = e_cfg_int.value();
			applied = true;
		}
	}

	e_cfg_value = cfg_json.Get("RetryPollIntervalSeconds");
	if (e_cfg_value) {
		const json::Json value_json = e_cfg_value.value();
		const auto e_cfg_int = value_json.GetInt();
		if (e_cfg_int) {
			this->retry_poll_interval_seconds = e_cfg_int.value();
			applied = true;
		}
	}

	e_cfg_value = cfg_json.Get("RetryPollCount");
	if (e_cfg_value) {
		const json::Json value_json = e_cfg_value.value();
		const auto e_cfg_int = value_json.GetInt();
		if (e_cfg_int) {
			this->retry_poll_count = e_cfg_int.value();
			applied = true;
		}
	}

	e_cfg_value = cfg_json.Get("StateScriptTimeoutSeconds");
	if (e_cfg_value) {
		const json::Json value_json = e_cfg_value.value();
		const auto e_cfg_int = value_json.GetInt();
		if (e_cfg_int) {
			this->state_script_timeout_seconds = e_cfg_int.value();
			applied = true;
		}
	}

	e_cfg_value = cfg_json.Get("StateScriptRetryTimeoutSeconds");
	if (e_cfg_value) {
		const json::Json value_json = e_cfg_value.value();
		const auto e_cfg_int = value_json.GetInt();
		if (e_cfg_int) {
			this->state_script_retry_timeout_seconds = e_cfg_int.value();
			applied = true;
		}
	}

	e_cfg_value = cfg_json.Get("StateScriptRetryIntervalSeconds");
	if (e_cfg_value) {
		const json::Json value_json = e_cfg_value.value();
		const auto e_cfg_int = value_json.GetInt();
		if (e_cfg_int) {
			this->state_script_retry_interval_seconds = e_cfg_int.value();
			applied = true;
		}
	}

	e_cfg_value = cfg_json.Get("ModuleTimeoutSeconds");
	if (e_cfg_value) {
		const json::Json value_json = e_cfg_value.value();
		const auto e_cfg_int = value_json.GetInt();
		if (e_cfg_int) {
			this->module_timeout_seconds = e_cfg_int.value();
			applied = true;
		}
	}


	e_cfg_value = cfg_json.Get("ArtifactVerifyKeys");
	if (e_cfg_value) {
		const json::Json value_array = e_cfg_value.value();
		const json::ExpectedSize e_n_items = value_array.GetArraySize();
		if (e_n_items) {
			for (size_t i = 0; i < e_n_items.value(); i++) {
				const json::ExpectedJson e_array_item = value_array.Get(i);
				if (e_array_item) {
					const json::ExpectedString e_item_string = e_array_item.value().GetString();
					if (e_item_string) {
						const string item_value = e_item_string.value();
						if (count(
								this->artifact_verify_keys.begin(),
								this->artifact_verify_keys.end(),
								item_value)
							== 0) {
							this->artifact_verify_keys.push_back(item_value);
							applied = true;
						}
					}
				}
			}
		}
	}

	e_cfg_value = cfg_json.Get("ArtifactVerifyKey");
	if (e_cfg_value) {
		const json::Json value_json = e_cfg_value.value();
		const json::ExpectedString e_cfg_string = value_json.GetString();
		if (e_cfg_string) {
			if (artifact_verify_keys.size() != 0) {
				auto err = MakeError(
					ConfigParserErrorCode::ValidationError,
					"Both 'ArtifactVerifyKey' and 'ArtifactVerifyKeys' are set");
				return expected::unexpected(err);
			}
			this->artifact_verify_keys.push_back(e_cfg_string.value());
			applied = true;
		}
	}

	e_cfg_value = cfg_json.Get("Servers");
	if (e_cfg_value) {
		const json::Json value_array = e_cfg_value.value();
		const json::ExpectedSize e_n_items = value_array.GetArraySize();
		if (e_n_items) {
			for (size_t i = 0; i < e_n_items.value(); i++) {
				const json::ExpectedJson e_array_item = value_array.Get(i);
				if (e_array_item) {
					const json::ExpectedJson e_item_json = e_array_item.value().Get("ServerURL");
					if (e_item_json) {
						const json::ExpectedString e_item_string = e_item_json.value().GetString();
						if (e_item_string) {
							const string item_value = e_item_string.value();
							if (count(this->servers.begin(), this->servers.end(), item_value)
								== 0) {
								this->servers.push_back(std::move(item_value));
								applied = true;
							}
						}
					}
				}
			}
		}
	}

	e_cfg_value = cfg_json.Get("ServerURL");
	if (e_cfg_value) {
		const json::Json value_json = e_cfg_value.value();
		const json::ExpectedString e_cfg_string = value_json.GetString();
		if (e_cfg_string) {
			if (servers.size() != 0) {
				auto err = MakeError(
					ConfigParserErrorCode::ValidationError,
					"Both 'Servers' AND 'ServerURL given in the configuration. Please set only one of these fields");
				return expected::unexpected(err);
			}
			this->servers.push_back(e_cfg_string.value());
			applied = true;
		}
	}

	/* Last but not least, complex values */
	e_cfg_value = cfg_json.Get("HttpsClient");
	if (e_cfg_value) {
		const json::Json value_json = e_cfg_value.value();
		json::ExpectedJson e_cfg_subval = value_json.Get("Certificate");
		if (e_cfg_subval) {
			const json::Json subval_json = e_cfg_subval.value();
			const json::ExpectedString e_cfg_string = subval_json.GetString();
			if (e_cfg_string) {
				this->https_client.certificate = e_cfg_string.value();
				applied = true;
			}
		}

		e_cfg_subval = value_json.Get("Key");
		if (e_cfg_subval) {
			const json::Json subval_json = e_cfg_subval.value();
			const json::ExpectedString e_cfg_string = subval_json.GetString();
			if (e_cfg_string) {
				this->https_client.key = e_cfg_string.value();
				applied = true;
			}
		}

		e_cfg_subval = value_json.Get("SSLEngine");
		if (e_cfg_subval) {
			const json::Json subval_json = e_cfg_subval.value();
			const json::ExpectedString e_cfg_string = subval_json.GetString();
			if (e_cfg_string) {
				this->https_client.ssl_engine = e_cfg_string.value();
				applied = true;
			}
		}
	}

	e_cfg_value = cfg_json.Get("Security");
	if (e_cfg_value) {
		const json::Json value_json = e_cfg_value.value();
		json::ExpectedJson e_cfg_subval = value_json.Get("AuthPrivateKey");
		if (e_cfg_subval) {
			const json::Json subval_json = e_cfg_subval.value();
			const json::ExpectedString e_cfg_string = subval_json.GetString();
			if (e_cfg_string) {
				this->security.auth_private_key = e_cfg_string.value();
				applied = true;
			}
		}

		e_cfg_subval = value_json.Get("SSLEngine");
		if (e_cfg_subval) {
			const json::Json subval_json = e_cfg_subval.value();
			const json::ExpectedString e_cfg_string = subval_json.GetString();
			if (e_cfg_string) {
				this->security.ssl_engine = e_cfg_string.value();
				applied = true;
			}
		}
	}

	e_cfg_value = cfg_json.Get("Connectivity");
	if (e_cfg_value) {
		const json::Json value_json = e_cfg_value.value();
		json::ExpectedJson e_cfg_subval = value_json.Get("DisableKeepAlive");
		if (e_cfg_subval) {
			const json::Json subval_json = e_cfg_subval.value();
			const json::ExpectedBool e_cfg_bool = subval_json.GetBool();
			if (e_cfg_bool) {
				this->connectivity.disable_keep_alive = e_cfg_bool.value();
				applied = true;
			}
		}
	}

	return applied;
}

void MenderConfigFromFile::Reset() {
	*this = MenderConfigFromFile();
}

} // namespace config_parser
} // namespace common
} // namespace mender
