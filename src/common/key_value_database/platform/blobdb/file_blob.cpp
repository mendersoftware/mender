// Copyright 2026 Northern.tech AS
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

#include <common/key_value_database_blobdb.hpp>
#include <common/key_value_database/platform/blobdb/file_blob.hpp>

// We need these two C headers for open() and close(), respectively, because we
// need to do flock() and there's no C++ API for that.
#include <fcntl.h>
#include <unistd.h>
#include <sys/file.h> // flock()

#include <cerrno>
#include <cstdio>

#include <common/path.hpp>

namespace mender {
namespace common {
namespace key_value_database {

namespace path = common::path;

error::Error FileBlobdbTransaction::SerializeDB(const DB &db) {
	// There can be no collision because no two transactions can be serializing
	// the DB at the same time so no random/special name needed here.
	string new_file_path = path_or_name_ + ".new";
	auto ex_ofs = io::OpenOfstream(new_file_path);
	if (!ex_ofs) {
		return ex_ofs.error().WithContext("Cannot save DB contents");
	}
	auto &ofs = ex_ofs.value();

	auto err = error::NoError;
	for (const auto &kv : db) {
		if (kv.first.size() > std::numeric_limits<uint32_t>::max()) {
			err = error::Error(
				generic_category().default_error_condition(EOVERFLOW),
				"Key data too long, cannot be serialized");
			break;
		}
		if (kv.second.size() > std::numeric_limits<uint32_t>::max()) {
			err = error::Error(
				generic_category().default_error_condition(EOVERFLOW),
				"Value data too long, cannot be serialized");
			break;
		}
		uint32_t data_size = static_cast<uint32_t>(kv.first.size());
		ofs.write(reinterpret_cast<const char *>(&data_size), sizeof(data_size));
		if (!ofs) {
			err = error::Error(
				generic_category().default_error_condition(errno), "Failed to write key size");
			break;
		}
		ofs.write(kv.first.data(), kv.first.size());
		if (!ofs) {
			err = error::Error(
				generic_category().default_error_condition(errno), "Failed to write key data");
			break;
		}
		data_size = static_cast<uint32_t>(kv.second.size());
		ofs.write(reinterpret_cast<const char *>(&data_size), sizeof(data_size));
		if (!ofs) {
			err = error::Error(
				generic_category().default_error_condition(errno), "Failed to write value size");
			break;
		}
		ofs.write(reinterpret_cast<const char *>(kv.second.data()), kv.second.size());
		if (!ofs) {
			err = error::Error(
				generic_category().default_error_condition(errno), "Failed to write value data");
			break;
		}
	}
	ofs.close();

	if (err == error::NoError) {
		if (std::rename(new_file_path.c_str(), path_or_name_.c_str()) != 0) {
			err = error::Error(
				generic_category().default_error_condition(errno), "Failed to replace DB contents");
		}
	}
	if (err != error::NoError) {
		std::remove(new_file_path.c_str());
	}
	return err;
}

ExpectedDB FileBlobdbTransaction::DeserializeDB() {
	if (!path::FileExists(path_or_name_)) {
		return DB();
	}
	auto ex_ifs = io::OpenIfstream(path_or_name_);
	if (!ex_ifs) {
		return expected::unexpected(ex_ifs.error().WithContext("Cannot load DB contents"));
	}
	auto &ifs = ex_ifs.value();

	auto err = error::NoError;
	DB loaded;
	while (ifs) {
		uint32_t data_size = 0;
		ifs.read(reinterpret_cast<char *>(&data_size), sizeof(data_size));
		if (data_size == 0) {
			if (!ifs.eof()) {
				err = error::Error(
					generic_category().default_error_condition(errno), "Failed to read key size");
			}
			break;
		}
		auto key = string(data_size, '\0');
		ifs.read(key.data(), data_size);
		if ((!ifs && !ifs.eof()) || (ifs.gcount() != data_size)) {
			err = error::Error(
				generic_category().default_error_condition(errno), "Failed to read key data");
			break;
		}
		data_size = 0;
		ifs.read(reinterpret_cast<char *>(&data_size), sizeof(data_size));
		if (data_size == 0) {
			err = error::Error(
				generic_category().default_error_condition(errno), "Failed to read value size");
			break;
		}
		auto value = vector<uint8_t>(data_size);
		ifs.read(reinterpret_cast<char *>(value.data()), data_size);
		if ((!ifs && !ifs.eof()) || (ifs.gcount() != data_size)) {
			err = error::Error(
				generic_category().default_error_condition(errno), "Failed to read value data");
			break;
		}
		loaded[key] = value;
	}
	if (err == error::NoError && !ifs.eof()) {
		err = error::Error(
			generic_category().default_error_condition(errno), "Error while reading DB data");
	}

	if (err != error::NoError) {
		return expected::unexpected(err);
	}
	return loaded;
}

error::Error FileBlobdbTransaction::LockDB() {
	fd_ = open(path_or_name_.c_str(), O_RDONLY | O_CREAT, S_IRUSR | S_IWUSR);
	if (fd_ == -1) {
		return error::Error(
			generic_category().default_error_condition(errno),
			"Failed to open DB at '" + path_or_name_ + "'");
	}

	int ret;
	if (write_) {
		ret = flock(fd_, LOCK_EX);
	} else {
		ret = flock(fd_, LOCK_SH);
	}
	if (ret != 0) {
		auto flock_errno = errno;
		close(fd_);
		fd_ = -1;
		return error::Error(
			generic_category().default_error_condition(flock_errno),
			"Failed to lock DB '" + path_or_name_ + "'");
	}

	return error::NoError;
}

error::Error FileBlobdbTransaction::UnlockDB() {
	errno = 0;
	close(fd_);
	if (errno != 0) {
		return error::Error(
			generic_category().default_error_condition(errno),
			"Failed to release lock on DB '" + path_or_name_ + "'");
	}
	return error::NoError;
}

} // namespace key_value_database
} // namespace common
} // namespace mender
