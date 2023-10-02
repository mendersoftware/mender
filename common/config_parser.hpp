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

#include <string>
#include <vector>
#include <common/error.hpp>
#include <common/expected.hpp>

#ifndef MENDER_COMMON_CONFIG_PARSER_HPP
#define MENDER_COMMON_CONFIG_PARSER_HPP

namespace mender {
namespace common {
namespace config_parser {

using namespace std;

using mender::common::expected::ExpectedBool;

namespace error = mender::common::error;

/** HttpsClient holds the configuration for the client side mTLS
	configuration */
struct HttpsClient {
	string certificate;
	string key;
	string ssl_engine;
};

/** Security structure holds the configuration for the client Added for MEN-3924
	in order to provide a way to specify PKI params outside HttpsClient. */
struct ClientSecurity {
	string auth_private_key;
	string ssl_engine;
};

/** Connectivity instructs the client how we want to treat the keep alive
	connections and when a connection is considered idle and therefore closed */
struct ClientConnectivity {
	bool disable_keep_alive = false;
	int idle_conn_timeout_seconds = 0;
};

enum ConfigParserErrorCode {
	NoError = 0,
	ValidationError,
};

class ConfigParserErrorCategoryClass : public std::error_category {
public:
	const char *name() const noexcept override;
	string message(int code) const override;
};
extern const ConfigParserErrorCategoryClass ConfigParserErrorCategory;

error::Error MakeError(ConfigParserErrorCode code, const string &msg);

class MenderConfigFromFile {
public:
	/** Path to the public key used to verify signed updates.  Only one of
		artifact_verify_key/artifact_verify_keys can be specified. This key still exists in
		the config, but we automatically "promote" this field to the `artifact_verify_keys`
		field, as long as only one is set.*/
	// string artifact_verify_key;

	/** List of verification keys for verifying signed updates.
		Starting in order from the first key in the list,
		each key will try to verify the artifact until one succeeds.
		Only one of artifact_verify_key/artifact_verify_keys can be specified. */
	vector<string> artifact_verify_keys;

	/** HTTPS client parameters */
	HttpsClient https_client;

	/** Security parameters */
	ClientSecurity security;

	/** Connectivity connection handling and transfer parameters */
	ClientConnectivity connectivity;

	/** Rootfs device paths. These are not parsed by the client anymore, since rootfs updates
		are now handled by an update module. But for historical reasons, they still share config
		files, so these options can still be in the file. */
	// string rootfs_part_A;
	// string rootfs_part_B;

	/** Command to set active partition */
	string boot_utilities_set_active_part;

	/** Command to get the partition which will boot next */
	string boot_utilities_get_next_active_part;

	/** Path to the device type file */
	string device_type_file;

	/** DBus configuration. This option was removed in Mender v4.0.0, where we use two daemons
		and are totally reliant on DBus to communicate between them. */
	// bool dbus_enabled = true;

	/** Expiration timeout for the control map. The Update Control feature has been removed from
		the C++ client. */
	// int update_control_map_expiration_time_seconds = 0;

	/** Expiration timeout for the control map when just booted. The Update Control feature has
		been removed from the C++ client. */
	// int update_control_map_boot_expiration_time_seconds = 600; // 10 min

	/** Poll interval for checking for new updates */
	int update_poll_interval_seconds = 1800;

	/** Poll interval for periodically sending inventory data */
	int inventory_poll_interval_seconds = 28800;

	/** Skip CA certificate validation */
	bool skip_verify = false;

	/** Global retry polling max interval for fetching update, authorize wait and update status */
	int retry_poll_interval_seconds = 0;

	/** Global max retry poll count */
	int retry_poll_count = 0;

	/* State script parameters */
	int state_script_timeout_seconds = 3600;       // 1 hour
	int state_script_retry_timeout_seconds = 1800; // 30 min

	/** Poll interval for checking for update (check-update) */
	int state_script_retry_interval_seconds = 60;

	/* Update module parameters */
	/** The timeout for the execution of the update module, after which it will
		be killed. */
	int module_timeout_seconds = 14400; // 4 hours

	/** Path to server SSL certificate */
	string server_certificate;

	/** Server URL (For single server conf) */
	string server_url;

	/** Path to deployment log file */
	string update_log_path;

	/** Server JWT TenantToken */
	string tenant_token;

	/** List of available servers, to which client can fall over */
	vector<string> servers;

	/** Log level which takes effect right before daemon startup */
	string daemon_log_level;

	/**
	 * Loads values from the given file and overrides the current values of the
	 * respective above fields with them.
	 *
	 * @return whether some new values were actually applied or not
	 * @note Invalid JSON data is ignored -- the whole file if it's not a valid
	 *       JSON file and if it is then extra fields are ignored and fields of
	 *       unexpected types are ignored too.
	 */
	ExpectedBool LoadFile(const string &path);

	void Reset();

	ExpectedBool ValidateConfig();
	ExpectedBool ValidateServerConfig() const;
};

} // namespace config_parser
} // namespace common
} // namespace mender

#endif // MENDER_COMMON_CONFIG_PARSER_HPP
