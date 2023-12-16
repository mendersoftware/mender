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

#include <common/path.hpp>

#include <fcntl.h>

#include <filesystem>
#include <string>

#include <common/error.hpp>

namespace mender {
namespace common {
namespace path {

using namespace std;
namespace fs = std::filesystem;

error::Error DataSyncRecursively(const string &dir) {
	// We need to be careful which method we use to sync data to disk. `sync()` is tempting,
	// because it is easy, but does not provide strong enough guarantees. POSIX says that it
	// does not wait for writes to succeed (it does on Linux, but not generally), which we
	// need. So then we need to use `fsync()` or `fdatasync()`, but they operate only on single
	// files/directories. Therefore we need to do it recursively.
	error_code ec;
	for (auto &entry : fs::recursive_directory_iterator(fs::path(dir), ec)) {
		if (not entry.is_directory() and not entry.is_regular_file()) {
			continue;
		}

		int fd = open(entry.path().string().c_str(), O_RDONLY);
		if (fd < 0) {
			return error::Error(
				generic_category().default_error_condition(errno),
				"Could not open path to sync: " + entry.path().string());
		}

		unique_ptr<int, void (*)(int *)> fd_closer(&fd, [](int *fd) {
			if (*fd >= 0) {
				close(*fd);
			}
		});

		int result = fdatasync(fd);
		if (result != 0) {
			return error::Error(
				generic_category().default_error_condition(errno),
				"Could sync path: " + entry.path().string());
		}
	}
	if (ec) {
		return error::Error(ec.default_error_condition(), "DataSyncRecursively");
	}

	return error::NoError;
}

} // namespace path
} // namespace common
} // namespace mender
