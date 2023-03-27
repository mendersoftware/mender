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
#include <fstream>

#include <boost/filesystem.hpp>

#include <common/conf.hpp>
#include <common/io.hpp>
#include <common/log.hpp>
#include <mender-update/context.hpp>

namespace mender {
namespace update {
namespace update_module {
namespace v3 {

using namespace std;

namespace error = mender::common::error;
namespace expected = mender::common::expected;
namespace conf = mender::common::conf;
namespace context = mender::update::context;
namespace io = mender::common::io;
namespace log = mender::common::log;
namespace fs = boost::filesystem;


error::Error UpdateModule::PrepareFileTree(const string &path) {
	// make sure all the required data can be gathered first before creating
	// directories and files
	auto ex_provides = ctx_.LoadProvides();
	if (!ex_provides) {
		return ex_provides.error();
	}

	auto ex_device_type = ctx_.GetDeviceType();
	if (!ex_device_type) {
		return ex_device_type.error();
	}

	const fs::path dir_path {path};
	const fs::path header_subdir_path = dir_path / "header";
	const fs::path tmp_subdir_path = dir_path / "tmp";
	try {
		// create the "/header" subdirectory right away, create_directories()
		// creates missing parents automatically
		fs::create_directories(header_subdir_path);
	} catch (const fs::filesystem_error &e) {
		return error::Error(
			e.code().default_error_condition(),
			"Failed to create directory '" + header_subdir_path.string() + "': " + e.what());
	}

	try {
		fs::create_directories(tmp_subdir_path);
	} catch (const fs::filesystem_error &e) {
		return error::Error(
			e.code().default_error_condition(),
			"Failed to create directory '" + tmp_subdir_path.string() + "': " + e.what());
	}

	auto create_data_file = [&](const string &file_name, const string &data) -> error::Error {
		string fpath = (dir_path / file_name).string();
		auto ex_os = io::OpenOfstream(fpath);
		if (!ex_os) {
			return ex_os.error();
		}
		ofstream &os = ex_os.value();
		if (data != "") {
			auto err = io::WriteStringIntoOfstream(os, data);
			return err;
		}
		return error::NoError;
	};

	auto provides = ex_provides.value();
	auto write_provides_into_file = [&](const string &key) {
		return create_data_file(
			"current_" + key, (provides.count(key) != 0) ? provides[key] + "\n" : "");
	};

	auto err = create_data_file("version", "3\n");
	if (err != error::NoError) {
		return err;
	}

	err = write_provides_into_file("artifact_name");
	if (err != error::NoError) {
		return err;
	}
	err = write_provides_into_file("artifact_group");
	if (err != error::NoError) {
		return err;
	}

	auto device_type = ex_device_type.value();
	err = create_data_file("current_device_type", device_type + "\n");
	if (err != error::NoError) {
		return err;
	}

	return error::NoError;
}

error::Error UpdateModule::DeleteFileTree(const string &path) {
	try {
		fs::remove_all(fs::path {path});
	} catch (const fs::filesystem_error &e) {
		return error::Error(
			e.code().default_error_condition(),
			"Failed to recursively remove directory '" + path + "': " + e.what());
	}

	return error::NoError;
}

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
