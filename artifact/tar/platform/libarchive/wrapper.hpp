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

#ifndef MENDER_LIB_ARCHIVE_WRAPPER_HPP
#define MENDER_LIB_ARCHIVE_WRAPPER_HPP

#include <config.h>

#include <archive.h>
#include <archive_entry.h>

#include <memory>
#include <vector>

#include <common/io.hpp>
#include <common/log.hpp>
#include <common/error.hpp>
#include <common/expected.hpp>

namespace mender {
namespace libarchive {
namespace wrapper {

using namespace std;

namespace error = mender::common::error;
namespace expected = mender::common::expected;
namespace io = mender::common::io;
using mender::common::error::Error;

namespace log = mender::common::log;

struct ReaderContainer {
	mender::common::io::Reader &reader_;
	std::vector<uint8_t> buff_;

	ReaderContainer(mender::common::io::Reader &reader, size_t block_size) :
		reader_(reader),
		buff_(block_size) {
	}
};

ssize_t reader_callback(archive *archive, void *in_reader_container, const void **buff);

class Handle : io::Reader {
private:
	std::unique_ptr<struct archive, decltype(&archive_read_free)> archive_;

	bool initalized_ {false};

	/* Structure passed to the libarchive reader callback, which handles reading
	 from the given reader stream, and placing it in a buffer, which can then be
	 further read from */
	ReaderContainer reader_container_;

public:
	Handle(mender::common::io::Reader &reader);

	Error Init();

	struct archive *Get() {
		return archive_.get();
	}

	// Disallow copying
	Handle(Handle &archive) = delete;
	Handle &operator=(const Handle &archive) = delete;

	expected::ExpectedSize Read(
		vector<uint8_t>::iterator start, vector<uint8_t>::iterator end) override;
};

} // namespace wrapper
} // namespace libarchive
} // namespace mender

#endif // MENDER_LIB_ARCHIVE_WRAPPER_HPP
