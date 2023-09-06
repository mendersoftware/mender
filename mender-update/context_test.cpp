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
#include <unordered_map>

#include <artifact/artifact.hpp>
#include <common/common.hpp>
#include <common/conf.hpp>
#include <common/key_value_database_lmdb.hpp>
#include <common/json.hpp>
#include <common/testing.hpp>

#include <gtest/gtest.h>

namespace artifact = mender::artifact;
namespace error = mender::common::error;
namespace common = mender::common;
namespace conf = mender::common::conf;
namespace json = mender::common::json;
namespace kv_db = mender::common::key_value_database;
namespace optional = mender::common::optional;
namespace context = mender::update::context;

using namespace std;
using namespace mender::common::testing;

class ContextTests : public testing::Test {
protected:
	TemporaryDirectory test_state_dir;
};

TEST_F(ContextTests, LoadProvidesValid) {
	conf::MenderConfig cfg;
	cfg.paths.SetDataStore(test_state_dir.Path());

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
	cfg.paths.SetDataStore(test_state_dir.Path());

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
	cfg.paths.SetDataStore(test_state_dir.Path());

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
	cfg.paths.SetDataStore(test_state_dir.Path());

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
	cfg.paths.SetDataStore(test_state_dir.Path());

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
	cfg.paths.SetDataStore(test_state_dir.Path());

	context::MenderContext ctx(cfg);
	auto err = ctx.Initialize();
	ASSERT_EQ(err, error::NoError);

	context::ProvidesData data;
	string artifact_name = "artifact_name value";
	string artifact_group = "artifact_group value";
	data["something_extra"] = "something_extra value";
	data["something_extra2"] = "something_extra2 value";

	err = ctx.CommitArtifactData(
		artifact_name,
		artifact_group,
		data,
		optional::optional<context::ClearsProvidesData>(),
		[](kv_db::Transaction &txn) { return error::NoError; });
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
	cfg.paths.SetDataStore(test_state_dir.Path());

	context::MenderContext ctx(cfg);
	auto err = ctx.Initialize();
	ASSERT_EQ(err, error::NoError);

	context::ProvidesData data;
	string artifact_name = "artifact_name value";
	string artifact_group = "artifact_group value";
	data["something_extra"] = "something_extra\nvalue";
	data["something_extra2"] = "something_extra2\tvalue";

	err = ctx.CommitArtifactData(
		artifact_name,
		artifact_group,
		data,
		optional::optional<context::ClearsProvidesData>(),
		[](kv_db::Transaction &txn) { return error::NoError; });
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

TEST_F(ContextTests, CommitLegacyArtifact) {
	// Legacy artifacts come without Provides and Clears Provides data

	conf::MenderConfig cfg;
	cfg.paths.SetDataStore(test_state_dir.Path());

	context::MenderContext ctx(cfg);
	auto err = ctx.Initialize();
	ASSERT_EQ(err, error::NoError);

	string artifact_name = "artifact_name value";
	string artifact_group = "artifact_group value";

	err = ctx.CommitArtifactData(
		artifact_name,
		artifact_group,
		optional::optional<context::ProvidesData>(),
		optional::optional<context::ClearsProvidesData>(),
		[](kv_db::Transaction &txn) { return error::NoError; });
	ASSERT_EQ(err, error::NoError);

	auto &db = ctx.GetMenderStoreDB();
	auto ex_data = db.Read("artifact-name");
	ASSERT_TRUE(ex_data);
	EXPECT_EQ(common::StringFromByteVector(ex_data.value()), "artifact_name value");

	ex_data = db.Read("artifact-group");
	ASSERT_TRUE(ex_data);
	EXPECT_EQ(common::StringFromByteVector(ex_data.value()), "artifact_group value");

	ex_data = db.Read("artifact-provides");
	ASSERT_FALSE(ex_data);
}

TEST_F(ContextTests, CommitArtifactWithClearsProvides) {
	// Legacy artifacts come without Provides and Clears Provides data

	conf::MenderConfig cfg;
	cfg.paths.SetDataStore(test_state_dir.Path());

	context::MenderContext ctx(cfg);
	auto err = ctx.Initialize();
	ASSERT_EQ(err, error::NoError);

	context::ProvidesData data;
	string artifact_name = "artifact_name value";
	string artifact_group = "artifact_group value";
	data["something_extra"] = "something_extra value";
	data["something_extra2"] = "something_extra2 value";
	data["something_different"] = "something_different value";

	// Initialize.

	err = ctx.CommitArtifactData(
		artifact_name,
		artifact_group,
		data,
		optional::optional<context::ClearsProvidesData>(),
		[](kv_db::Transaction &txn) { return error::NoError; });
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
		R"({"something_different":"something_different value","something_extra2":"something_extra2 value","something_extra":"something_extra value"})");

	// Use clears_provides to get rid of a wildcard value.

	context::ClearsProvidesData clears_provides;
	clears_provides.push_back("something_extra*");

	err = ctx.CommitArtifactData(
		artifact_name,
		string {},
		optional::optional<context::ProvidesData>(),
		clears_provides,
		[](kv_db::Transaction &txn) { return error::NoError; });
	ASSERT_EQ(err, error::NoError);

	ex_data = db.Read("artifact-name");
	ASSERT_TRUE(ex_data);
	EXPECT_EQ(common::StringFromByteVector(ex_data.value()), "artifact_name value");

	ex_data = db.Read("artifact-group");
	ASSERT_TRUE(ex_data);
	EXPECT_EQ(common::StringFromByteVector(ex_data.value()), "artifact_group value");

	ex_data = db.Read("artifact-provides");
	ASSERT_TRUE(ex_data);
	EXPECT_EQ(
		common::StringFromByteVector(ex_data.value()),
		R"({"something_different":"something_different value"})");

	// Use clears_provides to get rid of artifact_group.

	clears_provides.push_back("artifact_group");

	err = ctx.CommitArtifactData(
		artifact_name,
		string {},
		optional::optional<context::ProvidesData>(),
		clears_provides,
		[](kv_db::Transaction &txn) { return error::NoError; });
	ASSERT_EQ(err, error::NoError);

	ex_data = db.Read("artifact-name");
	ASSERT_TRUE(ex_data);
	EXPECT_EQ(common::StringFromByteVector(ex_data.value()), "artifact_name value");

	ex_data = db.Read("artifact-group");
	ASSERT_FALSE(ex_data);

	ex_data = db.Read("artifact-provides");
	ASSERT_TRUE(ex_data);
	EXPECT_EQ(
		common::StringFromByteVector(ex_data.value()),
		R"({"something_different":"something_different value"})");

	// Use clears_provides at the same time as new provides values..

	data.clear();
	data["something_extra"] = "something_extra value";
	clears_provides.push_back("something_different");
	clears_provides.push_back("something_extra");

	err = ctx.CommitArtifactData(
		artifact_name, string {}, data, clears_provides, [](kv_db::Transaction &txn) {
			return error::NoError;
		});
	ASSERT_EQ(err, error::NoError);

	ex_data = db.Read("artifact-name");
	ASSERT_TRUE(ex_data);
	EXPECT_EQ(common::StringFromByteVector(ex_data.value()), "artifact_name value");

	ex_data = db.Read("artifact-group");
	ASSERT_FALSE(ex_data);

	ex_data = db.Read("artifact-provides");
	ASSERT_TRUE(ex_data);
	EXPECT_EQ(
		common::StringFromByteVector(ex_data.value()),
		R"({"something_extra":"something_extra value"})");
}

TEST_F(ContextTests, GetDeviceTypeValid) {
	conf::MenderConfig cfg;
	cfg.paths.SetDataStore(test_state_dir.Path());

	context::MenderContext ctx(cfg);
	auto err = ctx.Initialize();
	ASSERT_EQ(err, error::NoError);

	ofstream os(cfg.paths.GetDataStore() + "/device_type");
	ASSERT_TRUE(os);
	os << "device_type=Some device type" << endl;
	os.close();

	auto ex_s = ctx.GetDeviceType();
	ASSERT_TRUE(ex_s);
	EXPECT_EQ(ex_s.value(), "Some device type");

	os.open(cfg.paths.GetDataStore() + "/device_type");
	ASSERT_TRUE(os);
	os << "device_type=Device type no endl";
	os.close();

	ex_s = ctx.GetDeviceType();
	ASSERT_TRUE(ex_s);
	EXPECT_EQ(ex_s.value(), "Device type no endl");
}

TEST_F(ContextTests, GetDeviceTypeNoexist) {
	conf::MenderConfig cfg;
	cfg.paths.SetDataStore(test_state_dir.Path());

	context::MenderContext ctx(cfg);
	auto err = ctx.Initialize();
	ASSERT_EQ(err, error::NoError);

	auto ex_s = ctx.GetDeviceType();
	ASSERT_FALSE(ex_s);
	EXPECT_TRUE(ex_s.error().IsErrno(ENOENT));
}

TEST_F(ContextTests, GetDeviceTypeEmpty) {
	conf::MenderConfig cfg;
	cfg.paths.SetDataStore(test_state_dir.Path());

	context::MenderContext ctx(cfg);
	auto err = ctx.Initialize();
	ASSERT_EQ(err, error::NoError);

	string dtype_fpath = cfg.paths.GetDataStore() + "/device_type";
	ofstream os(dtype_fpath);
	ASSERT_TRUE(os);
	os.close();

	auto ex_s = ctx.GetDeviceType();
	ASSERT_FALSE(ex_s);
	EXPECT_EQ(ex_s.error().code, context::MakeError(context::ParseError, "").code);
}

TEST_F(ContextTests, GetDeviceTypeInvalid) {
	conf::MenderConfig cfg;
	cfg.paths.SetDataStore(test_state_dir.Path());

	context::MenderContext ctx(cfg);
	auto err = ctx.Initialize();
	ASSERT_EQ(err, error::NoError);

	string dtype_fpath = cfg.paths.GetDataStore() + "/device_type";
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
	cfg.paths.SetDataStore(test_state_dir.Path());

	context::MenderContext ctx(cfg);
	auto err = ctx.Initialize();
	ASSERT_EQ(err, error::NoError);

	ofstream os(cfg.paths.GetDataStore() + "/device_type");
	ASSERT_TRUE(os);
	os << "device_type=Some device type" << endl;
	os << "some debris here" << endl;
	os.close();

	auto ex_s = ctx.GetDeviceType();
	ASSERT_FALSE(ex_s);
	EXPECT_EQ(ex_s.error().code, context::MakeError(context::ValueError, "").code);

	os.open(cfg.paths.GetDataStore() + "/device_type");
	ASSERT_TRUE(os);
	os << "device_type=Some device type" << endl;
	os << endl << "some debris here after a blank line" << endl;
	os.close();

	ex_s = ctx.GetDeviceType();
	ASSERT_FALSE(ex_s);
	EXPECT_EQ(ex_s.error().code, context::MakeError(context::ValueError, "").code);
}

TEST(ContextArtifactTests, ArtifactMatchesContextTest) {
	context::ProvidesData provides = {
		{"artifact_name", "artifact_name"}, {"artifact_group", "artifact_group"}};
	string device_type = "device_type";

	artifact::HeaderInfo hdr;
	artifact::TypeInfo ti;

	// MATCHES
	hdr.depends = {{"device_type"}, optional::nullopt, optional::nullopt};
	auto ex_match = context::ArtifactMatchesContext(provides, device_type, hdr, ti);
	ASSERT_TRUE(ex_match);
	EXPECT_TRUE(ex_match.value());

	hdr.depends = {
		{"device_type"},
		optional::optional<vector<string>>({"artifact_name"}),
		optional::optional<vector<string>>({"artifact_group"})};

	ex_match = context::ArtifactMatchesContext(provides, device_type, hdr, ti);
	ASSERT_TRUE(ex_match);
	EXPECT_TRUE(ex_match.value());

	hdr.depends = {
		{"device_type1", "device_type"},
		optional::optional<vector<string>>({"artifact_name1", "artifact_name"}),
		optional::optional<vector<string>>({"artifact_group1", "artifact_group"})};

	ex_match = context::ArtifactMatchesContext(provides, device_type, hdr, ti);
	ASSERT_TRUE(ex_match);
	EXPECT_TRUE(ex_match.value());

	hdr.depends = {
		{"device_type1", "device_type"},
		optional::optional<vector<string>>({"artifact_name1", "artifact_name"}),
		optional::nullopt};

	ex_match = context::ArtifactMatchesContext(provides, device_type, hdr, ti);
	ASSERT_TRUE(ex_match);
	EXPECT_TRUE(ex_match.value());

	hdr.depends = {
		{"device_type1", "device_type"},
		optional::nullopt,
		optional::optional<vector<string>>({"artifact_group1", "artifact_group"})};

	ex_match = context::ArtifactMatchesContext(provides, device_type, hdr, ti);
	ASSERT_TRUE(ex_match);
	EXPECT_TRUE(ex_match.value());

	// Mismatches in device type or artifact name/group
	hdr.depends = {
		{"device_type"},
		optional::optional<vector<string>>({"artifact_name_other"}),
		optional::optional<vector<string>>({"artifact_group"})};

	ex_match = context::ArtifactMatchesContext(provides, device_type, hdr, ti);
	ASSERT_TRUE(ex_match);
	EXPECT_FALSE(ex_match.value());

	hdr.depends = {
		{"device_type"},
		optional::optional<vector<string>>({"artifact_name"}),
		optional::optional<vector<string>>({"artifact_group_other"})};

	ex_match = context::ArtifactMatchesContext(provides, device_type, hdr, ti);
	ASSERT_TRUE(ex_match);
	EXPECT_FALSE(ex_match.value());

	hdr.depends = {
		{"device_type_other"},
		optional::optional<vector<string>>({"artifact_name"}),
		optional::optional<vector<string>>({"artifact_group"})};

	ex_match = context::ArtifactMatchesContext(provides, device_type, hdr, ti);
	ASSERT_TRUE(ex_match);
	EXPECT_FALSE(ex_match.value());

	// Missing artifact_group
	provides = {{"artifact_name", "artifact_name"}};
	hdr.depends = {
		{"device_type"},
		optional::optional<vector<string>>({"artifact_name"}),
		optional::optional<vector<string>>({"artifact_group"})};

	ex_match = context::ArtifactMatchesContext(provides, device_type, hdr, ti);
	ASSERT_TRUE(ex_match);
	EXPECT_FALSE(ex_match.value());

	// Match incl. extra provides/depends
	provides = {
		{"artifact_name", "artifact_name"},
		{"artifact_group", "artifact_group"},
		{"something_else", "something_else"}};
	hdr.depends = {
		{"device_type"},
		optional::optional<vector<string>>({"artifact_name"}),
		optional::optional<vector<string>>({"artifact_group"})};
	ti.artifact_depends = optional::optional<unordered_map<string, string>>(
		unordered_map<string, string> {{"something_else", "something_else"}});

	ex_match = context::ArtifactMatchesContext(provides, device_type, hdr, ti);
	ASSERT_TRUE(ex_match);
	EXPECT_TRUE(ex_match.value());

	// Extra provides/depends mismatch
	ti.artifact_depends = optional::optional<unordered_map<string, string>>(
		unordered_map<string, string> {{"something_else", "other_something_else"}});

	ex_match = context::ArtifactMatchesContext(provides, device_type, hdr, ti);
	ASSERT_TRUE(ex_match);
	EXPECT_FALSE(ex_match.value());

	// Missing extra provides/depends
	ti.artifact_depends = optional::optional<unordered_map<string, string>>(
		unordered_map<string, string> {{"other_something_else", "something_else"}});

	ex_match = context::ArtifactMatchesContext(provides, device_type, hdr, ti);
	ASSERT_TRUE(ex_match);
	EXPECT_FALSE(ex_match.value());
}

TEST(ContextArtifactTests, ArtifactMatchesContextErrorsTest) {
#ifndef NDEBUG
	GTEST_SKIP() << "requires assert() to be a no-op";
#else
	context::ProvidesData provides = {
		{"artifact_name", "artifact_name"}, {"artifact_group", "artifact_group"}};
	string device_type = "device_type";

	artifact::HeaderInfo hdr;
	artifact::TypeInfo ti;

	// Missing device type
	hdr.depends = {{}, optional::nullopt, optional::nullopt};

	auto ex_match = context::ArtifactMatchesContext(provides, device_type, hdr, ti);
	ASSERT_FALSE(ex_match);

	// Empty artifact names vector
	hdr.depends = {
		{"device_type"}, optional::optional<vector<string>>(vector<string>(0)), optional::nullopt};

	ex_match = context::ArtifactMatchesContext(provides, device_type, hdr, ti);
	ASSERT_FALSE(ex_match);

	// Empty artifact groups vector
	hdr.depends = {
		{"device_type"}, optional::nullopt, optional::optional<vector<string>>(vector<string>(0))};

	ex_match = context::ArtifactMatchesContext(provides, device_type, hdr, ti);
	ASSERT_FALSE(ex_match);
#endif
}
