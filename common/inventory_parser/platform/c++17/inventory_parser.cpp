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

#include <common/inventory_parser.hpp>

#include <filesystem>

#include <common/key_value_parser.hpp>
#include <common/processes.hpp>
#include <common/log.hpp>

namespace mender {
namespace common {
namespace inventory_parser {

using namespace std;
namespace kvp = mender::common::key_value_parser;
namespace procs = mender::common::processes;
namespace log = mender::common::log;
namespace err = mender::common::error;
namespace fs = std::filesystem;

kvp::ExpectedKeyValuesMap GetInventoryData(const string &generators_dir) {
	bool any_success = false;
	bool any_failure = false;
	kvp::KeyValuesMap data;

	fs::path dir_path(generators_dir);
	if (!fs::exists(dir_path)) {
		return kvp::ExpectedKeyValuesMap(data);
	}

	for (const auto &entry : fs::directory_iterator {dir_path}) {
		fs::path file_path = entry.path();
		if (!fs::is_regular_file(file_path)) {
			continue;
		}

		string file_path_str = file_path.string();
		if (file_path.filename().string().find("mender-inventory-") != 0) {
			log::Warning(
				"'" + file_path_str + "' doesn't have the 'mender-inventory-' prefix, skipping");
			continue;
		}

		fs::perms perms = entry.status().permissions();
		if ((perms & (fs::perms::owner_exec | fs::perms::group_exec | fs::perms::others_exec))
			== fs::perms::none) {
			log::Warning("'" + file_path_str + "' is not executable");
			continue;
		}
		procs::Process proc({file_path_str});
		auto ex_line_data = proc.GenerateLineData();
		if (!ex_line_data) {
			log::Error("'" + file_path_str + "' failed: " + ex_line_data.error().message);
			any_failure = true;
			continue;
		}

		auto err = kvp::AddParseKeyValues(data, ex_line_data.value());
		if (error::NoError != err) {
			log::Error("Failed to parse data from '" + file_path_str + "': " + err.message);
			any_failure = true;
		} else {
			any_success = true;
		}
	}

	if (any_success || !any_failure) {
		return kvp::ExpectedKeyValuesMap(data);
	} else {
		err::Error error = MakeError(
			kvp::KeyValueParserErrorCode::NoDataError,
			"No data successfully read from inventory scripts in '" + generators_dir + "'");
		return expected::unexpected(error);
	}
}

} // namespace inventory_parser
} // namespace common
} // namespace mender
