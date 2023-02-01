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

#ifndef MENDER_COMMON_TAR_HPP
#define MENDER_COMMON_TAR_HPP

#include <string>
#include <vector>

#include <config.h>

#include <common/io.hpp>
#include <common/expected.hpp>

#ifdef MENDER_TAR_LIBARCHIVE
#include <libarchive/wrapper.hpp>
#endif

namespace mender {
namespace tar {

namespace expected = mender::common::expected;

using namespace std;


struct Buffer {
	const char *buffer;
	size_t size;
};

class Entry : public mender::common::io::Reader {
private:
	std::string name_;
	Buffer buffer_;

public:
	Entry() = default;

	Entry(std::string name, const char *data, size_t size) :
		name_ {name},
		buffer_ {data, size} {
	}

	std::string Name() {
		return name_;
	}

	expected::ExpectedSize Read(vector<uint8_t> &dst);
};

class Reader {
private:
#ifdef MENDER_TAR_LIBARCHIVE
	archiver::ArchiveHandle archive_handle;
#endif

public:
	Reader(mender::common::io::Reader &reader);

	Entry Next();
};

} // namespace tar
} // namespace mender

#endif // MENDER_COMMON_TAR_HPP
