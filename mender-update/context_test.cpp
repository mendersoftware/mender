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

#include <mender-update/context.hpp>

#include <cerrno>
#include <fstream>

#include <common/common.hpp>
#include <common/conf.hpp>
#include <common/key_value_database_lmdb.hpp>
#include <common/json.hpp>
#include <common/testing.hpp>

#include <gtest/gtest.h>
#include <gmock/gmock.h>

namespace error = mender::common::error;
namespace common = mender::common;
namespace conf = mender::common::conf;
namespace json = mender::common::json;
namespace kv_db = mender::common::key_value_database;
namespace context = mender::update::context;

using namespace std;
using namespace mender::common::testing;

class ContextTests : public testing::Test {
protected:
	TemporaryDirectory test_state_dir;
};

TEST_F(ContextTests, LoadProvidesValid) {
	conf::MenderConfig cfg;
	cfg.data_store_dir = test_state_dir.Path();

	context::MenderContext ctx(cfg);
	auto err = ctx.Initialize();
	ASSERT_EQ(err, error::NoError);

	auto &db = ctx.GetMenderStoreDB();
	const string input_provides_data_str = R"({
  "something_else": "something_else value"
})";
	err = db.Write("artifact-name", common::ByteVectorFromString("artifact-name value"));
	ASSERT_EQ(err, error::NoError);
	err = db.Write("artifact-group", common::ByteVectorFromString("artifact-group value"));
	ASSERT_EQ(err, error::NoError);
	err = db.Write("artifact-provides", common::ByteVectorFromString(input_provides_data_str));
	ASSERT_EQ(err, error::NoError);

	auto ex_provides_data = ctx.LoadProvides();
	ASSERT_TRUE(ex_provides_data);

	auto provides_data = ex_provides_data.value();
	EXPECT_EQ(provides_data.size(), 3);
	EXPECT_EQ(provides_data["artifact_name"], "artifact-name value");
	EXPECT_EQ(provides_data["artifact_group"], "artifact-group value");
	EXPECT_EQ(provides_data["something_else"], "something_else value");
}

TEST_F(ContextTests, LoadProvidesEmpty) {
	conf::MenderConfig cfg;
	cfg.data_store_dir = test_state_dir.Path();

	context::MenderContext ctx(cfg);
	auto err = ctx.Initialize();
	ASSERT_EQ(err, error::NoError);

	auto ex_provides_data = ctx.LoadProvides();
	ASSERT_TRUE(ex_provides_data);

	auto provides_data = ex_provides_data.value();
	EXPECT_EQ(provides_data.size(), 0);
}

TEST_F(ContextTests, LoadProvidesInvalidJSON) {
	conf::MenderConfig cfg;
	cfg.data_store_dir = test_state_dir.Path();

	context::MenderContext ctx(cfg);
	auto err = ctx.Initialize();
	ASSERT_EQ(err, error::NoError);

	auto &db = ctx.GetMenderStoreDB();
	const string input_provides_data_str = R"({
  "something_else": "something_else" invalid
})";
	err = db.Write("artifact-name", common::ByteVectorFromString("artifact-name value"));
	ASSERT_EQ(err, error::NoError);
	err = db.Write("artifact-group", common::ByteVectorFromString("artifact-group value"));
	ASSERT_EQ(err, error::NoError);
	err = db.Write("artifact-provides", common::ByteVectorFromString(input_provides_data_str));
	ASSERT_EQ(err, error::NoError);

	auto ex_provides_data = ctx.LoadProvides();
	ASSERT_FALSE(ex_provides_data);
	EXPECT_EQ(
		ex_provides_data.error().code, json::MakeError(json::JsonErrorCode::ParseError, "").code);
}

TEST_F(ContextTests, LoadProvidesInvalidData) {
	conf::MenderConfig cfg;
	cfg.data_store_dir = test_state_dir.Path();

	context::MenderContext ctx(cfg);
	auto err = ctx.Initialize();
	ASSERT_EQ(err, error::NoError);

	auto &db = ctx.GetMenderStoreDB();
	const string input_provides_data_str = R"({
  "something_else_array": ["something_else_array value"]
})";
	err = db.Write("artifact-name", common::ByteVectorFromString("artifact-name value"));
	ASSERT_EQ(err, error::NoError);
	err = db.Write("artifact-group", common::ByteVectorFromString("artifact-group value"));
	ASSERT_EQ(err, error::NoError);
	err = db.Write("artifact-provides", common::ByteVectorFromString(input_provides_data_str));
	ASSERT_EQ(err, error::NoError);

	auto ex_provides_data = ctx.LoadProvides();
	ASSERT_FALSE(ex_provides_data);
	EXPECT_EQ(
		ex_provides_data.error().code, json::MakeError(json::JsonErrorCode::TypeError, "").code);
}

TEST_F(ContextTests, LoadProvidesClosedDB) {
#ifndef NDEBUG
	GTEST_SKIP() << "requires assert() to be a no-op";
#else
	conf::MenderConfig cfg;
	cfg.data_store_dir = test_state_dir.Path();

	context::MenderContext ctx(cfg);
	auto err = ctx.Initialize();
	ASSERT_EQ(err, error::NoError);

	auto &db = ctx.GetMenderStoreDB();
	const string input_provides_data_str = R"({
  "something_else": "something_else value"
})";
	err = db.Write("artifact-name", common::ByteVectorFromString("artifact-name value"));
	ASSERT_EQ(err, error::NoError);
	err = db.Write("artifact-group", common::ByteVectorFromString("artifact-group value"));
	ASSERT_EQ(err, error::NoError);
	err = db.Write("artifact-provides", common::ByteVectorFromString(input_provides_data_str));
	ASSERT_EQ(err, error::NoError);

	auto &lmdb = dynamic_cast<kv_db::KeyValueDatabaseLmdb &>(db);
	lmdb.Close();

	auto ex_provides_data = ctx.LoadProvides();
	ASSERT_FALSE(ex_provides_data);
	EXPECT_EQ(ex_provides_data.error().code, error::MakeError(error::ProgrammingError, "").code);
#endif // NDEBUG
}

TEST_F(ContextTests, CommitArtifactDataValid) {
	conf::MenderConfig cfg;
	cfg.data_store_dir = test_state_dir.Path();

	context::MenderContext ctx(cfg);
	auto err = ctx.Initialize();
	ASSERT_EQ(err, error::NoError);

	context::ProvidesData data;
	data["artifact_name"] = "artifact_name value";
	data["artifact_group"] = "artifact_group value";
	data["something_extra"] = "something_extra value";
	data["something_extra2"] = "something_extra2 value";

	err = ctx.CommitArtifactData(data);
	ASSERT_EQ(err, error::NoError);

	auto &db = ctx.GetMenderStoreDB();
	auto ex_data = db.Read("artifact-name");
	ASSERT_TRUE(ex_data);
	EXPECT_EQ(common::StringFromByteVector(ex_data.value()), "artifact_name value");

	ex_data = db.Read("artifact-group");
	ASSERT_TRUE(ex_data);
	EXPECT_EQ(common::StringFromByteVector(ex_data.value()), "artifact_group value");

	ex_data = db.Read("artifact-provides");
	ASSERT_TRUE(ex_data);
	EXPECT_EQ(
		common::StringFromByteVector(ex_data.value()),
		R"({"something_extra2":"something_extra2 value","something_extra":"something_extra value"})");
}

TEST_F(ContextTests, CommitArtifactDataEscaped) {
	conf::MenderConfig cfg;
	cfg.data_store_dir = test_state_dir.Path();

	context::MenderContext ctx(cfg);
	auto err = ctx.Initialize();
	ASSERT_EQ(err, error::NoError);

	context::ProvidesData data;
	data["artifact_name"] = "artifact_name value";
	data["artifact_group"] = "artifact_group value";
	data["something_extra"] = "something_extra\nvalue";
	data["something_extra2"] = "something_extra2\tvalue";

	err = ctx.CommitArtifactData(data);
	ASSERT_EQ(err, error::NoError);

	auto &db = ctx.GetMenderStoreDB();
	auto ex_data = db.Read("artifact-name");
	ASSERT_TRUE(ex_data);
	EXPECT_EQ(common::StringFromByteVector(ex_data.value()), "artifact_name value");

	ex_data = db.Read("artifact-group");
	ASSERT_TRUE(ex_data);
	EXPECT_EQ(common::StringFromByteVector(ex_data.value()), "artifact_group value");

	ex_data = db.Read("artifact-provides");
	ASSERT_TRUE(ex_data);
	EXPECT_EQ(
		common::StringFromByteVector(ex_data.value()),
		R"({"something_extra2":"something_extra2\tvalue","something_extra":"something_extra\nvalue"})");
}

TEST_F(ContextTests, GetDeviceTypeValid) {
	conf::MenderConfig cfg;
	cfg.data_store_dir = test_state_dir.Path();

	context::MenderContext ctx(cfg);
	auto err = ctx.Initialize();
	ASSERT_EQ(err, error::NoError);

	ofstream os(cfg.data_store_dir + "/device_type");
	ASSERT_TRUE(os);
	os << "device_type=Some device type" << endl;
	os.close();

	auto ex_s = ctx.GetDeviceType();
	ASSERT_TRUE(ex_s);
	EXPECT_EQ(ex_s.value(), "Some device type");
}

TEST_F(ContextTests, GetDeviceTypeNoexist) {
	conf::MenderConfig cfg;
	cfg.data_store_dir = test_state_dir.Path();

	context::MenderContext ctx(cfg);
	auto err = ctx.Initialize();
	ASSERT_EQ(err, error::NoError);

	auto ex_s = ctx.GetDeviceType();
	ASSERT_FALSE(ex_s);
	EXPECT_TRUE(ex_s.error().IsErrno(ENOENT));
}

TEST_F(ContextTests, GetDeviceTypeEmpty) {
	conf::MenderConfig cfg;
	cfg.data_store_dir = test_state_dir.Path();

	context::MenderContext ctx(cfg);
	auto err = ctx.Initialize();
	ASSERT_EQ(err, error::NoError);

	string dtype_fpath = cfg.data_store_dir + "/device_type";
	ofstream os(dtype_fpath);
	ASSERT_TRUE(os);
	os.close();

	auto ex_s = ctx.GetDeviceType();
	ASSERT_FALSE(ex_s);
	EXPECT_EQ(ex_s.error().code, context::MakeError(context::ParseError, "").code);
}

TEST_F(ContextTests, GetDeviceTypeInvalid) {
	conf::MenderConfig cfg;
	cfg.data_store_dir = test_state_dir.Path();

	context::MenderContext ctx(cfg);
	auto err = ctx.Initialize();
	ASSERT_EQ(err, error::NoError);

	string dtype_fpath = cfg.data_store_dir + "/device_type";
	ofstream os(dtype_fpath);
	ASSERT_TRUE(os);
	os << "Some device type" << endl;
	os.close();

	auto ex_s = ctx.GetDeviceType();
	ASSERT_FALSE(ex_s);
	EXPECT_EQ(ex_s.error().code, context::MakeError(context::ParseError, "").code);

	os.open(dtype_fpath);
	ASSERT_TRUE(os);
	os << "device_type Some device type" << endl;
	os.close();

	ex_s = ctx.GetDeviceType();
	ASSERT_FALSE(ex_s);
	EXPECT_EQ(ex_s.error().code, context::MakeError(context::ParseError, "").code);
}

TEST_F(ContextTests, GetDeviceTypeTrailingData) {
	conf::MenderConfig cfg;
	cfg.data_store_dir = test_state_dir.Path();

	context::MenderContext ctx(cfg);
	auto err = ctx.Initialize();
	ASSERT_EQ(err, error::NoError);

	ofstream os(cfg.data_store_dir + "/device_type");
	ASSERT_TRUE(os);
	os << "device_type=Some device type" << endl;
	os << "some debris here" << endl;
	os.close();

	auto ex_s = ctx.GetDeviceType();
	ASSERT_FALSE(ex_s);
	EXPECT_EQ(ex_s.error().code, context::MakeError(context::ValueError, "").code);

	os.open(cfg.data_store_dir + "/device_type");
	ASSERT_TRUE(os);
	os << "device_type=Some device type" << endl;
	os << endl << "some debris here after a blank line" << endl;
	os.close();

	ex_s = ctx.GetDeviceType();
	ASSERT_FALSE(ex_s);
	EXPECT_EQ(ex_s.error().code, context::MakeError(context::ValueError, "").code);
}
