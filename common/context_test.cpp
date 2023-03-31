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

#include <common/context.hpp>

#include <gtest/gtest.h>
#include <gmock/gmock.h>

#include <common/common.hpp>
#include <common/conf.hpp>
#include <common/key_value_database_lmdb.hpp>
#include <common/json.hpp>
#include <common/testing.hpp>

namespace error = mender::common::error;
namespace common = mender::common;
namespace conf = mender::common::conf;
namespace context = mender::common::context;
namespace json = mender::common::json;
namespace kv_db = mender::common::key_value_database;

using namespace std;
using namespace mender::common::testing;

class ContextTests : public testing::Test {
protected:
	TemporaryDirectory test_state_dir;
};

TEST_F(ContextTests, LoadProvidesValid) {
	conf::MenderConfig cfg;
	cfg.data_store_dir = test_state_dir.Path();

	context::MenderContext ctx;
	auto err = ctx.Initialize(cfg);
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

	context::MenderContext ctx;
	auto err = ctx.Initialize(cfg);
	ASSERT_EQ(err, error::NoError);

	auto ex_provides_data = ctx.LoadProvides();
	ASSERT_TRUE(ex_provides_data);

	auto provides_data = ex_provides_data.value();
	EXPECT_EQ(provides_data.size(), 0);
}

TEST_F(ContextTests, LoadProvidesInvalidJSON) {
	conf::MenderConfig cfg;
	cfg.data_store_dir = test_state_dir.Path();

	context::MenderContext ctx;
	auto err = ctx.Initialize(cfg);
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

	context::MenderContext ctx;
	auto err = ctx.Initialize(cfg);
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

	context::MenderContext ctx;
	auto err = ctx.Initialize(cfg);
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
