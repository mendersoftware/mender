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

#include <mender-update/update_module/v3/update_module.hpp>

#include <cerrno>

#include <boost/filesystem.hpp>

#include <common/conf.hpp>
#include <common/log.hpp>

namespace mender {
namespace update {
namespace update_module {
namespace v3 {

using namespace std;

namespace error = mender::common::error;
namespace expected = mender::common::expected;
namespace conf = mender::common::conf;
namespace log = mender::common::log;
namespace fs = boost::filesystem;

expected::ExpectedStringVector DiscoverUpdateModules(const conf::MenderConfig &config) {
	vector<string> ret {};
	fs::path dir_path = fs::path(config.data_store_dir) / "modules/v3";

	try {
		for (auto entry : fs::directory_iterator(dir_path)) {
			const fs::path file_path = entry.path();
			const string file_path_str = file_path.string();
			if (!fs::is_regular_file(file_path)) {
				log::Warning("'" + file_path_str + "' is not a regular file");
				continue;
			}

			const fs::perms perms = entry.status().permissions();
			if ((perms & (fs::perms::owner_exe | fs::perms::group_exe | fs::perms::others_exe))
				== fs::perms::no_perms) {
				log::Warning("'" + file_path_str + "' is not executable");
				continue;
			}

			// TODO: should check access(X_OK)?
			ret.push_back(file_path_str);
		}
	} catch (const fs::filesystem_error &e) {
		auto code = e.code();
		if (code.value() == ENOENT) {
			// directory not found is not an error, just return an empty vector
			return ret;
		}
		// everything (?) else is an error
		return expected::unexpected(error::Error(
			code.default_error_condition(),
			"Failed to discover update modules in '" + dir_path.string() + "': " + e.what()));
	}

	return expected::ExpectedStringVector(move(ret));
}

} // namespace v3
} // namespace update_module
} // namespace update
} // namespace mender
