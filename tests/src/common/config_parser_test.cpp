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
#include <common/json.hpp>

#include <gtest/gtest.h>
#include <gmock/gmock.h>

#include <fstream>

namespace config_parser = mender::common::config_parser;
namespace json = mender::common::json;

using namespace std;

const string complete_config = R"({
  "RootfsPartA": "RootfsPartA_value",
  "RootfsPartB": "RootfsPartB_value",
  "BootUtilitiesSetActivePart": "BootUtilitiesSetActivePart_value",
  "BootUtilitiesGetNextActivePart": "BootUtilitiesGetNextActivePart_value",
  "DeviceTypeFile": "DeviceTypeFile_value",
  "ServerCertificate": "ServerCertificate_value",
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

	EXPECT_EQ(mc.device_type_file, "");
	EXPECT_EQ(mc.server_certificate, "");
	EXPECT_EQ(mc.update_log_path, "");
	EXPECT_EQ(mc.tenant_token, "");
	EXPECT_EQ(mc.daemon_log_level, "");

	EXPECT_FALSE(mc.skip_verify);

	EXPECT_EQ(mc.update_poll_interval_seconds, 1800);
	EXPECT_EQ(mc.inventory_poll_interval_seconds, 28800);
	EXPECT_EQ(mc.retry_poll_interval_seconds, 0);
	EXPECT_EQ(mc.retry_poll_count, 0);
	EXPECT_EQ(mc.state_script_timeout_seconds, 3600);
	EXPECT_EQ(mc.state_script_retry_timeout_seconds, 1800);
	EXPECT_EQ(mc.state_script_retry_interval_seconds, 60);
	EXPECT_EQ(mc.module_timeout_seconds, 14400);

	EXPECT_EQ(mc.artifact_verify_keys.size(), 0);

	EXPECT_EQ(mc.servers.size(), 0);

	EXPECT_EQ(mc.https_client.certificate, "");
	EXPECT_EQ(mc.https_client.key, "");
	EXPECT_EQ(mc.https_client.ssl_engine, "");

	EXPECT_EQ(mc.security.auth_private_key, "");
	EXPECT_EQ(mc.security.ssl_engine, "");
}

TEST_F(ConfigParserTests, LoadComplete) {
	ofstream os(test_config_fname);
	os << complete_config;
	os.close();

	config_parser::MenderConfigFromFile mc;
	config_parser::ExpectedBool ret = mc.LoadFile(test_config_fname);
	ASSERT_TRUE(ret) << ret.error().String();
	EXPECT_TRUE(ret.value());

	EXPECT_EQ(mc.device_type_file, "DeviceTypeFile_value");
	EXPECT_EQ(mc.server_certificate, "ServerCertificate_value");
	EXPECT_EQ(mc.update_log_path, "UpdateLogPath_value");
	EXPECT_EQ(mc.tenant_token, "TenantToken_value");
	EXPECT_EQ(mc.daemon_log_level, "DaemonLogLevel_value");

	EXPECT_TRUE(mc.skip_verify);

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

	EXPECT_EQ(mc.device_type_file, "DeviceTypeFile_value");
	EXPECT_EQ(mc.server_certificate, "");
	EXPECT_EQ(mc.update_log_path, "");
	EXPECT_EQ(mc.tenant_token, "");
	EXPECT_EQ(mc.daemon_log_level, "");

	EXPECT_FALSE(mc.skip_verify);

	EXPECT_EQ(mc.update_poll_interval_seconds, 1800);
	EXPECT_EQ(mc.inventory_poll_interval_seconds, 28800);
	EXPECT_EQ(mc.retry_poll_interval_seconds, 0);
	EXPECT_EQ(mc.retry_poll_count, 0);
	EXPECT_EQ(mc.state_script_timeout_seconds, 3600);
	EXPECT_EQ(mc.state_script_retry_timeout_seconds, 1800);
	EXPECT_EQ(mc.state_script_retry_interval_seconds, 60);
	EXPECT_EQ(mc.module_timeout_seconds, 14400);

	EXPECT_EQ(mc.artifact_verify_keys.size(), 1);
	EXPECT_EQ(mc.artifact_verify_keys[0], "ArtifactVerifyKey_value");

	EXPECT_EQ(mc.servers.size(), 1);
	EXPECT_EQ(mc.servers[0], "ServerURL_value");

	EXPECT_EQ(mc.https_client.certificate, "");
	EXPECT_EQ(mc.https_client.key, "");
	EXPECT_EQ(mc.https_client.ssl_engine, "");

	EXPECT_EQ(mc.security.auth_private_key, "");
	EXPECT_EQ(mc.security.ssl_engine, "");
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
  "RootfsPartB": "RootfsPartB_value2",
  "BootUtilitiesSetActivePart": "BootUtilitiesSetActivePart_value2",
  "DeviceTypeFile": "DeviceTypeFile_value2",
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

	EXPECT_EQ(mc.device_type_file, "DeviceTypeFile_value2");
	EXPECT_EQ(mc.server_certificate, "ServerCertificate_value");
	EXPECT_EQ(mc.update_log_path, "UpdateLogPath_value");
	EXPECT_EQ(mc.tenant_token, "TenantToken_value");
	EXPECT_EQ(mc.daemon_log_level, "DaemonLogLevel_value");

	EXPECT_FALSE(mc.skip_verify);

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

	EXPECT_EQ(mc.device_type_file, "DeviceTypeFile_value");
	EXPECT_EQ(mc.server_certificate, "ServerCertificate_value");
	EXPECT_EQ(mc.update_log_path, "UpdateLogPath_value");
	EXPECT_EQ(mc.tenant_token, "TenantToken_value");
	EXPECT_EQ(mc.daemon_log_level, "DaemonLogLevel_value");

	EXPECT_TRUE(mc.skip_verify);

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
	EXPECT_EQ(ret.error().code, json::MakeError(json::JsonErrorCode::ParseError, "").code);

	EXPECT_EQ(mc.device_type_file, "DeviceTypeFile_value");
	EXPECT_EQ(mc.server_certificate, "ServerCertificate_value");
	EXPECT_EQ(mc.update_log_path, "UpdateLogPath_value");
	EXPECT_EQ(mc.tenant_token, "TenantToken_value");
	EXPECT_EQ(mc.daemon_log_level, "DaemonLogLevel_value");

	EXPECT_TRUE(mc.skip_verify);

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
  "RootfsPartA": 42,
  "RootfsPartB": "RootfsPartB_value2",
  "BootUtilitiesSetActivePart": "BootUtilitiesSetActivePart_value2",
  "DeviceTypeFile": "DeviceTypeFile_value2",
  "SkipVerify": false,
  "NewExtraField": ["nobody", "cares"]
})";
	os.close();

	ret = mc.LoadFile(test_config_fname);
	ASSERT_TRUE(ret);
	EXPECT_TRUE(ret.value());

	EXPECT_EQ(mc.device_type_file, "DeviceTypeFile_value2");
	EXPECT_EQ(mc.server_certificate, "ServerCertificate_value");
	EXPECT_EQ(mc.update_log_path, "UpdateLogPath_value");
	EXPECT_EQ(mc.tenant_token, "TenantToken_value");
	EXPECT_EQ(mc.daemon_log_level, "DaemonLogLevel_value");

	EXPECT_FALSE(mc.skip_verify);

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

	EXPECT_EQ(mc.device_type_file, "DeviceTypeFile_value");
	EXPECT_EQ(mc.server_certificate, "ServerCertificate_value");
	EXPECT_EQ(mc.update_log_path, "UpdateLogPath_value");
	EXPECT_EQ(mc.tenant_token, "TenantToken_value");
	EXPECT_EQ(mc.daemon_log_level, "DaemonLogLevel_value");

	EXPECT_TRUE(mc.skip_verify);

	EXPECT_EQ(mc.update_poll_interval_seconds, 3);
	EXPECT_EQ(mc.inventory_poll_interval_seconds, 4);
	EXPECT_EQ(mc.retry_poll_interval_seconds, 5);
	EXPECT_EQ(mc.retry_poll_count, 6);
	EXPECT_EQ(mc.state_script_timeout_seconds, 7);
	EXPECT_EQ(mc.state_script_retry_timeout_seconds, 8);
	EXPECT_EQ(mc.state_script_retry_interval_seconds, 9);
	EXPECT_EQ(mc.module_timeout_seconds, 10);

	EXPECT_EQ(mc.artifact_verify_keys.size(), 2);
	EXPECT_EQ(mc.artifact_verify_keys[0], "key4");
	EXPECT_EQ(mc.artifact_verify_keys[1], "key5");

	EXPECT_EQ(mc.servers.size(), 1);
	EXPECT_EQ(mc.servers[0], "server3");

	EXPECT_EQ(mc.https_client.certificate, "Certificate_value");
	EXPECT_EQ(mc.https_client.key, "Key_value");
	EXPECT_EQ(mc.https_client.ssl_engine, "SSLEngine_value");

	EXPECT_EQ(mc.security.auth_private_key, "AuthPrivateKey_value");
	EXPECT_EQ(mc.security.ssl_engine, "SecuritySSLEngine_value");
}

TEST_F(ConfigParserTests, LoadAndReset) {
	ofstream os(test_config_fname);
	os << complete_config;
	os.close();

	config_parser::MenderConfigFromFile mc;
	config_parser::ExpectedBool ret = mc.LoadFile(test_config_fname);
	ASSERT_TRUE(ret);
	EXPECT_TRUE(ret.value());

	mc.Reset();
	EXPECT_EQ(mc.device_type_file, "");
	EXPECT_EQ(mc.server_certificate, "");
	EXPECT_EQ(mc.update_log_path, "");
	EXPECT_EQ(mc.tenant_token, "");
	EXPECT_EQ(mc.daemon_log_level, "");

	EXPECT_FALSE(mc.skip_verify);

	EXPECT_EQ(mc.update_poll_interval_seconds, 1800);
	EXPECT_EQ(mc.inventory_poll_interval_seconds, 28800);
	EXPECT_EQ(mc.retry_poll_interval_seconds, 0);
	EXPECT_EQ(mc.retry_poll_count, 0);
	EXPECT_EQ(mc.state_script_timeout_seconds, 3600);
	EXPECT_EQ(mc.state_script_retry_timeout_seconds, 1800);
	EXPECT_EQ(mc.state_script_retry_interval_seconds, 60);
	EXPECT_EQ(mc.module_timeout_seconds, 14400);

	EXPECT_EQ(mc.artifact_verify_keys.size(), 0);

	EXPECT_EQ(mc.servers.size(), 0);

	EXPECT_EQ(mc.https_client.certificate, "");
	EXPECT_EQ(mc.https_client.key, "");
	EXPECT_EQ(mc.https_client.ssl_engine, "");

	EXPECT_EQ(mc.security.auth_private_key, "");
	EXPECT_EQ(mc.security.ssl_engine, "");
}

TEST_F(ConfigParserTests, ArtifactVerifyKeyNameCollision) {
	ofstream os(test_config_fname);
	os << R"({
  "ArtifactVerifyKey": "ArtifactVerifyKey_value1",
  "ArtifactVerifyKeys": [
    "ArtifactVerifyKey_value2"
  ],
  "RootfsPartB": "RootfsPartB_value",
  "BootUtilitiesSetActivePart": "BootUtilitiesSetActivePart_value",
  "DeviceTypeFile": "DeviceTypeFile_value",
  "ServerURL": "ServerURL_value"
})";
	os.close();

	config_parser::MenderConfigFromFile mc;
	config_parser::ExpectedBool ret = mc.LoadFile(test_config_fname);
	ASSERT_FALSE(ret);
	EXPECT_EQ(ret.error().code, config_parser::MakeError(config_parser::ValidationError, "").code)
		<< ret.error().String();
}

TEST_F(ConfigParserTests, ValidateServerConfig) {
	ofstream os(test_config_fname);
	os << R"({
  "RootfsPartB": "RootfsPartB_value",
  "BootUtilitiesSetActivePart": "BootUtilitiesSetActivePart_value",
  "DeviceTypeFile": "DeviceTypeFile_value",
  "ServerURL": "ServerURL_value",
  "Servers": [
    {
      "ServerURL": "ServerURL_value"
    }
  ]
})";
	os.close();

	config_parser::MenderConfigFromFile mc;
	config_parser::ExpectedBool ret = mc.LoadFile(test_config_fname);
	ASSERT_FALSE(ret);
	EXPECT_EQ(ret.error().code, config_parser::MakeError(config_parser::ValidationError, "").code);
	EXPECT_THAT(ret.error().String(), testing::HasSubstr("ServerURL"));
	EXPECT_THAT(ret.error().String(), testing::HasSubstr("Servers"));
}

TEST_F(ConfigParserTests, CaseInsensitiveParsing) {
	ofstream os(test_config_fname);
	os << R"({
  "artifactverifykey": "ArtifactVerifyKey_value",
  "deviceTypeFile": "DeviceTypeFile_value",
  "SERVERURL": "ServerURL_value"
})";
	os.close();

	config_parser::MenderConfigFromFile mc;
	config_parser::ExpectedBool ret = mc.LoadFile(test_config_fname);
	ASSERT_TRUE(ret);
	ASSERT_TRUE(ret.value());

	ASSERT_EQ(mc.artifact_verify_keys.size(), 1);
	EXPECT_EQ(mc.artifact_verify_keys[0], "ArtifactVerifyKey_value");

	EXPECT_EQ(mc.device_type_file, "DeviceTypeFile_value");

	ASSERT_EQ(mc.servers.size(), 1);
	EXPECT_EQ(mc.servers[0], "ServerURL_value");
}

TEST_F(ConfigParserTests, CaseInsensitiveCollision) {
	ofstream os(test_config_fname);
	os << R"({
  "ServerUrl": "ServerURL_value_1",
  "ServerUrl": "ServerURL_value_2",
  "serverurl": "ServerURL_value_3",
  "SERVERURL": "ServerURL_value_4"
})";
	os.close();

	config_parser::MenderConfigFromFile mc;
	config_parser::ExpectedBool ret = mc.LoadFile(test_config_fname);
	ASSERT_TRUE(ret);
	ASSERT_TRUE(ret.value());

	ASSERT_EQ(mc.servers.size(), 1);
	EXPECT_EQ(mc.servers[0], "ServerURL_value_4");
}
