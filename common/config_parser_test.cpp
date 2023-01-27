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

#include <gtest/gtest.h>
#include <gmock/gmock.h>

#include <fstream>

namespace config_parser = mender::common::config_parser;

using namespace std;

const string complete_config = R"({
  "ArtifactVerifyKey": "ArtifactVerifyKey_value",
  "RootfsPartA": "RootfsPartA_value",
  "RootfsPartB": "RootfsPartB_value",
  "BootUtilitiesSetActivePart": "BootUtilitiesSetActivePart_value",
  "BootUtilitiesGetNextActivePart": "BootUtilitiesGetNextActivePart_value",
  "DeviceTypeFile": "DeviceTypeFile_value",
  "ServerCertificate": "ServerCertificate_value",
  "ServerURL": "ServerURL_value",
  "UpdateLogPath": "UpdateLogPath_value",
  "TenantToken": "TenantToken_value",
  "DaemonLogLevel": "DaemonLogLevel_value",

  "SkipVerify": true,
  "DBus": { "Enabled": true },

  "UpdateControlMapExpirationTimeSeconds": 1,
  "UpdateControlMapBootExpirationTimeSeconds": 2,
  "UpdatePollIntervalSeconds": 3,
  "InventoryPollIntervalSeconds": 4,
  "RetryPollIntervalSeconds": 5,
  "RetryPollCount": 6,
  "StateScriptTimeoutSeconds": 7,
  "StateScriptRetryTimeoutSeconds": 8,
  "StateScriptRetryIntervalSeconds": 9,
  "ModuleTimeoutSeconds": 10,

  "ArtifactVerifyKeys": [
    "key1",
    "key2",
    "key3"
  ],

  "Servers": [
   {"ServerURL": "server1"},
   {"ServerURL": "server2"}
  ],

  "HttpsClient": {
    "Certificate": "Certificate_value",
    "Key": "Key_value",
    "SSLEngine": "SSLEngine_value"
  },

  "Security": {
    "AuthPrivateKey": "AuthPrivateKey_value",
    "SSLEngine": "SecuritySSLEngine_value"
  },

  "Connectivity": {
    "DisableKeepAlive": true,
    "IdleConnTimeoutSeconds": 11
  },

  "extra": ["this", "should", "be", "ignored"]
})";

class ConfigParserTests : public testing::Test {
protected:
	const char *test_config_fname = "test.json";

	void TearDown() override {
		remove(test_config_fname);
	}
};

TEST(ConfigParserDefaultsTests, ConfigParserDefaults) {
	config_parser::MenderConfigFromFile mc;

	EXPECT_EQ(mc.artifact_verify_key, "");
	EXPECT_EQ(mc.rootfs_part_A, "");
	EXPECT_EQ(mc.rootfs_part_B, "");
	EXPECT_EQ(mc.boot_utilities_set_active_part, "");
	EXPECT_EQ(mc.boot_utilities_get_next_active_part, "");
	EXPECT_EQ(mc.device_type_file, "");
	EXPECT_EQ(mc.server_certificate, "");
	EXPECT_EQ(mc.server_url, "");
	EXPECT_EQ(mc.update_log_path, "");
	EXPECT_EQ(mc.tenant_token, "");
	EXPECT_EQ(mc.daemon_log_level, "");

	EXPECT_FALSE(mc.skip_verify);
	EXPECT_FALSE(mc.dbus_enabled);

	EXPECT_EQ(mc.update_control_map_expiration_time_seconds, 0);
	EXPECT_EQ(mc.update_control_map_boot_expiration_time_seconds, 0);
	EXPECT_EQ(mc.update_poll_interval_seconds, 0);
	EXPECT_EQ(mc.inventory_poll_interval_seconds, 0);
	EXPECT_EQ(mc.retry_poll_interval_seconds, 0);
	EXPECT_EQ(mc.retry_poll_count, 0);
	EXPECT_EQ(mc.state_script_timeout_seconds, 0);
	EXPECT_EQ(mc.state_script_retry_timeout_seconds, 0);
	EXPECT_EQ(mc.state_script_retry_interval_seconds, 0);
	EXPECT_EQ(mc.module_timeout_seconds, 0);

	EXPECT_EQ(mc.artifact_verify_keys.size(), 0);

	EXPECT_EQ(mc.servers.size(), 0);

	EXPECT_EQ(mc.https_client.certificate, "");
	EXPECT_EQ(mc.https_client.key, "");
	EXPECT_EQ(mc.https_client.ssl_engine, "");

	EXPECT_EQ(mc.security.auth_private_key, "");
	EXPECT_EQ(mc.security.ssl_engine, "");

	EXPECT_FALSE(mc.connectivity.disable_keep_alive);
	EXPECT_EQ(mc.connectivity.idle_conn_timeout_seconds, 0);
}

TEST_F(ConfigParserTests, LoadComplete) {
	ofstream os(test_config_fname);
	os << complete_config;
	os.close();

	config_parser::MenderConfigFromFile mc;
	config_parser::ExpectedBool ret = mc.LoadFile(test_config_fname);
	ASSERT_TRUE(ret);
	EXPECT_TRUE(ret.value());

	EXPECT_EQ(mc.artifact_verify_key, "ArtifactVerifyKey_value");
	EXPECT_EQ(mc.rootfs_part_A, "RootfsPartA_value");
	EXPECT_EQ(mc.rootfs_part_B, "RootfsPartB_value");
	EXPECT_EQ(mc.boot_utilities_set_active_part, "BootUtilitiesSetActivePart_value");
	EXPECT_EQ(mc.boot_utilities_get_next_active_part, "BootUtilitiesGetNextActivePart_value");
	EXPECT_EQ(mc.device_type_file, "DeviceTypeFile_value");
	EXPECT_EQ(mc.server_certificate, "ServerCertificate_value");
	EXPECT_EQ(mc.server_url, "ServerURL_value");
	EXPECT_EQ(mc.update_log_path, "UpdateLogPath_value");
	EXPECT_EQ(mc.tenant_token, "TenantToken_value");
	EXPECT_EQ(mc.daemon_log_level, "DaemonLogLevel_value");

	EXPECT_TRUE(mc.skip_verify);
	EXPECT_TRUE(mc.dbus_enabled);

	EXPECT_EQ(mc.update_control_map_expiration_time_seconds, 1);
	EXPECT_EQ(mc.update_control_map_boot_expiration_time_seconds, 2);
	EXPECT_EQ(mc.update_poll_interval_seconds, 3);
	EXPECT_EQ(mc.inventory_poll_interval_seconds, 4);
	EXPECT_EQ(mc.retry_poll_interval_seconds, 5);
	EXPECT_EQ(mc.retry_poll_count, 6);
	EXPECT_EQ(mc.state_script_timeout_seconds, 7);
	EXPECT_EQ(mc.state_script_retry_timeout_seconds, 8);
	EXPECT_EQ(mc.state_script_retry_interval_seconds, 9);
	EXPECT_EQ(mc.module_timeout_seconds, 10);

	EXPECT_EQ(mc.artifact_verify_keys.size(), 3);
	EXPECT_EQ(mc.artifact_verify_keys[0], "key1");
	EXPECT_EQ(mc.artifact_verify_keys[1], "key2");
	EXPECT_EQ(mc.artifact_verify_keys[2], "key3");

	EXPECT_EQ(mc.servers.size(), 2);
	EXPECT_EQ(mc.servers[0], "server1");
	EXPECT_EQ(mc.servers[1], "server2");

	EXPECT_EQ(mc.https_client.certificate, "Certificate_value");
	EXPECT_EQ(mc.https_client.key, "Key_value");
	EXPECT_EQ(mc.https_client.ssl_engine, "SSLEngine_value");

	EXPECT_EQ(mc.security.auth_private_key, "AuthPrivateKey_value");
	EXPECT_EQ(mc.security.ssl_engine, "SecuritySSLEngine_value");

	EXPECT_TRUE(mc.connectivity.disable_keep_alive);
	EXPECT_EQ(mc.connectivity.idle_conn_timeout_seconds, 11);
}

TEST_F(ConfigParserTests, LoadPartial) {
	ofstream os(test_config_fname);
	os << R"({
  "ArtifactVerifyKey": "ArtifactVerifyKey_value",
  "RootfsPartB": "RootfsPartB_value",
  "BootUtilitiesSetActivePart": "BootUtilitiesSetActivePart_value",
  "DeviceTypeFile": "DeviceTypeFile_value",
  "ServerURL": "ServerURL_value"
})";
	os.close();

	config_parser::MenderConfigFromFile mc;
	config_parser::ExpectedBool ret = mc.LoadFile(test_config_fname);
	ASSERT_TRUE(ret);
	EXPECT_TRUE(ret.value());

	EXPECT_EQ(mc.artifact_verify_key, "ArtifactVerifyKey_value");
	EXPECT_EQ(mc.rootfs_part_A, "");
	EXPECT_EQ(mc.rootfs_part_B, "RootfsPartB_value");
	EXPECT_EQ(mc.boot_utilities_set_active_part, "BootUtilitiesSetActivePart_value");
	EXPECT_EQ(mc.boot_utilities_get_next_active_part, "");
	EXPECT_EQ(mc.device_type_file, "DeviceTypeFile_value");
	EXPECT_EQ(mc.server_certificate, "");
	EXPECT_EQ(mc.server_url, "ServerURL_value");
	EXPECT_EQ(mc.update_log_path, "");
	EXPECT_EQ(mc.tenant_token, "");
	EXPECT_EQ(mc.daemon_log_level, "");

	EXPECT_FALSE(mc.skip_verify);
	EXPECT_FALSE(mc.dbus_enabled);

	EXPECT_EQ(mc.update_control_map_expiration_time_seconds, 0);
	EXPECT_EQ(mc.update_control_map_boot_expiration_time_seconds, 0);
	EXPECT_EQ(mc.update_poll_interval_seconds, 0);
	EXPECT_EQ(mc.inventory_poll_interval_seconds, 0);
	EXPECT_EQ(mc.retry_poll_interval_seconds, 0);
	EXPECT_EQ(mc.retry_poll_count, 0);
	EXPECT_EQ(mc.state_script_timeout_seconds, 0);
	EXPECT_EQ(mc.state_script_retry_timeout_seconds, 0);
	EXPECT_EQ(mc.state_script_retry_interval_seconds, 0);
	EXPECT_EQ(mc.module_timeout_seconds, 0);

	EXPECT_EQ(mc.artifact_verify_keys.size(), 0);

	EXPECT_EQ(mc.servers.size(), 0);

	EXPECT_EQ(mc.https_client.certificate, "");
	EXPECT_EQ(mc.https_client.key, "");
	EXPECT_EQ(mc.https_client.ssl_engine, "");

	EXPECT_EQ(mc.security.auth_private_key, "");
	EXPECT_EQ(mc.security.ssl_engine, "");

	EXPECT_FALSE(mc.connectivity.disable_keep_alive);
	EXPECT_EQ(mc.connectivity.idle_conn_timeout_seconds, 0);
}

TEST_F(ConfigParserTests, LoadOverrides) {
	ofstream os(test_config_fname);
	os << complete_config;
	os.close();

	config_parser::MenderConfigFromFile mc;
	config_parser::ExpectedBool ret = mc.LoadFile(test_config_fname);
	ASSERT_TRUE(ret);
	EXPECT_TRUE(ret.value());

	os.open(test_config_fname);
	os << R"({
  "ArtifactVerifyKey": "ArtifactVerifyKey_value2",
  "RootfsPartB": "RootfsPartB_value2",
  "BootUtilitiesSetActivePart": "BootUtilitiesSetActivePart_value2",
  "DeviceTypeFile": "DeviceTypeFile_value2",
  "ServerURL": "ServerURL_value2",
  "SkipVerify": false,
  "HttpsClient": {
    "Certificate": "Certificate_value2"
  },
  "Connectivity": {
    "DisableKeepAlive": false,
    "IdleConnTimeoutSeconds": 15
  }
})";
	os.close();

	ret = mc.LoadFile(test_config_fname);
	ASSERT_TRUE(ret);
	EXPECT_TRUE(ret.value());

	EXPECT_EQ(mc.artifact_verify_key, "ArtifactVerifyKey_value2");
	EXPECT_EQ(mc.rootfs_part_A, "RootfsPartA_value");
	EXPECT_EQ(mc.rootfs_part_B, "RootfsPartB_value2");
	EXPECT_EQ(mc.boot_utilities_set_active_part, "BootUtilitiesSetActivePart_value2");
	EXPECT_EQ(mc.boot_utilities_get_next_active_part, "BootUtilitiesGetNextActivePart_value");
	EXPECT_EQ(mc.device_type_file, "DeviceTypeFile_value2");
	EXPECT_EQ(mc.server_certificate, "ServerCertificate_value");
	EXPECT_EQ(mc.server_url, "ServerURL_value2");
	EXPECT_EQ(mc.update_log_path, "UpdateLogPath_value");
	EXPECT_EQ(mc.tenant_token, "TenantToken_value");
	EXPECT_EQ(mc.daemon_log_level, "DaemonLogLevel_value");

	EXPECT_FALSE(mc.skip_verify);
	EXPECT_TRUE(mc.dbus_enabled);

	EXPECT_EQ(mc.update_control_map_expiration_time_seconds, 1);
	EXPECT_EQ(mc.update_control_map_boot_expiration_time_seconds, 2);
	EXPECT_EQ(mc.update_poll_interval_seconds, 3);
	EXPECT_EQ(mc.inventory_poll_interval_seconds, 4);
	EXPECT_EQ(mc.retry_poll_interval_seconds, 5);
	EXPECT_EQ(mc.retry_poll_count, 6);
	EXPECT_EQ(mc.state_script_timeout_seconds, 7);
	EXPECT_EQ(mc.state_script_retry_timeout_seconds, 8);
	EXPECT_EQ(mc.state_script_retry_interval_seconds, 9);
	EXPECT_EQ(mc.module_timeout_seconds, 10);

	EXPECT_EQ(mc.artifact_verify_keys.size(), 3);
	EXPECT_EQ(mc.artifact_verify_keys[0], "key1");
	EXPECT_EQ(mc.artifact_verify_keys[1], "key2");
	EXPECT_EQ(mc.artifact_verify_keys[2], "key3");

	EXPECT_EQ(mc.servers.size(), 2);
	EXPECT_EQ(mc.servers[0], "server1");
	EXPECT_EQ(mc.servers[1], "server2");

	EXPECT_EQ(mc.https_client.certificate, "Certificate_value2");
	EXPECT_EQ(mc.https_client.key, "Key_value");
	EXPECT_EQ(mc.https_client.ssl_engine, "SSLEngine_value");

	EXPECT_EQ(mc.security.auth_private_key, "AuthPrivateKey_value");
	EXPECT_EQ(mc.security.ssl_engine, "SecuritySSLEngine_value");

	EXPECT_FALSE(mc.connectivity.disable_keep_alive);
	EXPECT_EQ(mc.connectivity.idle_conn_timeout_seconds, 15);
}

TEST_F(ConfigParserTests, LoadNoOverrides) {
	ofstream os(test_config_fname);
	os << complete_config;
	os.close();

	config_parser::MenderConfigFromFile mc;
	config_parser::ExpectedBool ret = mc.LoadFile(test_config_fname);
	ASSERT_TRUE(ret);
	EXPECT_TRUE(ret.value());

	os.open(test_config_fname);
	os << R"({})";
	os.close();

	ret = mc.LoadFile(test_config_fname);
	ASSERT_TRUE(ret);
	EXPECT_FALSE(ret.value());

	EXPECT_EQ(mc.artifact_verify_key, "ArtifactVerifyKey_value");
	EXPECT_EQ(mc.rootfs_part_A, "RootfsPartA_value");
	EXPECT_EQ(mc.rootfs_part_B, "RootfsPartB_value");
	EXPECT_EQ(mc.boot_utilities_set_active_part, "BootUtilitiesSetActivePart_value");
	EXPECT_EQ(mc.boot_utilities_get_next_active_part, "BootUtilitiesGetNextActivePart_value");
	EXPECT_EQ(mc.device_type_file, "DeviceTypeFile_value");
	EXPECT_EQ(mc.server_certificate, "ServerCertificate_value");
	EXPECT_EQ(mc.server_url, "ServerURL_value");
	EXPECT_EQ(mc.update_log_path, "UpdateLogPath_value");
	EXPECT_EQ(mc.tenant_token, "TenantToken_value");
	EXPECT_EQ(mc.daemon_log_level, "DaemonLogLevel_value");

	EXPECT_TRUE(mc.skip_verify);
	EXPECT_TRUE(mc.dbus_enabled);

	EXPECT_EQ(mc.update_control_map_expiration_time_seconds, 1);
	EXPECT_EQ(mc.update_control_map_boot_expiration_time_seconds, 2);
	EXPECT_EQ(mc.update_poll_interval_seconds, 3);
	EXPECT_EQ(mc.inventory_poll_interval_seconds, 4);
	EXPECT_EQ(mc.retry_poll_interval_seconds, 5);
	EXPECT_EQ(mc.retry_poll_count, 6);
	EXPECT_EQ(mc.state_script_timeout_seconds, 7);
	EXPECT_EQ(mc.state_script_retry_timeout_seconds, 8);
	EXPECT_EQ(mc.state_script_retry_interval_seconds, 9);
	EXPECT_EQ(mc.module_timeout_seconds, 10);

	EXPECT_EQ(mc.artifact_verify_keys.size(), 3);
	EXPECT_EQ(mc.artifact_verify_keys[0], "key1");
	EXPECT_EQ(mc.artifact_verify_keys[1], "key2");
	EXPECT_EQ(mc.artifact_verify_keys[2], "key3");

	EXPECT_EQ(mc.servers.size(), 2);
	EXPECT_EQ(mc.servers[0], "server1");
	EXPECT_EQ(mc.servers[1], "server2");

	EXPECT_EQ(mc.https_client.certificate, "Certificate_value");
	EXPECT_EQ(mc.https_client.key, "Key_value");
	EXPECT_EQ(mc.https_client.ssl_engine, "SSLEngine_value");

	EXPECT_EQ(mc.security.auth_private_key, "AuthPrivateKey_value");
	EXPECT_EQ(mc.security.ssl_engine, "SecuritySSLEngine_value");

	EXPECT_TRUE(mc.connectivity.disable_keep_alive);
	EXPECT_EQ(mc.connectivity.idle_conn_timeout_seconds, 11);
}

TEST_F(ConfigParserTests, LoadInvalidOverrides) {
	ofstream os(test_config_fname);
	os << complete_config;
	os.close();

	config_parser::MenderConfigFromFile mc;
	config_parser::ExpectedBool ret = mc.LoadFile(test_config_fname);
	ASSERT_TRUE(ret);
	EXPECT_TRUE(ret.value());

	os.open(test_config_fname);
	os << R"({invalid: json)";
	os.close();

	ret = mc.LoadFile(test_config_fname);
	ASSERT_FALSE(ret);
	EXPECT_EQ(ret.error().code, config_parser::ConfigParserErrorCode::ParseError);

	EXPECT_EQ(mc.artifact_verify_key, "ArtifactVerifyKey_value");
	EXPECT_EQ(mc.rootfs_part_A, "RootfsPartA_value");
	EXPECT_EQ(mc.rootfs_part_B, "RootfsPartB_value");
	EXPECT_EQ(mc.boot_utilities_set_active_part, "BootUtilitiesSetActivePart_value");
	EXPECT_EQ(mc.boot_utilities_get_next_active_part, "BootUtilitiesGetNextActivePart_value");
	EXPECT_EQ(mc.device_type_file, "DeviceTypeFile_value");
	EXPECT_EQ(mc.server_certificate, "ServerCertificate_value");
	EXPECT_EQ(mc.server_url, "ServerURL_value");
	EXPECT_EQ(mc.update_log_path, "UpdateLogPath_value");
	EXPECT_EQ(mc.tenant_token, "TenantToken_value");
	EXPECT_EQ(mc.daemon_log_level, "DaemonLogLevel_value");

	EXPECT_TRUE(mc.skip_verify);
	EXPECT_TRUE(mc.dbus_enabled);

	EXPECT_EQ(mc.update_control_map_expiration_time_seconds, 1);
	EXPECT_EQ(mc.update_control_map_boot_expiration_time_seconds, 2);
	EXPECT_EQ(mc.update_poll_interval_seconds, 3);
	EXPECT_EQ(mc.inventory_poll_interval_seconds, 4);
	EXPECT_EQ(mc.retry_poll_interval_seconds, 5);
	EXPECT_EQ(mc.retry_poll_count, 6);
	EXPECT_EQ(mc.state_script_timeout_seconds, 7);
	EXPECT_EQ(mc.state_script_retry_timeout_seconds, 8);
	EXPECT_EQ(mc.state_script_retry_interval_seconds, 9);
	EXPECT_EQ(mc.module_timeout_seconds, 10);

	EXPECT_EQ(mc.artifact_verify_keys.size(), 3);
	EXPECT_EQ(mc.artifact_verify_keys[0], "key1");
	EXPECT_EQ(mc.artifact_verify_keys[1], "key2");
	EXPECT_EQ(mc.artifact_verify_keys[2], "key3");

	EXPECT_EQ(mc.servers.size(), 2);
	EXPECT_EQ(mc.servers[0], "server1");
	EXPECT_EQ(mc.servers[1], "server2");

	EXPECT_EQ(mc.https_client.certificate, "Certificate_value");
	EXPECT_EQ(mc.https_client.key, "Key_value");
	EXPECT_EQ(mc.https_client.ssl_engine, "SSLEngine_value");

	EXPECT_EQ(mc.security.auth_private_key, "AuthPrivateKey_value");
	EXPECT_EQ(mc.security.ssl_engine, "SecuritySSLEngine_value");

	EXPECT_TRUE(mc.connectivity.disable_keep_alive);
	EXPECT_EQ(mc.connectivity.idle_conn_timeout_seconds, 11);
}

TEST_F(ConfigParserTests, LoadOverridesExtra) {
	ofstream os(test_config_fname);
	os << complete_config;
	os.close();

	config_parser::MenderConfigFromFile mc;
	config_parser::ExpectedBool ret = mc.LoadFile(test_config_fname);
	ASSERT_TRUE(ret);
	EXPECT_TRUE(ret.value());

	os.open(test_config_fname);
	os << R"({
  "ArtifactVerifyKey": "ArtifactVerifyKey_value2",
  "RootfsPartA": 42,
  "RootfsPartB": "RootfsPartB_value2",
  "BootUtilitiesSetActivePart": "BootUtilitiesSetActivePart_value2",
  "DeviceTypeFile": "DeviceTypeFile_value2",
  "ServerURL": "ServerURL_value2",
  "SkipVerify": false,
  "NewExtraField": ["nobody", "cares"]
})";
	os.close();

	ret = mc.LoadFile(test_config_fname);
	ASSERT_TRUE(ret);
	EXPECT_TRUE(ret.value());

	EXPECT_EQ(mc.artifact_verify_key, "ArtifactVerifyKey_value2");
	EXPECT_EQ(mc.rootfs_part_A, "RootfsPartA_value");
	EXPECT_EQ(mc.rootfs_part_B, "RootfsPartB_value2");
	EXPECT_EQ(mc.boot_utilities_set_active_part, "BootUtilitiesSetActivePart_value2");
	EXPECT_EQ(mc.boot_utilities_get_next_active_part, "BootUtilitiesGetNextActivePart_value");
	EXPECT_EQ(mc.device_type_file, "DeviceTypeFile_value2");
	EXPECT_EQ(mc.server_certificate, "ServerCertificate_value");
	EXPECT_EQ(mc.server_url, "ServerURL_value2");
	EXPECT_EQ(mc.update_log_path, "UpdateLogPath_value");
	EXPECT_EQ(mc.tenant_token, "TenantToken_value");
	EXPECT_EQ(mc.daemon_log_level, "DaemonLogLevel_value");

	EXPECT_FALSE(mc.skip_verify);
	EXPECT_TRUE(mc.dbus_enabled);

	EXPECT_EQ(mc.update_control_map_expiration_time_seconds, 1);
	EXPECT_EQ(mc.update_control_map_boot_expiration_time_seconds, 2);
	EXPECT_EQ(mc.update_poll_interval_seconds, 3);
	EXPECT_EQ(mc.inventory_poll_interval_seconds, 4);
	EXPECT_EQ(mc.retry_poll_interval_seconds, 5);
	EXPECT_EQ(mc.retry_poll_count, 6);
	EXPECT_EQ(mc.state_script_timeout_seconds, 7);
	EXPECT_EQ(mc.state_script_retry_timeout_seconds, 8);
	EXPECT_EQ(mc.state_script_retry_interval_seconds, 9);
	EXPECT_EQ(mc.module_timeout_seconds, 10);

	EXPECT_EQ(mc.artifact_verify_keys.size(), 3);
	EXPECT_EQ(mc.artifact_verify_keys[0], "key1");
	EXPECT_EQ(mc.artifact_verify_keys[1], "key2");
	EXPECT_EQ(mc.artifact_verify_keys[2], "key3");

	EXPECT_EQ(mc.servers.size(), 2);
	EXPECT_EQ(mc.servers[0], "server1");
	EXPECT_EQ(mc.servers[1], "server2");

	EXPECT_EQ(mc.https_client.certificate, "Certificate_value");
	EXPECT_EQ(mc.https_client.key, "Key_value");
	EXPECT_EQ(mc.https_client.ssl_engine, "SSLEngine_value");

	EXPECT_EQ(mc.security.auth_private_key, "AuthPrivateKey_value");
	EXPECT_EQ(mc.security.ssl_engine, "SecuritySSLEngine_value");

	EXPECT_TRUE(mc.connectivity.disable_keep_alive);
	EXPECT_EQ(mc.connectivity.idle_conn_timeout_seconds, 11);
}

TEST_F(ConfigParserTests, LoadOverridesExtraArrayItems) {
	ofstream os(test_config_fname);
	os << complete_config;
	os.close();

	config_parser::MenderConfigFromFile mc;
	config_parser::ExpectedBool ret = mc.LoadFile(test_config_fname);
	ASSERT_TRUE(ret);
	EXPECT_TRUE(ret.value());

	os.open(test_config_fname);
	os << R"({
  "ArtifactVerifyKeys": [
    "key4",
    "key5"
  ],

  "Servers": [
   {"ServerURL": "server3"}
  ]
})";
	os.close();

	ret = mc.LoadFile(test_config_fname);
	ASSERT_TRUE(ret);
	EXPECT_TRUE(ret.value());

	EXPECT_EQ(mc.artifact_verify_key, "ArtifactVerifyKey_value");
	EXPECT_EQ(mc.rootfs_part_A, "RootfsPartA_value");
	EXPECT_EQ(mc.rootfs_part_B, "RootfsPartB_value");
	EXPECT_EQ(mc.boot_utilities_set_active_part, "BootUtilitiesSetActivePart_value");
	EXPECT_EQ(mc.boot_utilities_get_next_active_part, "BootUtilitiesGetNextActivePart_value");
	EXPECT_EQ(mc.device_type_file, "DeviceTypeFile_value");
	EXPECT_EQ(mc.server_certificate, "ServerCertificate_value");
	EXPECT_EQ(mc.server_url, "ServerURL_value");
	EXPECT_EQ(mc.update_log_path, "UpdateLogPath_value");
	EXPECT_EQ(mc.tenant_token, "TenantToken_value");
	EXPECT_EQ(mc.daemon_log_level, "DaemonLogLevel_value");

	EXPECT_TRUE(mc.skip_verify);
	EXPECT_TRUE(mc.dbus_enabled);

	EXPECT_EQ(mc.update_control_map_expiration_time_seconds, 1);
	EXPECT_EQ(mc.update_control_map_boot_expiration_time_seconds, 2);
	EXPECT_EQ(mc.update_poll_interval_seconds, 3);
	EXPECT_EQ(mc.inventory_poll_interval_seconds, 4);
	EXPECT_EQ(mc.retry_poll_interval_seconds, 5);
	EXPECT_EQ(mc.retry_poll_count, 6);
	EXPECT_EQ(mc.state_script_timeout_seconds, 7);
	EXPECT_EQ(mc.state_script_retry_timeout_seconds, 8);
	EXPECT_EQ(mc.state_script_retry_interval_seconds, 9);
	EXPECT_EQ(mc.module_timeout_seconds, 10);

	EXPECT_EQ(mc.artifact_verify_keys.size(), 5);
	EXPECT_EQ(mc.artifact_verify_keys[0], "key1");
	EXPECT_EQ(mc.artifact_verify_keys[1], "key2");
	EXPECT_EQ(mc.artifact_verify_keys[2], "key3");
	EXPECT_EQ(mc.artifact_verify_keys[3], "key4");
	EXPECT_EQ(mc.artifact_verify_keys[4], "key5");

	EXPECT_EQ(mc.servers.size(), 3);
	EXPECT_EQ(mc.servers[0], "server1");
	EXPECT_EQ(mc.servers[1], "server2");
	EXPECT_EQ(mc.servers[2], "server3");

	EXPECT_EQ(mc.https_client.certificate, "Certificate_value");
	EXPECT_EQ(mc.https_client.key, "Key_value");
	EXPECT_EQ(mc.https_client.ssl_engine, "SSLEngine_value");

	EXPECT_EQ(mc.security.auth_private_key, "AuthPrivateKey_value");
	EXPECT_EQ(mc.security.ssl_engine, "SecuritySSLEngine_value");

	EXPECT_TRUE(mc.connectivity.disable_keep_alive);
	EXPECT_EQ(mc.connectivity.idle_conn_timeout_seconds, 11);
}

TEST(ValidateConfig, ArtifactVerifyKeyNameCollision) {
	namespace conf = mender::common::config_parser;
	{
		conf::MenderConfigFromFile config = {.artifact_verify_keys = {"key1", "key2"}};

		auto ret = config.ValidateArtifactKeyCondition();
		EXPECT_TRUE(ret);
	}
	{
		conf::MenderConfigFromFile config = {.artifact_verify_key = "key1"};

		auto ret = config.ValidateArtifactKeyCondition();
		EXPECT_TRUE(ret);
	}
	{
		conf::MenderConfigFromFile config = {
			.artifact_verify_key = "key1", .artifact_verify_keys = {"key1", "key2"}};

		auto ret = config.ValidateArtifactKeyCondition();
		EXPECT_FALSE(ret);
		EXPECT_EQ(ret.error().code, conf::ConfigParserErrorCode::ParseError);
		EXPECT_EQ(ret.error().message, "Both 'ArtifactVerifyKey' and 'ArtifactVerifyKeys' are set");
	}
}

TEST(ValidateConfig, ValidateServerConfig) {
	namespace conf = mender::common::config_parser;
	{
		// Error: Both 'Servers' and 'ServerURL' set
		conf::MenderConfigFromFile config = {
			.server_url = "foo.hosted.mender.io",
			.servers = {"bar.hosted.mender.io", "baz.hosted.mender.io"}};

		auto ret = config.ValidateServerConfig();
		EXPECT_FALSE(ret);
	}
	{
		// NoError - Only ServerURL set
		conf::MenderConfigFromFile config = {
			.server_url = "foo.hosted.mender.io",
		};
		ASSERT_EQ(config.server_url, "foo.hosted.mender.io");

		auto ret = config.ValidateServerConfig();
		EXPECT_TRUE(ret);
	}
	{
		// NoError - Only Servers set
		conf::MenderConfigFromFile config = {
			.servers = {"bar.hosted.mender.io", "baz.hosted.mender.io"}};

		ASSERT_EQ(config.server_url.size(), 0) << "Unexpected length of the server_url string";

		auto ret = config.ValidateServerConfig();
		EXPECT_TRUE(ret);
	}
}
