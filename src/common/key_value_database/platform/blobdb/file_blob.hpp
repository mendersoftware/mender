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

#ifndef MENDER_COMMON_FILE_BLOBDB_HPP
#define MENDER_COMMON_FILE_BLOBDB_HPP

#include <common/key_value_database_blobdb.hpp>

namespace mender {
namespace common {
namespace key_value_database {

class FileBlobdbTransaction : public BlobdbTransaction {
public:
	FileBlobdbTransaction(const string &path_or_name, bool write) :
		BlobdbTransaction(path_or_name, write) {};

	error::Error SerializeDB(const DB &db) override;
	ExpectedDB DeserializeDB() override;
	error::Error LockDB() override;
	error::Error UnlockDB() override;

private:
	int fd_ = -1;
};

} // namespace key_value_database
} // namespace common
} // namespace mender

#endif // MENDER_COMMON_FILE_BLOBDB_HPP
