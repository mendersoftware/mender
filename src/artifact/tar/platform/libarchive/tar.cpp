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

#include <artifact/tar/tar.hpp>

#include <cstdint>
#include <vector>
#include <algorithm>
#include <string>
#include <iostream>

#include <common/io.hpp>
#include <common/log.hpp>

namespace mender {
namespace tar {

using namespace std;

namespace log = mender::common::log;
namespace expected = mender::common::expected;
namespace error = mender::common::error;

ExpectedSize Entry::Read(vector<uint8_t>::iterator start, vector<uint8_t>::iterator end) {
	ExpectedSize read_bytes = reader_.Read(start, end);

	if (!read_bytes) {
		return read_bytes;
	}

	nr_bytes_read_ += read_bytes.value();

	return read_bytes;
}


ExpectedSize Reader::Read(vector<uint8_t>::iterator start, vector<uint8_t>::iterator end) {
	return this->archive_handle_.Read(start, end);
}


Reader::Reader(mender::common::io::Reader &reader) :
	archive_handle_ {reader} {
}

// Read the next Tar header, and populate the meta-data:
// * name
// * Archive size
ExpectedEntry Reader::Next() {
	struct archive_entry *current_entry;

	if (archive_handle_.Get() == nullptr) {
		return expected::unexpected(MakeError(TarEntryError, "No underlying stream to read from"));
	}

	int r = archive_read_next_header(archive_handle_.Get(), &current_entry);
	if (r == ARCHIVE_EOF) {
		return expected::unexpected(MakeError(TarEOFError, "Reached the end of the archive"));
	}
	if (r != ARCHIVE_OK) {
		return expected::unexpected(MakeError(
			TarReaderError,
			"archive_read_next failed in LibArchive. Error code: " + to_string(r)
				+ " Error message: " + archive_error_string(archive_handle_.Get())));
	}

	const char *archive_name = archive_entry_pathname(current_entry);
	if (archive_name == nullptr) {
		return expected::unexpected(
			MakeError(TarReaderError, "Failed to get the name of the archive entry"));
	}

	const la_int64_t archive_entry_size_ {archive_entry_size(current_entry)};
	if (archive_entry_size_ < 0) {
		return expected::unexpected(
			MakeError(TarReaderError, "Failed to get the size of the archive"));
	}

	return Entry(archive_name, static_cast<size_t>(archive_entry_size_), *this);
}

} // namespace tar
} // namespace mender
