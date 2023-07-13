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

#include <archive.h>

#include <common/log.hpp>

namespace mender {
namespace libarchive {
namespace wrapper {

size_t libarchive_read_buffer_size {MENDER_BUFSIZE};

namespace expected = mender::common::expected;

using ExpectedSize = expected::ExpectedSize;

// The reader_callback is invoked whenever the library requires raw bytes from
// the archive. The read callback reads data into a buffer, sets the const void
// **buffer argument to point to the available data, and returns a count of the
// number of bytes available. LibArchive will invoke the read callback again
// only after it has consumed this data. LibArchive imposes no constraints on
// the size of the data blocks returned.
//
// - On EOF return 0.
// - On error return -1.
ssize_t reader_callback(archive *archive, void *in_reader_container, const void **buff) {
	ReaderContainer *p_reader_container = static_cast<ReaderContainer *>(in_reader_container);

	auto ret = p_reader_container->reader_.Read(
		p_reader_container->buff_.begin(), p_reader_container->buff_.end());
	if (!ret) {
		log::Error("Failed to read from the archive stream: Error: " + ret.error().message);
		return -1;
	}

	size_t bytes_read {ret.value()};
	*buff = p_reader_container->buff_.data();

	return bytes_read;
};

Error Handle::Init() {
	int r;
#ifdef MENDER_ARTIFACT_GZIP_COMPRESSION
	r = archive_read_support_filter_gzip(archive_.get());
	if (r != ARCHIVE_OK) {
		return MakeError(error::GenericError, "Gzip compression is not supported on this platform");
	}
#endif // MENDER_ARTIFACT_GZIP_COMPRESSION
#ifdef MENDER_ARTIFACT_LZMA_COMPRESSION
	r = archive_read_support_filter_xz(archive_.get());
	if (r != ARCHIVE_OK) {
		return MakeError(error::GenericError, "xz compression is not supported on this platform");
	}
#endif // MENDER_ARTIFACT_LZMA_COMPRESSION
#ifdef MENDER_ARTIFACT_ZSTD_COMPRESSION
	r = archive_read_support_filter_zstd(archive_.get());
	if (r != ARCHIVE_OK) {
		return MakeError(error::GenericError, "xz compression is not supported on this platform");
	}
#endif // MENDER_ARTIFACT_ZSTD_COMPRESSION
	r = archive_read_support_format_tar(archive_.get());
	if (r != ARCHIVE_OK) {
		return MakeError(error::GenericError, "the tar format is not supported on this platform");
	}
	r = archive_read_open(archive_.get(), &reader_container_, nullptr, reader_callback, nullptr);
	if (r != ARCHIVE_OK) {
		return MakeError(
			error::GenericError,
			"Failed to initalize the 'libarchive' C bindings. LibArchive error message: '"
				+ string(archive_error_string(archive_.get()))
				+ "' error code: " + std::to_string(archive_errno(archive_.get())));
	}
	this->initalized_ = true;
	return error::NoError;
}

int FreeLibArchiveHandle(archive *a) {
	int r {0};
	if (a != nullptr) {
		r = archive_read_free(a);
		if (r != ARCHIVE_OK) {
			log::Error("Failed to free the resources from the Archive");
		}
	}
	return r;
}

Handle::Handle(io::Reader &reader) :
	archive_(archive_read_new(), FreeLibArchiveHandle),
	reader_container_ {reader, libarchive_read_buffer_size} {
	auto err = Init();
	if (error::NoError != err) {
		log::Error("Failed to initialize the Archive handle: " + err.message);
	}
}


ExpectedSize Handle::Read(vector<uint8_t>::iterator start, vector<uint8_t>::iterator end) {
	if (!initalized_) {
		return expected::unexpected(common::error::MakeError(
			common::error::GenericError,
			"Unable to read from a tar reader which is not initialized properly"));
	}
	size_t iterator_size {static_cast<size_t>(end - start)};
	ssize_t read_bytes {archive_read_data(archive_.get(), &start[0], iterator_size)};

	switch (read_bytes) {
	case ARCHIVE_OK:
		return read_bytes;
		break;
	case ARCHIVE_EOF:
		return 0;
	/* Fallthroughs */
	case ARCHIVE_RETRY:
	case ARCHIVE_WARN:
	case ARCHIVE_FAILED:
	case ARCHIVE_FATAL:
		return expected::unexpected(MakeError(
			error::GenericError,
			"Recieved error code: " + std::to_string(archive_errno(archive_.get()))
				+ " and error message: " + archive_error_string(archive_.get())));
	}
	return read_bytes;
}

} // namespace wrapper
} // namespace libarchive
} // namespace mender
