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

#include <cerrno>
#include <filesystem>
#include <string>

#include <common/error.hpp>

namespace mender {
namespace common {
namespace path {

using namespace std;
namespace fs = std::filesystem;

static unordered_map<Perms, mode_t> perm_map = {
	{Perms::Owner_read, S_IRUSR},
	{Perms::Owner_write, S_IWUSR},
	{Perms::Owner_exec, S_IXUSR},
	{Perms::Group_read, S_IRGRP},
	{Perms::Group_write, S_IWGRP},
	{Perms::Group_exec, S_IXGRP},
	{Perms::Others_read, S_IROTH},
	{Perms::Others_write, S_IWOTH},
	{Perms::Others_exec, S_IXOTH},
};

expected::ExpectedInt FileCreate(const string &path, vector<Perms> perms) {
	mode_t mode = 0;
	std::for_each(
		perms.cbegin(), perms.cend(), [&mode](const Perms perm) { mode |= perm_map.at(perm); });
	int fd = open(path.c_str(), O_CREAT | O_EXCL | O_WRONLY | O_TRUNC, mode);
	int err = errno;
	if (fd != -1) {
		return fd;
	}

	return expected::unexpected(error::Error(
		std::generic_category().default_error_condition(err),
		"Failed to create file '" + path + "': " + strerror(err)));
}

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
