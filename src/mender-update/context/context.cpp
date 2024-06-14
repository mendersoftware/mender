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

#include <cctype>

#include <algorithm>
#include <set>

#include <artifact/artifact.hpp>
#include <common/common.hpp>
#include <common/error.hpp>
#include <common/expected.hpp>
#include <common/io.hpp>
#include <common/json.hpp>
#include <common/key_value_database.hpp>
#include <common/log.hpp>
#include <common/path.hpp>

namespace mender {
namespace update {
namespace context {

using namespace std;
namespace artifact = mender::artifact;
namespace common = mender::common;
namespace error = mender::common::error;
namespace expected = mender::common::expected;
namespace io = mender::common::io;
namespace json = mender::common::json;
namespace kv_db = mender::common::key_value_database;
namespace log = mender::common::log;
namespace path = mender::common::path;

const string MenderContext::broken_artifact_name_suffix {"_INCONSISTENT"};

const string MenderContext::artifact_name_key {"artifact-name"};
const string MenderContext::artifact_group_key {"artifact-group"};
const string MenderContext::artifact_provides_key {"artifact-provides"};
const string MenderContext::standalone_state_key {"standalone-state"};
const string MenderContext::state_data_key {"state"};
const string MenderContext::state_data_key_uncommitted {"state-uncommitted"};
const string MenderContext::update_control_maps {"update-control-maps"};
const string MenderContext::auth_token_name {"authtoken"};
const string MenderContext::auth_token_cache_invalidator_name {"auth-token-cache-invalidator"};

const int MenderContext::standalone_data_version {1};

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
	case NoSuchUpdateModuleError:
		return "Update Module not found for given artifact type";
	case DatabaseValueError:
		return "Value in database is invalid or corrupted";
	case RebootRequiredError:
		return "Reboot required";
	case NoUpdateInProgressError:
		return "No update in progress";
	case UnexpectedHttpResponse:
		return "Unexpected HTTP response";
	case StateDataStoreCountExceededError:
		return "State data store count exceeded";
	}
	assert(false);
	return "Unknown";
}

error::Error MakeError(MenderContextErrorCode code, const string &msg) {
	return error::Error(error_condition(code, MenderContextErrorCategory), msg);
}

error::Error MenderContext::Initialize() {
#ifdef MENDER_USE_LMDB
	auto err = mender_store_.Open(path::Join(config_.paths.GetDataStore(), "mender-store"));
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
	ExpectedProvidesData data;
	auto err = mender_store_.ReadTransaction([this, &data](kv_db::Transaction &txn) {
		data = LoadProvides(txn);
		if (!data) {
			return data.error();
		}
		return error::NoError;
	});
	if (err != error::NoError) {
		return expected::unexpected(err);
	}
	return data;
}

ExpectedProvidesData MenderContext::LoadProvides(kv_db::Transaction &txn) {
	string artifact_name;
	string artifact_group;
	string artifact_provides_str;

	auto err = kv_db::ReadString(txn, artifact_name_key, artifact_name, true);
	if (err != error::NoError) {
		return expected::unexpected(err);
	}
	err = kv_db::ReadString(txn, artifact_group_key, artifact_group, true);
	if (err != error::NoError) {
		return expected::unexpected(err);
	}
	err = kv_db::ReadString(txn, artifact_provides_key, artifact_provides_str, true);
	if (err != error::NoError) {
		return expected::unexpected(err);
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
		return ret;
	}

	auto ex_j = json::Load(artifact_provides_str);
	if (!ex_j) {
		return expected::unexpected(ex_j.error());
	}
	auto ex_children = ex_j.value().GetChildren();
	if (!ex_children) {
		return expected::unexpected(ex_children.error());
	}

	auto children = ex_children.value();
	if (!all_of(children.cbegin(), children.cend(), [](const json::ChildrenMap::value_type &it) {
			return it.second.IsString();
		})) {
		auto err = json::MakeError(json::TypeError, "Unexpected non-string data in provides");
		return expected::unexpected(err);
	}
	for (const auto &it : ex_children.value()) {
		ret[it.first] = it.second.GetString().value();
	}

	return ret;
}

expected::ExpectedString MenderContext::GetDeviceType() {
	string device_type_fpath;
	if (config_.device_type_file != "") {
		device_type_fpath = config_.device_type_file;
	} else {
		device_type_fpath = path::Join(config_.paths.GetDataStore(), "device_type");
	}
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

	if (!is.eof()) {
		errno = 0;
		getline(is, line);
		if ((line != "") || (!is.eof())) {
			auto err = MakeError(ValueError, "Trailing device_type data");
			return expected::ExpectedString(expected::unexpected(err));
		}
	}

	return expected::ExpectedString(ret);
}

bool CheckClearsMatch(const string &to_match, const string &clears_string) {
	if (clears_string.empty()) {
		return to_match.empty();
	}

	vector<std::string> sub_strings;
	string escaped;
	for (const auto chr : clears_string) {
		if (chr == '*') {
			sub_strings.push_back(escaped);
			escaped.clear();
		} else {
			escaped.push_back(chr);
		}
	}
	sub_strings.push_back(escaped);

	// Make sure that that front of vector starts at index 0
	if (sub_strings.front() != ""
		&& to_match.compare(0, sub_strings.front().size(), sub_strings.front()) != 0) {
		return false;
	}
	// Checks if no trailing wildcard
	if (sub_strings.back() != ""
		&& to_match.compare(
			   to_match.size() - sub_strings.back().size(), to_match.size(), sub_strings.back())
			   != 0) {
		return false;
	}

	// Iterate over substrings, set boundary if found to avoid
	// matching same substring twice
	size_t boundary = 0;
	for (const auto &str : sub_strings) {
		if (!str.empty()) {
			size_t find = to_match.find(str, boundary);
			if (find == string::npos) {
				return false;
			}
			boundary = find + str.size();
		}
	}
	return true;
}

error::Error FilterProvides(
	const ProvidesData &new_provides,
	const ClearsProvidesData &clears_provides,
	ProvidesData &to_modify) {
	// Use clears_provides to filter out unwanted provides.
	for (auto to_clear : clears_provides) {
		set<string> keys;
		for (auto provide : to_modify) {
			if (CheckClearsMatch(provide.first, to_clear)) {
				keys.insert(provide.first);
			}
		}
		for (auto key : keys) {
			to_modify.erase(key);
		}
	}

	// Now add the provides from the new_provides set.
	for (auto provide : new_provides) {
		to_modify[provide.first] = provide.second;
	}

	return error::NoError;
}

error::Error MenderContext::CommitArtifactData(
	string artifact_name,
	string artifact_group,
	const optional<ProvidesData> &new_provides,
	const optional<ClearsProvidesData> &clears_provides,
	function<error::Error(kv_db::Transaction &)> txn_func) {
	return mender_store_.WriteTransaction([&](kv_db::Transaction &txn) {
		auto exp_existing = LoadProvides(txn);
		if (!exp_existing) {
			return exp_existing.error();
		}
		auto modified_provides = exp_existing.value();

		error::Error err;
		if (!new_provides && !clears_provides) {
			// Neither provides nor clear_provides came with the artifact. This means
			// erase everything. `artifact_name` and `artifact_group` will still be
			// preserved through special cases below.
			modified_provides.clear();
		} else if (!new_provides) {
			// No new provides came with the artifact. This means filter what we have,
			// but don't add any new provides fields.
			ProvidesData empty_provides;
			err = FilterProvides(empty_provides, clears_provides.value(), modified_provides);
		} else if (!clears_provides) {
			// Missing clears_provides is equivalent to `["*"]`, for historical reasons.
			modified_provides = new_provides.value();
		} else {
			// Standard case, filter existing provides using clears_provides, and then
			// add new ones on top.
			err = FilterProvides(new_provides.value(), clears_provides.value(), modified_provides);
		}
		if (err != error::NoError) {
			return err;
		}

		if (artifact_name != "") {
			modified_provides["artifact_name"] = artifact_name;
		}
		if (artifact_group != "") {
			modified_provides["artifact_group"] = artifact_group;
		}

		string artifact_provides_str {"{"};
		for (const auto &it : modified_provides) {
			if (it.first != "artifact_name" && it.first != "artifact_group") {
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

		if (modified_provides["artifact_name"] != "") {
			err = txn.Write(
				artifact_name_key,
				common::ByteVectorFromString(modified_provides["artifact_name"]));
			if (err != error::NoError) {
				return err;
			}
		} else {
			// This should not happen.
			AssertOrReturnError(false);
		}

		if (modified_provides["artifact_group"] != "") {
			err = txn.Write(
				artifact_group_key,
				common::ByteVectorFromString(modified_provides["artifact_group"]));
		} else {
			err = txn.Remove(artifact_group_key);
		}
		if (err != error::NoError) {
			return err;
		}

		if (artifact_provides_str != "") {
			err = txn.Write(
				artifact_provides_key, common::ByteVectorFromString(artifact_provides_str));
			if (err != error::NoError) {
				return err;
			}
		}
		return txn_func(txn);
	});
}

expected::ExpectedBool MenderContext::MatchesArtifactDepends(const artifact::HeaderView &hdr_view) {
	auto ex_dev_type = GetDeviceType();
	if (!ex_dev_type) {
		return expected::unexpected(ex_dev_type.error());
	}
	auto ex_provides = LoadProvides();
	if (!ex_provides) {
		return expected::unexpected(ex_provides.error());
	}
	auto &provides = ex_provides.value();
	return ArtifactMatchesContext(provides, ex_dev_type.value(), hdr_view);
}

expected::ExpectedBool ArtifactMatchesContext(
	const ProvidesData &provides, const string &device_type, const artifact::HeaderView &hdr_view) {
	using common::MapContainsStringKey;
	if (!MapContainsStringKey(provides, "artifact_name")) {
		return expected::unexpected(
			MakeError(ValueError, "Missing artifact_name value in provides"));
	}

	auto hdr_depends = hdr_view.GetDepends();
	AssertOrReturnUnexpected(hdr_depends["device_type"].size() > 0);
	if (!common::VectorContainsString(hdr_depends["device_type"], device_type)) {
		log::Error("Artifact device type doesn't match");
		return false;
	}
	hdr_depends.erase("device_type");

	AssertOrReturnUnexpected(
		!MapContainsStringKey(hdr_depends, "artifact_name")
		|| (hdr_depends["artifact_name"].size() > 0));

	AssertOrReturnUnexpected(
		!MapContainsStringKey(hdr_depends, "artifact_group")
		|| (hdr_depends["artifact_group"].size() > 0));

	for (auto it : hdr_depends) {
		if (!common::MapContainsStringKey(provides, it.first)) {
			log::Error("Missing '" + it.first + "' in provides, required by artifact depends");
			return false;
		}
		if (!common::VectorContainsString(hdr_depends[it.first], provides.at(it.first))) {
			log::Error(
				"Provides value '" + provides.at(it.first) + "' doesn't match any of the '"
				+ it.first + "' artifact depends ("
				+ common::StringVectorToString(hdr_depends[it.first]) + ")");
			return false;
		}
	}

	return true;
}

} // namespace context
} // namespace update
} // namespace mender
