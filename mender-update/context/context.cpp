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

#include <common/common.hpp>
#include <common/conf/paths.hpp>
#include <common/error.hpp>
#include <common/expected.hpp>
#include <common/io.hpp>
#include <common/json.hpp>
#include <common/key_value_database.hpp>
#include <common/path.hpp>

namespace mender {
namespace update {
namespace context {

using namespace std;
namespace common = mender::common;
namespace conf = mender::common::conf;
namespace error = mender::common::error;
namespace expected = mender::common::expected;
namespace io = mender::common::io;
namespace json = mender::common::json;
namespace kv_db = mender::common::key_value_database;
namespace path = mender::common::path;

const string MenderContext::artifact_name_key {"artifact-name"};
const string MenderContext::artifact_group_key {"artifact-group"};
const string MenderContext::artifact_provides_key {"artifact-provides"};
const string MenderContext::standalone_state_key {"standalone-state"};
const string MenderContext::state_data_key {"state"};
const string MenderContext::state_data_key_uncommitted {"state-uncommitted"};
const string MenderContext::update_control_maps {"update-control-maps"};
const string MenderContext::auth_token_name {"authtoken"};
const string MenderContext::auth_token_cache_invalidator_name {"auth-token-cache-invalidator"};

const MenderContextErrorCategoryClass MenderContextErrorCategory;

const char *MenderContextErrorCategoryClass::name() const noexcept {
	return "MenderContextErrorCategory";
}

string MenderContextErrorCategoryClass::message(int code) const {
	switch (code) {
	case NoError:
		return "Success";
	case ParseError:
		return "Parse error";
	case ValueError:
		return "Value error";
	}
	assert(false);
	return "Unknown";
}

error::Error MakeError(MenderContextErrorCode code, const string &msg) {
	return error::Error(error_condition(code, MenderContextErrorCategory), msg);
}

error::Error MenderContext::Initialize() {
#if MENDER_USE_LMDB
	auto err = mender_store_.Open(path::Join(config_.data_store_dir, "mender-store"));
	if (error::NoError != err) {
		return err;
	}
	err = mender_store_.Remove(auth_token_name);
	if (error::NoError != err) {
		// key not existing in the DB is not treated as an error so this must be
		// a real error
		return err;
	}
	err = mender_store_.Remove(auth_token_cache_invalidator_name);
	if (error::NoError != err) {
		// same as above -- a real error
		return err;
	}

	return error::NoError;
#else
	return error::NoError;
#endif
}

kv_db::KeyValueDatabase &MenderContext::GetMenderStoreDB() {
	return mender_store_;
}

ExpectedProvidesData MenderContext::LoadProvides() {
	string artifact_name;
	string artifact_group;
	string artifact_provides_str;

	auto err = mender_store_.ReadTransaction([&](kv_db::Transaction &txn) {
		auto err = kv_db::ReadString(txn, artifact_name_key, artifact_name, true);
		if (err != error::NoError) {
			return err;
		}
		err = kv_db::ReadString(txn, artifact_group_key, artifact_group, true);
		if (err != error::NoError) {
			return err;
		}
		err = kv_db::ReadString(txn, artifact_provides_key, artifact_provides_str, true);
		if (err != error::NoError) {
			return err;
		}
		return err;
	});
	if (err != error::NoError) {
		return ExpectedProvidesData(expected::unexpected(err));
	}

	ProvidesData ret {};
	if (artifact_name != "") {
		ret["artifact_name"] = artifact_name;
	}
	if (artifact_group != "") {
		ret["artifact_group"] = artifact_group;
	}
	if (artifact_provides_str == "") {
		// nothing more to do
		return ExpectedProvidesData(ret);
	}

	auto ex_j = json::Load(artifact_provides_str);
	if (!ex_j) {
		return ExpectedProvidesData(expected::unexpected(ex_j.error()));
	}
	auto ex_children = ex_j.value().GetChildren();
	if (!ex_children) {
		return ExpectedProvidesData(expected::unexpected(ex_children.error()));
	}

	auto children = ex_children.value();
	if (!all_of(children.cbegin(), children.cend(), [](const json::ChildrenMap::value_type &it) {
			return it.second.IsString();
		})) {
		auto err = json::MakeError(json::TypeError, "Unexpected non-string data in provides");
		return ExpectedProvidesData(expected::unexpected(err));
	}
	for (const auto &it : ex_children.value()) {
		ret[it.first] = it.second.GetString().value();
	}

	return ExpectedProvidesData(ret);
}

expected::ExpectedString MenderContext::GetDeviceType() {
	string device_type_fpath = path::Join(config_.data_store_dir, "device_type");
	auto ex_is = io::OpenIfstream(device_type_fpath);
	if (!ex_is) {
		return expected::ExpectedString(expected::unexpected(ex_is.error()));
	}

	auto &is = ex_is.value();
	string line;
	errno = 0;
	getline(is, line);
	if (is.bad()) {
		int io_errno = errno;
		error::Error err {
			generic_category().default_error_condition(io_errno),
			"Failed to read device type from '" + device_type_fpath + "'"};
		return expected::ExpectedString(expected::unexpected(err));
	}

	const string::size_type eq_pos = 12;
	if (line.substr(0, eq_pos) != "device_type=") {
		auto err = MakeError(ParseError, "Failed to parse device_type data '" + line + "'");
		return expected::ExpectedString(expected::unexpected(err));
	}

	string ret = line.substr(eq_pos, string::npos);

	errno = 0;
	getline(is, line);
	if ((line != "") || (!is.eof())) {
		auto err = MakeError(ValueError, "Trailing device_type data");
		return expected::ExpectedString(expected::unexpected(err));
	}

	return expected::ExpectedString(ret);
}

error::Error MenderContext::CommitArtifactData(const ProvidesData &data) {
	string artifact_name;
	string artifact_group;
	string artifact_provides_str {"{"};
	for (const auto &it : data) {
		if (it.first == "artifact_name") {
			artifact_name = it.second;
		} else if (it.first == "artifact_group") {
			artifact_group = it.second;
		} else {
			artifact_provides_str +=
				"\"" + it.first + "\":" + "\"" + json::EscapeString(it.second) + "\",";
		}
	}

	// if some key-value pairs were added, replace the trailing comma with the
	// closing '}' to make a valid JSON
	if (artifact_provides_str != "{") {
		artifact_provides_str[artifact_provides_str.length() - 1] = '}';
	} else {
		// set to an empty value for consistency with the other two items
		artifact_provides_str = "";
	}

	auto commit_data_to_db = [&](kv_db::Transaction &txn) {
		if (artifact_name != "") {
			auto err = txn.Write(artifact_name_key, common::ByteVectorFromString(artifact_name));
			if (err != error::NoError) {
				return err;
			}
		}
		if (artifact_group != "") {
			auto err = txn.Write(artifact_group_key, common::ByteVectorFromString(artifact_group));
			if (err != error::NoError) {
				return err;
			}
		}
		if (artifact_provides_str != "") {
			auto err = txn.Write(
				artifact_provides_key, common::ByteVectorFromString(artifact_provides_str));
			if (err != error::NoError) {
				return err;
			}
		}
		return error::NoError;
	};

	return mender_store_.WriteTransaction(commit_data_to_db);
}

} // namespace context
} // namespace update
} // namespace mender
