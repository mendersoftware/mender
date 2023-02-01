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

#include <libarchive/wrapper.hpp>
#include <common/log.hpp>

namespace mender {
namespace archiver {

ssize_t reader_callback(archive *archive, void *in_reader_container, const void **buff) {
	ReaderContainer *p_reader_container = reinterpret_cast<ReaderContainer *>(in_reader_container);

	auto ret = p_reader_container->reader_.Read(p_reader_container->buff_);
	if (!ret) {
		log::Error("Failed to read from the archive stream: Error: " + ret.error().message);
		return 0;
	}

	int bytes_read = ret.value();
	*buff = &p_reader_container->buff_[0];

	return bytes_read;
};



Error ArchiveHandle::Init() {
	archive_read_support_filter_none(archive_.get());
	archive_read_support_format_tar(archive_.get());
	int r =
		archive_read_open(archive_.get(), &reader_container_, nullptr, reader_callback, nullptr);
	if (r != ARCHIVE_OK) {
		return Error(
			std::error_condition(std::errc::inappropriate_io_control_operation),
			"Failed to initalize the 'libarchive' C bindings");
	}
	return error::NoError;
}

ArchiveHandle::ArchiveHandle(mender::common::io::Reader &reader) :
	archive_(
		archive_read_new(),
		[](archive *a) {
			if (a) {
				int r = archive_read_free(a);
				if (r != ARCHIVE_OK) {
					log::Error("Failed to free the resources from the Archive");
				}
			}
		}),
	reader_container_ {reader, 4096} {
	auto err = Init();
	if (err) {
		log::Error("Failed to initialize the Archive handle");
	}
}

} // namespace archiver
} // namespace mender
