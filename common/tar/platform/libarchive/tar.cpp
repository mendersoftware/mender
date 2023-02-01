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

#include <vector>
#include <algorithm>

#include <common/io.hpp>
#include <common/log.hpp>
#include <common/tar.hpp>

namespace mender {
namespace tar {

using namespace std;

namespace log = mender::common::log;

mender::common::expected::Expected<size_t, mender::common::error::Error> Entry::Read(
	vector<uint8_t> &dst) {
	size_t i = 0;
	for (; i < std::min(dst.size(), buffer_.size); ++i) {
		dst[i] = buffer_.buffer[i];
	}
	return i;
}

Reader::Reader(mender::common::io::Reader &reader) :
	archive_handle {reader} {
}

Entry Reader::Next() {
	int r;
	const void *tar_entry_buffer;
	size_t size;
	int64_t offset;

	if (archive_handle.Get() == nullptr) {
		return Entry {};
	}

	struct archive_entry *current_entry {};

	r = archive_read_next_header(archive_handle.Get(), &current_entry);
	if (r == ARCHIVE_EOF) {
		return Entry {};
	}
	if (r != ARCHIVE_OK) {
		log::Error("archive_read_next failed\nError code: " + to_string(r));
		return Entry {};
	}

	const char *archive_name = archive_entry_pathname(current_entry);

	r = archive_read_data_block(archive_handle.Get(), &tar_entry_buffer, &size, &offset);
	if (r != ARCHIVE_OK) {
		log::Error("Failed to read the data block");
		return Entry {};
	}

	Entry entry = Entry(archive_name, static_cast<const char *>(tar_entry_buffer), size);

	return entry;
}

} // namespace tar
} // namespace mender
